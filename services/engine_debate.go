package services

import (
	"code-shield/models"
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

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// DebateEngine 实现多智能体三方对抗辩论流水线引擎
type DebateEngine struct {
	Mode string // "debate_full", "debate_selective", "chunked_fast"
}

func init() {
	// 注册下一代 AI 扫描引擎模式
	RegisterEngine("debate_full", &DebateEngine{Mode: "debate_full"})
	RegisterEngine("debate_selective", &DebateEngine{Mode: "debate_selective"})
	RegisterEngine("chunked_fast", &DebateEngine{Mode: "chunked_fast"})
}

// Run 启动辩论引擎任务流
func (e *DebateEngine) Run(ctx *taskContext) error {
	// 推进任务状态为静态分析中
	updateTaskStatus(ctx.report.ID, models.StatusAnalyzing)

	cfg := ChunkConfig{MaxFiles: DefaultChunkMaxFiles, Depth: DefaultChunkDepth, Concurrency: DefaultChunkConcurrency}
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = DefaultChunkMaxFiles
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultChunkConcurrency
	}

	targetScope := "all"
	if ctx.runParams.TargetScope != nil {
		targetScope = *ctx.runParams.TargetScope
	}

	// 1. 查询当前代码仓与任务类型的历史负样本例外规则
	var negativeRules []string
	if models.DB != nil {
		var dbRules []models.RepoFeedbackRule
		models.DB.Where("repo_id = ? AND (task_type_id = ? OR scope_type = 'GLOBAL')", ctx.repo.ID, ctx.taskType.ID).Find(&dbRules)
		for _, r := range dbRules {
			negativeRules = append(negativeRules, fmt.Sprintf("[%s] %s: %s", r.ScopeType, r.Pattern, r.Reason))
		}
	}

	// 2. 扫描并构建语义感知分片包 (含同名投影与宏注入)
	bundles, err := BuildSemanticBundles(ctx.codesPath, cfg, targetScope, negativeRules)
	if err != nil {
		return err
	}

	log.Printf("[DebateEngine] EngineMode: %s | Found %d semantic bundles for repo %s\n",
		e.Mode, len(bundles), ctx.repo.Name)

	// 3. 准备分片工作目录
	nameParts := strings.Split(ctx.repo.Name, "/")
	repoShort := nameParts[len(nameParts)-1]
	chunkDir := filepath.Join(filepath.Dir(ctx.reportPath), fmt.Sprintf("debate-chunks-%d-%s", ctx.report.ID, repoShort))
	os.MkdirAll(chunkDir, 0755)

	var allFindings []models.AnalysisFinding
	var mu sync.Mutex
	var wg sync.WaitGroup
	var chunkErrors []string
	var errMu sync.Mutex
	semaphore := make(chan struct{}, cfg.Concurrency)
	totalChunks := len(bundles)
	chunkIndex := 0
	overallStartTime := time.Now()

	models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Update("total_chunks", totalChunks)

	chunkDetailsList := make([]ChunkDetails, totalChunks)

bundleLoop:
	for _, b := range bundles {
		if ctx.ctx.Err() != nil {
			break bundleLoop
		}

		chunkIndex++
		currentIndex := chunkIndex
		bundle := b

		wg.Add(1)
		select {
		case <-ctx.ctx.Done():
			wg.Done()
			break bundleLoop
		case semaphore <- struct{}{}:
		}

		if ctx.ctx.Err() != nil {
			select {
			case <-semaphore:
			default:
			}
			wg.Done()
			break bundleLoop
		}

		go func(bnd SemanticBundle, idx int) {
			defer wg.Done()
			defer func() { <-semaphore }()
			defer func() {
				models.DB.Model(&models.TaskReport{}).
					Where("id = ?", ctx.report.ID).
					UpdateColumn("processed_chunks", gorm.Expr("processed_chunks + ?", 1))
			}()

			log.Printf("[DebateEngine] Processing bundle %d/%d [%s] (Files: %d)\n", idx, totalChunks, bnd.Name, len(bnd.AllFiles))

			bundleStartTime := time.Now()
			findings, debateLogs, err := e.ProcessBundle(ctx, bnd, idx, chunkDir)
			bundleEndTime := time.Now()

			details := ChunkDetails{
				ChunkName:       bnd.Name,
				Files:           bnd.AllFiles,
				Attempts:        1,
				StartTime:       bundleStartTime,
				EndTime:         bundleEndTime,
				DurationSeconds: bundleEndTime.Sub(bundleStartTime).Seconds(),
			}

			if err != nil {
				log.Printf("[DebateEngine] Bundle [%s] debate failed: %v\n", bnd.Name, err)
				errMu.Lock()
				chunkErrors = append(chunkErrors, fmt.Sprintf("Bundle [%s] failed: %v", bnd.Name, err))
				errMu.Unlock()

				details.Status = "failed"
				details.ErrorMessage = err.Error()
				chunkDetailsList[idx-1] = details
				return
			}

			details.Status = "success"
			chunkDetailsList[idx-1] = details

			// 保存辩论日志到数据库（清洗 jsonb 字段，并逐条容错保存，避免单条 jsonb 格式异常拖累 bundle）
			if len(debateLogs) > 0 && models.DB != nil {
				for _, dl := range debateLogs {
					dl.TaskReportID = ctx.report.ID
					dl.HunterOutput = SanitizeJSONForPostgresJSONB([]byte(dl.HunterOutput))
					dl.ChallengerOutput = SanitizeJSONForPostgresJSONB([]byte(dl.ChallengerOutput))
					dl.JudgeOutput = SanitizeJSONForPostgresJSONB([]byte(dl.JudgeOutput))
					dl.TokenUsage = SanitizeJSONForPostgresJSONB([]byte(dl.TokenUsage))

					if err := models.DB.Create(&dl).Error; err != nil {
						log.Printf("[DebateEngine] Warning: Failed to insert TaskDebateLog for candidate %s: %v (skipping to protect bundle)", dl.CandidateID, err)
					}
				}
			}

			if len(findings) > 0 {
				mu.Lock()
				allFindings = append(allFindings, findings...)
				mu.Unlock()
			}
		}(bundle, currentIndex)
	}

	wg.Wait()

	overallEndTime := time.Now()
	successfulChunks := 0
	failedChunks := 0
	for _, details := range chunkDetailsList {
		if details.Status == "success" {
			successfulChunks++
		} else {
			failedChunks++
		}
	}

	// 记录成功分片数
	if models.DB != nil {
		models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Update("success_chunks", successfulChunks)
	}

	// 填充 Summary 轨迹诊断数据
	ctx.Summary.Analysis.StartTime = overallStartTime
	ctx.Summary.Analysis.EndTime = overallEndTime
	ctx.Summary.Analysis.DurationSeconds = overallEndTime.Sub(overallStartTime).Seconds()
	ctx.Summary.Analysis.TotalChunks = totalChunks
	ctx.Summary.Analysis.SuccessChunks = successfulChunks
	ctx.Summary.Analysis.FailedChunks = failedChunks
	ctx.Summary.Analysis.TotalFindings = len(allFindings)
	ctx.Summary.Analysis.Chunks = chunkDetailsList

	if failedChunks > 0 {
		ctx.Summary.Analysis.Status = "failed"
		ctx.hasFailedChunks = true
	} else {
		ctx.Summary.Analysis.Status = "success"
		ctx.hasFailedChunks = false
	}

	// 保存任务汇总报告文件
	ctx.writeSummaryReport()

	if len(chunkErrors) > 0 {
		if len(allFindings) == 0 {
			return fmt.Errorf("all debate bundles failed: %s", strings.Join(chunkErrors, "; "))
		}
		log.Printf("[DebateEngine] Warning: %d bundles failed, proceeding with %d confirmed findings\n",
			len(chunkErrors), len(allFindings))
	}

	// 推进任务状态为报告总结/综合中
	updateTaskStatus(ctx.report.ID, models.StatusSynthesis)

	// 4. 确定性严重度校准
	allFindings = CalibrateFindings(allFindings)

	// 5. 跨扫描增量比对与状态机标记 (Phase 3 接入点)
	var scannedFiles []string
	for _, b := range bundles {
		scannedFiles = append(scannedFiles, b.AllFiles...)
	}
	enrichedFindings, diffErr := DiffAndEnrichFindings(ctx.repo.ID, ctx.report.ID, ctx.taskType.ID, scannedFiles, allFindings, ctx.codesPath)
	if diffErr == nil && len(enrichedFindings) > 0 {
		allFindings = enrichedFindings
	}

	log.Printf("[DebateEngine] Synthesis starting with %d vetted & calibrated findings\n", len(allFindings))
	ctx.findings = allFindings
	return ctx.executeSynthesis(allFindings)
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
func (e *DebateEngine) ProcessBundle(ctx *taskContext, bundle SemanticBundle, chunkIdx int, chunkDir string) ([]models.AnalysisFinding, []models.TaskDebateLog, error) {
	startTime := time.Now()
	safeName := sanitizeDebateChunkName(bundle.Name)

	var hunterOutPath, challOutPath, judgeOutPath string
	if chunkDir != "" {
		hunterOutPath = filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s-1-hunter.json", chunkIdx, safeName))
		challOutPath = filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s-2-challenger.json", chunkIdx, safeName))
		judgeOutPath = filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s-3-judge.json", chunkIdx, safeName))
	}

	// ── 步骤 1: 调度 Tier 1 快模型执行 Hunter 初筛 ──
	hunterOut, hunterTokens, err := e.runHunterStage(ctx, bundle, hunterOutPath)
	if err != nil {
		return nil, nil, fmt.Errorf("hunter stage failed: %w", err)
	}

	// 记录 Tier 1 Token
	if hunterTokens > 0 && models.DB != nil {
		models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).
			UpdateColumn("tier1_tokens", gorm.Expr("tier1_tokens + ?", hunterTokens))
	}

	// 零候选快速放行 (Early Exit)
	if len(hunterOut.Candidates) == 0 {
		log.Printf("[DebateEngine] Bundle [%s]: Hunter found 0 candidates. Fast Pass.", bundle.Name)
		return []models.AnalysisFinding{}, nil, nil
	}

	log.Printf("[DebateEngine] Bundle [%s]: Hunter identified %d candidates.", bundle.Name, len(hunterOut.Candidates))

	// 若处于快速规则流模式 (chunked_fast)，直接转为 Finding，跳过辩论
	if e.Mode == "chunked_fast" {
		var fastFindings []models.AnalysisFinding
		for _, c := range hunterOut.Candidates {
			titleCandidate := c.Title
			if titleCandidate == "" {
				titleCandidate = c.AttackHypothesis
			}
			conciseTitle := deriveConciseTitle(titleCandidate, c.CWECategory)
			detail := c.SuspectedTrigger
			if detail == "" {
				detail = c.AttackHypothesis
			} else if !strings.Contains(detail, c.AttackHypothesis) {
				detail = fmt.Sprintf("【成因与攻击假设】: %s\n\n【疑似触发条件】: %s", c.AttackHypothesis, detail)
			}
			fastFindings = append(fastFindings, models.AnalysisFinding{
				FilePath:    c.FilePath,
				LineNumber:  c.LineRange,
				TriggerLine: c.TriggerLine,
				ScopeSymbol: c.ScopeSymbol,
				CodeSnippet: c.CodeSnippet,
				Category:    c.CWECategory,
				Title:       conciseTitle,
				Detail:      detail,
				HunterClaim: c.AttackHypothesis,
				CreatedAt:   time.Now(),
			})
		}
		return fastFindings, nil, nil
	}

	// 单分片候选数安全保护上限 (默认放宽至 100，避免后排文件候选被粗暴丢弃导致覆盖漏扫)
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
		challengerOut = &ChallengerOutput{
			Summary: "[Challenger Degraded: 辩护人服务暂时超时，请法官独立基于源码客观裁决]",
		}
	}

	// ── 步骤 3: 调度 Tier 2 强推理模型执行 Judge 终审仲裁 ──
	judgeOut, judgeTokens, err := e.runJudgeStage(ctx, bundle, hunterOut, challengerOut, judgeOutPath)
	if err != nil {
		log.Printf("[DebateEngine] Warning: Judge failed (%v), fallback to Hunter claims", err)
		judgeOut = fallbackJudgeFromHunter(hunterOut)
	}

	// 记录 Tier 2 Token
	totalTier2Tokens := challTokens + judgeTokens
	if totalTier2Tokens > 0 && models.DB != nil {
		models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).
			UpdateColumn("tier2_tokens", gorm.Expr("tier2_tokens + ?", totalTier2Tokens))
	}

	// ── 步骤 4: 转换裁决结论为系统标准 Finding 与 DebateLog ──
	var confirmedFindings []models.AnalysisFinding
	var debateLogs []models.TaskDebateLog

	totalDurationMs := int(time.Since(startTime).Milliseconds())

	// 建立 Case 查找映射
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

		// 组装辩论轨迹记录
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

		// 仅确认存在或条件触发的缺陷进入最终 Findings 列表
		if jv.Verdict == models.DebateVerdictConfirmed || jv.Verdict == models.DebateVerdictConditional {
			challArgText := challCase.DefenseVerdict
			if challCase.MitigatingFactors != "" {
				challArgText += " (" + challCase.MitigatingFactors + ")"
			}

			cleanCategory := SanitizeCategory(jv.Category, ctx.taskType.GetAllowedCategories())
			normScope := NormalizeScopeSymbol(jv.ScopeSymbol)
			cleanTrigger := CleanSourceToken(jv.TriggerLine)
			calibratedLine := jv.LineNumber

			// 如果有物理源码目录，通过 SourceEnricher 物理校准真实行号与代码 Token
			if ctx.codesPath != "" {
				if anchor, err := EnrichSourceAnchor(ctx.codesPath, jv.FilePath, jv.LineNumber, jv.TriggerLine); err == nil {
					normScope = anchor.NormalizedScope
					cleanTrigger = anchor.PhysicalToken
					calibratedLine = fmt.Sprintf("%d-%d", anchor.StartLine, anchor.EndLine)
				}
			}

			finding := models.AnalysisFinding{
				FilePath:      jv.FilePath,
				LineNumber:    calibratedLine,
				TriggerLine:   cleanTrigger,
				ScopeSymbol:   normScope,
				CodeSnippet:   jv.CodeSnippet,
				Category:      cleanCategory,
				Title:         deriveConciseTitle(jv.Title, cleanCategory),
				Detail:        fmt.Sprintf("%s\n\n【仲裁法官裁决词】: %s", jv.Title, jv.JudgementRationale),
				Suggestion:    jv.Suggestion,
				HunterClaim:   origCand.AttackHypothesis,
				ChallengerArg: challArgText,
				JudgeVerdict:  jv.JudgementRationale,
				Severity:      jv.SeverityPreliminary,
				CreatedAt:     time.Now(),
			}
			confirmedFindings = append(confirmedFindings, finding)
			log.Printf("[DebateEngine] Candidate %s [%s]: %s\n", jv.CandidateID, jv.Verdict, jv.Title)
		} else {
			log.Printf("[DebateEngine] Candidate %s [%s]: %s (Reason: %s)\n",
				jv.CandidateID, jv.Verdict, jv.Title, summarizeRationale(jv.JudgementRationale))
		}
	}

	// 记录全量详细判词与事实推演至 chunkDir 下的 debate-verdicts.log，供审计与深度回溯
	if chunkDir != "" && len(judgeOut.FinalVerdicts) > 0 {
		logFilePath := filepath.Join(chunkDir, "debate-verdicts.log")
		var logSb strings.Builder
		for _, jv := range judgeOut.FinalVerdicts {
			logSb.WriteString("================================================================================\n")
			logSb.WriteString(fmt.Sprintf("[%s] Candidate: %s | Verdict: %s | Title: %s\n", time.Now().Format("2006-01-02 15:04:05"), jv.CandidateID, jv.Verdict, jv.Title))
			logSb.WriteString(fmt.Sprintf("File: %s:%s | Trigger: %s | Scope: %s\n", jv.FilePath, jv.LineNumber, jv.TriggerLine, jv.ScopeSymbol))
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

	return confirmedFindings, debateLogs, nil
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

// runHunterStage 运行猎手初筛阶段 (Tier 1 快模型)
func (e *DebateEngine) runHunterStage(ctx *taskContext, bundle SemanticBundle, outPath string) (*HunterOutput, int64, error) {
	router := GetTierRouter()
	acq, err := router.AcquireTier(ctx.ctx, "tier1_fast", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	prompt := buildHunterPrompt(ctx, bundle)
	tierCfg := models.AppConfig.GetTierConfig("tier1_fast")
	workCtx := &LLMWorkContext{
		ReportID: ctx.report.ID,
		RepoName: ctx.repo.Name,
		TaskType: ctx.taskType.DisplayName,
		Stage:    "Tier 1: 猎手初筛",
		SubTask:  fmt.Sprintf("分片束 %s (%d 个候选文件)", bundle.Name, len(bundle.AllFiles)),
	}
	rawOutput, tokens, err := callAITier(ctx.ctx, acq.Backend, acq.ModelName, prompt, ctx.codesPath, outPath, tierCfg.TimeoutSeconds, workCtx)
	if err != nil {
		return nil, tokens, err
	}

	var hunterOut HunterOutput
	if parseErr := parseJSONFromAIOutput(rawOutput, &hunterOut, ctx.codesPath); parseErr != nil {
		// 容错: 尝试从非结构化文本中提取基本候选
		hunterOut.Summary = rawOutput
	}

	return &hunterOut, tokens, nil
}

// defaultMaxCandidatesPerBatch 辩论与仲裁阶段单批次处理候选数上限（防 128K 溢出并显著降低单次推理延迟）
const defaultMaxCandidatesPerBatch = 5

// runChallengerStage 运行辩护人对抗阶段 (Tier 2 强推理模型，支持分批切拆与聚合以防御 128K 窗口溢出与超时)
func (e *DebateEngine) runChallengerStage(ctx *taskContext, bundle SemanticBundle, hunterOut *HunterOutput, outPath string) (*ChallengerOutput, int64, error) {
	if len(hunterOut.Candidates) == 0 {
		return &ChallengerOutput{}, 0, nil
	}

	router := GetTierRouter()
	acq, err := router.AcquireTier(ctx.ctx, "tier2_reasoning", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	tierCfg := models.AppConfig.GetTierConfig("tier2_reasoning")

	// 1. 拆分批次 (Batching)
	var batches [][]HunterCandidate
	for i := 0; i < len(hunterOut.Candidates); i += defaultMaxCandidatesPerBatch {
		end := i + defaultMaxCandidatesPerBatch
		if end > len(hunterOut.Candidates) {
			end = len(hunterOut.Candidates)
		}
		batches = append(batches, hunterOut.Candidates[i:end])
	}

	var mergedChallengerOut ChallengerOutput
	var totalTokens int64
	var batchSummaries []string

	// 2. 逐批调用辩护人
	for bIdx, batch := range batches {
		subHunterOut := &HunterOutput{
			Candidates: sanitizeCandidatesForPrompt(batch),
			Summary:    hunterOut.Summary,
		}
		prompt := buildChallengerPrompt(ctx, bundle, subHunterOut)

		// 为避免多批次输出覆盖，生成子批次临时输出路径
		subOutPath := outPath
		if len(batches) > 1 && outPath != "" {
			subOutPath = fmt.Sprintf("%s.batch-%d.tmp", outPath, bIdx+1)
		}

		if len(batches) > 1 {
			log.Printf("[DebateEngine] Bundle [%s]: Invoking Challenger Batch %d/%d (Candidates: %d)...",
				bundle.Name, bIdx+1, len(batches), len(batch))
		}

		workCtx := &LLMWorkContext{
			ReportID: ctx.report.ID,
			RepoName: ctx.repo.Name,
			TaskType: ctx.taskType.DisplayName,
			Stage:    "Tier 2: 辩护对抗 (Challenger)",
			SubTask:  fmt.Sprintf("批次 %d/%d (候选点 %d 个)", bIdx+1, len(batches), len(batch)),
		}
		rawOutput, tokens, callErr := callAITier(ctx.ctx, acq.Backend, acq.ModelName, prompt, ctx.codesPath, subOutPath, tierCfg.TimeoutSeconds, workCtx)
		if len(batches) > 1 && subOutPath != "" && subOutPath != outPath {
			_ = os.Remove(subOutPath)
		}

		totalTokens += tokens
		if callErr != nil {
			log.Printf("[DebateEngine] Warning: Challenger Batch %d/%d failed (%v), degrading this batch", bIdx+1, len(batches), callErr)
			// 该批次降级，为该批次内的每个候选缺陷生成兜底辩护记录
			for _, cand := range batch {
				mergedChallengerOut.DefenseCases = append(mergedChallengerOut.DefenseCases, ChallengerDefenseCase{
					CandidateID:       cand.CandidateID,
					DefenseVerdict:    "CHALLENGE_FAILED",
					MitigatingFactors: "[Challenger Degraded: 辩护人该批次调用超时，请法官独立基于源码客观裁决]",
				})
			}
			batchSummaries = append(batchSummaries, fmt.Sprintf("[Batch %d Degraded: %v]", bIdx+1, callErr))
			continue
		}

		var challSubOut ChallengerOutput
		if parseErr := parseJSONFromAIOutput(rawOutput, &challSubOut, ctx.codesPath); parseErr != nil {
			challSubOut.Summary = rawOutput
		}

		mergedChallengerOut.DefenseCases = append(mergedChallengerOut.DefenseCases, challSubOut.DefenseCases...)
		if challSubOut.Summary != "" {
			batchSummaries = append(batchSummaries, challSubOut.Summary)
		}
	}

	mergedChallengerOut.Summary = strings.Join(batchSummaries, " | ")

	// 若所有批次全部失败且没有有效结果
	if len(mergedChallengerOut.DefenseCases) == 0 {
		return nil, totalTokens, fmt.Errorf("all challenger batches failed")
	}

	// 最终聚合写入 outPath
	if outPath != "" {
		if outBytes, jsonErr := json.MarshalIndent(mergedChallengerOut, "", "  "); jsonErr == nil {
			_ = os.WriteFile(outPath, outBytes, 0644)
		}
	}

	return &mergedChallengerOut, totalTokens, nil
}

// runJudgeStage 运行终审法官阶段 (Tier 2 强推理模型，支持分批切拆与聚合以防御 128K 窗口溢出与超时)
func (e *DebateEngine) runJudgeStage(ctx *taskContext, bundle SemanticBundle, hunterOut *HunterOutput, challengerOut *ChallengerOutput, outPath string) (*JudgeOutput, int64, error) {
	if len(hunterOut.Candidates) == 0 {
		return &JudgeOutput{}, 0, nil
	}

	router := GetTierRouter()
	acq, err := router.AcquireTier(ctx.ctx, "tier2_reasoning", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	tierCfg := models.AppConfig.GetTierConfig("tier2_reasoning")

	// 建立 Case 查找映射以方便子批次提取
	caseMap := make(map[string]ChallengerDefenseCase)
	if challengerOut != nil {
		for _, dc := range challengerOut.DefenseCases {
			caseMap[dc.CandidateID] = dc
		}
	}

	// 1. 拆分批次 (Batching)
	var batches [][]HunterCandidate
	for i := 0; i < len(hunterOut.Candidates); i += defaultMaxCandidatesPerBatch {
		end := i + defaultMaxCandidatesPerBatch
		if end > len(hunterOut.Candidates) {
			end = len(hunterOut.Candidates)
		}
		batches = append(batches, hunterOut.Candidates[i:end])
	}

	var mergedJudgeOut JudgeOutput
	var totalTokens int64

	// 2. 逐批调用法官裁决
	for bIdx, batch := range batches {
		var subDefenseCases []ChallengerDefenseCase
		for _, c := range batch {
			if dc, ok := caseMap[c.CandidateID]; ok {
				subDefenseCases = append(subDefenseCases, dc)
			}
		}

		subHunterOut := &HunterOutput{
			Candidates: sanitizeCandidatesForPrompt(batch),
			Summary:    hunterOut.Summary,
		}
		var summary string
		if challengerOut != nil {
			summary = challengerOut.Summary
		}
		subChallOut := &ChallengerOutput{
			DefenseCases: subDefenseCases,
			Summary:      summary,
		}

		prompt := buildJudgePrompt(ctx, bundle, subHunterOut, subChallOut)

		subOutPath := outPath
		if len(batches) > 1 && outPath != "" {
			subOutPath = fmt.Sprintf("%s.judge-batch-%d.tmp", outPath, bIdx+1)
		}

		if len(batches) > 1 {
			log.Printf("[DebateEngine] Bundle [%s]: Invoking Judge Batch %d/%d (Candidates: %d)...",
				bundle.Name, bIdx+1, len(batches), len(batch))
		}

		workCtx := &LLMWorkContext{
			ReportID: ctx.report.ID,
			RepoName: ctx.repo.Name,
			TaskType: ctx.taskType.DisplayName,
			Stage:    "Tier 2: 裁判终审 (Judge)",
			SubTask:  fmt.Sprintf("批次 %d/%d (候选点 %d 个交叉仲裁)", bIdx+1, len(batches), len(batch)),
		}
		rawOutput, tokens, callErr := callAITier(ctx.ctx, acq.Backend, acq.ModelName, prompt, ctx.codesPath, subOutPath, tierCfg.TimeoutSeconds, workCtx)
		if len(batches) > 1 && subOutPath != "" && subOutPath != outPath {
			_ = os.Remove(subOutPath)
		}

		totalTokens += tokens
		if callErr != nil {
			log.Printf("[DebateEngine] Warning: Judge Batch %d/%d failed (%v), fallback this batch to Hunter claims", bIdx+1, len(batches), callErr)
			fbJudge := fallbackJudgeFromHunter(&HunterOutput{Candidates: batch})
			mergedJudgeOut.FinalVerdicts = append(mergedJudgeOut.FinalVerdicts, fbJudge.FinalVerdicts...)
			continue
		}

		var subJudgeOut JudgeOutput
		if parseErr := parseJSONFromAIOutput(rawOutput, &subJudgeOut, ctx.codesPath); parseErr != nil {
			log.Printf("[DebateEngine] Warning: Judge Batch %d/%d JSON parse failed (%v), fallback this batch", bIdx+1, len(batches), parseErr)
			fbJudge := fallbackJudgeFromHunter(&HunterOutput{Candidates: batch})
			mergedJudgeOut.FinalVerdicts = append(mergedJudgeOut.FinalVerdicts, fbJudge.FinalVerdicts...)
			continue
		}

		mergedJudgeOut.FinalVerdicts = append(mergedJudgeOut.FinalVerdicts, subJudgeOut.FinalVerdicts...)
	}

	if len(mergedJudgeOut.FinalVerdicts) == 0 {
		return nil, totalTokens, fmt.Errorf("all judge batches failed")
	}

	// 最终聚合写入 outPath
	if outPath != "" {
		if outBytes, jsonErr := json.MarshalIndent(mergedJudgeOut, "", "  "); jsonErr == nil {
			_ = os.WriteFile(outPath, outBytes, 0644)
		}
	}

	return &mergedJudgeOut, totalTokens, nil
}

// fallbackJudgeFromHunter 当法官或辩论失败时的兜底构造
func fallbackJudgeFromHunter(hunterOut *HunterOutput) *JudgeOutput {
	var verdicts []JudgeFinalVerdict
	for _, c := range hunterOut.Candidates {
		titleCandidate := c.Title
		if titleCandidate == "" {
			titleCandidate = c.AttackHypothesis
		}
		conciseTitle := deriveConciseTitle(titleCandidate, c.CWECategory)
		rationale := "[Fallback: 智能体辩论超时，保留猎手原始判定]"
		if c.AttackHypothesis != "" {
			rationale = fmt.Sprintf("【成因与攻击假设】: %s\n\n%s", c.AttackHypothesis, rationale)
		}
		verdicts = append(verdicts, JudgeFinalVerdict{
			CandidateID:         c.CandidateID,
			Verdict:             models.DebateVerdictConfirmed,
			SeverityPreliminary: "严重",
			Category:            c.CWECategory,
			FilePath:            c.FilePath,
			LineNumber:          c.LineRange,
			TriggerLine:         c.TriggerLine,
			ScopeSymbol:         c.ScopeSymbol,
			Title:               conciseTitle,
			JudgementRationale:  rationale,
			CodeSnippet:         c.CodeSnippet,
			Suggestion:          "建议人工复核此可疑风险点",
		})
	}
	return &JudgeOutput{FinalVerdicts: verdicts}
}

// deriveConciseTitle 从漏洞成因假设或长文本中提炼简明扼要的标题，防止长段落文本污染标题或溢出
func deriveConciseTitle(rawTitle, fallbackCategory string) string {
	t := strings.TrimSpace(rawTitle)
	if t == "" {
		if fallbackCategory != "" {
			return fallbackCategory
		}
		return "潜在代码缺陷"
	}
	// 剔除换行，优先取第一行
	if idx := strings.IndexAny(t, "\r\n"); idx != -1 {
		t = strings.TrimSpace(t[:idx])
	}
	// 若首行依然过长，按中文或英文标点断句截取首个完整语义子句
	for _, sep := range []string{"。", "；", ";", ". "} {
		if idx := strings.Index(t, sep); idx != -1 && idx+len(sep) < len(t) {
			candidate := strings.TrimSpace(t[:idx+len(sep)])
			if len([]rune(candidate)) >= 6 {
				t = candidate
				break
			}
		}
	}
	// 长度软截断 (最多 200 个字符)
	runes := []rune(t)
	if len(runes) > 200 {
		t = string(runes[:197]) + "..."
	}
	return t
}

func buildHunterPrompt(ctx *taskContext, bundle SemanticBundle) string {
	var sb strings.Builder
	sb.WriteString("# Role: Code-Shield 漏洞挖掘猎手 (Hunter Agent)\n\n")

	// 1. 优先注入该任务类型的专属分析指令与领域规则 (Domain Guidelines)
	analysisPromptPath := models.AppConfig.GetAbsPath(ctx.taskType.AnalysisPromptFile())
	if promptBytes, err := os.ReadFile(analysisPromptPath); err == nil && len(promptBytes) > 0 {
		sb.WriteString("## 专项检视领域规范与任务指令 (Domain Guidelines):\n")
		sb.WriteString(string(promptBytes))
		sb.WriteString("\n\n---\n\n")
	}

	sb.WriteString("## 猎手挖掘原则与任务目标\n请在所提供的代码分片中尽可能多地发掘符合上述专项规范、可能导致崩溃、越界、空指针、内存破坏、并发竞争或安全隐患的缺陷。秉持零信任原则，最大化召回率。\n\n")

	if len(bundle.MacroContext) > 0 {
		sb.WriteString("### 环境变量与构建宏定义 (Macro Context):\n")
		for k, v := range bundle.MacroContext {
			sb.WriteString(fmt.Sprintf("- `#define %s %s`\n", k, v))
		}
		sb.WriteString("\n")
	}

	if bundle.HeaderOutline != "" {
		sb.WriteString("### 基础核心头文件声明摘要 (Header Outline):\n```cpp\n")
		sb.WriteString(bundle.HeaderOutline)
		sb.WriteString("\n```\n\n")
	}

	if len(bundle.NegativeRules) > 0 {
		sb.WriteString("### 历史已确认负样本与例外规则 (需避免误报):\n")
		for _, nr := range bundle.NegativeRules {
			sb.WriteString(fmt.Sprintf("- %s\n", nr))
		}
		sb.WriteString("\n")
	}

	allowedCats := ctx.taskType.GetAllowedCategories()
	if len(allowedCats) > 0 {
		sb.WriteString("### 必须严格遵守的缺陷分类受控白名单 (Strict Categories):\n")
		sb.WriteString("候选输出中的分类（category）**必须且只能严格从以下受控列表中选择**，严禁输出非列表中的自由发散文本：\n")
		for _, cat := range allowedCats {
			sb.WriteString(fmt.Sprintf("- `%s`\n", cat))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("### 待分析文件列表:\n")
	for _, f := range bundle.AllFiles {
		sb.WriteString(fmt.Sprintf("- %s\n", f))
	}
	sb.WriteString("\n### 输出格式规范 (JSON Only):\n")
	sb.WriteString("```json\n{\n  \"candidates\": [\n    {\n      \"candidate_id\": \"H-001\",\n      \"file_path\": \"src/file.cc\",\n      \"line_range\": \"10-20\",\n      \"trigger_line\": \"c = *++it;\",\n      \"scope_symbol\": \"Class::Method\",\n      \"code_snippet\": \"...\",\n      \"category\": \"受控分类（必须从上述受控列表中多选一）\",\n      \"title\": \"精确简明的缺陷标题（一句话概括，如：析构中delete单例指针导致悬空指针与UAF，严禁输出长段落）\",\n      \"attack_hypothesis\": \"漏洞成因与攻击假设\",\n      \"suspected_trigger\": \"触发输入条件\"\n    }\n  ],\n  \"summary\": \"概括\"\n}\n```\n")

	return sb.String()
}

// sanitizeCandidateSnippet 对单个候选缺陷的原始代码片段进行上下文保护软截断（防超长代码撑爆 128K 窗口）
func sanitizeCandidateSnippet(snippet string) string {
	lines := strings.Split(snippet, "\n")
	const maxLines = 150
	const maxChars = 4000
	if len(lines) <= maxLines && len(snippet) <= maxChars {
		return snippet
	}

	if len(lines) > maxLines {
		keepHead := 80
		keepTail := 40
		omitted := len(lines) - keepHead - keepTail
		headPart := strings.Join(lines[:keepHead], "\n")
		tailPart := strings.Join(lines[len(lines)-keepTail:], "\n")
		snippet = fmt.Sprintf("%s\n\n// ... [代码片段过长已自动折叠截断，已省略 %d 行，保护 128K 上下文] ...\n\n%s", headPart, omitted, tailPart)
	}

	if len(snippet) > maxChars {
		snippet = snippet[:maxChars] + "\n// ... [代码字符超过 4000 已软截断] ..."
	}
	return snippet
}

// sanitizeCandidatesForPrompt 返回经过 Snippet 软截断保护的候选列表拷贝
func sanitizeCandidatesForPrompt(candidates []HunterCandidate) []HunterCandidate {
	sanitized := make([]HunterCandidate, len(candidates))
	for i, c := range candidates {
		cCopy := c
		cCopy.CodeSnippet = sanitizeCandidateSnippet(c.CodeSnippet)
		sanitized[i] = cCopy
	}
	return sanitized
}

func buildChallengerPrompt(ctx *taskContext, bundle SemanticBundle, hunterOut *HunterOutput) string {
	candidates := sanitizeCandidatesForPrompt(hunterOut.Candidates)
	candJSON, _ := json.MarshalIndent(candidates, "", "  ")

	var sb strings.Builder
	sb.WriteString("# Role: Code-Shield 代码安全辩护人 (Challenger Agent)\n\n")
	sb.WriteString("## 任务目标\n对 Hunter Agent 提出的候选缺陷进行最严厉的反向质询与代码辩护。全力找出该缺陷不会发生、无法触发或属于无效假阳性（False Positive）的代码证据（四维质询：前置断言、条件宏隔离、语言标准契约、架构防御）。\n\n")
	sb.WriteString("### Hunter 提出的候选缺陷:\n```json\n")
	sb.WriteString(string(candJSON))
	sb.WriteString("\n```\n\n")
	sb.WriteString("### 输出格式规范 (JSON Only):\n")
	sb.WriteString("```json\n{\n  \"defense_cases\": [\n    {\n      \"candidate_id\": \"H-001\",\n      \"defense_verdict\": \"DEFENSE_SUCCESSFUL / DEFENSE_PARTIAL / CHALLENGE_FAILED\",\n      \"defense_arguments\": [\n        {\"dimension\": \"Guards & Assertions\", \"finding\": \"发现前置判空保护\"}\n      ],\n      \"mitigating_factors\": \"缓解因素说明\",\n      \"counter_evidence_snippet\": \"\"\n    }\n  ],\n  \"summary\": \"辩护综述\"\n}\n```\n")

	return sb.String()
}

func buildJudgePrompt(ctx *taskContext, bundle SemanticBundle, hunterOut *HunterOutput, challOut *ChallengerOutput) string {
	candidates := sanitizeCandidatesForPrompt(hunterOut.Candidates)
	hJSON, _ := json.MarshalIndent(candidates, "", "  ")
	var cCases []ChallengerDefenseCase
	if challOut != nil {
		cCases = challOut.DefenseCases
	}
	cJSON, _ := json.MarshalIndent(cCases, "", "  ")

	var sb strings.Builder
	sb.WriteString("# Role: Code-Shield 漏洞终审法官 (Judge Agent)\n\n")
	sb.WriteString("## 任务目标\n审阅 Hunter 提出的指控与 Challenger 提出的抗辩，对照源码事实，做出最终具有权威性的事实裁决（CONFIRMED / REJECTED / CONDITIONAL）。\n\n")
	sb.WriteString("### Hunter 候选列表:\n```json\n")
	sb.WriteString(string(hJSON))
	sb.WriteString("\n```\n\n")
	sb.WriteString("### Challenger 抗辩列表:\n```json\n")
	sb.WriteString(string(cJSON))
	sb.WriteString("\n```\n\n")
	sb.WriteString("## 裁决词撰写规约 (judgement_rationale 结构化要求)\n")
	sb.WriteString("请在 judgement_rationale 中按段落清晰使用以下标签书写（方便系统结构化展示）：\n")
	sb.WriteString("- 【综合裁决】：总结结论（裁决结果如 CONFIRMED / CONDITIONAL / REJECTED，初步定级与影响综述）\n")
	sb.WriteString("- 【源码事实】：具体文件行号路径、参数传递与校验缺失事实\n")
	sb.WriteString("- 【实测验证】：复现/PoC 触发表现、信号或异常分析\n")
	sb.WriteString("- 【决定性证据】：契约、官方测试用例或规范依据\n")
	sb.WriteString("- 【对照参考实现】：与标准库/参照组实现的对比差异\n")
	sb.WriteString("- 【抗辩响应】：对辩护人抗辩主张的审理与判定\n")
	sb.WriteString("- 【缓和因素】：触发前提约束与影响范围限制\n\n")
	sb.WriteString("## 严重等级定义规约\n")
	sb.WriteString("字段 `severity_preliminary` 的取值必须严格限定为系统四大标准等级之一：`致命`（破坏内存/写越界/UAF）、`严重`（确定性空指针/段错误/崩溃/读越界）、`一般`（条件宏保护/资源耗尽/DoS/锁竞争）、`建议`（架构风格/防御性缺失）。严禁输出除此四者之外的其他词汇。\n\n")

	allowedCats := ctx.taskType.GetAllowedCategories()
	if len(allowedCats) > 0 {
		sb.WriteString("## 缺陷分类受控规约 (Strict Categories)\n")
		sb.WriteString("裁决输出中 `category` 的取值**必须严格限定为以下受控标准分类之一**，严禁自由捏造：\n")
		for _, cat := range allowedCats {
			sb.WriteString(fmt.Sprintf("- `%s`\n", cat))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("### 输出格式规范 (JSON Only):\n")
	sb.WriteString("```json\n{\n  \"final_verdicts\": [\n    {\n      \"candidate_id\": \"H-001\",\n      \"verdict\": \"CONFIRMED\",\n      \"severity_preliminary\": \"严重\",\n      \"category\": \"内存管理问题-越界访问\",\n      \"file_path\": \"src/file.cc\",\n      \"line_number\": \"10-20\",\n      \"trigger_line\": \"c = *++it;\",\n      \"scope_symbol\": \"Class::Method\",\n      \"title\": \"精确漏洞标题\",\n      \"judgement_rationale\": \"【综合裁决】: ...\\n\\n【源码事实】: ...\\n\\n【实测验证】: ...\\n\\n【决定性证据】: ...\\n\\n【抗辩响应】: ...\\n\\n【缓和因素】: ...\",\n      \"code_snippet\": \"...\",\n      \"suggestion\": \"修复建议代码\"\n    }\n  ]\n}\n```\n")

	return sb.String()
}

// parseJSONFromAIOutput 从大模型可能包含 Markdown ```json 包裹的输出中精准解析 JSON（支持自动截取与 AI 自愈修复）
func parseJSONFromAIOutput(output string, target interface{}, workDir string) error {
	trimmed := strings.TrimSpace(output)
	if strings.Contains(trimmed, "```") {
		start := strings.Index(trimmed, "```json")
		if start != -1 {
			trimmed = trimmed[start+7:]
		} else {
			start = strings.Index(trimmed, "```")
			if start != -1 {
				trimmed = trimmed[start+3:]
			}
		}
		end := strings.LastIndex(trimmed, "```")
		if end != -1 {
			trimmed = trimmed[:end]
		}
	}
	trimmed = strings.TrimSpace(trimmed)

	// 智能寻找最外层的有效 JSON 边界 { ... }
	firstBrace := strings.Index(trimmed, "{")
	lastBrace := strings.LastIndex(trimmed, "}")
	if firstBrace != -1 && lastBrace != -1 && lastBrace > firstBrace {
		trimmed = trimmed[firstBrace : lastBrace+1]
	}

	// 1. 尝试直接标准反序列化
	err := json.Unmarshal([]byte(trimmed), target)
	if err == nil {
		return nil
	}

	// 发现语法瑕疵，输出简明 Warning 提示进入自动修复，不倾倒完整 raw 内容以防控制台刷屏
	log.Printf("[DebateEngine] Warning: AI output is malformed JSON (%v), attempting auto-repair...\n", err)

	// 2. 内存级快速语法自愈（修复未转义反斜杠 \、\u0000、字符串内字面量换行、尾部多余逗号等常见大模型输出瑕疵）
	fastRepaired := repairMalformedJSON(trimmed)
	if firstB := strings.Index(fastRepaired, "{"); firstB != -1 {
		if lastB := strings.LastIndex(fastRepaired, "}"); lastB != -1 && lastB > firstB {
			fastRepaired = fastRepaired[firstB : lastB+1]
		}
	}
	if unmarshalErr := json.Unmarshal([]byte(fastRepaired), target); unmarshalErr == nil {
		log.Printf("[DebateEngine] Successfully fast-repaired malformed AI JSON output in memory\n")
		return nil
	}

	// 3. 仍解析失败时，回退调用外部 AI RepairJSON 修复
	tmpFile, tmpErr := os.CreateTemp("", "debate-parse-repair-*.json")
	if tmpErr == nil {
		tmpPath := tmpFile.Name()
		_ = os.WriteFile(tmpPath, []byte(fastRepaired), 0644)
		tmpFile.Close()
		defer os.Remove(tmpPath)

		repaired, repErr := RepairJSON(workDir, tmpPath, "")
		if repErr == nil {
			repTrimmed := strings.TrimSpace(string(repaired))
			if f := strings.Index(repTrimmed, "{"); f != -1 {
				if l := strings.LastIndex(repTrimmed, "}"); l != -1 && l > f {
					repTrimmed = repTrimmed[f : l+1]
				}
			}
			if unmarshalErr := json.Unmarshal([]byte(repTrimmed), target); unmarshalErr == nil {
				log.Printf("[DebateEngine] Successfully auto-repaired malformed AI JSON output via RepairJSON\n")
				return nil
			}
		}
	}

	// 4. 所有自愈与 AI 修复均告失败，输出紧凑 Error（仅保留前 200 字符预览）
	preview := trimmed
	if len(preview) > 200 {
		preview = preview[:200] + "... [truncated]"
	}
	log.Printf("[DebateEngine] Error: All JSON repair attempts failed (%v). Preview: %s\n", err, preview)

	return fmt.Errorf("failed to parse and repair JSON (%w)", err)
}

// SanitizeJSONForPostgresJSONB 清洗 JSON 字节流，移除 PostgreSQL jsonb 不支持的 \u0000 Unicode 转义序列与控制字符
func SanitizeJSONForPostgresJSONB(data []byte) datatypes.JSON {
	if len(data) == 0 {
		return datatypes.JSON([]byte("{}"))
	}
	s := string(data)
	// 消除 PostgreSQL jsonb 严禁的 \u0000 及空字符
	s = strings.ReplaceAll(s, `\u0000`, " ")
	s = strings.ReplaceAll(s, `\U0000`, " ")
	s = strings.ReplaceAll(s, "\x00", "")

	cleaned := []byte(s)
	if !json.Valid(cleaned) {
		// 若已有语法瑕疵，尝试快速自愈
		repaired := repairMalformedJSON(s)
		if json.Valid([]byte(repaired)) {
			cleaned = []byte(repaired)
		} else {
			// 极端兜底：包装为包含 raw 字段的合法 JSON，确保 jsonb 字段安全落库而不丢失日志
			type fallbackWrap struct {
				Raw string `json:"raw"`
			}
			fb, _ := json.Marshal(fallbackWrap{Raw: s})
			cleaned = fb
		}
	}
	return datatypes.JSON(cleaned)
}

// repairMalformedJSON 对大模型输出的非标 JSON 进行内存级语法清洗与自愈
func repairMalformedJSON(input string) string {
	runes := []rune(input)
	n := len(runes)
	var sb strings.Builder
	sb.Grow(n + 64)

	inString := false
	var isEscaped bool

	for i := 0; i < n; i++ {
		r := runes[i]

		if inString {
			if isEscaped {
				isEscaped = false
				// 检查合规的标准转义字符: ", \, /, b, f, n, r, t, u
				switch r {
				case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
					sb.WriteRune('\\')
					sb.WriteRune(r)
				case 'u', 'U':
					// 检查是否为 4 位十六进制
					if i+4 < n && isHex4(runes[i+1:i+5]) {
						hexStr := string(runes[i+1 : i+5])
						if hexStr == "0000" {
							// PostgreSQL jsonb 严禁 \u0000，替换为空格
							sb.WriteString(" ")
						} else {
							sb.WriteString("\\u")
							sb.WriteString(hexStr)
						}
						i += 4
					} else {
						// 伪 Unicode 转义，将前置反斜杠转义保护
						sb.WriteString("\\\\")
						sb.WriteRune(r)
					}
				default:
					// 非标准转义符（如 \s, \d, \0, \x, 路径斜杠等），双反斜杠转义保护
					sb.WriteString("\\\\")
					sb.WriteRune(r)
				}
				continue
			}

			if r == '\\' {
				isEscaped = true
				continue
			}

			if r == '"' {
				inString = false
				sb.WriteRune('"')
				continue
			}

			// 处理字符串字面量内部未转义的原始控制字符
			switch r {
			case '\n':
				sb.WriteString("\\n")
			case '\r':
				sb.WriteString("\\r")
			case '\t':
				sb.WriteString("\\t")
			default:
				if r < 0x20 {
					// 忽略其他不可打印 ASCII 控制字符
					continue
				}
				sb.WriteRune(r)
			}
		} else {
			if r == '"' {
				inString = true
				sb.WriteRune('"')
				continue
			}

			// 处理非字符串区域的尾部多余逗号 (trailing commas: `, }` 或 `, ]`)
			if r == ',' {
				// 前瞻下一个非空字符
				j := i + 1
				for j < n && (runes[j] == ' ' || runes[j] == '\t' || runes[j] == '\r' || runes[j] == '\n') {
					j++
				}
				if j < n && (runes[j] == '}' || runes[j] == ']') {
					// 忽略该多余逗号
					continue
				}
			}

			sb.WriteRune(r)
		}
	}

	// 若字符串未正常闭合，补全闭合引号
	if inString {
		sb.WriteRune('"')
	}

	return sb.String()
}

func isHex4(runes []rune) bool {
	if len(runes) < 4 {
		return false
	}
	for _, r := range runes {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// callAITier 底层调用指定后端和模型的 AI Invoker 工具
func callAITier(ctx context.Context, backend string, modelName string, prompt string, workDir string, outPath string, timeoutSeconds int, workCtx ...*LLMWorkContext) (string, int64, error) {
	if backend == "" {
		backend = models.AppConfig.AI.Backend
	}

	invoker := GetAIInvoker(backend)
	if invoker == nil {
		return "", 0, fmt.Errorf("unsupported AI backend: %s", backend)
	}

	outputPath := outPath
	shouldCleanup := false
	if outputPath == "" {
		// 未指定目标目录时回退到临时文件
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

	var selectedWorkCtx *LLMWorkContext
	if len(workCtx) > 0 && workCtx[0] != nil {
		selectedWorkCtx = workCtx[0]
	}

	req := AIRequest{
		ParentContext:  ctx,
		WorkDir:        workDir,
		PromptMsg:      prompt,
		OutputPath:     outputPath,
		TimeoutMin:     timeoutMin,
		ModelName:      modelName,
		ResponseFormat: "json",
		WorkContext:    selectedWorkCtx,
	}

	if err := invoker.Invoke(req); err != nil {
		return "", 0, err
	}

	outBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read AI output: %w", err)
	}

	outStr := string(outBytes)
	estimatedTokens := int64((len(prompt) + len(outStr)) / 4)
	return outStr, estimatedTokens, nil
}
