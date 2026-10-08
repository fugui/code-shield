package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/dispatcher"
	"code-shield/services/invoker"
)

// ExecuteSynthesis 驱动大模型综合报告合成阶段。P0 阶段只处理本轮观测，
// 不读取历史报告，也不执行跨轮生命周期裁决。
func ExecuteSynthesis(ctx *TaskContext, allFindings []models.AnalysisFinding, coverageOpt ...*coverage.Coverage) error {
	UpdateTaskStatus(ctx.Report.ID, models.StatusSynthesis)

	safeRepoName := strings.ReplaceAll(ctx.Repo.Name, "/", "-")
	reportDir := filepath.Dir(ctx.ReportPath)

	// 1. 将本轮 raw findings 只读落盘为不可变证据
	rawFindingsPath := filepath.Join(reportDir, fmt.Sprintf("report-%d-raw-findings.json", ctx.Report.ID))
	rawFindingsJSON, _ := json.MarshalIndent(allFindings, "", "  ")
	if err := os.WriteFile(rawFindingsPath, rawFindingsJSON, 0644); err != nil {
		log.Printf("[Synthesis] Warning: Failed to write raw findings: %v\n", err)
	}

	scanCoverage := effectiveCoverage(ctx, coverageOpt)
	fileManifestPath := filepath.Join(reportDir, fmt.Sprintf("report-%d-file-manifest.json", ctx.Report.ID))
	if manifestBytes, manifestErr := scanCoverage.MarshalPretty(); manifestErr == nil {
		if writeErr := os.WriteFile(fileManifestPath, manifestBytes, 0644); writeErr != nil {
			log.Printf("[Synthesis] Warning: failed to write file manifest: %v", writeErr)
		}
	}

	// 2. 记录本轮扫描覆盖事实，仅用于报告与后续新 ledger，不参与跨轮裁决。
	if models.DB != nil {
		coverageSummary := scanCoverage.Summary()
		_, _ = models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, map[string]interface{}{
			"coverage_complete":       coverageSummary.CoverageComplete,
			"coverage_degraded":       coverageSummary.CoverageDegraded,
			"coverage_not_applicable": coverageSummary.CoverageNotApplicable,
		})
	}

	// 3. 持久化本轮 findings 快照，供报告合成和后续新 ledger 使用。
	synthesisInputPath := filepath.Join(reportDir, fmt.Sprintf("report-%d-synthesis-%s.json", ctx.Report.ID, safeRepoName))
	findingsJSON, err := json.MarshalIndent(allFindings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal current findings: %w", err)
	}
	if err := os.WriteFile(synthesisInputPath, findingsJSON, 0644); err != nil {
		return fmt.Errorf("failed to write synthesis input: %w", err)
	}

	// 4. 本轮报告合成输入就是本轮观测，不做活动/归档筛选。
	activeItems := allFindings

	// 将归并后的全量活动条目同步回任务上下文，确保后续后处理评分基于归并后总数
	ctx.Findings = activeItems

	// 若活动条目为空，直接写入静态空报告快照
	if len(activeItems) == 0 {
		emptyReportMarkdown := fmt.Sprintf(`# Code-Shield 代码检视报告

## 一、检视结果概要

本次扫描已完成，未发现任何安全隐患或代码缺陷。

- **扫描仓库**: %s
- **任务类型**: %s
- **完成时间**: %s
- **综合得分**: 100分

## 二、发现的问题

本次分析未发现符合 %s 规则的缺陷。代码状态良好，符合安全规范。

## 三、优化建议

暂无。本模块相应规则评估合格，建议继续保持。
`, ctx.Repo.Name, ctx.TaskType.DisplayName, time.Now().Format("2006-01-02 15:04:05"), ctx.TaskType.DisplayName)

		if err := os.WriteFile(ctx.ReportPath, []byte(emptyReportMarkdown), 0644); err != nil {
			return fmt.Errorf("failed to write static empty report: %w", err)
		}

		log.Printf("[Synthesis] No findings detected in active ledger. Skipped LLM synthesis for ReportID %d", ctx.Report.ID)
		ctx.Summary.Synthesis.Status = "success"
		ctx.Summary.Synthesis.StartTime = time.Now()
		ctx.Summary.Synthesis.EndTime = time.Now()
		ctx.Summary.Synthesis.DurationSeconds = 0
		return nil
	}

	// 6. 统计严重度并生成精准注入的 Prompt 约束
	counts := map[string]int{
		"致命": 0, "fatal": 0, "blocker": 0, "阻塞": 0, "blocking": 0,
		"严重": 0, "critical": 0, "major_error": 0, "error": 0,
		"一般": 0, "minor": 0, "warning": 0, "主要": 0, "major": 0, "提示": 0, "info": 0, "hint": 0,
		"建议": 0, "suggestion": 0, "comment": 0,
		"合格": 0, "pass": 0,
		"高风险": 0, "high": 0, "high_risk": 0,
		"中风险": 0, "medium": 0, "medium_risk": 0,
		"低风险": 0, "low": 0, "low_risk": 0,
	}
	for _, f := range activeItems {
		counts[strings.ToLower(f.Severity)]++
	}

	fatalCount := counts["致命"] + counts["阻塞"] + counts["blocking"] + counts["fatal"] + counts["blocker"]
	criticalCount := counts["严重"] + counts["critical"] + counts["major_error"] + counts["error"]
	minorCount := counts["一般"] + counts["minor"] + counts["warning"] + counts["主要"] + counts["major"] + counts["提示"] + counts["info"] + counts["hint"]
	suggestionCount := counts["建议"] + counts["suggestion"] + counts["comment"]

	suffixPrompt := fmt.Sprintf("【重要硬性指标约束（必须严格遵守）】：为了确保报告的统计数据100%%精确，请不要根据输入的 JSON 数量进行统计，而**必须**将以下精确的统计结果原封不动地输出在报告的『一、检视结果概要』章节中：\n```\n## 检视结果概要\n\n致命：%d，严重：%d，一般：%d，建议：%d\n```",
		fatalCount, criticalCount, minorCount, suggestionCount)

	summaryFindingCount := len(activeItems) - tier4DetailedFindings
	if summaryFindingCount < 0 {
		summaryFindingCount = 0
	}
	summaryRowCount := summaryFindingCount
	if summaryRowCount > tier4MaxSummaryRows {
		summaryRowCount = tier4MaxSummaryRows
	}
	suffixPrompt += fmt.Sprintf("\n\n【Tier4 输出预算硬约束（优先级高于任务提示词中的完整清单要求）】：本次输入采用受限汇总契约。只可为 detailed_findings 中的前 %d 条输出完整分析小节；其余 %d 条不完整展开，其中前 %d 条以受限摘要形式包含在 summary_findings 中，不要生成逐条表格、逐条清单或重复代码片段。不要输出 JSON 代码围栏。Markdown 正文控制在 %d 字节以内，并保留精确统计行。",
		tier4DetailedFindings, summaryFindingCount, summaryRowCount, tier4AIPromptOutputBytes)

	// 7. 排序并生成确定性报告。  The model call below is optional polish only:
	// the deterministic Markdown must always exist first so the task can be
	// delivered even when the remote LLM is slow or unavailable.
	severityWeight := map[string]int{
		"致命": 4, "fatal": 4, "blocker": 4, "阻塞": 4, "blocking": 4,
		"严重": 3, "critical": 3, "major_error": 3, "error": 3,
		"一般": 2, "minor": 2, "warning": 2, "主要": 2, "major": 2, "提示": 2, "info": 2, "hint": 2,
		"建议": 1, "suggestion": 1, "comment": 1,
		"合格": 0, "pass": 0,
		"高风险": 4, "high": 4, "high_risk": 4,
		"中风险": 2, "medium": 2, "medium_risk": 2,
		"低风险": 1, "low": 1, "low_risk": 1,
	}

	sortedFindings := make([]models.AnalysisFinding, len(activeItems))
	copy(sortedFindings, activeItems)
	sort.Slice(sortedFindings, func(i, j int) bool {
		wI := severityWeight[strings.ToLower(sortedFindings[i].Severity)]
		wJ := severityWeight[strings.ToLower(sortedFindings[j].Severity)]
		if wI != wJ {
			return wI > wJ
		}
		return sortedFindings[i].FilePath < sortedFindings[j].FilePath
	})

	deterministicReport, renderErr := RenderTier4Report(ctx, sortedFindings, scanCoverage)
	if renderErr != nil {
		return fmt.Errorf("render deterministic tier4 report: %w", renderErr)
	}
	if err := writeTier4Report(ctx.ReportPath, deterministicReport); err != nil {
		return fmt.Errorf("write deterministic tier4 report: %w", err)
	}

	// 8. Serialize the bounded, structured AI payload.  Detailed findings are
	// capped to ten and every remaining finding is reduced to identity fields.
	aiFindingsJSON, inputErr := buildTier4AIInput(ctx, sortedFindings, scanCoverage)
	if inputErr != nil {
		return fmt.Errorf("build tier4 AI input: %w", inputErr)
	}
	if len(aiFindingsJSON) > tier4MaxAIInputBytes {
		log.Printf("[Synthesis] Tier4 AI input is %d bytes (limit %d); using deterministic report without AI polish for ReportID %d\n",
			len(aiFindingsJSON), tier4MaxAIInputBytes, ctx.Report.ID)
		ctx.Summary.Synthesis.Status = "skipped"
		ctx.Summary.Synthesis.StartTime = time.Now()
		ctx.Summary.Synthesis.EndTime = time.Now()
		ctx.Summary.Synthesis.DurationSeconds = ctx.Summary.Synthesis.EndTime.Sub(ctx.Summary.Synthesis.StartTime).Seconds()
		ctx.Summary.Synthesis.ErrorMessage = fmt.Sprintf("AI polish skipped: INPUT_LIMIT_EXCEEDED (%d bytes)", len(aiFindingsJSON))
		return nil
	}
	synthesisAIInputPath := filepath.Join(reportDir, fmt.Sprintf("report-%d-synthesis-%s-for-ai.json", ctx.Report.ID, safeRepoName))
	if err := os.WriteFile(synthesisAIInputPath, aiFindingsJSON, 0644); err != nil {
		return fmt.Errorf("failed to write AI synthesis input: %w", err)
	}
	defer os.Remove(synthesisAIInputPath)

	synthStart := time.Now()
	lastErr := ExecuteSynthesisOnce(ctx, synthesisAIInputPath, suffixPrompt)
	if lastErr == nil {
		log.Printf("[Synthesis] Synthesis phase complete for ReportID %d\n", ctx.Report.ID)
		ctx.Summary.Synthesis.Status = "success"
		ctx.Summary.Synthesis.StartTime = synthStart
		ctx.Summary.Synthesis.EndTime = time.Now()
		ctx.Summary.Synthesis.DurationSeconds = ctx.Summary.Synthesis.EndTime.Sub(synthStart).Seconds()
		return nil
	}
	ctx.Summary.Synthesis.Status = "failed"
	ctx.Summary.Synthesis.StartTime = synthStart
	ctx.Summary.Synthesis.EndTime = time.Now()
	ctx.Summary.Synthesis.DurationSeconds = ctx.Summary.Synthesis.EndTime.Sub(synthStart).Seconds()
	ctx.Summary.Synthesis.ErrorMessage = lastErr.Error()
	// A model polish failure must not invalidate the complete deterministic
	// report.  Regenerate it because ExecuteSynthesisOnce cleans the output
	// path before each remote attempt.
	if fallbackErr := writeTier4Report(ctx.ReportPath, deterministicReport); fallbackErr != nil {
		return fmt.Errorf("restore deterministic tier4 report: %w", fallbackErr)
	}
	ctx.Summary.Synthesis.Status = "degraded"
	ctx.Summary.Synthesis.ErrorMessage = fmt.Sprintf("AI polish failed; deterministic report used: %v", lastErr)
	log.Printf("[Synthesis] Tier4 AI polish failed for ReportID %d; delivering deterministic report: %v\n", ctx.Report.ID, lastErr)
	return nil
}

func effectiveCoverage(ctx *TaskContext, coverageOpt []*coverage.Coverage) *coverage.Coverage {
	for _, item := range coverageOpt {
		if item != nil {
			return item
		}
	}
	if ctx != nil && ctx.Coverage != nil {
		return ctx.Coverage
	}
	return &coverage.Coverage{
		PolicyVersion:   "v1",
		CommitHash:      ctx.Report.HeadCommit,
		ManifestMissing: true,
	}
}

// ExecuteSynthesisOnce 单次执行报告合成大模型调用
func ExecuteSynthesisOnce(ctx *TaskContext, synthesisInputPath string, suffixPrompt string) error {
	tierCfg := models.AppConfig.GetTierConfig("tier4_synthesis")
	stageParent := ctx.Ctx
	if stageParent == nil {
		stageParent = context.Background()
	}
	stageCtx := stageParent
	if tierCfg.TimeoutSeconds > 0 {
		stageCtxCancel, cancelStage := context.WithTimeout(stageParent, time.Duration(tierCfg.TimeoutSeconds)*time.Second)
		defer cancelStage()
		stageCtx = stageCtxCancel
	}

	promptMsg := "请基于以下 JSON 分析发现，生成综合 Markdown 报告"
	if suffixPrompt != "" {
		promptMsg += "\n\n" + suffixPrompt
	}

	absPrompt := models.AppConfig.GetAbsPath(ctx.TaskType.SynthesisPromptFile())
	workCtx := &invoker.LLMWorkContext{
		ReportID: ctx.Report.ID,
		RepoName: ctx.Repo.Name,
		TaskType: ctx.TaskType.DisplayName,
		Stage:    "Tier 4: 全仓态势汇总",
		SubTask:  "聚合分片发现并生成 Markdown 诊断报告",
		TierName: "tier4_synthesis",
	}

	timeoutMin := (tierCfg.TimeoutSeconds + 59) / 60
	if timeoutMin <= 0 {
		timeoutMin = ctx.TaskType.Timeout
	}

	baselineBytes := readSynthesisAuditBaseline(ctx.ReportPath)
	baselineSignature, baselineKnown := synthesisBusinessSignature(baselineBytes)
	audit := newSynthesisAuditMetrics(baselineBytes)
	var lastCandidate dispatcher.TierCandidate
	stats := &dispatcher.TierRecoveryStats{}
	_, _, _, callErr := dispatcher.RunTierInvocationWithRecovery(
		stageCtx,
		"tier4_synthesis",
		tierCfg.AttemptTimeoutSeconds,
		stats,
		func(callCtx context.Context, candidate dispatcher.TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error) {
			CleanSynthesisTempFiles(ctx.ReportPath)
			backend := candidate.Driver
			if backend == "" {
				backend = models.AppConfig.AI.Backend
			}
			modelName := candidate.Model
			if modelName == "" {
				modelName = tierCfg.Model
			}
			workCtx.ResourceID = candidate.ResourceID
			lastCandidate = candidate
			aiInv := GetAIInvoker(backend)
			req := invoker.AIRequest{
				ParentContext:           callCtx,
				WorkDir:                 ctx.CodesPath,
				PromptFile:              absPrompt,
				PromptMsg:               promptMsg,
				InputFiles:              []string{synthesisInputPath},
				OutputPath:              ctx.ReportPath,
				TimeoutMin:              (timeoutSeconds + 59) / 60,
				AttemptTimeoutSeconds:   tierCfg.AttemptTimeoutSeconds,
				FirstByteTimeoutSeconds: tierCfg.FirstByteTimeoutSeconds,
				IdleTimeoutSeconds:      tierCfg.IdleTimeoutSeconds,
				MaxOutputBytes:          tierCfg.MaxOutputBytes,
				ModelName:               modelName,
				ResponseFormat:          "text",
				Temperature:             models.AppConfig.DeterministicTemperature(),
				WorkContext:             workCtx,
				Metrics:                 metrics,
			}
			invokeErr := aiInv.Invoke(req)
			if invokeErr != nil {
				addSynthesisInvocationAttempt(audit, "", 0, invokeErr)
				return "", 0, invokeErr
			}
			attemptBytes, readErr := os.ReadFile(ctx.ReportPath)
			if readErr != nil {
				attemptErr := fmt.Errorf("read synthesis attempt output: %w", readErr)
				addSynthesisInvocationAttempt(audit, "", 0, attemptErr)
				return "", 0, attemptErr
			}
			attemptRaw := string(attemptBytes)
			attemptTokens := int64((len(promptMsg) + len(attemptRaw)) / 4)
			addSynthesisInvocationAttempt(audit, attemptRaw, attemptTokens, nil)
			return attemptRaw, attemptTokens, nil
		},
	)
	applySynthesisRecoveryStats(ctx, stats)
	audit.Driver = lastCandidate.Driver
	audit.ResourceID = lastCandidate.ResourceID
	if callErr != nil {
		if len(audit.RepairAttempts) == 0 {
			addSynthesisInvocationAttempt(audit, "", 0, callErr)
		}
		addSynthesisAuditIssue(audit, "artifact", callErr.Error(), false)
		audit.RepairDriftUnchecked = true
		audit.SetFinalStatus("failed")
		persistSynthesisRepairAudit(stageCtx, ctx.Report.ID, ctx.Report.RepoID, ctx.Report.TaskTypeID, audit)
		return callErr
	}
	log.Printf("[Synthesis] Invoked Synthesis driver=%s resource=%s report=%d attempts=%d\n",
		lastCandidate.Driver, lastCandidate.ResourceID, ctx.Report.ID, len(audit.RepairAttempts))

	reportBytes, err := os.ReadFile(ctx.ReportPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("report file %s was not generated", ctx.ReportPath)
		}
		return fmt.Errorf("failed to read report file: %w", err)
	}
	if len(bytes.TrimSpace(reportBytes)) == 0 {
		emptyErr := fmt.Errorf("generated report file is empty")
		acceptLastSynthesisAttempt(audit, baselineSignature, baselineKnown, false, emptyErr)
		addSynthesisAuditIssue(audit, "artifact", emptyErr.Error(), false)
		audit.SetFinalStatus("failed")
		persistSynthesisRepairAudit(stageCtx, ctx.Report.ID, ctx.Report.RepoID, ctx.Report.TaskTypeID, audit)
		return emptyErr
	}
	if err := ValidateTier4AIReport(reportBytes, synthesisInputPath); err != nil {
		validationErr := fmt.Errorf("validate tier4 AI report: %w", err)
		acceptLastSynthesisAttempt(audit, baselineSignature, baselineKnown, false, validationErr)
		addSynthesisAuditIssue(audit, "artifact", validationErr.Error(), false)
		audit.SetFinalStatus("failed")
		persistSynthesisRepairAudit(stageCtx, ctx.Report.ID, ctx.Report.RepoID, ctx.Report.TaskTypeID, audit)
		return validationErr
	}

	acceptLastSynthesisAttempt(audit, baselineSignature, baselineKnown, true, nil)
	cleanedReport := SanitizeMarkdownReport(reportBytes)
	evaluateSynthesisAttemptDrift(audit, baselineSignature, baselineKnown, cleanedReport)
	if !bytes.Equal(cleanedReport, reportBytes) {
		audit.AddLocalRepair(cleanedReport, true, nil)
		if writeErr := os.WriteFile(ctx.ReportPath, cleanedReport, 0644); writeErr != nil {
			log.Printf("[Synthesis] Warning: failed to save sanitized markdown report: %v\n", writeErr)
		}
	}

	audit.SetFinalStatus("success")
	persistSynthesisRepairAudit(stageCtx, ctx.Report.ID, ctx.Report.RepoID, ctx.Report.TaskTypeID, audit)
	return nil
}

func applySynthesisRecoveryStats(ctx *TaskContext, stats *dispatcher.TierRecoveryStats) {
	if stats == nil {
		return
	}
	ctx.Summary.Synthesis.Attempts = len(stats.ErrorClasses)
	ctx.Summary.Synthesis.ResourceID = stats.LastResourceID
	ctx.Summary.Synthesis.ResourceFailovers = stats.ResourceFailovers
	ctx.Summary.Synthesis.DriverFailovers = stats.DriverFailovers
	ctx.Summary.Synthesis.ResourceChain = stats.ResourceChain
	ctx.Summary.Synthesis.ErrorClasses = stats.ErrorClasses
	ctx.Summary.Synthesis.QueueWaitMS = stats.QueueWaitMS
	ctx.Summary.Synthesis.AttemptDurationSeconds = stats.DurationSeconds
}
