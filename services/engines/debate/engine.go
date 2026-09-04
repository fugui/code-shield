package debate

import (
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
	"code-shield/services/defects"
	"code-shield/services/dispatcher"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/governance"
	"code-shield/services/invoker"

	"gorm.io/datatypes"
)

// DebateEngine 实现多智能体三方对抗辩论流水线引擎
type DebateEngine struct {
	Mode string // "debate_full", "debate_selective", "chunked_fast"
}

func init() {
	engines.RegisterEngine("debate_full", &DebateEngine{Mode: "debate_full"})
	engines.RegisterEngine("debate_selective", &DebateEngine{Mode: "debate_selective"})
	engines.RegisterEngine("chunked_fast", &DebateEngine{Mode: "chunked_fast"})
}

func (e *DebateEngine) Name() string {
	if e.Mode != "" {
		return e.Mode
	}
	return "debate_full"
}

// Run 启动辩论引擎任务流（纯内存计算，不直接操作 DB）
func (e *DebateEngine) Run(ctx *engines.EngineContext) (*engines.EngineResult, error) {
	cfg := engines.ChunkConfig{
		MaxFiles:    engines.DefaultChunkMaxFiles,
		Depth:       engines.DefaultChunkDepth,
		Concurrency: engines.DefaultChunkConcurrency,
	}
	if len(ctx.EngineConfig) > 0 {
		_ = json.Unmarshal(ctx.EngineConfig, &cfg)
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = engines.DefaultChunkMaxFiles
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = engines.DefaultChunkConcurrency
	}

	targetScope := "all"
	if ctx.RunParams.TargetScope != nil {
		targetScope = *ctx.RunParams.TargetScope
	}

	// 1. 使用上下文已预加载的免扫/负样本例外规则
	negativeRules := ctx.NegativeRules

	// 2. 扫描并构建语义感知分片包 (含同名投影与宏注入)
	bundles, err := chunker.BuildSemanticBundles(ctx.CodesPath, cfg, targetScope, negativeRules)
	if err != nil {
		return nil, err
	}

	log.Printf("[DebateEngine] EngineMode: %s | Found %d semantic bundles for repo %s\n",
		e.Mode, len(bundles), ctx.RepoName)

	nameParts := strings.Split(ctx.RepoName, "/")
	repoShort := nameParts[len(nameParts)-1]
	chunkDir := filepath.Join(filepath.Dir(ctx.ReportPath), fmt.Sprintf("debate-chunks-%d-%s", ctx.ReportID, repoShort))
	_ = os.MkdirAll(chunkDir, 0755)

	var allFindings []models.AnalysisFinding
	var allDebateLogs []models.TaskDebateLog
	var totalHunterTokens, totalTier2Tokens int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	var chunkErrors []string
	var errMu sync.Mutex
	semaphore := make(chan struct{}, cfg.Concurrency)
	totalChunks := len(bundles)
	chunkIndex := 0

	if ctx.ProgressReport != nil {
		ctx.ProgressReport(totalChunks, 0, 0)
	}

	chunkDetailsList := make([]engines.ChunkDetails, totalChunks)
	processedChunks := 0
	successChunks := 0

bundleLoop:
	for _, b := range bundles {
		if ctx.Ctx.Err() != nil {
			break bundleLoop
		}

		chunkIndex++
		currentIndex := chunkIndex
		bundle := b

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
			findings, debateLogs, hTokens, t2Tokens, bundleErr := e.ProcessBundle(ctx, bnd, idx, chunkDir)
			bundleEndTime := time.Now()

			details := engines.ChunkDetails{
				ChunkName:       bnd.Name,
				Attempts:        1,
				StartTime:       bundleStartTime,
				EndTime:         bundleEndTime,
				DurationSeconds: bundleEndTime.Sub(bundleStartTime).Seconds(),
			}

			if bundleErr != nil {
				log.Printf("[DebateEngine] Bundle [%s] debate failed: %v\n", bnd.Name, bundleErr)
				errMu.Lock()
				chunkErrors = append(chunkErrors, fmt.Sprintf("Bundle [%s] failed: %v", bnd.Name, bundleErr))
				errMu.Unlock()

				details.Status = "failed"
				details.ErrorMessage = bundleErr.Error()
			} else {
				details.Status = "success"
			}

			mu.Lock()
			chunkDetailsList[idx-1] = details
			processedChunks++
			if details.Status == "success" {
				successChunks++
				allFindings = append(allFindings, findings...)
				allDebateLogs = append(allDebateLogs, debateLogs...)
				totalHunterTokens += hTokens
				totalTier2Tokens += t2Tokens
			}
			if ctx.ProgressReport != nil {
				ctx.ProgressReport(totalChunks, processedChunks, successChunks)
			}
			mu.Unlock()
		}(bundle, currentIndex)
	}

	wg.Wait()

	hasFailedChunks := len(chunkErrors) > 0
	if hasFailedChunks {
		log.Printf("[DebateEngine] Warning: %d bundles failed, proceeding with %d confirmed findings\n",
			len(chunkErrors), len(allFindings))
	}

	// 确定性严重度校准
	allFindings = governance.CalibrateFindings(allFindings)

	result := &engines.EngineResult{
		Findings:        allFindings,
		DebateLogs:      allDebateLogs,
		SummaryChunks:   chunkDetailsList,
		HasFailedChunks: hasFailedChunks,
		HunterTokens:    totalHunterTokens,
		Tier2Tokens:     totalTier2Tokens,
	}

	if ctx.SynthesisExecutor != nil {
		if synthErr := ctx.SynthesisExecutor(allFindings); synthErr != nil {
			log.Printf("[DebateEngine] Warning: SynthesisExecutor failed: %v", synthErr)
		}
	}

	return result, nil
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
func (e *DebateEngine) ProcessBundle(ctx *engines.EngineContext, bundle chunker.SemanticBundle, chunkIdx int, chunkDir string) ([]models.AnalysisFinding, []models.TaskDebateLog, int64, int64, error) {
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
		return nil, nil, 0, 0, fmt.Errorf("hunter stage failed: %w", err)
	}

	if len(hunterOut.Candidates) == 0 {
		log.Printf("[DebateEngine] Bundle [%s]: Hunter found 0 candidates. Fast Pass.", bundle.Name)
		return []models.AnalysisFinding{}, nil, hunterTokens, 0, nil
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
			conciseTitle := DeriveConciseTitle(titleCandidate, c.CWECategory)
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
		return fastFindings, nil, hunterTokens, 0, nil
	}

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

			cleanCategory := governance.SanitizeCategory(jv.Category, nil)
			normScope := defects.NormalizeScopeSymbol(jv.ScopeSymbol)
			cleanTrigger := defects.CleanSourceToken(jv.TriggerLine)
			calibratedLine := jv.LineNumber

			if ctx.CodesPath != "" {
				if anchor, err := defects.EnrichSourceAnchor(ctx.CodesPath, jv.FilePath, jv.LineNumber, jv.TriggerLine); err == nil {
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
				Title:         DeriveConciseTitle(jv.Title, cleanCategory),
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

	return confirmedFindings, debateLogs, hunterTokens, totalTier2Tokens, nil
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
func (e *DebateEngine) runHunterStage(ctx *engines.EngineContext, bundle chunker.SemanticBundle, outPath string) (*HunterOutput, int64, error) {
	router := dispatcher.GetTierRouter()
	acq, err := router.AcquireTier(ctx.Ctx, "tier1_fast", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	prompt := buildHunterPrompt(ctx, bundle)
	tierCfg := models.AppConfig.GetTierConfig("tier1_fast")
	workCtx := &invoker.LLMWorkContext{
		ReportID: ctx.ReportID,
		RepoName: ctx.RepoName,
		TaskType: ctx.TaskTypeName,
		Stage:    "Tier 1: 猎手初筛",
		SubTask:  fmt.Sprintf("分片束 %s (%d 个候选文件)", bundle.Name, len(bundle.AllFiles)),
	}
	rawOutput, tokens, err := callAITier(ctx.Ctx, acq.Backend, acq.ModelName, prompt, ctx.CodesPath, outPath, tierCfg.TimeoutSeconds, workCtx)
	if err != nil {
		return nil, tokens, err
	}

	var hunterOut HunterOutput
	if parseErr := parseJSONFromAIOutput(rawOutput, &hunterOut, ctx.CodesPath); parseErr != nil {
		hunterOut.Summary = rawOutput
	}

	return &hunterOut, tokens, nil
}

const defaultMaxCandidatesPerBatch = 5

// runChallengerStage 运行辩护人对抗阶段
func (e *DebateEngine) runChallengerStage(ctx *engines.EngineContext, bundle chunker.SemanticBundle, hunterOut *HunterOutput, outPath string) (*ChallengerOutput, int64, error) {
	if len(hunterOut.Candidates) == 0 {
		return &ChallengerOutput{}, 0, nil
	}

	router := dispatcher.GetTierRouter()
	acq, err := router.AcquireTier(ctx.Ctx, "tier2_reasoning", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	tierCfg := models.AppConfig.GetTierConfig("tier2_reasoning")

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

	for bIdx, batch := range batches {
		subHunterOut := &HunterOutput{
			Candidates: sanitizeCandidatesForPrompt(batch),
			Summary:    hunterOut.Summary,
		}
		prompt := buildChallengerPrompt(ctx, bundle, subHunterOut)

		subOutPath := outPath
		if len(batches) > 1 && outPath != "" {
			subOutPath = fmt.Sprintf("%s.batch-%d.tmp", outPath, bIdx+1)
		}

		workCtx := &invoker.LLMWorkContext{
			ReportID: ctx.ReportID,
			RepoName: ctx.RepoName,
			TaskType: ctx.TaskTypeName,
			Stage:    "Tier 2: 辩护对抗 (Challenger)",
			SubTask:  fmt.Sprintf("批次 %d/%d (候选点 %d 个)", bIdx+1, len(batches), len(batch)),
		}
		rawOutput, tokens, callErr := callAITier(ctx.Ctx, acq.Backend, acq.ModelName, prompt, ctx.CodesPath, subOutPath, tierCfg.TimeoutSeconds, workCtx)
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
			batchSummaries = append(batchSummaries, fmt.Sprintf("[Batch %d Degraded: %v]", bIdx+1, callErr))
			continue
		}

		var subChallengerOut ChallengerOutput
		if parseErr := parseJSONFromAIOutput(rawOutput, &subChallengerOut, ctx.CodesPath); parseErr != nil {
			log.Printf("[DebateEngine] Warning: Challenger Batch %d/%d JSON parse failed (%v), degrading this batch", bIdx+1, len(batches), parseErr)
			for _, cand := range batch {
				mergedChallengerOut.DefenseCases = append(mergedChallengerOut.DefenseCases, ChallengerDefenseCase{
					CandidateID:       cand.CandidateID,
					DefenseVerdict:    "CHALLENGE_FAILED",
					MitigatingFactors: "[Challenger Degraded: 辩护人输出解析失败，请法官独立基于源码客观裁决]",
				})
			}
			batchSummaries = append(batchSummaries, fmt.Sprintf("[Batch %d Degraded: %v]", bIdx+1, parseErr))
			continue
		}

		mergedChallengerOut.DefenseCases = append(mergedChallengerOut.DefenseCases, subChallengerOut.DefenseCases...)
		if subChallengerOut.Summary != "" {
			batchSummaries = append(batchSummaries, subChallengerOut.Summary)
		}
	}

	mergedChallengerOut.Summary = strings.Join(batchSummaries, "\n---\n")

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

	router := dispatcher.GetTierRouter()
	acq, err := router.AcquireTier(ctx.Ctx, "tier2_reasoning", "")
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	tierCfg := models.AppConfig.GetTierConfig("tier2_reasoning")

	caseMap := make(map[string]ChallengerDefenseCase)
	for _, dc := range challOut.DefenseCases {
		caseMap[dc.CandidateID] = dc
	}

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

	for bIdx, batch := range batches {
		subHunterOut := &HunterOutput{
			Candidates: sanitizeCandidatesForPrompt(batch),
			Summary:    hunterOut.Summary,
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
			Summary:      challOut.Summary,
		}

		prompt := buildJudgePrompt(ctx, bundle, subHunterOut, subChallOut)

		subOutPath := outPath
		if len(batches) > 1 && outPath != "" {
			subOutPath = fmt.Sprintf("%s.batch-%d.tmp", outPath, bIdx+1)
		}

		workCtx := &invoker.LLMWorkContext{
			ReportID: ctx.ReportID,
			RepoName: ctx.RepoName,
			TaskType: ctx.TaskTypeName,
			Stage:    "Tier 2: 终审法官 (Judge)",
			SubTask:  fmt.Sprintf("批次 %d/%d (裁决点 %d 个)", bIdx+1, len(batches), len(batch)),
		}
		rawOutput, tokens, callErr := callAITier(ctx.Ctx, acq.Backend, acq.ModelName, prompt, ctx.CodesPath, subOutPath, tierCfg.TimeoutSeconds, workCtx)
		if len(batches) > 1 && subOutPath != "" && subOutPath != outPath {
			_ = os.Remove(subOutPath)
		}

		totalTokens += tokens
		if callErr != nil {
			log.Printf("[DebateEngine] Warning: Judge Batch %d/%d failed (%v), fallback this batch", bIdx+1, len(batches), callErr)
			fbJudge := fallbackJudgeFromHunter(&HunterOutput{Candidates: batch})
			mergedJudgeOut.FinalVerdicts = append(mergedJudgeOut.FinalVerdicts, fbJudge.FinalVerdicts...)
			continue
		}

		var subJudgeOut JudgeOutput
		if parseErr := parseJSONFromAIOutput(rawOutput, &subJudgeOut, ctx.CodesPath); parseErr != nil {
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
		conciseTitle := DeriveConciseTitle(titleCandidate, c.CWECategory)
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
	for _, sep := range []string{"。", "；", ";", "."} {
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
	}
	return sanitized
}

// buildHunterPrompt 组装猎手 Prompt
func buildHunterPrompt(ctx *engines.EngineContext, bundle chunker.SemanticBundle) string {
	var sb strings.Builder
	sb.WriteString("# Role\n你是一个代码安全漏洞猎手 (Vulnerability Hunter)。你的唯一目标是挖掘当前代码分片中所有真实、可疑的严重缺陷与漏洞。\n\n")

	if len(bundle.MacroContext) > 0 {
		sb.WriteString("## 构建宏环境定义 (Build Macros Context)\n")
		for k, v := range bundle.MacroContext {
			sb.WriteString(fmt.Sprintf("- `%s = %s`\n", k, v))
		}
		sb.WriteString("\n")
	}

	if bundle.HeaderOutline != "" {
		sb.WriteString("## 核心头文件大纲声明 (Header Outline)\n```cpp\n")
		sb.WriteString(bundle.HeaderOutline)
		sb.WriteString("\n```\n\n")
	}

	if len(bundle.NegativeRules) > 0 {
		sb.WriteString("## 历史负样本与例外规则 (False Positive / Negative Rules)\n")
		sb.WriteString("以下模式已在历史审计中确认安全或被研发标记为免扫，切勿针对它们误报：\n")
		for _, r := range bundle.NegativeRules {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## 待检视文件列表\n")
	for _, f := range bundle.AllFiles {
		sb.WriteString(fmt.Sprintf("- `%s`\n", f))
	}
	sb.WriteString("\n## 任务输出格式规范\n必须以合法纯 JSON 格式输出，不得包裹 Markdown 标记代码块：\n")
	sb.WriteString(`{
  "candidates": [
    {
      "candidate_id": "H-001",
      "file_path": "src/example.cc",
      "line_range": "42-50",
      "trigger_line": "*ptr = 100;",
      "scope_symbol": "ExampleClass::doSomething",
      "category": "内存管理问题-空指针解引用",
      "title": "ptr 指针解引用前未判空导致 coredump",
      "code_snippet": "...",
      "attack_hypothesis": "当输入参数为空时直接解引用发生崩溃",
      "suspected_trigger": "传入非法指针"
    }
  ],
  "summary": "初筛发现 1 个候选漏洞"
}`)

	return sb.String()
}

// buildChallengerPrompt 组装辩护人 Prompt
func buildChallengerPrompt(ctx *engines.EngineContext, bundle chunker.SemanticBundle, hunterOut *HunterOutput) string {
	var sb strings.Builder
	sb.WriteString("# Role\n你是一个严苛的代码安全辩护人 (Security Challenger)。你的使命是保护研发代码，找出猎手初筛缺陷中的误报与站不住脚的论点。\n\n")

	sb.WriteString("## 猎手提出的候选缺陷清单\n```json\n")
	candBytes, _ := json.MarshalIndent(hunterOut.Candidates, "", "  ")
	sb.Write(candBytes)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## 辩护维度\n请从以下角度进行辩护（若确实有漏洞则如实报告 CHALLENGE_FAILED）：\n")
	sb.WriteString("1. Guards（前置判空守卫或断言保证安全）\n2. MacroIsolation（受非默认宏隔离保护）\n3. Architecture（业务上下文或生命周期保证绝不可能触发）\n\n")

	sb.WriteString("## 输出格式规范\n必须以合法纯 JSON 格式输出：\n")
	sb.WriteString(`{
  "defense_cases": [
    {
      "candidate_id": "H-001",
      "defense_verdict": "DEFENSE_SUCCESSFUL",
      "defense_arguments": [
        {"dimension": "Guards", "finding": "第 38 行已做 ASSERT 判空，不可达"}
      ],
      "mitigating_factors": "前置断言保护",
      "counter_evidence_snippet": "assert(ptr != nullptr);"
    }
  ],
  "summary": "成功辩护 1 处误报"
}`)

	return sb.String()
}

// buildJudgePrompt 组装法官 Prompt
func buildJudgePrompt(ctx *engines.EngineContext, bundle chunker.SemanticBundle, hunterOut *HunterOutput, challOut *ChallengerOutput) string {
	var sb strings.Builder
	sb.WriteString("# Role\n你是一个资深的最高仲裁法官 (Security Judge)。你需要基于猎手控告和辩护人论点，独立研判源码事实，做出终审裁决。\n\n")

	sb.WriteString("## 控辩双方材料\n### 猎手初筛清单:\n```json\n")
	hBytes, _ := json.MarshalIndent(hunterOut.Candidates, "", "  ")
	sb.Write(hBytes)
	sb.WriteString("\n```\n\n### 辩护人对抗意见:\n```json\n")
	cBytes, _ := json.MarshalIndent(challOut.DefenseCases, "", "  ")
	sb.Write(cBytes)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## 终审裁决规范 (Verdict Options)\n- CONFIRMED: 漏洞确凿无误\n- REJECTED: 确系误报，予以驳回\n- CONDITIONAL: 条件性触发（如依赖特殊宏开启或极端配置）\n\n")

	sb.WriteString("## 输出格式规范\n必须以合法纯 JSON 格式输出：\n")
	sb.WriteString(`{
  "final_verdicts": [
    {
      "candidate_id": "H-001",
      "verdict": "CONFIRMED",
      "severity_preliminary": "严重",
      "category": "内存管理问题-空指针解引用",
      "file_path": "src/example.cc",
      "line_number": "42-50",
      "trigger_line": "*ptr = 100;",
      "scope_symbol": "ExampleClass::doSomething",
      "title": "ptr 指针解引用前未判空导致 coredump",
      "judgement_rationale": "【综合裁决】: 确认存在空指针解引用风险。辩护人提出的 assert 在 Release 编译下失效，且上游缺乏显式空指针校验。",
      "code_snippet": "...",
      "suggestion": "在解引用前添加 if (ptr == nullptr) return -1; 严格判空守卫"
    }
  ]
}`)

	return sb.String()
}

// callAITier 底层调用大模型驱动
func callAITier(ctx context.Context, backend string, modelName string, prompt string, workDir string, outPath string, timeoutSeconds int, workCtx ...*invoker.LLMWorkContext) (string, int64, error) {
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

	req := invoker.AIRequest{
		ParentContext:  ctx,
		WorkDir:        workDir,
		PromptMsg:      prompt,
		OutputPath:     outputPath,
		TimeoutMin:     timeoutMin,
		ModelName:      modelName,
		ResponseFormat: "json",
		WorkContext:    selectedWorkCtx,
	}

	if err := wrappedInv.Invoke(req); err != nil {
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

// parseJSONFromAIOutput 清洗并解析大模型输出的 JSON
func parseJSONFromAIOutput(rawOutput string, v interface{}, workDir string) error {
	cleaned := cleanJSONOutput([]byte(rawOutput))
	if err := json.Unmarshal(cleaned, v); err == nil {
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
