package services

import (
	"code-shield/models"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

	cfg := ChunkConfig{MaxFiles: 20, Depth: 1, Concurrency: 6}
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 6
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

			// 保存辩论日志到数据库
			if len(debateLogs) > 0 && models.DB != nil {
				for _, dl := range debateLogs {
					dl.TaskReportID = ctx.report.ID
					models.DB.Create(&dl)
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
	enrichedFindings, diffErr := DiffAndEnrichFindings(ctx.repo.ID, ctx.report.ID, ctx.taskType.ID, scannedFiles, allFindings)
	if diffErr == nil && len(enrichedFindings) > 0 {
		allFindings = enrichedFindings
	}

	log.Printf("[DebateEngine] Synthesis starting with %d vetted & calibrated findings\n", len(allFindings))
	ctx.findings = allFindings
	return ctx.executeSynthesis(allFindings)
}

// ProcessBundle 对单个分片执行完整的智能体协作流
func (e *DebateEngine) ProcessBundle(ctx *taskContext, bundle SemanticBundle, chunkIdx int, chunkDir string) ([]models.AnalysisFinding, []models.TaskDebateLog, error) {
	startTime := time.Now()

	// ── 步骤 1: 调度 Tier 1 快模型执行 Hunter 初筛 ──
	hunterOut, hunterTokens, err := e.runHunterStage(ctx, bundle)
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
			fastFindings = append(fastFindings, models.AnalysisFinding{
				FilePath:    c.FilePath,
				LineNumber:  c.LineRange,
				TriggerLine: c.TriggerLine,
				ScopeSymbol: c.ScopeSymbol,
				CodeSnippet: c.CodeSnippet,
				Category:    c.CWECategory,
				Title:       c.AttackHypothesis,
				Detail:      c.SuspectedTrigger,
				HunterClaim: c.AttackHypothesis,
				CreatedAt:   time.Now(),
			})
		}
		return fastFindings, nil, nil
	}

	// 单分片候选数上限限流截断 (防异常爆炸，最多取 Top 20 核心疑点辩论)
	maxCap := 20
	if models.AppConfig.AI.Debate.MaxCandidatesPerChunk > 0 {
		maxCap = models.AppConfig.AI.Debate.MaxCandidatesPerChunk
	}
	if len(hunterOut.Candidates) > maxCap {
		log.Printf("[DebateEngine] Bundle [%s]: Truncating %d candidates to top %d for debate",
			bundle.Name, len(hunterOut.Candidates), maxCap)
		hunterOut.Candidates = hunterOut.Candidates[:maxCap]
	}

	// ── 步骤 2: 调度 Tier 2 强推理模型执行 Challenger 对抗辩护 ──
	challengerOut, challTokens, err := e.runChallengerStage(ctx, bundle, hunterOut)
	if err != nil {
		log.Printf("[DebateEngine] Warning: Challenger failed (%v), proceeding with degraded Challenger note", err)
		challengerOut = &ChallengerOutput{
			Summary: "[Challenger Degraded: 辩护人服务暂时超时，请法官独立基于源码客观裁决]",
		}
	}

	// ── 步骤 3: 调度 Tier 2 强推理模型执行 Judge 终审仲裁 ──
	judgeOut, judgeTokens, err := e.runJudgeStage(ctx, bundle, hunterOut, challengerOut)
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

			finding := models.AnalysisFinding{
				FilePath:      jv.FilePath,
				LineNumber:    jv.LineNumber,
				TriggerLine:   jv.TriggerLine,
				ScopeSymbol:   jv.ScopeSymbol,
				CodeSnippet:   jv.CodeSnippet,
				Category:      jv.Category,
				Title:         jv.Title,
				Detail:        fmt.Sprintf("%s\n\n【仲裁法官裁决词】: %s", jv.Title, jv.JudgementRationale),
				Suggestion:    jv.Suggestion,
				HunterClaim:   origCand.AttackHypothesis,
				ChallengerArg: challArgText,
				JudgeVerdict:  jv.JudgementRationale,
				CreatedAt:     time.Now(),
			}
			confirmedFindings = append(confirmedFindings, finding)
		} else {
			log.Printf("[DebateEngine] Candidate %s REJECTED: %s (Reason: %s)\n",
				jv.CandidateID, jv.Title, jv.JudgementRationale)
		}
	}

	return confirmedFindings, debateLogs, nil
}

// runHunterStage 运行猎手初筛阶段 (Tier 1 快模型)
func (e *DebateEngine) runHunterStage(ctx *taskContext, bundle SemanticBundle) (*HunterOutput, int64, error) {
	router := GetTierRouter()
	acq, err := router.AcquireTier(ctx.ctx, "tier1_fast", ctx.taskType.TierFastBackend)
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	prompt := buildHunterPrompt(ctx, bundle)
	tierCfg := models.AppConfig.GetTierConfig("tier1_fast")
	rawOutput, tokens, err := callAITier(ctx.ctx, acq.Backend, acq.ModelName, prompt, ctx.codesPath, tierCfg.TimeoutSeconds)
	if err != nil {
		return nil, tokens, err
	}

	var hunterOut HunterOutput
	if parseErr := parseJSONFromAIOutput(rawOutput, &hunterOut); parseErr != nil {
		// 容错: 尝试从非结构化文本中提取基本候选
		hunterOut.Summary = rawOutput
	}

	return &hunterOut, tokens, nil
}

// runChallengerStage 运行辩护人对抗阶段 (Tier 2 强推理模型)
func (e *DebateEngine) runChallengerStage(ctx *taskContext, bundle SemanticBundle, hunterOut *HunterOutput) (*ChallengerOutput, int64, error) {
	router := GetTierRouter()
	acq, err := router.AcquireTier(ctx.ctx, "tier2_reasoning", ctx.taskType.TierReasoningBackend)
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	prompt := buildChallengerPrompt(ctx, bundle, hunterOut)
	tierCfg := models.AppConfig.GetTierConfig("tier2_reasoning")
	rawOutput, tokens, err := callAITier(ctx.ctx, acq.Backend, acq.ModelName, prompt, ctx.codesPath, tierCfg.TimeoutSeconds)
	if err != nil {
		return nil, tokens, err
	}

	var challOut ChallengerOutput
	if parseErr := parseJSONFromAIOutput(rawOutput, &challOut); parseErr != nil {
		challOut.Summary = rawOutput
	}
	return &challOut, tokens, nil
}

// runJudgeStage 运行终审法官阶段 (Tier 2 强推理模型)
func (e *DebateEngine) runJudgeStage(ctx *taskContext, bundle SemanticBundle, hunterOut *HunterOutput, challengerOut *ChallengerOutput) (*JudgeOutput, int64, error) {
	router := GetTierRouter()
	acq, err := router.AcquireTier(ctx.ctx, "tier2_reasoning", ctx.taskType.TierReasoningBackend)
	if err != nil {
		return nil, 0, err
	}
	defer acq.Release()

	prompt := buildJudgePrompt(ctx, bundle, hunterOut, challengerOut)
	tierCfg := models.AppConfig.GetTierConfig("tier2_reasoning")
	rawOutput, tokens, err := callAITier(ctx.ctx, acq.Backend, acq.ModelName, prompt, ctx.codesPath, tierCfg.TimeoutSeconds)
	if err != nil {
		return nil, tokens, err
	}

	var judgeOut JudgeOutput
	if parseErr := parseJSONFromAIOutput(rawOutput, &judgeOut); parseErr != nil {
		return nil, tokens, fmt.Errorf("failed to parse judge JSON: %w (raw: %s)", parseErr, rawOutput)
	}
	return &judgeOut, tokens, nil
}

// fallbackJudgeFromHunter 当法官或辩论失败时的兜底构造
func fallbackJudgeFromHunter(hunterOut *HunterOutput) *JudgeOutput {
	var verdicts []JudgeFinalVerdict
	for _, c := range hunterOut.Candidates {
		verdicts = append(verdicts, JudgeFinalVerdict{
			CandidateID:         c.CandidateID,
			Verdict:             models.DebateVerdictConfirmed,
			SeverityPreliminary: "严重",
			Category:            c.CWECategory,
			FilePath:            c.FilePath,
			LineNumber:          c.LineRange,
			TriggerLine:         c.TriggerLine,
			ScopeSymbol:         c.ScopeSymbol,
			Title:               c.AttackHypothesis,
			JudgementRationale:  "[Fallback: 智能体辩论超时，保留猎手原始判定]",
			CodeSnippet:         c.CodeSnippet,
			Suggestion:          "建议人工复核此可疑风险点",
		})
	}
	return &JudgeOutput{FinalVerdicts: verdicts}
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

	sb.WriteString("### 待分析文件列表:\n")
	for _, f := range bundle.AllFiles {
		sb.WriteString(fmt.Sprintf("- %s\n", f))
	}
	sb.WriteString("\n### 输出格式规范 (JSON Only):\n")
	sb.WriteString("```json\n{\n  \"candidates\": [\n    {\n      \"candidate_id\": \"H-001\",\n      \"file_path\": \"src/file.cc\",\n      \"line_range\": \"10-20\",\n      \"trigger_line\": \"c = *++it;\",\n      \"scope_symbol\": \"Class::Method\",\n      \"code_snippet\": \"...\",\n      \"cwe_category\": \"CWE-125: Out-of-bounds Read\",\n      \"attack_hypothesis\": \"漏洞成因与攻击假设\",\n      \"suspected_trigger\": \"触发输入条件\"\n    }\n  ],\n  \"summary\": \"概括\"\n}\n```\n")

	return sb.String()
}

func buildChallengerPrompt(ctx *taskContext, bundle SemanticBundle, hunterOut *HunterOutput) string {
	candJSON, _ := json.MarshalIndent(hunterOut.Candidates, "", "  ")

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
	hJSON, _ := json.MarshalIndent(hunterOut.Candidates, "", "  ")
	cJSON, _ := json.MarshalIndent(challOut.DefenseCases, "", "  ")

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
	sb.WriteString("### 输出格式规范 (JSON Only):\n")
	sb.WriteString("```json\n{\n  \"final_verdicts\": [\n    {\n      \"candidate_id\": \"H-001\",\n      \"verdict\": \"CONFIRMED\",\n      \"severity_preliminary\": \"严重\",\n      \"category\": \"内存管理问题-越界访问\",\n      \"file_path\": \"src/file.cc\",\n      \"line_number\": \"10-20\",\n      \"trigger_line\": \"c = *++it;\",\n      \"scope_symbol\": \"Class::Method\",\n      \"title\": \"精确漏洞标题\",\n      \"judgement_rationale\": \"【综合裁决】: ...\\n\\n【源码事实】: ...\\n\\n【实测验证】: ...\\n\\n【决定性证据】: ...\\n\\n【抗辩响应】: ...\\n\\n【缓和因素】: ...\",\n      \"code_snippet\": \"...\",\n      \"suggestion\": \"修复建议代码\"\n    }\n  ]\n}\n```\n")

	return sb.String()
}

// parseJSONFromAIOutput 从大模型可能包含 Markdown ```json 包裹的输出中精准解析 JSON
func parseJSONFromAIOutput(output string, target interface{}) error {
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
	return json.Unmarshal([]byte(trimmed), target)
}

// callAITier 底层调用指定后端和模型的 AI Invoker 工具
func callAITier(ctx context.Context, backend string, modelName string, prompt string, workDir string, timeoutSeconds int) (string, int64, error) {
	if backend == "" {
		backend = models.AppConfig.AI.Backend
	}

	invoker := GetAIInvoker(backend)
	if invoker == nil {
		return "", 0, fmt.Errorf("unsupported AI backend: %s", backend)
	}

	// 创建临时输出文件
	tmpFile, err := os.CreateTemp("", "debate-stage-output-*.json")
	if err != nil {
		return "", 0, err
	}
	outputPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(outputPath)

	timeoutMin := 30
	if timeoutSeconds > 0 {
		timeoutMin = (timeoutSeconds + 59) / 60
	} else if models.AppConfig.AI.Debate.StageTimeoutSeconds > 0 {
		timeoutMin = (models.AppConfig.AI.Debate.StageTimeoutSeconds + 59) / 60
	}

	req := AIRequest{
		ParentContext: ctx,
		WorkDir:       workDir,
		PromptMsg:     prompt,
		OutputPath:    outputPath,
		TimeoutMin:    timeoutMin,
		ModelName:     modelName,
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
