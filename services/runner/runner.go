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
	"code-shield/services/defects"
	"code-shield/services/engines"
	"code-shield/services/governance"

	"gorm.io/gorm"
)

var (
	activeTasksMu sync.Mutex
	activeTasks   = make(map[uint]*TaskContext) // reportID -> TaskContext
)

// CancelRunningTask 取消正在执行的任务
func CancelRunningTask(reportID uint) bool {
	activeTasksMu.Lock()
	defer activeTasksMu.Unlock()
	if ctx, ok := activeTasks[reportID]; ok {
		log.Printf("[TaskRunner] Cancelling active task for ReportID %d\n", reportID)
		ctx.Cancel()
		return true
	}
	return false
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
	ctx := &TaskContext{AutoNotify: autoNotify}

	// 1. 初始化并加载关联数据
	if err := ctx.Load(reportID, taskTypeID); err != nil {
		return err
	}

	ctx.Summary = TaskSummaryReport{
		TaskID:     reportID,
		RepoName:   ctx.Repo.Name,
		TaskType:   ctx.TaskType.Name,
		EngineMode: ctx.TaskType.EngineMode,
		StartTime:  time.Now(),
	}

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

	log.Printf("[TaskRunner] Starting task for ReportID: %d, URL: %s, TaskType: %s (Mode: %s)\n",
		ctx.Report.ID, repoURL, ctx.TaskType.Name, ctx.TaskType.EngineMode)

	// Stage 1: 准备与代码同步
	codesPath, err := PrepareAndSync(ctx.Ctx, ctx.Repo, ctx.Report.ID, repoURL)
	if err != nil {
		MarkFailed(ctx, err.Error())
		return err
	}
	ctx.CodesPath = codesPath

	// Stage 2: 准入门禁判定
	skipped, err := CheckPrecondition(ctx.Ctx, ctx.Report.ID, ctx.TaskType, ctx.CodesPath)
	if err != nil {
		MarkFailed(ctx, err.Error())
		return err
	} else if skipped {
		return ErrSkipped
	}

	// Stage 3 & 4: 装配只读 EngineContext 并驱动静态分析引擎
	overallStartTime := time.Now()
	engine := engines.GetEngine(ctx.TaskType.EngineMode)
	engCtx := &engines.EngineContext{
		Ctx:           ctx.Ctx,
		ReportID:      ctx.Report.ID,
		RepoID:        ctx.Repo.ID,
		RepoName:      ctx.Repo.Name,
		TaskTypeID:    ctx.TaskType.ID,
		TaskTypeName:  ctx.TaskType.DisplayName,
		CodesPath:     ctx.CodesPath,
		ReportPath:    ctx.ReportPath,
		JSONPath:      ctx.JsonPath,
		EngineConfig:  json.RawMessage(ctx.TaskType.EngineConfig),
		RunParams:     ctx.RunParams,
		NegativeRules: governance.GetNegativeRulesForScan(ctx.Repo.ID, ctx.TaskType.ID),
		ProgressReport: func(total, processed, success int) {
			if models.DB != nil {
				models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.Report.ID).Updates(map[string]interface{}{
					"total_chunks":     total,
					"processed_chunks": processed,
					"success_chunks":   success,
				})
			}
		},
		AnalysisExecutor: func(fileList []string) ([]models.AnalysisFinding, error) {
			return ExecuteAnalysis(ctx, fileList)
		},
		SynthesisExecutor: func(findings []models.AnalysisFinding, scannedFilesOpt ...[]string) error {
			// 严重级别确定性校准
			findings = governance.CalibrateFindings(findings)
			// 增量指纹比对与跨任务状态机打标（注入实际扫描文件集，激活双层物理守卫与平滑观察期）
			var scannedFiles []string
			if len(scannedFilesOpt) > 0 {
				scannedFiles = scannedFilesOpt[0]
			}
			findings, _ = defects.DiffAndEnrichFindings(ctx.Repo.ID, ctx.Report.ID, ctx.TaskType.ID, scannedFiles, findings, ctx.CodesPath)
			ctx.Findings = findings
			return ExecuteSynthesis(ctx, findings)
		},
	}

	result, runErr := engine.Run(engCtx)
	overallEndTime := time.Now()

	if result != nil {
		ctx.HasFailedChunks = result.HasFailedChunks
		ctx.Findings = result.Findings

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

		convertedChunks := make([]ChunkDetails, len(result.SummaryChunks))
		for i, sc := range result.SummaryChunks {
			convertedChunks[i] = ChunkDetails{
				ChunkName:       sc.ChunkName,
				StartTime:       sc.StartTime,
				EndTime:         sc.EndTime,
				DurationSeconds: sc.DurationSeconds,
				Attempts:        sc.Attempts,
				Retries:         sc.Retries,
				Status:          sc.Status,
				ErrorMessage:    sc.ErrorMessage,
			}
		}
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
			models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.Report.ID).Updates(updates)
		}
	}

	if runErr != nil {
		MarkFailed(ctx, runErr.Error())
		return runErr
	}

	// Stage 5: 后处理评分计算
	UpdateTaskStatus(ctx.Report.ID, models.StatusPostProcessing)
	taskResult := RunPostProcess(ctx.Findings, ctx.TaskType)

	// Stage 6: 事务最终化、治理归并与交付通告
	return Finalize(ctx, taskResult)
}
