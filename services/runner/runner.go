package runner

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/defectlifecycle"
	"code-shield/services/engines"
	"code-shield/services/engines/planner"
	"code-shield/services/engines/profile"
	"code-shield/services/governance"

	"gorm.io/gorm"
)

var (
	activeTasksMu sync.Mutex
	activeTasks   = make(map[uint]*TaskContext) // reportID -> TaskContext
	// cancelRequests 保存“任务已要求取消，但尚未进入 runner”的报告 ID。
	// worker 从队列抢占任务到 runner 注册任务上下文之间存在短暂窗口，
	// 没有这份 intent 会导致删除接口删掉记录后任务仍继续执行。
	cancelRequests   = make(map[uint]time.Time)
	cancelMu         sync.Mutex
	cancelRequestTTL = 5 * time.Minute
)

// CancelRunningTask 取消正在执行的任务
func CancelRunningTask(reportID uint) bool {
	cancelMu.Lock()
	now := time.Now()
	for id, requestedAt := range cancelRequests {
		if now.Sub(requestedAt) > cancelRequestTTL {
			delete(cancelRequests, id)
		}
	}
	cancelRequests[reportID] = now
	cancelMu.Unlock()

	activeTasksMu.Lock()
	defer activeTasksMu.Unlock()
	if ctx, ok := activeTasks[reportID]; ok {
		log.Printf("[TaskRunner] Cancelling active task for ReportID %d\n", reportID)
		ctx.Cancel()
		cancelMu.Lock()
		delete(cancelRequests, reportID)
		cancelMu.Unlock()
		return true
	}
	return false
}

// consumeCancelRequest 消费一次任务启动前的取消请求。
func consumeCancelRequest(reportID uint) bool {
	cancelMu.Lock()
	defer cancelMu.Unlock()
	requestedAt, canceled := cancelRequests[reportID]
	if canceled && time.Since(requestedAt) > cancelRequestTTL {
		canceled = false
	}
	delete(cancelRequests, reportID)
	return canceled
}

// CancelAllRunningTasks 取消所有正在执行的任务
func CancelAllRunningTasks() {
	activeTasksMu.Lock()
	defer activeTasksMu.Unlock()
	log.Printf("[TaskRunner] Cancelling all %d active tasks\n", len(activeTasks))
	for id, ctx := range activeTasks {
		log.Printf("[TaskRunner] Cancelling task for ReportID %d\n", id)
		ctx.Cancel()
	}
}

// GetRunningTasks 获取当前内存中所有正在执行的任务快照列表
func GetRunningTasks() []RunningTaskInfo {
	activeTasksMu.Lock()
	defer activeTasksMu.Unlock()

	var list []RunningTaskInfo
	now := time.Now()
	for reportID, ctx := range activeTasks {
		startTime := ctx.Summary.StartTime
		if startTime.IsZero() {
			startTime = ctx.Report.CreatedAt
		}
		duration := int64(now.Sub(startTime).Seconds())
		if duration < 0 {
			duration = 0
		}

		repoName := ctx.Repo.Name
		if repoName == "" {
			repoName = ctx.Summary.RepoName
		}
		if repoName == "" && ctx.Repo.URL != "" {
			parts := strings.Split(strings.TrimSuffix(ctx.Repo.URL, ".git"), "/")
			if len(parts) > 0 {
				repoName = parts[len(parts)-1]
			}
		}

		list = append(list, RunningTaskInfo{
			ReportID:        reportID,
			RepoID:          ctx.Repo.ID,
			RepoName:        repoName,
			RepoURL:         ctx.Repo.URL,
			TaskType:        ctx.TaskType.Name,
			TaskDisplayName: ctx.TaskType.DisplayName,
			EngineMode:      ctx.TaskType.EngineMode,
			Status:          ctx.Report.Status,
			StartTime:       startTime,
			DurationSec:     duration,
			TotalChunks:     ctx.Report.TotalChunks,
			ProcessedChunks: ctx.Report.ProcessedChunks,
			SuccessChunks:   ctx.Report.SuccessChunks,
			Attempts:        ctx.Attempts,
		})
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].StartTime.Before(list[j].StartTime)
	})

	return list
}

// RunTaskSync 同步驱动单次扫描任务穿过 6 个标准化流水线阶段
func RunTaskSync(reportID uint, repoURL string, taskTypeID uint, autoNotify bool, runParams models.RunParams) error {
	if consumeCancelRequest(reportID) {
		return ErrTaskCanceled
	}

	ctx := &TaskContext{AutoNotify: autoNotify}

	// 1. 初始化并加载关联数据
	if err := ctx.Load(reportID, taskTypeID); err != nil {
		return err
	}

	ctx.Summary = TaskSummaryReport{
		TaskID:     reportID,
		RepoName:   ctx.Repo.Name,
		TaskType:   ctx.TaskType.Name,
		EngineMode: ctx.Report.EngineMode,
		StartTime:  time.Now(),
	}
	ctx.Summary.Analysis.ArtifactComplete = true

	taskCtx, cancel := context.WithCancel(context.Background())
	ctx.Ctx = taskCtx
	ctx.Cancel = cancel

	activeTasksMu.Lock()
	activeTasks[reportID] = ctx
	activeTasksMu.Unlock()
	defer func() {
		activeTasksMu.Lock()
		delete(activeTasks, reportID)
		activeTasksMu.Unlock()
		cancel()
	}()

	// 合并运行参数
	ctx.ResolveRunParams(runParams)
	ctx.PrepareOutputPaths()
	if err := ctx.LoadExecutionSnapshot(); err != nil {
		MarkFailed(ctx, err.Error())
		return err
	}

	log.Printf("[TaskRunner] Starting task for ReportID: %d, URL: %s, TaskType: %s (Mode: %s)\n",
		ctx.Report.ID, repoURL, ctx.TaskType.Name, ctx.Report.EngineMode)

	// Stage 1: 准备与代码同步
	codesPath, err := PrepareAndSync(ctx.Ctx, ctx.Repo, ctx.Report.ID, repoURL)
	if err != nil {
		MarkFailed(ctx, err.Error())
		return err
	}
	ctx.CodesPath = codesPath

	headCommit, headErr := planner.ResolveRepositoryHead(ctx.Ctx, codesPath)
	if headErr != nil {
		MarkFailed(ctx, headErr.Error())
		return headErr
	}
	ctx.Report.HeadCommit = headCommit
	if models.DB != nil {
		if _, err := models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, map[string]interface{}{
			"head_commit": headCommit,
		}); err != nil {
			MarkFailed(ctx, err.Error())
			return err
		}
	}

	// Stage 2: Scan Profile scope planner
	scanPlan, primaryPlan, parsedProfile, gateErr := RunScanProfileGate(ctx)
	if gateErr != nil {
		return gateErr
	}

	// Stage 3 & 4: 装配只读 EngineContext 并驱动静态分析引擎
	UpdateTaskStatus(ctx.Report.ID, models.StatusAnalyzing)
	overallStartTime := time.Now()
	engineConfig, engineConfigErr := json.Marshal(profile.EngineConfig{ScanProfile: parsedProfile.Profile})
	if engineConfigErr != nil {
		MarkFailed(ctx, engineConfigErr.Error())
		return engineConfigErr
	}
	engine, engineErr := engines.GetEngineStrict(ctx.Report.EngineMode)
	if engineErr != nil {
		MarkFailed(ctx, engineErr.Error())
		return engineErr
	}
	var engCtx *engines.EngineContext
	chunkPolicyID := coverage.ChunkPolicyID(ctx.TaskType.Name, engineConfig)
	ctx.ChunkPolicyID = chunkPolicyID
	engCtx = &engines.EngineContext{
		Ctx:                       ctx.Ctx,
		ReportID:                  ctx.Report.ID,
		RepoID:                    ctx.Repo.ID,
		RepoName:                  ctx.Repo.Name,
		TaskTypeID:                ctx.TaskType.ID,
		TaskTypeName:              ctx.TaskType.DisplayName,
		TaskTypeKey:               ctx.TaskType.Name,
		EngineMode:                ctx.Report.EngineMode,
		AssessmentConfig:          ctx.ExecutionSnapshot.AssessmentConfig,
		Profile:                   parsedProfile.Profile,
		ChunkPolicyID:             chunkPolicyID,
		TaskDir:                   ctx.TaskType.TaskDir(),
		AnalysisPromptContent:     ctx.PromptContent,
		AnalysisPromptContentHash: ctx.PromptContentHash,
		AnalysisPromptPath:        "",
		AllowedCategories:         ctx.ExecutionSnapshot.Categories,
		Taxonomy:                  ctx.ExecutionSnapshot.Taxonomy,
		TaxonomyHash:              ctx.ExecutionSnapshot.TaxonomyHash,
		DomainFamily:              ctx.ExecutionSnapshot.DomainFamily,
		DefenseDimensions:         ctx.ExecutionSnapshot.GetDefenseDimensions(),
		TargetSemantics:           ctx.ExecutionSnapshot.GetTargetSemantics(),
		DisplaySemantics:          ctx.ExecutionSnapshot.GetDisplaySemantics(),
		CodesPath:                 ctx.CodesPath,
		WorkDir:                   ctx.CodesPath,
		ReportPath:                ctx.ReportPath,
		JSONPath:                  ctx.JsonPath,
		EngineConfig:              engineConfig,
		RunParams:                 ctx.RunParams,
		ScanPlan:                  scanPlan,
		PlanManifestHash:          primaryPlan.PrimaryManifestHash(),
		PrimaryUnits:              primaryPlan.PrimaryUnits(),
		NegativeRules:             governance.GetNegativeRulesForScan(ctx.Repo.ID, ctx.TaskType.ID),
		CategoryAliasRecorder: func(usage models.CategoryAliasUsage) {
			if models.DB == nil {
				return
			}
			usage.TaskTypeID = ctx.TaskType.ID
			usage.LastReportID = ctx.Report.ID
			_ = governance.RecordCategoryAliasHit(models.DB, usage)
		},

		ProgressReport: func(total, processed, success int) {
			UpdateTaskProgress(ctx.Report.ID, total, processed, success, "")
		},
		AnalysisExecutor: func(fileList []string) ([]models.AnalysisFinding, error) {
			return ExecuteAnalysis(ctx, fileList)
		},
		ChunkAnalysisExecutor: func(req engines.ChunkExecutionRequest) (engines.ChunkExecutionResult, error) {
			return ExecuteChunkAnalysis(ctx, req)
		},
	}
	if parsedProfile.Profile.Name == profile.NameChangeReview {
		engCtx.ChangeBaseCommit = ctx.Report.BaseCommit
		engCtx.ChangeHeadCommit = ctx.Report.HeadCommit
		engCtx.DiffManifestHash = ctx.Report.DiffManifestHash
	}

	result, runErr := engine.Run(engCtx)
	overallEndTime := time.Now()

	if result != nil {
		ctx.Coverage = engCtx.Coverage
		taxonomy := models.CategoryTaxonomy{}
		if taskTaxonomy := ctx.TaskType.GetCategoryTaxonomy(); taskTaxonomy != nil {
			taxonomy = *taskTaxonomy
		}
		ctx.Findings = governance.CalibrateFindingsWithTaxonomy(taxonomy, result.Findings)
		if result.PlanReconciliation.PlannedUnits > 0 {
			ctx.Summary.PlanReconciliation = &result.PlanReconciliation
		}
	}

	if models.DB != nil {
		_, persistErr := defectlifecycle.PersistScanFacts(defectlifecycle.ScanInput{
			DB:            models.DB,
			Report:        ctx.Report,
			Repo:          ctx.Repo,
			RepoRoot:      ctx.CodesPath,
			TaskType:      ctx.TaskType,
			Findings:      ctx.Findings,
			Coverage:      ctx.Coverage,
			RenameTargets: defectlifecycle.BuildRenameTargets(ctx.CodesPath),
			Arbitrator:    defectlifecycle.NewRuntimeArbitrator(ctx.Report.ID, ctx.Repo.Name, ctx.TaskType.DisplayName),
		})
		if persistErr != nil {
			MarkFailed(ctx, persistErr.Error())
			return persistErr
		}
	}

	if result != nil {
		ctx.HasFailedChunks = result.HasFailedChunks
		if len(ctx.Findings) == 0 {
			ctx.Findings = result.Findings
		}

		successfulChunks := 0
		failedChunks := 0
		for _, sc := range result.SummaryChunks {
			if sc.Status == "success" {
				successfulChunks++
			} else {
				failedChunks++
			}
		}

		ctx.Summary.Analysis.StartTime = overallStartTime
		ctx.Summary.Analysis.EndTime = overallEndTime
		ctx.Summary.Analysis.DurationSeconds = overallEndTime.Sub(overallStartTime).Seconds()
		ctx.Summary.Analysis.TotalChunks = len(result.SummaryChunks)
		ctx.Summary.Analysis.SuccessChunks = successfulChunks
		ctx.Summary.Analysis.FailedChunks = failedChunks
		ctx.Summary.Analysis.TotalFindings = len(result.Findings)
		ctx.Summary.Analysis.Attempts = result.AnalysisMetrics.Attempts
		ctx.Summary.Analysis.Retries = result.AnalysisMetrics.Retries
		ctx.Summary.Analysis.ContractRepairs = result.AnalysisMetrics.ContractRepairs
		ctx.Summary.Analysis.ResourceFailovers = result.AnalysisMetrics.ResourceFailovers
		ctx.Summary.Analysis.DriverFailovers = result.AnalysisMetrics.DriverFailovers
		ctx.Summary.Analysis.SplitInvocations = result.AnalysisMetrics.SplitInvocations
		ctx.Summary.Analysis.RecoveredChunks = result.AnalysisMetrics.ResumedChunks
		ctx.Summary.Analysis.ArtifactComplete = result.AnalysisMetrics.ArtifactComplete
		ctx.Summary.Analysis.ArtifactState = result.AnalysisMetrics.ArtifactState
		ctx.Summary.Analysis.ArtifactQualityDegraded = result.AnalysisMetrics.ArtifactQualityDegraded
		ctx.Summary.Analysis.UnresolvedIssueCount = result.AnalysisMetrics.UnresolvedIssueCount
		ctx.Summary.Analysis.NormalizedIssueCount = result.AnalysisMetrics.NormalizedIssueCount
		ctx.Summary.Analysis.SchemaRepairAttempts = result.AnalysisMetrics.SchemaRepairAttempts
		ctx.Summary.Analysis.SchemaRepairSuccesses = result.AnalysisMetrics.SchemaRepairSuccesses
		ctx.Summary.Analysis.JSONSyntaxRepairs = result.AnalysisMetrics.JSONSyntaxRepairs
		ctx.Summary.Analysis.RepairBaselineKnown = result.AnalysisMetrics.RepairBaselineKnown
		ctx.Summary.Analysis.RepairRepairedKnown = result.AnalysisMetrics.RepairRepairedKnown
		ctx.Summary.Analysis.RepairUnverified = result.AnalysisMetrics.RepairUnverified
		ctx.Summary.Analysis.RepairOutcome = result.AnalysisMetrics.RepairOutcome
		ctx.Summary.Analysis.CandidateQuarantineCount = result.AnalysisMetrics.CandidateQuarantineCount
		ctx.Summary.Analysis.ArtifactSchemaID = result.AnalysisMetrics.ArtifactSchemaID
		ctx.Summary.Analysis.ArtifactSchemaHash = result.AnalysisMetrics.ArtifactSchemaHash
		ctx.Summary.Analysis.ResponseFormatMode = result.AnalysisMetrics.ResponseFormatMode
		ctx.Summary.Analysis.ResponseFormatFallbacks = result.AnalysisMetrics.ResponseFormatFallbacks
		if category := result.AnalysisMetrics.Category; category.Hunter.ModelCandidates > 0 || category.Judge.ModelCandidates > 0 {
			ctx.Summary.Analysis.Category = &category
		}

		convertedChunks := ConvertEngineChunkDetails(result.SummaryChunks)
		ctx.Summary.Analysis.Chunks = convertedChunks

		if failedChunks > 0 {
			ctx.Summary.Analysis.Status = "failed"
		} else {
			ctx.Summary.Analysis.Status = "success"
		}

		WriteSummaryReport(ctx)

		// 持久化辩论轨迹与 Token 消耗
		if len(result.DebateLogs) > 0 && models.DB != nil {
			for _, dl := range result.DebateLogs {
				dl.TaskReportID = ctx.Report.ID
				if dbErr := models.DB.Create(&dl).Error; dbErr != nil {
					log.Printf("[TaskRunner] Warning: failed to persist DebateLog: %v", dbErr)
				}
			}
		}

		if (result.HunterTokens > 0 || result.Tier2Tokens > 0) && models.DB != nil {
			updates := map[string]interface{}{}
			if result.HunterTokens > 0 {
				updates["tier1_tokens"] = gorm.Expr("tier1_tokens + ?", result.HunterTokens)
			}
			if result.Tier2Tokens > 0 {
				updates["tier2_tokens"] = gorm.Expr("tier2_tokens + ?", result.Tier2Tokens)
			}
			if _, err := models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, updates); err != nil {
				log.Printf("[TaskRunner] Warning: failed to persist token usage for report %d: %v", ctx.Report.ID, err)
			}
		}
	}

	if runErr != nil {
		MarkFailed(ctx, runErr.Error())
		return runErr
	}

	UpdateTaskStatus(ctx.Report.ID, models.StatusSynthesis)
	if synthErr := ExecuteSynthesis(ctx, ctx.Findings, ctx.Coverage); synthErr != nil {
		MarkFailed(ctx, synthErr.Error())
		return synthErr
	}

	// Stage 5: 后处理评分计算（必须基于归并后的全量总集计算）
	UpdateTaskStatus(ctx.Report.ID, models.StatusPostProcessing)
	effectiveFindings := GetEffectiveFindings(ctx)
	ctx.Findings = effectiveFindings
	taskResult := RunPostProcess(effectiveFindings, ctx.TaskType)

	// Stage 6: 事务最终化、治理归并与交付通告
	return Finalize(ctx, taskResult)
}
