package debate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/defectlifecycle"
	"code-shield/services/dispatcher"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
	assessmentprofiles "code-shield/services/engines/assessment/profiles"
	"code-shield/services/engines/chunked"
	"code-shield/services/engines/chunker"
	"code-shield/services/governance"
	"code-shield/services/invoker"

	"gorm.io/datatypes"
)

// DebateEngine 实现多智能体三方对抗辩论流水线引擎
type DebateEngine struct{}

func init() {
	engines.RegisterEngine("debate_full", &DebateEngine{})
}

func (e *DebateEngine) Name() string {
	return "debate_full"
}

// Run 启动辩论引擎任务流（纯内存计算，不直接操作 DB）
func (e *DebateEngine) Run(ctx *engines.EngineContext) (*engines.EngineResult, error) {
	parsedProfile, err := engines.ParseProfileConfig(ctx.EngineConfig)
	if err != nil {
		return nil, err
	}
	cfg := parsedProfile.Config
	scanProfile := ctx.Profile
	if scanProfile.Name == "" {
		scanProfile = parsedProfile.Profile
	}
	targetScope := parsedProfile.Profile.TargetScope
	if ctx.RunParams.TargetScope != nil {
		targetScope = *ctx.RunParams.TargetScope
	}

	// 1. 使用上下文已预加载的免扫/负样本例外规则
	negativeRules := ctx.NegativeRules

	profileName := assessment.ResolveProfileName(ctx.AssessmentConfig, scanProfile.Name)
	var assessmentPlanner assessment.Planner
	if profileName != "" {
		registry, registryErr := assessmentprofiles.Registry()
		if registryErr != nil {
			return nil, registryErr
		}
		registration, registrationErr := registry.Get(profileName)
		if registrationErr != nil {
			return nil, registrationErr
		}
		assessmentPlanner = registration.Planner
		ctx.AssessmentProfile = profileName
	}

	// 2. 构建 planner 提供或重新计算的语义分片包 (含同名投影与宏注入)
	var bundles []chunker.SemanticBundle
	var coveragePlan coverage.ScanPlan
	if assessmentPlanner != nil && len(ctx.PrimaryUnits) > 0 {
		bundles, err = assessmentPlanner.BuildBundles(assessment.PlanContext{
			EngineContext: ctx,
			Config:        cfg,
			Profile:       scanProfile,
		}, ctx.PrimaryUnits)
		if ctx.ScanPlan != nil {
			coveragePlan = *ctx.ScanPlan
		}
	} else if ctx.ScanPlan != nil {
		bundles, err = chunker.BuildSemanticBundlesFromPlan(ctx.CodesPath, *ctx.ScanPlan, cfg, negativeRules)
		coveragePlan = *ctx.ScanPlan
	} else {
		bundles, coveragePlan, err = chunker.BuildSemanticBundlesWithPlan(ctx.CodesPath, cfg, targetScope, negativeRules)
	}
	if err != nil {
		return nil, err
	}

	log.Printf("[DebateEngine] EngineMode: %s | Found %d semantic bundles for repo %s\n",
		e.Name(), len(bundles), ctx.RepoName)

	nameParts := strings.Split(ctx.RepoName, "/")
	repoShort := nameParts[len(nameParts)-1]
	chunkDir := filepath.Join(filepath.Dir(ctx.ReportPath), fmt.Sprintf("debate-chunks-%d-%s", ctx.ReportID, repoShort))
	_ = os.MkdirAll(chunkDir, 0755)
	resumeMetadata := BuildBundleResumeMetadata(
		ctx.CodesPath,
		ctx.EngineConfig,
		ctx.AnalysisPromptContent,
		ctx.PlanManifestHash,
	)
	resumeMetadata.ChangeBaseCommit = ctx.ChangeBaseCommit
	resumeMetadata.ChangeHeadCommit = ctx.ChangeHeadCommit
	resumeMetadata.DiffManifestHash = ctx.DiffManifestHash
	resumeCheckpoints := LoadBundleResumeCheckpoints(chunkDir)

	var allFindings []models.AnalysisFinding
	var allDebateLogs []models.TaskDebateLog
	var allAssessments []coverage.AssessmentRecord
	changePlannedOwners := map[int][]coverage.PlannedFile{}
	pluginPlanCtx := assessment.PlanContext{
		EngineContext: ctx,
		Config:        cfg,
		Profile:       scanProfile,
	}
	if assessmentPlanner != nil {
		for bundleIndex, bundle := range bundles {
			changePlannedOwners[bundleIndex] = append(
				changePlannedOwners[bundleIndex], assessmentPlanner.PluginPlan(pluginPlanCtx, bundle)...)
		}
	}
	var totalHunterTokens, totalTier2Tokens int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	var chunkErrors []string
	var errMu sync.Mutex
	semaphore := make(chan struct{}, models.AppConfig.GetChunkConcurrency())
	totalChunks := len(bundles)

	chunkDetailsList := make([]engines.ChunkDetails, totalChunks)
	processedChunks := 0
	successChunks := 0
	reusedCount := 0
	invalidResumeCount := 0
	reusedDetails := make(map[int]engines.ChunkDetails)

	// 启动时先同步复用完整成功的 bundle，避免它们进入并发扫描队列。
	for bundleIdx, b := range bundles {
		cp, exists := resumeCheckpoints[b.Name]
		if !exists {
			continue
		}
		snapshotHash, snapshotErr := chunked.ChunkSnapshotHash(ctx.CodesPath, b.AllFiles)
		if snapshotErr != nil {
			log.Printf("[BundleResume] Checkpoint snapshot hash failed task=%d bundle=%q err=%v\n",
				ctx.ReportID, b.Name, snapshotErr)
			delete(resumeCheckpoints, b.Name)
			continue
		}
		if !ValidBundleResumeCheckpoint(cp, ctx.ReportID, e.Name(), b, resumeMetadata, snapshotHash) {
			delete(resumeCheckpoints, b.Name)
			invalidResumeCount++
			continue
		}

		detail := engines.ChunkDetails{
			ChunkUID:        coverage.StableChunkID(chunkPolicyIDForContext(ctx), b.AllFiles),
			ChunkName:       b.Name,
			PrimaryUnitID:   primaryUnitIDForBundle(b),
			StartTime:       cp.CompletedAt,
			EndTime:         cp.CompletedAt,
			DurationSeconds: 0,
			Attempts:        1,
			Retries:         0,
			Status:          "success",
			Files:           append([]string(nil), b.AllFiles...),
			Resumed:         true,
		}
		artifact := cp.Result.Artifact.normalize()
		detail.ArtifactComplete = artifact.ArtifactComplete
		detail.ArtifactState = artifact.ArtifactState
		detail.ArtifactQualityDegraded = artifact.ArtifactQualityDegraded
		detail.UnresolvedIssueCount = artifact.UnresolvedIssueCount
		detail.NormalizedIssueCount = artifact.NormalizedIssueCount
		detail.SchemaRepairAttempts = artifact.SchemaRepairAttempts
		detail.SchemaRepairSuccesses = artifact.SchemaRepairSuccesses
		detail.CandidateQuarantineCount = artifact.CandidateQuarantineCount
		detail.SchemaRepairIssues = artifact.SchemaRepairIssues
		detail.Category = artifact.Category

		mu.Lock()
		allFindings = append(allFindings, cp.Result.Findings...)
		allDebateLogs = append(allDebateLogs, cp.Result.DebateLogs...)
		if ctx.AssessmentProfile != "" {
			allAssessments = append(allAssessments, assessmentRecordsFromFindings(cp.Result.Findings...)...)
		} else {
			allAssessments = append(allAssessments, fileAssessmentRecords(b)...)
		}
		totalHunterTokens += cp.Result.HunterTokens
		totalTier2Tokens += cp.Result.Tier2Tokens
		reusedDetails[bundleIdx] = detail
		processedChunks++
		successChunks++
		reusedCount++
		mu.Unlock()
	}

	// bundle 列表是稳定顺序，checkpoint 复用明细必须回到原始索引位，保证 coverage 不漂移。
	for idx, detail := range reusedDetails {
		chunkDetailsList[idx] = detail
	}
	processedChunks = reusedCount
	successChunks = reusedCount
	if ctx.ProgressReport != nil {
		ctx.ProgressReport(totalChunks, processedChunks, successChunks)
	}
	log.Printf("[BundleResume] task=%d total=%d reused=%d replay=%d invalid=%d\n",
		ctx.ReportID, totalChunks, reusedCount, totalChunks-reusedCount, invalidResumeCount)

bundleLoop:
	for bundleIdx, b := range bundles {
		if ctx.Ctx.Err() != nil {
			break bundleLoop
		}

		bundle := b
		if _, reused := reusedDetails[bundleIdx]; reused {
			continue
		}

		wg.Add(1)
		select {
		case <-ctx.Ctx.Done():
			wg.Done()
			break bundleLoop
		case semaphore <- struct{}{}:
		}

		if ctx.Ctx.Err() != nil {
			select {
			case <-semaphore:
			default:
			}
			wg.Done()
			break bundleLoop
		}

		go func(bnd chunker.SemanticBundle, idx int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			log.Printf("[DebateEngine] Processing bundle %d/%d [%s] (Files: %d)\n", idx, totalChunks, bnd.Name, len(bnd.AllFiles))

			bundleStartTime := time.Now()
			bundleCtx := *ctx
			bundleCtx.Ctx = beginSplitStats(ctx.Ctx)
			bundleCategory := engines.CategoryStageMetrics{}
			findings, debateLogs, assessmentRecords, hTokens, t2Tokens, bundleErr := e.ProcessBundle(&bundleCtx, bnd, idx, chunkDir, &bundleCategory)
			bundleEndTime := time.Now()
			bundleSnapshotHash, snapshotHashErr := chunked.ChunkSnapshotHash(ctx.CodesPath, bnd.AllFiles)
			if snapshotHashErr != nil {
				log.Printf("[BundleResume] Checkpoint snapshot hash failed task=%d bundle=%q err=%v\n",
					ctx.ReportID, bnd.Name, snapshotHashErr)
			}
			if stats := splitStatsFromContext(bundleCtx.Ctx); stats != nil {
				log.Printf("[DebateEngine] Bundle split metrics task=%d bundle=%q depth=%d splits=%d\n",
					ctx.ReportID, bnd.Name, stats.Depth, stats.Count)
			}

			details := engines.ChunkDetails{
				ChunkUID:        coverage.StableChunkID(chunkPolicyIDForContext(ctx), bnd.AllFiles),
				ChunkName:       bnd.Name,
				PrimaryUnitID:   primaryUnitIDForBundle(bnd),
				Attempts:        1,
				Retries:         0,
				StartTime:       bundleStartTime,
				EndTime:         bundleEndTime,
				DurationSeconds: bundleEndTime.Sub(bundleStartTime).Seconds(),
				Files:           append([]string(nil), bnd.AllFiles...),
				PlannedFiles:    append([]coverage.PlannedFile(nil), changePlannedOwners[bundleIdx]...),
				Category:        bundleCategory,
			}
			if stats := splitStatsFromContext(bundleCtx.Ctx); stats != nil {
				if len(stats.ResourceChain) > 0 {
					details.Attempts = len(stats.ResourceChain)
					details.Retries = stats.FreshRetries + stats.ResourceFailovers
				}
				details.ResourceFailovers = stats.ResourceFailovers
				details.ResourceChain = append([]string(nil), stats.ResourceChain...)
				details.ErrorClasses = append([]string(nil), stats.ErrorClasses...)
				details.QueueWaitMS = append([]int64(nil), stats.QueueWaitMS...)
				details.AttemptDurationSeconds = append([]float64(nil), stats.DurationSeconds...)
				details.SplitDepth = stats.Depth
				details.SplitCount = stats.Count
				details.ArtifactComplete = engines.ArtifactCompletePtr(stats.ArtifactComplete)
				details.ArtifactState = "observed"
				details.ArtifactQualityDegraded = stats.ArtifactQualityDegraded
				details.UnresolvedIssueCount = stats.UnresolvedIssueCount
				details.NormalizedIssueCount = stats.NormalizedIssueCount
				details.SchemaRepairAttempts = stats.SchemaRepairAttempts
				details.SchemaRepairSuccesses = stats.SchemaRepairSuccesses
				details.JSONSyntaxRepairs = stats.JSONSyntaxRepairs
				details.RepairBaselineKnown = stats.RepairBaselineKnown
				details.RepairRepairedKnown = stats.RepairRepairedKnown
				details.RepairUnverified = stats.RepairUnverified
				details.RepairOutcome = stats.RepairOutcome
				details.CandidateQuarantineCount = stats.CandidateQuarantineCount
				details.SchemaRepairIssues = stats.SchemaRepairIssues
				details.ArtifactSchemaID = stats.ArtifactSchemaID
				details.ArtifactSchemaHash = stats.ArtifactSchemaHash
				details.ResponseFormatMode = stats.ResponseFormatMode
				details.ResponseFormatFallbacks = stats.ResponseFormatFallbacks
			}

			if bundleErr != nil {
				log.Printf("[DebateEngine] Bundle [%s] debate failed: %v\n", bnd.Name, bundleErr)
				errMu.Lock()
				chunkErrors = append(chunkErrors, fmt.Sprintf("Bundle [%s] failed: %v", bnd.Name, bundleErr))
				errMu.Unlock()

				details.Status = "failed"
				details.ErrorMessage = bundleErr.Error()
				details.ErrorClass = string(invoker.ClassifyError(bundleErr))
			} else {
				details.Status = "success"
				if snapshotHashErr == nil {
					if err := WriteBundleResumeCheckpoint(
						chunkDir,
						ctx.ReportID,
						e.Name(),
						idx,
						bnd,
						findings,
						debateLogs,
						hTokens,
						t2Tokens,
						resumeMetadata,
						bundleSnapshotHash,
						NewBundleArtifactMetrics(details),
					); err != nil {
						log.Printf("[BundleResume] Checkpoint write failed task=%d bundle=%q err=%v\n",
							ctx.ReportID, bnd.Name, err)
					}
				}
			}

			mu.Lock()
			chunkDetailsList[idx] = details
			processedChunks++
			if details.Status == "success" {
				successChunks++
				allFindings = append(allFindings, findings...)
				allDebateLogs = append(allDebateLogs, debateLogs...)
				if ctx.AssessmentProfile != "" {
					allAssessments = append(allAssessments, assessmentRecords...)
				} else {
					allAssessments = append(allAssessments, fileAssessmentRecords(bundle)...)
				}
				totalHunterTokens += hTokens
				totalTier2Tokens += t2Tokens
			}
			if ctx.ProgressReport != nil {
				ctx.ProgressReport(totalChunks, processedChunks, successChunks)
			}
			mu.Unlock()
		}(bundle, bundleIdx)
	}

	wg.Wait()

	ctx.Coverage = engines.BuildScanCoverage(ctx, chunkDetailsList, &coveragePlan)

	hasFailedChunks := len(chunkErrors) > 0
	if hasFailedChunks {
		log.Printf("[DebateEngine] Warning: %d bundles failed, proceeding with %d confirmed findings\n",
			len(chunkErrors), len(allFindings))
	}

	// 确定性严重度校准：受控分类策略优先，通用规则兜底。
	allFindings = governance.CalibrateFindingsWithTaxonomy(ctx.Taxonomy, allFindings)

	result := &engines.EngineResult{
		Findings:        allFindings,
		DebateLogs:      allDebateLogs,
		SummaryChunks:   chunkDetailsList,
		HasFailedChunks: hasFailedChunks,
		HunterTokens:    totalHunterTokens,
		Tier2Tokens:     totalTier2Tokens,
		AnalysisMetrics: engines.AggregateAnalysisMetrics(chunkDetailsList),
	}
	primaryUnits := ctx.PrimaryUnits
	if len(primaryUnits) == 0 {
		for _, plannedFile := range coveragePlan.Selected {
			primaryUnits = append(primaryUnits, coverage.PlanUnit{
				ID:   plannedFile.Path,
				Kind: coverage.PlanUnitFile,
				Path: plannedFile.Path,
			})
		}
	}
	result.AssessmentRecords = allAssessments
	result.PlanReconciliation = coverage.ReconcilePlanUnits(primaryUnits, allAssessments)
	ctx.Coverage.PlanReconciliation = &result.PlanReconciliation

	return result, nil
}

func primaryUnitIDForBundle(bundle chunker.SemanticBundle) string {
	if len(bundle.PrimaryUnits) > 0 {
		return bundle.PrimaryUnits[0].ID
	}
	if len(bundle.PrimaryFiles) == 1 {
		return bundle.PrimaryFiles[0]
	}
	if len(bundle.AllFiles) == 1 {
		return bundle.AllFiles[0]
	}
	return ""
}

func chunkPolicyIDForContext(ctx *engines.EngineContext) string {
	if ctx.ChunkPolicyID != "" {
		return ctx.ChunkPolicyID
	}
	return coverage.ChunkPolicyID(ctx.TaskTypeKey, ctx.EngineConfig)
}

func fileAssessmentRecords(bundle chunker.SemanticBundle) []coverage.AssessmentRecord {
	records := make([]coverage.AssessmentRecord, 0, len(bundle.AllFiles))
	for _, path := range bundle.AllFiles {
		if path == "" {
			continue
		}
		records = append(records, coverage.AssessmentRecord{
			PrimaryUnitID: path,
			Status:        "assessed",
		})
	}
	return records
}

// sanitizeDebateChunkName 将分片名转换为安全的文件名
func sanitizeDebateChunkName(name string) string {
	reg := regexp.MustCompile(`[^a-zA-Z0-9_\-]+`)
	safe := reg.ReplaceAllString(name, "_")
	if len(safe) > 40 {
		safe = safe[:40]
	}
	return safe
}

// ProcessBundle 对单个分片执行完整的智能体协作流
func (e *DebateEngine) ProcessBundle(ctx *engines.EngineContext, bundle chunker.SemanticBundle, chunkIdx int, chunkDir string, category *engines.CategoryStageMetrics) ([]models.AnalysisFinding, []models.TaskDebateLog, []coverage.AssessmentRecord, int64, int64, error) {
	startTime := time.Now()
	safeName := sanitizeDebateChunkName(bundle.Name)
	if ctx.EngineMode == "" {
		ctx.EngineMode = e.Name()
	}

	var hunterOutPath, challOutPath, judgeOutPath string
	if chunkDir != "" {
		hunterOutPath = filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s-1-hunter.json", chunkIdx, safeName))
		challOutPath = filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s-2-challenger.json", chunkIdx, safeName))
		judgeOutPath = filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s-3-judge.json", chunkIdx, safeName))
	}

	if ctx.AssessmentProfile != "" && len(bundle.PrimaryUnits) > 0 {
		assessmentOutPath := ""
		if chunkDir != "" {
			assessmentOutPath = filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s-1-assessment.json", chunkIdx, safeName))
		}
		findings, assessmentRecords, tokens, assessmentErr := e.runSpecializedAssessment(ctx, bundle, assessmentOutPath)
		if assessmentErr != nil {
			if isContractMismatch(assessmentErr) {
				return nil, nil, nil, 0, 0, fmt.Errorf("specialized assessment stage failed: %w", assessmentErr)
			}
			return nil, nil, nil, 0, 0, fmt.Errorf("specialized assessment stage failed: %w", assessmentErr)
		}
		return findings, nil, assessmentRecords, tokens, 0, nil
	}

	// ── 步骤 1: 调度 Tier 1 快模型执行 Hunter 初筛 ──
	hunterOut, hunterTokens, err := e.runHunterStage(ctx, bundle, hunterOutPath)
	if err != nil {
		return nil, nil, nil, 0, 0, fmt.Errorf("hunter stage failed: %w", err)
	}

	if len(hunterOut.Candidates) == 0 {
		log.Printf("[DebateEngine] Bundle [%s]: Hunter found 0 candidates. Fast Pass.", bundle.Name)
		return []models.AnalysisFinding{}, nil, nil, hunterTokens, 0, nil
	}

	log.Printf("[DebateEngine] Bundle [%s]: Hunter identified %d candidates.", bundle.Name, len(hunterOut.Candidates))

	maxCap := 100
	if models.AppConfig.AI.Debate.MaxCandidatesPerChunk > 0 {
		maxCap = models.AppConfig.AI.Debate.MaxCandidatesPerChunk
	}
	if len(hunterOut.Candidates) > maxCap {
		log.Printf("[DebateEngine] Bundle [%s]: Truncating %d candidates to top %d for debate",
			bundle.Name, len(hunterOut.Candidates), maxCap)
		hunterOut.Candidates = hunterOut.Candidates[:maxCap]
	}

	// ── 步骤 2: 调度 Tier 2 强推理模型执行 Challenger 对抗辩护 ──
	challengerOut, challTokens, err := e.runChallengerStage(ctx, bundle, hunterOut, challOutPath)
	if err != nil {
		log.Printf("[DebateEngine] Warning: Challenger failed (%v), proceeding with degraded Challenger note", err)
		challengerOut = &ChallengerOutput{}
	}

	// ── 步骤 3: 调度 Tier 2 强推理模型执行 Judge 终审仲裁 ──
	judgeOut, judgeTokens, err := e.runJudgeStage(ctx, bundle, hunterOut, challengerOut, judgeOutPath)
	if err != nil {
		log.Printf("[DebateEngine] Warning: Judge failed (%v), fallback to Hunter claims", err)
		judgeOut = fallbackJudgeFromHunter(fmt.Sprintf("Judge 执行失败: %v", err), hunterOut)
	}
	if category != nil {
		if hunterOut != nil {
			category.Hunter = hunterOut.CategoryMetrics
		}
		if judgeOut != nil {
			category.Judge = judgeOut.CategoryMetrics
		}
	}

	totalTier2Tokens := challTokens + judgeTokens

	// ── 步骤 4: 转换裁决结论为系统标准 Finding 与 DebateLog ──
	var confirmedFindings []models.AnalysisFinding
	var debateLogs []models.TaskDebateLog

	totalDurationMs := int(time.Since(startTime).Milliseconds())

	caseMap := make(map[string]ChallengerDefenseCase)
	for _, dc := range challengerOut.DefenseCases {
		caseMap[dc.CandidateID] = dc
	}
	candidateMap := make(map[string]HunterCandidate)
	for _, hc := range hunterOut.Candidates {
		candidateMap[hc.CandidateID] = hc
	}

	for _, jv := range judgeOut.FinalVerdicts {
		origCand := candidateMap[jv.CandidateID]
		challCase := caseMap[jv.CandidateID]

		hBytes, _ := json.Marshal(origCand)
		cBytes, _ := json.Marshal(challCase)
		jBytes, _ := json.Marshal(jv)
		tokenStats, _ := json.Marshal(map[string]int64{
			"hunter_tokens":     hunterTokens,
			"challenger_tokens": challTokens,
			"judge_tokens":      judgeTokens,
		})

		logEntry := models.TaskDebateLog{
			ChunkName:        bundle.Name,
			CandidateID:      jv.CandidateID,
			TriggerLine:      jv.TriggerLine,
			HunterOutput:     datatypes.JSON(hBytes),
			ChallengerOutput: datatypes.JSON(cBytes),
			JudgeOutput:      datatypes.JSON(jBytes),
			Verdict:          jv.Verdict,
			DurationMs:       totalDurationMs,
			TokenUsage:       datatypes.JSON(tokenStats),
			CreatedAt:        time.Now(),
		}
		debateLogs = append(debateLogs, logEntry)

		if jv.Verdict == models.DebateVerdictConfirmed || jv.Verdict == models.DebateVerdictConditional {
			challArgText := challCase.DefenseVerdict
			if challCase.MitigatingFactors != "" {
				challArgText += " (" + challCase.MitigatingFactors + ")"
			}

			categoryCode, categorySource, categoryStatus := resolveJudgeCategoryInheritance(&jv, origCand)
			normalizedCategory := governance.NormalizeCategory(jv.Category, ctx.AllowedCategories)
			normalizedTaxonomyHash := ctx.TaxonomyHash
			if normalizedTaxonomyHash == "" {
				normalizedTaxonomyHash = ctx.Taxonomy.Hash
			}
			cleanCategory := normalizedCategory.Category
			normScope := defectlifecycle.NormalizeScopeSymbol(jv.ScopeSymbol)
			cleanTrigger := defectlifecycle.CleanSourceToken(jv.TriggerLine)
			modelLine := jv.LineRange
			if strings.TrimSpace(modelLine) == "" {
				modelLine = jv.LineNumber
			}
			calibratedLine := modelLine

			if ctx.CodesPath != "" {
				if anchor, err := defectlifecycle.EnrichSourceAnchor(ctx.CodesPath, jv.FilePath, modelLine, jv.TriggerLine); err == nil {
					normScope = anchor.NormalizedScope
					cleanTrigger = anchor.PhysicalToken
					calibratedLine = fmt.Sprintf("%d-%d", anchor.StartLine, anchor.EndLine)
				}
			}

			finding := models.AnalysisFinding{
				FilePath:                jv.FilePath,
				LineNumber:              calibratedLine,
				TriggerLine:             cleanTrigger,
				ScopeSymbol:             normScope,
				CodeSnippet:             jv.CodeSnippet,
				Category:                cleanCategory,
				CategoryDetail:          normalizedCategory.Detail,
				CategoryCode:            categoryCode,
				CategorySource:          categorySource,
				CategoryStatus:          categoryStatus,
				ClassificationRationale: jv.ClassificationRationale,
				TaxonomyHash:            normalizedTaxonomyHash,
				TaxonomyGoverned:        normalizedCategory.Governed,
				ReviewRequired:          jv.ReviewRequired,
				Title:                   DeriveConciseTitle(jv.Title, cleanCategory),
				Detail:                  deriveDefectDetail(origCand, jv),
				Suggestion:              jv.Suggestion,
				HunterClaim:             origCand.AttackHypothesis,
				ChallengerArg:           challArgText,
				JudgeVerdict:            jv.JudgementRationale,
				Severity:                jv.SeverityPreliminary,
				CreatedAt:               time.Now(),
			}
			confirmedFindings = append(confirmedFindings, finding)
			log.Printf("[DebateEngine] Candidate %s [%s]: %s\n", jv.CandidateID, jv.Verdict, jv.Title)
		} else {
			log.Printf("[DebateEngine] Candidate %s [%s]: %s (Reason: %s)\n",
				jv.CandidateID, jv.Verdict, jv.Title, summarizeRationale(jv.JudgementRationale))
		}
	}

	if chunkDir != "" && len(judgeOut.FinalVerdicts) > 0 {
		logFilePath := filepath.Join(chunkDir, "debate-verdicts.log")
		var logSb strings.Builder
		for _, jv := range judgeOut.FinalVerdicts {
			logSb.WriteString("================================================================================\n")
			logSb.WriteString(fmt.Sprintf("[%s] Candidate: %s | Verdict: %s | Title: %s\n", time.Now().Format("2006-01-02 15:04:05"), jv.CandidateID, jv.Verdict, jv.Title))
			modelLine := jv.LineRange
			if strings.TrimSpace(modelLine) == "" {
				modelLine = jv.LineNumber
			}
			logSb.WriteString(fmt.Sprintf("File: %s:%s | Trigger: %s | Scope: %s\n", jv.FilePath, modelLine, jv.TriggerLine, jv.ScopeSymbol))
			logSb.WriteString(fmt.Sprintf("Category: %s | Severity: %s\n\n", jv.Category, jv.SeverityPreliminary))
			logSb.WriteString(fmt.Sprintf("【详细裁判词与事实推演】:\n%s\n\n", jv.JudgementRationale))
			if jv.Suggestion != "" {
				logSb.WriteString(fmt.Sprintf("【建议修复方案】:\n%s\n\n", jv.Suggestion))
			}
		}
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			_, _ = f.WriteString(logSb.String())
			_ = f.Close()
		}
	}

	return confirmedFindings, debateLogs, nil, hunterTokens, totalTier2Tokens, nil
}

// summarizeRationale 将冗长的仲裁裁判词浓缩为控制台单行可读的简要结论
func summarizeRationale(text string) string {
	trimmed := strings.TrimSpace(text)
	if idx := strings.Index(trimmed, "【源码事实】"); idx != -1 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	if idx := strings.Index(trimmed, "\n"); idx != -1 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	trimmed = strings.TrimPrefix(trimmed, "【综合裁决】:")
	trimmed = strings.TrimPrefix(trimmed, "【综合裁决】：")
	trimmed = strings.TrimSpace(trimmed)
	runes := []rune(trimmed)
	if len(runes) > 60 {
		return string(runes[:60]) + "..."
	}
	if len(runes) == 0 {
		return "已驳回"
	}
	return trimmed
}

// runHunterStage runs Hunter for a semantic bundle. `timeout_seconds` is the
// whole split budget; `attempt_timeout_seconds` is each individual AI call.
// A timeout is not retried with the same payload: the bundle is recursively
// split into smaller bundles until either the work completes or the budget ends.
func (e *DebateEngine) runHunterStage(ctx *engines.EngineContext, bundle chunker.SemanticBundle, outPath string) (*HunterOutput, int64, error) {
	tierCfg := models.AppConfig.GetTierConfig("tier1_hunter")
	budgetSeconds := tierCfg.TimeoutSeconds
	if budgetSeconds <= 0 {
		budgetSeconds = 600
	}
	attemptSeconds := tierCfg.AttemptTimeoutSeconds
	if attemptSeconds <= 0 {
		attemptSeconds = budgetSeconds
	}

	stageCtx := *ctx
	budgetCtx, cancel := context.WithTimeout(ctx.Ctx, time.Duration(budgetSeconds)*time.Second)
	defer cancel()
	stageCtx.Ctx = budgetCtx

	return e.runHunterStageWithBudget(&stageCtx, bundle, outPath, budgetSeconds, attemptSeconds)
}

func (e *DebateEngine) runHunterStageWithBudget(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	outPath string,
	budgetSeconds int,
	attemptSeconds int,
) (*HunterOutput, int64, error) {
	remainingSeconds := hunterRemainingSeconds(ctx.Ctx, budgetSeconds)
	if remainingSeconds <= 0 {
		return nil, 0, fmt.Errorf("hunter split budget exhausted: %w", ctx.Ctx.Err())
	}
	return e.runHunterStageRecovery(ctx, bundle, outPath, budgetSeconds, attemptSeconds)
}

func (e *DebateEngine) runHunterStageRecovery(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	outPath string,
	budgetSeconds int,
	attemptSeconds int,
) (*HunterOutput, int64, error) {
	tierCfg := models.AppConfig.GetTierConfig("tier1_hunter")
	recovery := tierCfg.Recovery
	if len(recovery.SplitOn) == 0 {
		recovery.SplitOn = []string{
			string(invoker.ErrorClassIdleTimeout),
			string(invoker.ErrorClassTimeout),
			string(invoker.ErrorClassOutputMissing),
		}
	}
	remainingSeconds := hunterRemainingSeconds(ctx.Ctx, budgetSeconds)
	if remainingSeconds <= 0 {
		return nil, 0, fmt.Errorf("hunter stage budget exhausted: %w", ctx.Ctx.Err())
	}
	attemptSeconds = min(attemptSeconds, remainingSeconds)

	var hunterOut *HunterOutput
	_, _, tokens, invocationErr := runTierInvocationWithRecovery(
		ctx.Ctx,
		"tier1_hunter",
		attemptSeconds,
		nil,
		func(callCtx context.Context, candidate dispatcher.TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error) {
			callEngineCtx := *ctx
			callEngineCtx.Ctx = callCtx
			out, tokens, err := e.runHunterStageOnceWithPlan(&callEngineCtx, bundle, outPath, timeoutSeconds, candidate, metrics)
			if err == nil {
				hunterOut = out
			}
			return "", tokens, err
		},
		dispatcher.WithTierSplitRecovery(func(
			_ context.Context,
			_ dispatcher.TierCandidate,
			tokens int64,
			class invoker.ErrorClass,
			err error,
		) (bool, string, int64, error) {
			if !shouldSplitBundleOnRecovery(ctx.Ctx, recovery, class, bundle, err) {
				return false, "", tokens, err
			}
			splitOut, splitTokens, splitErr := e.splitHunterBundle(
				ctx,
				bundle,
				outPath,
				budgetSeconds,
				attemptSeconds,
				nil,
				tokens,
				err,
				class,
			)
			if splitErr == nil {
				hunterOut = splitOut
			}
			return true, "", splitTokens, splitErr
		}),
	)
	return hunterOut, tokens, invocationErr
}

func (e *DebateEngine) splitHunterBundle(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	outPath string,
	budgetSeconds int,
	attemptSeconds int,
	out *HunterOutput,
	tokens int64,
	err error,
	class invoker.ErrorClass,
) (*HunterOutput, int64, error) {
	tierCfg := models.AppConfig.GetTierConfig("tier1_hunter")
	if !canSplit(ctx.Ctx, tierCfg) || len(bundle.AllFiles) <= 1 {
		log.Printf("[DebateEngine] Bundle [%s]: Hunter recovery cannot split further: class=%s max_split_depth=%d files=%d (%v)",
			bundle.Name, class, tierCfg.MaxSplitDepth, len(bundle.AllFiles), err)
		return nil, tokens, err
	}

	subBundles := chunker.SplitSemanticBundleInHalf(bundle)
	if len(subBundles) == 0 {
		log.Printf("[DebateEngine] Bundle [%s]: Hunter timeout cannot be reduced further: %v", bundle.Name, err)
		return nil, tokens, err
	}

	log.Printf("[DebateEngine] Bundle [%s]: Hunter %s (%v); splitting %d files into %d smaller bundles",
		bundle.Name, class, err, len(bundle.AllFiles), len(subBundles))
	beginSplit(ctx.Ctx)

	var merged HunterOutput
	totalTokens := tokens
	categoryMetrics := engines.CategoryStageMetrics{}
	if out != nil {
		categoryMetrics.Hunter = out.CategoryMetrics
	}
	for i, subBundle := range subBundles {
		subOutPath := hunterSplitOutputPath(outPath, i+1)
		subBudgetSeconds := hunterRemainingSeconds(ctx.Ctx, budgetSeconds)
		if subBudgetSeconds <= 0 {
			return nil, totalTokens, fmt.Errorf("hunter split budget exhausted before bundle %d/%d: %w", i+1, len(subBundles), ctx.Ctx.Err())
		}
		subOut, subTokens, subErr := e.runHunterStageWithBudget(ctx, subBundle, subOutPath, subBudgetSeconds, attemptSeconds)
		totalTokens += subTokens
		if subErr != nil {
			return nil, totalTokens, fmt.Errorf("hunter split bundle %d/%d failed: %w", i+1, len(subBundles), subErr)
		}
		merged.Candidates = append(merged.Candidates, subOut.Candidates...)
		categoryMetrics = categoryMetrics.Add(engines.CategoryStageMetrics{Hunter: subOut.CategoryMetrics})
	}
	merged.CategoryMetrics = categoryMetrics.Hunter

	for i := range merged.Candidates {
		merged.Candidates[i].CandidateID = fmt.Sprintf("H-%03d", i+1)
	}

	if outPath != "" {
		if outBytes, jsonErr := json.MarshalIndent(&merged, "", "  "); jsonErr == nil {
			if writeErr := os.WriteFile(outPath, outBytes, 0644); writeErr != nil {
				return nil, totalTokens, fmt.Errorf("write merged hunter output: %w", writeErr)
			}
		}
	}
	cleanupSplitArtifacts(outPath)
	return &merged, totalTokens, nil
}

func hunterSplitOutputPath(outPath string, index int) string {
	if outPath == "" {
		return ""
	}
	ext := filepath.Ext(outPath)
	base := strings.TrimSuffix(outPath, ext)
	return fmt.Sprintf("%s.split-%d%s", base, index, ext)
}

func resolveJudgeCategoryInheritance(verdict *JudgeFinalVerdict, hunter HunterCandidate) (string, string, string) {
	if verdict == nil {
		return "", CategorySourceJudgeInherit, CategoryStatusReviewRequired
	}
	if !verdict.ReviewRequired {
		return verdict.CategoryCode, CategorySourceJudge, CategoryStatusValid
	}
	if strings.TrimSpace(hunter.Category) != "" {
		verdict.Category = hunter.Category
	}
	return hunter.CategoryCode, CategorySourceJudgeInherit, CategoryStatusReviewRequired
}

func cleanupSplitArtifacts(outPath string) {
	if outPath == "" {
		return
	}
	ext := filepath.Ext(outPath)
	matches, err := filepath.Glob(strings.TrimSuffix(outPath, ext) + ".split-*" + ext)
	if err != nil {
		return
	}
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

func isInvocationTimeout(err error) bool {
	if err == nil {
		return false
	}
	// Compatibility only: callers already emit typed errors from invoker,
	// CLI runner and dispatcher. New code must use invoker.ClassifyError.
	switch invoker.ClassifyError(err) {
	case invoker.ErrorClassTimeout, invoker.ErrorClassIdleTimeout:
		return true
	default:
		return false
	}
}

func hunterRemainingSeconds(ctx context.Context, fallbackSeconds int) int {
	if ctx == nil || ctx.Err() != nil {
		return 0
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := int(time.Until(deadline).Seconds() + 0.5)
		if remaining < 1 {
			return 0
		}
		return remaining
	}
	if fallbackSeconds <= 0 {
		return 600
	}
	return fallbackSeconds
}

// runHunterStageOnce performs one Hunter invocation, including one contract
// mismatch repair retry. The caller supplies the single-invocation timeout.
func (e *DebateEngine) runHunterStageOnce(ctx *engines.EngineContext, bundle chunker.SemanticBundle, outPath string, timeoutSeconds int) (*HunterOutput, int64, error) {
	return e.runHunterStageOnceWithPlan(ctx, bundle, outPath, timeoutSeconds, dispatcher.TierCandidate{})
}

func (e *DebateEngine) runHunterStageOnceWithPlan(ctx *engines.EngineContext, bundle chunker.SemanticBundle, outPath string, timeoutSeconds int, candidate dispatcher.TierCandidate, metricsOpt ...*invoker.InvocationMetrics) (*HunterOutput, int64, error) {
	// The split wrapper owns the whole-budget context. Bound this function
	// separately so both the initial call and its contract repair retry share
	// one per-attempt timeout rather than consuming the remaining split budget.
	attemptCtx, cancelAttempt := context.WithTimeout(ctx.Ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancelAttempt()
	attemptEngineCtx := *ctx
	attemptEngineCtx.Ctx = attemptCtx
	ctx = &attemptEngineCtx

	router := dispatcher.GetTierRouter()
	acq, err := router.AcquireTier(ctx.Ctx, "tier1_hunter", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	prompt := buildHunterPrompt(ctx, bundle)
	tierCfg := models.AppConfig.GetTierConfig("tier1_hunter")
	contract, contractErr := ContractForStageWithTaxonomy(ctx.EngineMode, "hunter", ctx.AllowedCategories, ctx.Taxonomy)
	if contractErr != nil {
		return nil, 0, fmt.Errorf("load hunter output contract: %w", contractErr)
	}
	workCtx := &invoker.LLMWorkContext{
		ReportID: ctx.ReportID,
		RepoName: ctx.RepoName,
		TaskType: ctx.TaskTypeName,
		Stage:    "Tier 1: 猎手初筛",
		SubTask:  fmt.Sprintf("分片束 %s (%d 个候选文件)", bundle.Name, len(bundle.AllFiles)),
		TierName: "tier1_hunter",
	}
	backend := acq.Backend
	modelName := acq.ModelName
	driver := backend
	resourceID := acq.ResourceID
	if candidate.ResourceID != "" {
		backend = candidate.Driver
		modelName = candidate.Model
		driver = candidate.Driver
		resourceID = candidate.ResourceID
		workCtx.ResourceID = candidate.ResourceID
	}
	metrics := &invoker.InvocationMetrics{}
	if len(metricsOpt) > 0 && metricsOpt[0] != nil {
		metrics = metricsOpt[0]
	}
	rawOutput, tokens, err := callAITierWithMetrics(ctx.Ctx, backend, modelName, prompt, ctx.CodesPath, outPath, timeoutSeconds, tierTimeoutPolicy(tierCfg), metrics, workCtx)
	if err != nil {
		return nil, tokens, err
	}

	state, repairAttempts, repairSuccesses, repairTokens, recoverErr := parseAndRecoverHunterArtifact(
		ctx.Ctx, rawOutput, contract, ctx.CodesPath, bundle, ctx.AllowedCategories, ctx.Taxonomy, ctx.CategoryAliasRecorder,
	)
	tokens += repairTokens
	if state.RepairAudit != nil {
		state.RepairAudit.Driver = driver
		state.RepairAudit.ResourceID = resourceID
		if recoverErr != nil {
			state.RepairAudit.setFinalStatus("failed")
		}
		persistStageArtifactRepairAudit(ctx.Ctx, bundle, bundle.Name, ctx.ReportID, ctx.RepoID, ctx.TaskTypeID, state.RepairAudit)
		if stats := splitStatsFromContext(ctx.Ctx); stats != nil {
			stats.ArtifactSchemaID = state.RepairAudit.SchemaID
			stats.ArtifactSchemaHash = state.RepairAudit.SchemaHash
			stats.ResponseFormatMode = state.RepairAudit.ResponseFormatMode
		}
	}
	repairIssueMessages := append(unresolvedIssueMessages(state), categoryRepairAuditMessages(state)...)
	normalizedIssueCount := len(state.Issues) - state.unresolvedIssueCount() - state.Quarantined
	if normalizedIssueCount < 0 {
		normalizedIssueCount = 0
	}
	recordArtifactRecovery(
		ctx.Ctx,
		normalizedIssueCount,
		repairAttempts,
		repairSuccesses,
		state.Quarantined,
		state.ArtifactComplete,
		state.ArtifactQualityDegraded,
		state.UnresolvedIssueCount,
		repairIssueMessages,
		state.RepairAudit,
	)
	if (repairSuccesses > 0 || (state.RepairAudit != nil && state.RepairAudit.SyntaxRepairs > 0)) &&
		outPath != "" && len(state.NormalizedRaw) > 0 {
		if writeErr := os.WriteFile(outPath, state.NormalizedRaw, 0644); writeErr != nil {
			log.Printf("[DebateEngine] Failed to persist repaired hunter artifact %s: %v\n", outPath, writeErr)
		}
	}
	if recoverErr != nil {
		return nil, tokens, invoker.WrapClassifiedError(
			invoker.ErrorClassContractMismatch, recoverErr, "hunter output contract mismatch",
		)
	}
	if state.Output == nil {
		return nil, tokens, invoker.NewClassifiedError(
			invoker.ErrorClassOutputMissing, "hunter artifact recovery produced no output",
		)
	}

	hunterOut := state.Output
	normalizeNumericHunterTriggers(ctx.CodesPath, hunterOut)
	return hunterOut, tokens, nil
}

func normalizeNumericHunterTriggers(repoRoot string, hunterOut *HunterOutput) {
	if hunterOut == nil || repoRoot == "" {
		return
	}
	for i := range hunterOut.Candidates {
		candidate := &hunterOut.Candidates[i]
		candidate.TriggerLine = defectlifecycle.ResolvePhysicalTriggerLine(
			repoRoot,
			candidate.FilePath,
			candidate.LineRange,
			candidate.TriggerLine,
		)
	}
}

const defaultMaxCandidatesPerBatch = 5

func splitCandidatesBalanced(candidates []HunterCandidate, maxPerBatch int) [][]HunterCandidate {
	if len(candidates) == 0 || maxPerBatch <= 0 {
		return nil
	}

	batchCount := (len(candidates) + maxPerBatch - 1) / maxPerBatch
	baseSize := len(candidates) / batchCount
	remainder := len(candidates) % batchCount

	batches := make([][]HunterCandidate, 0, batchCount)
	offset := 0
	for i := 0; i < batchCount; i++ {
		batchSize := baseSize
		if i < remainder {
			batchSize++
		}
		batches = append(batches, candidates[offset:offset+batchSize])
		offset += batchSize
	}
	return batches
}

type splitStatsKey struct{}

type splitStats struct {
	Depth                    int
	Count                    int
	FreshRetries             int
	ContractRepairs          int
	ResourceFailovers        int
	DriverFailovers          int
	ResourceChain            []string
	ErrorClasses             []string
	QueueWaitMS              []int64
	DurationSeconds          []float64
	ArtifactComplete         bool
	ArtifactQualityDegraded  bool
	UnresolvedIssueCount     int
	NormalizedIssueCount     int
	SchemaRepairAttempts     int
	SchemaRepairSuccesses    int
	JSONSyntaxRepairs        int
	RepairBaselineKnown      bool
	RepairRepairedKnown      bool
	RepairUnverified         bool
	RepairOutcome            string
	CandidateQuarantineCount int
	SchemaRepairIssues       []string
	ArtifactSchemaID         string
	ArtifactSchemaHash       string
	ResponseFormatMode       string
	ResponseFormatFallbacks  int
}

func splitStatsFromContext(ctx context.Context) *splitStats {
	if ctx == nil {
		return nil
	}
	if stats, ok := ctx.Value(splitStatsKey{}).(*splitStats); ok {
		return stats
	}
	return nil
}

func beginSplitStats(ctx context.Context) context.Context {
	return context.WithValue(ctx, splitStatsKey{}, &splitStats{
		ArtifactComplete:    true,
		RepairBaselineKnown: true,
		RepairRepairedKnown: true,
	})
}

func canSplit(ctx context.Context, tierCfg models.TierConfig) bool {
	stats := splitStatsFromContext(ctx)
	if stats == nil {
		return true
	}
	maxDepth := tierCfg.MaxSplitDepth
	if maxDepth <= 0 {
		maxDepth = 8
	}
	return stats.Depth < maxDepth
}

func beginSplit(ctx context.Context) {
	if stats := splitStatsFromContext(ctx); stats != nil {
		stats.Depth++
		stats.Count++
	}
}

func recoveryAllows(classes []string, class invoker.ErrorClass) bool {
	for _, item := range classes {
		if strings.EqualFold(strings.TrimSpace(item), string(class)) {
			return true
		}
	}
	return false
}

func shouldSplitBundleOnRecovery(
	ctx context.Context,
	recovery models.TierRecoveryConfig,
	class invoker.ErrorClass,
	bundle chunker.SemanticBundle,
	err error,
) bool {
	if err == nil || len(bundle.AllFiles) <= 1 {
		return false
	}
	switch class {
	case invoker.ErrorClassTimeout, invoker.ErrorClassIdleTimeout:
		return recoveryAllows(recovery.SplitOn, class)
	case invoker.ErrorClassOutputMissing:
		return recoveryAllows(recovery.SplitOn, class)
	default:
		return false
	}
}

func runTierInvocationWithRecovery(
	ctx context.Context,
	tierName string,
	attemptSeconds int,
	workCtx *invoker.LLMWorkContext,
	invoke func(ctx context.Context, candidate dispatcher.TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error),
	options ...dispatcher.TierRecoveryOption,
) (string, dispatcher.TierCandidate, int64, error) {
	recoveryStats := &dispatcher.TierRecoveryStats{}
	rawOutput, candidate, tokens, err := dispatcher.RunTierInvocationWithRecovery(
		ctx, tierName, attemptSeconds, recoveryStats, invoke, options...,
	)
	recordDispatcherRecoveryStats(ctx, recoveryStats)
	_ = workCtx
	return rawOutput, candidate, tokens, err
}

func recordDispatcherRecoveryStats(ctx context.Context, source *dispatcher.TierRecoveryStats) {
	if source == nil {
		return
	}
	if stats := splitStatsFromContext(ctx); stats != nil {
		stats.FreshRetries += source.FreshRetries
		stats.ResourceFailovers += source.ResourceFailovers
		stats.DriverFailovers += source.DriverFailovers
		stats.ResourceChain = append(stats.ResourceChain, source.ResourceChain...)
		stats.ErrorClasses = append(stats.ErrorClasses, source.ErrorClasses...)
		stats.QueueWaitMS = append(stats.QueueWaitMS, source.QueueWaitMS...)
		stats.DurationSeconds = append(stats.DurationSeconds, source.DurationSeconds...)
	}
}

func contractRepairCountFromContext(ctx context.Context) int {
	if stats := splitStatsFromContext(ctx); stats != nil {
		return stats.ContractRepairs
	}
	return 0
}

func recordInternalCallMetrics(ctx context.Context, metrics *invoker.InvocationMetrics) {
	if metrics == nil {
		return
	}
	if stats := splitStatsFromContext(ctx); stats != nil {
		if len(stats.QueueWaitMS) > 0 {
			stats.QueueWaitMS[len(stats.QueueWaitMS)-1] += metrics.QueueWaitMs
		} else {
			stats.QueueWaitMS = append(stats.QueueWaitMS, metrics.QueueWaitMs)
		}
		if len(stats.DurationSeconds) > 0 {
			stats.DurationSeconds[len(stats.DurationSeconds)-1] += float64(metrics.DurationMs) / 1000
		} else {
			stats.DurationSeconds = append(stats.DurationSeconds, float64(metrics.DurationMs)/1000)
		}
		stats.DriverFailovers += metrics.DriverFailovers
	}
	metrics.QueueWaitMs = 0
	metrics.DurationMs = 0
	metrics.DriverFailovers = 0
}

func recordArtifactRecovery(
	ctx context.Context,
	normalized, repairAttempts, repairSuccesses, quarantined int,
	artifactComplete, qualityDegraded bool,
	unresolvedIssueCount int,
	repairIssues []string,
	audit *ArtifactRepairMetrics,
) {
	if stats := splitStatsFromContext(ctx); stats != nil {
		stats.ContractRepairs += repairAttempts
		stats.NormalizedIssueCount += normalized
		stats.SchemaRepairAttempts += repairAttempts
		stats.SchemaRepairSuccesses += repairSuccesses
		stats.CandidateQuarantineCount += quarantined
		stats.ArtifactComplete = stats.ArtifactComplete && artifactComplete
		stats.ArtifactQualityDegraded = stats.ArtifactQualityDegraded || qualityDegraded
		stats.UnresolvedIssueCount += unresolvedIssueCount
		stats.SchemaRepairIssues = append(stats.SchemaRepairIssues, repairIssues...)
		if audit != nil {
			stats.JSONSyntaxRepairs += audit.SyntaxRepairs
			stats.RepairBaselineKnown = stats.RepairBaselineKnown && audit.BaselineSignatureKnown
			stats.RepairRepairedKnown = stats.RepairRepairedKnown && audit.RepairedSignatureKnown
			stats.RepairUnverified = stats.RepairUnverified || audit.UnverifiedRepair
			if audit.RepairOutcome != "" {
				stats.RepairOutcome = audit.RepairOutcome
			}
		}
	}
}

func unresolvedIssueMessages(state *hunterArtifactState) []string {
	// The issue list intentionally retains pre-repair observations for audit,
	// so the persisted unresolved count is the only reliable signal for
	// whether any issue is still outstanding.
	if state == nil || state.UnresolvedIssueCount == 0 {
		return nil
	}
	messages := make([]string, 0)
	for _, issue := range state.Issues {
		if !issue.Recoverable {
			messages = append(messages, issue.Message)
		}
	}
	if len(messages) == 0 {
		return nil
	}
	return messages
}

// runChallengerStage 运行辩护人对抗阶段
func (e *DebateEngine) runChallengerStage(ctx *engines.EngineContext, bundle chunker.SemanticBundle, hunterOut *HunterOutput, outPath string) (*ChallengerOutput, int64, error) {
	if len(hunterOut.Candidates) == 0 {
		return &ChallengerOutput{}, 0, nil
	}

	tierCfg := models.AppConfig.GetTierConfig("tier2_challenger")
	stageCtx := *ctx
	if tierCfg.TimeoutSeconds > 0 {
		stageCtxCancel, cancelStage := context.WithTimeout(ctx.Ctx, time.Duration(tierCfg.TimeoutSeconds)*time.Second)
		defer cancelStage()
		stageCtx.Ctx = stageCtxCancel
	}

	router := dispatcher.GetTierRouter()
	acq, err := router.AcquireTier(stageCtx.Ctx, "tier2_challenger", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	batches := splitCandidatesBalanced(hunterOut.Candidates, defaultMaxCandidatesPerBatch)

	var mergedChallengerOut ChallengerOutput
	var totalTokens int64

	for bIdx, batch := range batches {
		subHunterOut := &HunterOutput{
			Candidates: sanitizeCandidatesForPrompt(batch),
		}
		subOutPath := outPath
		if len(batches) > 1 && outPath != "" {
			subOutPath = fmt.Sprintf("%s.batch-%d.tmp", outPath, bIdx+1)
		}

		evidencePacks, evidenceErr := BuildJudgeEvidencePacks(ctx, bundle, batch, map[string]ChallengerDefenseCase{})
		if evidenceErr != nil {
			log.Printf("[DebateEngine] Warning: Challenger Batch %d/%d evidence build failed (%v), degrading this batch", bIdx+1, len(batches), evidenceErr)
			for _, cand := range batch {
				mergedChallengerOut.DefenseCases = append(mergedChallengerOut.DefenseCases, ChallengerDefenseCase{
					CandidateID:       cand.CandidateID,
					DefenseVerdict:    "CHALLENGE_FAILED",
					MitigatingFactors: "[Challenger Degraded: 证据包构建失败，请法官独立基于源码客观裁决]",
				})
			}
			continue
		}

		prompt, promptErr := buildChallengerPrompt(ctx, bundle, subHunterOut, evidencePacks)
		if promptErr != nil {
			return nil, totalTokens, fmt.Errorf("failed to build challenger prompt: %w", promptErr)
		}

		promptPath := stagePromptFilePath(subOutPath, "challenger", 0)
		if promptErr := writeStagePromptFile(promptPath, prompt); promptErr != nil {
			return nil, totalTokens, fmt.Errorf("write challenger prompt file: %w", promptErr)
		}

		workCtx := &invoker.LLMWorkContext{
			ReportID: ctx.ReportID,
			RepoName: ctx.RepoName,
			TaskType: ctx.TaskTypeName,
			Stage:    "Tier 2: 辩护对抗 (Challenger)",
			SubTask:  fmt.Sprintf("批次 %d/%d (候选点 %d 个)", bIdx+1, len(batches), len(batch)),
			TierName: "tier2_challenger",
		}
		attemptSeconds := tierCfg.AttemptTimeoutSeconds
		if attemptSeconds <= 0 {
			attemptSeconds = tierCfg.TimeoutSeconds
		}
		rawOutput, candidate, tokens, callErr := runTierInvocationWithRecovery(
			stageCtx.Ctx,
			"tier2_challenger",
			attemptSeconds,
			workCtx,
			func(callCtx context.Context, candidate dispatcher.TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error) {
				workCtx.ResourceID = candidate.ResourceID
				return callAITierPromptFileWithMetrics(
					callCtx,
					candidate.Driver,
					candidate.Model,
					promptPath,
					stageCtx.CodesPath,
					subOutPath,
					timeoutSeconds,
					tierTimeoutPolicy(tierCfg),
					metrics,
					workCtx,
				)
			},
		)
		if len(batches) > 1 && subOutPath != "" && subOutPath != outPath {
			_ = os.Remove(subOutPath)
		}

		totalTokens += tokens
		if callErr != nil {
			log.Printf("[DebateEngine] Warning: Challenger Batch %d/%d failed (%v), degrading this batch", bIdx+1, len(batches), callErr)
			for _, cand := range batch {
				mergedChallengerOut.DefenseCases = append(mergedChallengerOut.DefenseCases, ChallengerDefenseCase{
					CandidateID:       cand.CandidateID,
					DefenseVerdict:    "CHALLENGE_FAILED",
					MitigatingFactors: "[Challenger Degraded: 辩护人该批次调用超时，请法官独立基于源码客观裁决]",
				})
			}
			continue
		}

		allowedDimensions := (&PromptAssembler{}).defenseDimensionKeys(ctx)
		challengerContract := newChallengerContract(allowedDimensions)
		batchIDs := make([]string, 0, len(batch))
		for _, item := range batch {
			batchIDs = append(batchIDs, item.CandidateID)
		}
		parseChallenger := func(raw string) (*ChallengerOutput, []byte, bool, error) {
			var output ChallengerOutput
			if parseErr := decodeContractJSON(raw, challengerContract, &output); parseErr != nil {
				return nil, nil, false, parseErr
			}
			beforeNormalize, beforeErr := json.Marshal(output)
			NormalizeChallengerEvidencePacks(evidencePacks, &output)
			normalized, normalizedErr := json.Marshal(output)
			localChanged := beforeErr == nil && normalizedErr == nil && !bytes.Equal(beforeNormalize, normalized)
			if validateErr := validateChallengerOutput(&output, batchIDs, allowedDimensions); validateErr != nil {
				return &output, normalized, localChanged, validateErr
			}
			if _, verifyErr := VerifyChallengerEvidence(evidencePacks, &output); verifyErr != nil {
				return &output, normalized, localChanged, verifyErr
			}
			return &output, normalized, localChanged, nil
		}

		audit := newArtifactRepairMetrics([]byte(rawOutput), challengerContract)
		audit.Driver = candidate.Driver
		audit.ResourceID = candidate.ResourceID
		baselineSignature, baselineKnown := challengerBusinessSignature([]byte(rawOutput))
		var baselineChallengerOut ChallengerOutput
		if decodeErr := json.Unmarshal(normalizeContractJSON(cleanJSONOutput([]byte(rawOutput))), &baselineChallengerOut); decodeErr == nil {
			NormalizeChallengerEvidencePacks(evidencePacks, &baselineChallengerOut)
			if baselineRaw, baselineMarshalErr := json.Marshal(baselineChallengerOut); baselineMarshalErr == nil {
				baselineSignature, baselineKnown = challengerBusinessSignature(baselineRaw)
			}
		}
		subChallengerOut, normalizedRaw, localChanged, parseErr := parseChallenger(rawOutput)
		audit.addDirectAttempt(rawOutput, parseErr == nil, parseErr)
		if localChanged {
			audit.addLocalRepair(normalizedRaw, parseErr == nil, parseErr)
		}
		if parseErr != nil {
			audit.addIssue(ArtifactIssue{
				Stage: challengerContract.Stage, Schema: challengerContract.SchemaID,
				JSONPath: "$", Field: "artifact", Code: IssueUnresolved,
				Message: parseErr.Error(), Recoverable: isContractMismatch(parseErr),
			})
		}
		if parseErr != nil && isContractMismatch(parseErr) && contractRepairEnabled("tier2_challenger", parseErr) {
			parseErr = invoker.WrapClassifiedError(invoker.ErrorClassContractMismatch, parseErr, "challenger output contract mismatch")
			if stats := splitStatsFromContext(stageCtx.Ctx); stats != nil {
				stats.ContractRepairs++
			}
			retryPrompt := buildContractRetryPrompt(prompt, challengerContract, parseErr)
			retryPromptPath := stagePromptFilePath(subOutPath, "challenger", 1)
			if retryWriteErr := writeStagePromptFile(retryPromptPath, retryPrompt); retryWriteErr != nil {
				return nil, totalTokens, fmt.Errorf("write challenger retry prompt file: %w", retryWriteErr)
			}
			retryMetrics := &invoker.InvocationMetrics{}
			retryRaw, retryTokens, retryErr := callAITierPromptFileWithMetrics(
				stageCtx.Ctx,
				candidate.Driver,
				candidate.Model,
				retryPromptPath,
				ctx.CodesPath,
				subOutPath,
				attemptSeconds,
				tierTimeoutPolicy(tierCfg),
				retryMetrics,
				workCtx,
			)
			recordInternalCallMetrics(stageCtx.Ctx, retryMetrics)
			totalTokens += retryTokens
			if retryErr == nil {
				if retryOut, retryNormalized, retryChanged, retryParseErr := parseChallenger(retryRaw); retryParseErr == nil {
					drifted, driftUnchecked, driftRef := compareArtifactBusinessSignature(baselineSignature, baselineKnown, retryNormalized)
					audit.addLLMRepair(retryRaw, retryTokens, true, drifted, driftUnchecked, nil)
					audit.RepairDriftUnchecked = audit.RepairDriftUnchecked || driftUnchecked
					audit.RepairDriftUnitRef = driftRef
					if retryChanged {
						audit.addLocalRepair(retryNormalized, true, nil)
					}
					subChallengerOut = retryOut
					parseErr = nil
					if drifted {
						audit.addIssue(ArtifactIssue{
							Stage: challengerContract.Stage, Schema: challengerContract.SchemaID,
							JSONPath: "$", Field: "repair", Code: IssueUnresolved,
							Message: "BUSINESS_DRIFT_REJECTED on repair attempt", Recoverable: false,
						})
					} else if driftUnchecked {
						audit.addIssue(ArtifactIssue{
							Stage: challengerContract.Stage, Schema: challengerContract.SchemaID,
							JSONPath: "$", Field: "repair", Code: IssueUnresolved,
							Message: "BUSINESS_DRIFT_UNCHECKED on repair attempt", Recoverable: false,
						})
					}
				} else {
					audit.addLLMRepair(retryRaw, retryTokens, false, false, false, retryParseErr)
					if retryChanged {
						audit.addLocalRepair(retryNormalized, false, retryParseErr)
					}
					audit.addIssue(ArtifactIssue{
						Stage: challengerContract.Stage, Schema: challengerContract.SchemaID,
						JSONPath: "$", Field: "repair", Code: IssueUnresolved,
						Message: fmt.Sprintf("LLM repair attempt failed: %v", retryParseErr), Recoverable: true,
					})
					parseErr = retryParseErr
				}
			} else {
				audit.addLLMRepair("", retryTokens, false, false, false, retryErr)
				audit.addIssue(ArtifactIssue{
					Stage: challengerContract.Stage, Schema: challengerContract.SchemaID,
					JSONPath: "$", Field: "repair", Code: IssueUnresolved,
					Message: fmt.Sprintf("LLM repair attempt failed: %v", retryErr), Recoverable: false,
				})
				parseErr = retryErr
			}
		}
		if parseErr != nil {
			audit.setFinalStatus("failed")
			persistStageArtifactRepairAudit(
				stageCtx.Ctx, bundle,
				fmt.Sprintf("%s/challenger/batch-%d", bundle.Name, bIdx+1),
				ctx.ReportID, ctx.RepoID, ctx.TaskTypeID, audit,
			)
			log.Printf("[DebateEngine] Warning: Challenger Batch %d/%d validation failed (%v), degrading this batch", bIdx+1, len(batches), parseErr)
			for _, cand := range batch {
				mergedChallengerOut.DefenseCases = append(mergedChallengerOut.DefenseCases, ChallengerDefenseCase{
					CandidateID:       cand.CandidateID,
					DefenseVerdict:    "CHALLENGE_FAILED",
					MitigatingFactors: "[Challenger Degraded: 辩护人输出校验失败，请法官独立基于源码客观裁决]",
				})
			}
			continue
		}

		audit.setFinalStatus("success")
		persistStageArtifactRepairAudit(
			stageCtx.Ctx, bundle,
			fmt.Sprintf("%s/challenger/batch-%d", bundle.Name, bIdx+1),
			ctx.ReportID, ctx.RepoID, ctx.TaskTypeID, audit,
		)
		mergedChallengerOut.DefenseCases = append(mergedChallengerOut.DefenseCases, subChallengerOut.DefenseCases...)
	}

	if outPath != "" {
		if outBytes, jsonErr := json.MarshalIndent(mergedChallengerOut, "", "  "); jsonErr == nil {
			_ = os.WriteFile(outPath, outBytes, 0644)
		}
	}

	return &mergedChallengerOut, totalTokens, nil
}

// runJudgeStage 运行终审法官阶段
func (e *DebateEngine) runJudgeStage(ctx *engines.EngineContext, bundle chunker.SemanticBundle, hunterOut *HunterOutput, challOut *ChallengerOutput, outPath string) (*JudgeOutput, int64, error) {
	if len(hunterOut.Candidates) == 0 {
		return &JudgeOutput{}, 0, nil
	}

	tierCfg := models.AppConfig.GetTierConfig("tier3_judge")
	stageCtx := *ctx
	if tierCfg.TimeoutSeconds > 0 {
		stageCtxCancel, cancelStage := context.WithTimeout(ctx.Ctx, time.Duration(tierCfg.TimeoutSeconds)*time.Second)
		defer cancelStage()
		stageCtx.Ctx = stageCtxCancel
	}

	router := dispatcher.GetTierRouter()
	acq, err := router.AcquireTier(stageCtx.Ctx, "tier3_judge", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	caseMap := make(map[string]ChallengerDefenseCase)
	for _, dc := range challOut.DefenseCases {
		caseMap[dc.CandidateID] = dc
	}

	batches := splitCandidatesBalanced(hunterOut.Candidates, defaultMaxCandidatesPerBatch)

	var mergedJudgeOut JudgeOutput
	var totalTokens int64
	var failureReasons []string

	for bIdx, batch := range batches {
		subHunterOut := &HunterOutput{
			Candidates: sanitizeCandidatesForPrompt(batch),
		}
		var subCases []ChallengerDefenseCase
		for _, cand := range batch {
			if dc, ok := caseMap[cand.CandidateID]; ok {
				subCases = append(subCases, dc)
			} else {
				subCases = append(subCases, ChallengerDefenseCase{
					CandidateID:       cand.CandidateID,
					DefenseVerdict:    "CHALLENGE_FAILED",
					MitigatingFactors: "[Challenger Degraded: 辩护人无此候选辩护记录，请法官独立基于源码客观裁决]",
				})
			}
		}
		subChallOut := &ChallengerOutput{
			DefenseCases: subCases,
		}

		batchIDs := make([]string, 0, len(batch))
		for _, candidate := range batch {
			batchIDs = append(batchIDs, candidate.CandidateID)
		}

		judgeContract, contractErr := ContractForStageWithTaxonomy(ctx.EngineMode, "judge", ctx.AllowedCategories, ctx.Taxonomy)
		if contractErr != nil {
			return nil, totalTokens, contractErr
		}

		subOutPath := outPath
		if len(batches) > 1 && outPath != "" {
			subOutPath = fmt.Sprintf("%s.batch-%d.tmp", outPath, bIdx+1)
		}

		evidencePacks, evidenceErr := BuildJudgeEvidencePacks(ctx, bundle, batch, caseMap)
		if evidenceErr != nil {
			return nil, totalTokens, fmt.Errorf("build judge evidence pack: %w", evidenceErr)
		}

		prompt, promptErr := buildJudgePrompt(ctx, bundle, subHunterOut, subChallOut, evidencePacks)
		if promptErr != nil {
			return nil, totalTokens, fmt.Errorf("failed to build judge prompt: %w", promptErr)
		}

		promptPath := stagePromptFilePath(subOutPath, "judge", 0)
		if promptErr := writeStagePromptFile(promptPath, prompt); promptErr != nil {
			return nil, totalTokens, fmt.Errorf("write judge prompt file: %w", promptErr)
		}

		workCtx := &invoker.LLMWorkContext{
			ReportID: ctx.ReportID,
			RepoName: ctx.RepoName,
			TaskType: ctx.TaskTypeName,
			Stage:    "Tier 3: 终审法官 (Judge)",
			SubTask:  fmt.Sprintf("批次 %d/%d (裁决点 %d 个)", bIdx+1, len(batches), len(batch)),
			TierName: "tier3_judge",
		}
		attemptSeconds := tierCfg.AttemptTimeoutSeconds
		if attemptSeconds <= 0 {
			attemptSeconds = tierCfg.TimeoutSeconds
		}
		rawOutput, candidate, tokens, callErr := runTierInvocationWithRecovery(
			stageCtx.Ctx,
			"tier3_judge",
			attemptSeconds,
			workCtx,
			func(callCtx context.Context, candidate dispatcher.TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error) {
				workCtx.ResourceID = candidate.ResourceID
				return callAITierPromptFileWithMetrics(
					callCtx,
					candidate.Driver,
					candidate.Model,
					promptPath,
					stageCtx.CodesPath,
					subOutPath,
					timeoutSeconds,
					tierTimeoutPolicy(tierCfg),
					metrics,
					workCtx,
				)
			},
		)
		totalTokens += tokens
		if callErr != nil {
			audit := newArtifactRepairMetrics([]byte(rawOutput), judgeContract)
			audit.Driver = candidate.Driver
			audit.ResourceID = candidate.ResourceID
			audit.addDirectAttemptWithTokens(rawOutput, tokens, false, callErr)
			audit.RepairDriftUnchecked = true
			audit.addIssue(ArtifactIssue{
				Stage: judgeContract.Stage, Schema: judgeContract.SchemaID,
				JSONPath: "$", Field: "artifact", Code: IssueUnresolved,
				Message: callErr.Error(), Recoverable: false,
			})
			audit.setFinalStatus("failed")
			persistStageArtifactRepairAudit(
				stageCtx.Ctx, bundle,
				fmt.Sprintf("%s/judge/batch-%d", bundle.Name, bIdx+1),
				ctx.ReportID, ctx.RepoID, ctx.TaskTypeID, audit,
			)
			if len(batches) > 1 && subOutPath != "" && subOutPath != outPath {
				_ = os.Remove(subOutPath)
			}
		} else if len(batches) > 1 && subOutPath != "" && subOutPath != outPath {
			_ = os.Remove(subOutPath)
		}

		if callErr != nil {
			log.Printf("[DebateEngine] Warning: Judge Batch %d/%d failed (%v), fallback this batch", bIdx+1, len(batches), callErr)
			reason := fmt.Sprintf("Judge 调用失败: %v", callErr)
			failureReasons = append(failureReasons, reason)
			fbJudge := fallbackJudgeFromHunter(reason, &HunterOutput{Candidates: batch}, evidencePacks...)
			mergedJudgeOut.FinalVerdicts = append(mergedJudgeOut.FinalVerdicts, fbJudge.FinalVerdicts...)
			continue
		}

		parseJudge := func(raw string) (*JudgeOutput, []byte, bool, error) {
			var candidate JudgeOutput
			if parseErr := decodeContractJSON(raw, judgeContract, &candidate); parseErr != nil {
				return nil, nil, false, parseErr
			}
			beforeNormalize, beforeErr := json.Marshal(candidate)
			NormalizeJudgeEvidencePacks(evidencePacks, &candidate)
			normalized, normalizedErr := json.Marshal(candidate)
			localChanged := beforeErr == nil && normalizedErr == nil && !bytes.Equal(beforeNormalize, normalized)
			addJudgeCategoryMetrics(&candidate, ctx.Taxonomy)
			if validateErr := validateJudgeOutput(&candidate, batchIDs, ctx.AllowedCategories, ctx.Taxonomy); validateErr != nil {
				return &candidate, normalized, localChanged, validateErr
			}
			if _, verifyErr := VerifyJudgeEvidencePacks(evidencePacks, &candidate); verifyErr != nil {
				return &candidate, normalized, localChanged, fmt.Errorf("invalid final_verdict: %w", verifyErr)
			}
			return &candidate, normalized, localChanged, nil
		}

		audit := newArtifactRepairMetrics([]byte(rawOutput), judgeContract)
		audit.Driver = candidate.Driver
		audit.ResourceID = candidate.ResourceID
		subJudgeOut, normalizedRaw, localChanged, parseErr := parseJudge(rawOutput)
		audit.addDirectAttemptWithTokens(rawOutput, tokens, parseErr == nil, parseErr)
		if localChanged {
			audit.addLocalRepair(normalizedRaw, parseErr == nil, parseErr)
		}
		baselineBusiness, baselineEvidence := map[string]string(nil), map[string]string(nil)
		baselineBusinessKnown, baselineEvidenceKnown := false, false
		if baselineOut, baselineErr := decodeJudgeBaseline(rawOutput); baselineErr == nil && len(baselineOut.FinalVerdicts) > 0 {
			NormalizeJudgeEvidencePacks(evidencePacks, baselineOut)
			if baselineRaw, baselineMarshalErr := json.Marshal(baselineOut); baselineMarshalErr == nil {
				baselineBusiness, baselineBusinessKnown = judgeBusinessSignature(baselineRaw)
				baselineEvidence, baselineEvidenceKnown = judgeEvidenceSignature(baselineRaw)
			}
		}
		if parseErr != nil {
			audit.addIssue(ArtifactIssue{
				Stage: judgeContract.Stage, Schema: judgeContract.SchemaID,
				JSONPath: "$", Field: "artifact", Code: IssueUnresolved,
				Message: parseErr.Error(), Recoverable: isContractMismatch(parseErr),
			})
		}
		if parseErr != nil && isContractMismatch(parseErr) && contractRepairEnabled("tier3_judge", parseErr) {
			parseErr = invoker.WrapClassifiedError(invoker.ErrorClassContractMismatch, parseErr, "judge output contract mismatch")
			if stats := splitStatsFromContext(stageCtx.Ctx); stats != nil {
				stats.ContractRepairs++
			}
			retryPrompt := buildContractRetryPrompt(prompt, judgeContract, parseErr)
			retryPromptPath := stagePromptFilePath(subOutPath, "judge", 1)
			if retryWriteErr := writeStagePromptFile(retryPromptPath, retryPrompt); retryWriteErr != nil {
				return nil, totalTokens, fmt.Errorf("write judge retry prompt file: %w", retryWriteErr)
			}
			retryMetrics := &invoker.InvocationMetrics{}
			retryRaw, retryTokens, retryErr := callAITierPromptFileWithMetrics(
				stageCtx.Ctx,
				candidate.Driver,
				candidate.Model,
				retryPromptPath,
				ctx.CodesPath,
				subOutPath,
				attemptSeconds,
				tierTimeoutPolicy(tierCfg),
				retryMetrics,
				workCtx,
			)
			recordInternalCallMetrics(stageCtx.Ctx, retryMetrics)
			totalTokens += retryTokens
			if retryErr == nil {
				if retryOut, retryNormalized, retryChanged, retryParseErr := parseJudge(retryRaw); retryParseErr == nil {
					drifted, driftUnchecked, evidenceDrifted, driftRef := compareJudgeRepairArtifacts(
						baselineBusiness, baselineBusinessKnown,
						baselineEvidence, baselineEvidenceKnown,
						retryNormalized,
					)
					audit.addLLMRepair(retryRaw, retryTokens, true, drifted, driftUnchecked, nil)
					audit.RepairDriftUnchecked = audit.RepairDriftUnchecked || driftUnchecked
					audit.RepairDriftUnitRef = driftRef
					if retryChanged {
						audit.addLocalRepair(retryNormalized, true, nil)
					}
					subJudgeOut = retryOut
					parseErr = nil
					if driftUnchecked {
						audit.addIssue(ArtifactIssue{
							Stage: judgeContract.Stage, Schema: judgeContract.SchemaID,
							JSONPath: "$", Field: "repair", Code: IssueUnresolved,
							Message: "JUDGE_REPAIR_DRIFT_UNCHECKED on repair attempt", Recoverable: false,
						})
					} else if evidenceDrifted {
						audit.addIssue(ArtifactIssue{
							Stage: judgeContract.Stage, Schema: judgeContract.SchemaID,
							JSONPath: "$", Field: "evidence", Code: IssueUnresolved,
							Message: "JUDGE_EVIDENCE_DRIFT on repair attempt", Recoverable: false,
						})
					} else if drifted {
						audit.addIssue(ArtifactIssue{
							Stage: judgeContract.Stage, Schema: judgeContract.SchemaID,
							JSONPath: "$", Field: "repair", Code: IssueUnresolved,
							Message: "JUDGE_REPAIR_DRIFT on repair attempt", Recoverable: false,
						})
					}
				} else {
					audit.addLLMRepair(retryRaw, retryTokens, false, false, retryParseErr != nil && !isContractMismatch(retryParseErr), retryParseErr)
					if retryChanged {
						audit.addLocalRepair(retryNormalized, false, retryParseErr)
					}
					audit.addIssue(ArtifactIssue{
						Stage: judgeContract.Stage, Schema: judgeContract.SchemaID,
						JSONPath: "$", Field: "repair", Code: IssueUnresolved,
						Message: fmt.Sprintf("LLM repair attempt failed: %v", retryParseErr), Recoverable: true,
					})
					parseErr = retryParseErr
				}
			} else {
				audit.addLLMRepair(retryRaw, retryTokens, false, false, true, retryErr)
				audit.addIssue(ArtifactIssue{
					Stage: judgeContract.Stage, Schema: judgeContract.SchemaID,
					JSONPath: "$", Field: "repair", Code: IssueUnresolved,
					Message: fmt.Sprintf("LLM repair attempt failed: %v", retryErr), Recoverable: false,
				})
				parseErr = retryErr
			}
		}
		if parseErr != nil {
			audit.setFinalStatus("failed")
			persistStageArtifactRepairAudit(
				stageCtx.Ctx, bundle,
				fmt.Sprintf("%s/judge/batch-%d", bundle.Name, bIdx+1),
				ctx.ReportID, ctx.RepoID, ctx.TaskTypeID, audit,
			)
			log.Printf("[DebateEngine] Warning: Judge Batch %d/%d validation failed (%v), fallback this batch", bIdx+1, len(batches), parseErr)
			reason := fmt.Sprintf("Judge 输出校验失败: %v", parseErr)
			failureReasons = append(failureReasons, reason)
			fbJudge := fallbackJudgeFromHunter(reason, &HunterOutput{Candidates: batch}, evidencePacks...)
			mergedJudgeOut.FinalVerdicts = append(mergedJudgeOut.FinalVerdicts, fbJudge.FinalVerdicts...)
			continue
		}

		categoryTokens, _, categoryRepairErr := repairJudgeCategories(
			stageCtx.Ctx, ctx.WorkDir, subJudgeOut, batch, ctx.Taxonomy, judgeContract,
		)
		totalTokens += categoryTokens
		if categoryRepairErr != nil {
			log.Printf("[DebateEngine] Warning: Judge category repair batch %d/%d failed: %v", bIdx+1, len(batches), categoryRepairErr)
		}

		audit.setFinalStatus("success")
		persistStageArtifactRepairAudit(
			stageCtx.Ctx, bundle,
			fmt.Sprintf("%s/judge/batch-%d", bundle.Name, bIdx+1),
			ctx.ReportID, ctx.RepoID, ctx.TaskTypeID, audit,
		)
		mergedJudgeOut.FinalVerdicts = append(mergedJudgeOut.FinalVerdicts, subJudgeOut.FinalVerdicts...)
	}

	if len(mergedJudgeOut.FinalVerdicts) == 0 {
		return nil, totalTokens, fmt.Errorf("all judge batches failed: %s", strings.Join(failureReasons, "; "))
	}

	if outPath != "" {
		if outBytes, jsonErr := json.MarshalIndent(mergedJudgeOut, "", "  "); jsonErr == nil {
			_ = os.WriteFile(outPath, outBytes, 0644)
		}
	}

	return &mergedJudgeOut, totalTokens, nil
}

// fallbackJudgeFromHunter 当法官或辩论失败时的兜底构造
func fallbackJudgeFromHunter(reason string, hunterOut *HunterOutput, evidencePacks ...JudgeCaseEvidencePack) *JudgeOutput {
	var verdicts []JudgeFinalVerdict
	packByID := make(map[string]JudgeCaseEvidencePack, len(evidencePacks))
	for _, pack := range evidencePacks {
		packByID[pack.CandidateID] = pack
	}
	for _, c := range hunterOut.Candidates {
		titleCandidate := c.Title
		if titleCandidate == "" {
			titleCandidate = c.AttackHypothesis
		}
		conciseTitle := DeriveConciseTitle(titleCandidate, c.CWECategory)
		// reason can contain CLI/TUI diagnostics. Keep the complete text in
		// server logs and invocation artifacts, but never inline it into the
		// user-facing report rationale.
		rationale := fmt.Sprintf("[Fallback: Judge 未能完成可验证终审（原因：%s），保留猎手原始判定]", sanitizeFallbackReason(reason))
		if c.AttackHypothesis != "" {
			rationale = fmt.Sprintf("【成因与攻击假设】: %s\n\n%s", c.AttackHypothesis, rationale)
		}
		rationale += "\n\n【Fallback 守卫】: Judge 未能完成可验证终审；保留 Hunter 候选供人工复核，不作为 CONFIRMED 依据。"
		var evidence []JudgeEvidenceRef
		if pack, ok := packByID[c.CandidateID]; ok {
			evidence = append(evidence, pack.PrimaryEvidence...)
			evidence = append(evidence, pack.RelatedEvidence...)
		}
		verdicts = append(verdicts, JudgeFinalVerdict{
			CandidateID:         c.CandidateID,
			Verdict:             models.DebateVerdictConditional,
			SeverityPreliminary: "一般",
			Category:            c.GetCategory(),
			FilePath:            c.FilePath,
			LineNumber:          c.LineRange,
			TriggerLine:         c.TriggerLine,
			ScopeSymbol:         c.ScopeSymbol,
			Title:               conciseTitle,
			JudgementRationale:  rationale,
			CodeSnippet:         c.CodeSnippet,
			Suggestion:          "建议人工复核此可疑风险点",
			Evidence:            evidence,
			EvidenceDegraded:    true,
		})
	}
	return &JudgeOutput{FinalVerdicts: verdicts}
}

// sanitizeFallbackReason converts detailed invocation or validation failures
// into a stable reason suitable for end-user reports. Diagnostics remain in
// server logs and CLI output artifacts.
func sanitizeFallbackReason(reason string) string {
	switch {
	case strings.Contains(reason, "Judge 调用失败"):
		return "Judge 调用失败"
	case strings.Contains(reason, "Judge 输出校验失败"):
		return "Judge 输出校验失败"
	default:
		return "Judge 未完成"
	}
}

// DeriveConciseTitle 提取紧凑标题
func DeriveConciseTitle(rawTitle, fallbackCategory string) string {
	title := strings.TrimSpace(rawTitle)
	if title == "" {
		if fallbackCategory != "" {
			return fallbackCategory
		}
		return "未命名缺陷"
	}
	title = strings.ReplaceAll(title, "\r\n", "\n")
	if idx := strings.Index(title, "\n"); idx != -1 {
		title = strings.TrimSpace(title[:idx])
	}
	for _, sep := range []string{"。", "；", ";"} {
		if idx := strings.Index(title, sep); idx != -1 && idx > 5 {
			candidate := strings.TrimSpace(title[:idx+len(sep)])
			if len([]rune(candidate)) >= 10 {
				title = candidate
				break
			}
		}
	}
	runes := []rune(title)
	if len(runes) > 200 {
		return string(runes[:197]) + "..."
	}
	return title
}

// deriveDefectDetail 提取并构造面向开发者的客观缺陷成因与触发机理描述
func deriveDefectDetail(origCand HunterCandidate, jv JudgeFinalVerdict) string {
	claim := strings.TrimSpace(origCand.AttackHypothesis)
	if claim == "" {
		claim = strings.TrimSpace(origCand.TriggerCondition)
	}
	if claim == "" {
		claim = strings.TrimSpace(origCand.SuspectedTrigger)
	}
	if claim != "" {
		return claim
	}
	if strings.TrimSpace(jv.Title) != "" {
		return jv.Title
	}
	return "未提供缺陷机理描述"
}

// sanitizeCandidatesForPrompt 清洗与缩减 candidate，避免大 Prompt 溢出
func sanitizeCandidatesForPrompt(candidates []HunterCandidate) []HunterCandidate {
	sanitized := make([]HunterCandidate, len(candidates))
	copy(sanitized, candidates)
	for i := range sanitized {
		c := &sanitized[i]
		if len(c.CodeSnippet) > 1500 {
			c.CodeSnippet = c.CodeSnippet[:1500] + "\n... (truncated)"
		}
		if len(c.AttackHypothesis) > 500 {
			c.AttackHypothesis = c.AttackHypothesis[:500] + "..."
		}
		if len(c.TriggerCondition) > 500 {
			c.TriggerCondition = c.TriggerCondition[:500] + "..."
		}
	}
	return sanitized
}

// buildHunterPrompt 委托 PromptAssembler 动态装配猎手 Prompt
func buildHunterPrompt(ctx *engines.EngineContext, bundle chunker.SemanticBundle) string {
	assembler := &PromptAssembler{}
	return assembler.BuildHunterPrompt(ctx, bundle)
}

// buildChallengerPrompt 委托 PromptAssembler 动态装配辩护人 Prompt
func buildChallengerPrompt(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	hunterOut *HunterOutput,
	evidencePacks []JudgeCaseEvidencePack,
) (string, error) {
	assembler := &PromptAssembler{}
	return assembler.BuildChallengerPrompt(ctx, bundle, hunterOut, evidencePacks)
}

// buildJudgePrompt 委托 PromptAssembler 动态装配法官 Prompt
func buildJudgePrompt(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	hunterOut *HunterOutput,
	challOut *ChallengerOutput,
	evidencePacks []JudgeCaseEvidencePack,
) (string, error) {
	assembler := &PromptAssembler{}
	return assembler.BuildJudgePrompt(ctx, bundle, hunterOut, challOut, evidencePacks)
}

// aiTimeoutPolicy allows a stage to distinguish wall-clock protection from
// inactivity detection. Attempt/FirstByte only apply to native SSE calls.
type aiTimeoutPolicy struct {
	AttemptTimeoutSeconds   int
	FirstByteTimeoutSeconds int
	IdleTimeoutSeconds      int
	MaxOutputBytes          int
}

func tierTimeoutPolicy(cfg models.TierConfig) aiTimeoutPolicy {
	return aiTimeoutPolicy{
		AttemptTimeoutSeconds:   cfg.AttemptTimeoutSeconds,
		FirstByteTimeoutSeconds: cfg.FirstByteTimeoutSeconds,
		IdleTimeoutSeconds:      cfg.IdleTimeoutSeconds,
		MaxOutputBytes:          cfg.MaxOutputBytes,
	}
}

// callAITier 底层调用大模型驱动
func callAITier(ctx context.Context, backend string, modelName string, prompt string, workDir string, outPath string, timeoutSeconds int, timeoutPolicy aiTimeoutPolicy, workCtx ...*invoker.LLMWorkContext) (string, int64, error) {
	return callAITierWithMetrics(ctx, backend, modelName, prompt, workDir, outPath, timeoutSeconds, timeoutPolicy, nil, workCtx...)
}

func callAITierWithMetrics(ctx context.Context, backend string, modelName string, prompt string, workDir string, outPath string, timeoutSeconds int, timeoutPolicy aiTimeoutPolicy, metrics *invoker.InvocationMetrics, workCtx ...*invoker.LLMWorkContext) (string, int64, error) {
	return invokeAITier(ctx, backend, modelName, prompt, "", workDir, outPath, timeoutSeconds, timeoutPolicy, metrics, workCtx...)
}

func callAITierPromptFile(ctx context.Context, backend string, modelName string, promptPath string, workDir string, outPath string, timeoutSeconds int, timeoutPolicy aiTimeoutPolicy, workCtx ...*invoker.LLMWorkContext) (string, int64, error) {
	return callAITierPromptFileWithMetrics(ctx, backend, modelName, promptPath, workDir, outPath, timeoutSeconds, timeoutPolicy, nil, workCtx...)
}

func callAITierPromptFileWithMetrics(ctx context.Context, backend string, modelName string, promptPath string, workDir string, outPath string, timeoutSeconds int, timeoutPolicy aiTimeoutPolicy, metrics *invoker.InvocationMetrics, workCtx ...*invoker.LLMWorkContext) (string, int64, error) {
	return invokeAITier(ctx, backend, modelName, "", promptPath, workDir, outPath, timeoutSeconds, timeoutPolicy, metrics, workCtx...)
}

func invokeAITier(
	ctx context.Context,
	backend string,
	modelName string,
	prompt string,
	promptPath string,
	workDir string,
	outPath string,
	timeoutSeconds int,
	timeoutPolicy aiTimeoutPolicy,
	metrics *invoker.InvocationMetrics,
	workCtx ...*invoker.LLMWorkContext,
) (string, int64, error) {
	return invokeAITierWithRequestOptions(ctx, backend, modelName, prompt, promptPath, workDir, outPath, timeoutSeconds, timeoutPolicy, metrics, nil, workCtx...)
}

func invokeAITierWithRequestOptions(
	ctx context.Context,
	backend string,
	modelName string,
	prompt string,
	promptPath string,
	workDir string,
	outPath string,
	timeoutSeconds int,
	timeoutPolicy aiTimeoutPolicy,
	metrics *invoker.InvocationMetrics,
	requestOptions func(*invoker.AIRequest),
	workCtx ...*invoker.LLMWorkContext,
) (string, int64, error) {
	if backend == "" {
		backend = models.AppConfig.AI.Backend
	}

	rawInv, ok := invoker.GetRawInvoker(backend)
	if !ok || rawInv == nil {
		return "", 0, fmt.Errorf("unsupported AI backend: %s", backend)
	}

	wrappedInv := dispatcher.WrapInvoker(rawInv)

	outputPath := outPath
	shouldCleanup := false
	if outputPath == "" {
		tmpFile, err := os.CreateTemp("", "debate-stage-output-*.json")
		if err != nil {
			return "", 0, err
		}
		outputPath = tmpFile.Name()
		tmpFile.Close()
		shouldCleanup = true
	}
	if shouldCleanup {
		defer os.Remove(outputPath)
	}

	timeoutMin := 30
	if timeoutSeconds > 0 {
		timeoutMin = (timeoutSeconds + 59) / 60
	} else if models.AppConfig.AI.Debate.StageTimeoutSeconds > 0 {
		timeoutMin = (models.AppConfig.AI.Debate.StageTimeoutSeconds + 59) / 60
	}

	var selectedWorkCtx *invoker.LLMWorkContext
	if len(workCtx) > 0 && workCtx[0] != nil {
		selectedWorkCtx = workCtx[0]
	}

	requestPrompt := prompt
	requestPromptPath := promptPath
	if promptPath != "" && strings.EqualFold(backend, "native") {
		promptBytes, readErr := os.ReadFile(promptPath)
		if readErr != nil {
			return "", 0, fmt.Errorf("read prompt file for native backend: %w", readErr)
		}
		requestPrompt = string(promptBytes)
		requestPromptPath = ""
	}

	req := invoker.AIRequest{
		ParentContext:           ctx,
		WorkDir:                 workDir,
		PromptMsg:               requestPrompt,
		PromptFile:              requestPromptPath,
		OutputPath:              outputPath,
		TimeoutMin:              timeoutMin,
		AttemptTimeoutSeconds:   timeoutPolicy.AttemptTimeoutSeconds,
		FirstByteTimeoutSeconds: timeoutPolicy.FirstByteTimeoutSeconds,
		IdleTimeoutSeconds:      timeoutPolicy.IdleTimeoutSeconds,
		MaxOutputBytes:          timeoutPolicy.MaxOutputBytes,
		ModelName:               modelName,
		ResponseFormat:          "json",
		Temperature:             models.AppConfig.DeterministicTemperature(),
		WorkContext:             selectedWorkCtx,
		Metrics:                 metrics,
	}
	if requestOptions != nil {
		requestOptions(&req)
	}

	if err := wrappedInv.Invoke(req); err != nil {
		return "", 0, err
	}

	outBytes, err := os.ReadFile(outputPath)
	if err != nil {
		cliOutputPath := outputPath + ".output.txt"
		if metaBytes, metaErr := os.ReadFile(cliOutputPath); metaErr == nil && len(strings.TrimSpace(string(metaBytes))) > 0 {
			metaStr := strings.TrimSpace(string(metaBytes))
			if strings.Contains(metaStr, "blocked by Gemini's filters") || strings.Contains(metaStr, "content policy") {
				return "", 0, fmt.Errorf("AI content safety filter blocked request: %s", metaStr)
			}
			return "", 0, fmt.Errorf("failed to read AI output (%w), CLI output: %s", err, metaStr)
		}
		return "", 0, fmt.Errorf("failed to read AI output: %w", err)
	}

	outStr := string(outBytes)
	estimatedPromptBytes := len(prompt)
	if promptPath != "" {
		if stat, statErr := os.Stat(promptPath); statErr == nil {
			estimatedPromptBytes = int(stat.Size())
		}
	}
	estimatedTokens := int64((estimatedPromptBytes + len(outStr)) / 4)
	return outStr, estimatedTokens, nil
}

func stagePromptFilePath(outputPath string, stage string, retry int) string {
	if strings.TrimSpace(outputPath) == "" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("debate-%s-prompt-%d.md", stage, time.Now().UnixNano()))
	}
	if retry == 0 {
		return fmt.Sprintf("%s.%s-prompt.md", outputPath, stage)
	}
	return fmt.Sprintf("%s.%s-prompt.retry-%d.md", outputPath, stage, retry)
}

func writeStagePromptFile(path string, prompt string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(prompt), 0644)
}

// parseJSONFromAIOutput 清洗并解析大模型输出的 JSON
func parseJSONFromAIOutput(rawOutput string, v interface{}, workDir string) error {
	cleaned := cleanJSONOutput([]byte(rawOutput))
	if err := json.Unmarshal(cleaned, v); err == nil {
		sanitizeNullStrings(v)
		return nil
	}

	return fmt.Errorf("failed to parse AI json output")
}

func cleanJSONOutput(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	if !strings.HasPrefix(s, "{") {
		if start := strings.Index(s, "{"); start != -1 {
			if end := strings.LastIndex(s, "}"); end > start {
				s = s[start : end+1]
			}
		}
	}
	return []byte(s)
}
