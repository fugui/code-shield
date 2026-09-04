package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/dispatcher"
	"code-shield/services/invoker"
	"code-shield/services/reconciliation"
)

// ExecuteSynthesis 驱动大模型综合报告合成阶段（包含 R2R 对账关联与预算截断）
func ExecuteSynthesis(ctx *TaskContext, allFindings []models.AnalysisFinding) error {
	UpdateTaskStatus(ctx.Report.ID, models.StatusSynthesis)

	safeRepoName := strings.ReplaceAll(ctx.Repo.Name, "/", "-")
	reportDir := filepath.Dir(ctx.ReportPath)

	// 1. 将本轮 raw findings 只读落盘为不可变证据
	rawFindingsPath := filepath.Join(reportDir, fmt.Sprintf("report-%d-raw-findings.json", ctx.Report.ID))
	rawFindingsJSON, _ := json.MarshalIndent(allFindings, "", "  ")
	if err := os.WriteFile(rawFindingsPath, rawFindingsJSON, 0644); err != nil {
		log.Printf("[Synthesis] Warning: Failed to write raw findings: %v\n", err)
	}

	// 2. 动态发现同仓同任务基线报告 (用于 R2R 报告间系统化对账)
	var baseReport models.TaskReport
	var baseSynthesisBytes []byte
	var hasBaseReport bool
	if models.DB != nil {
		err := models.DB.Where("repo_id = ? AND task_type_id = ? AND id < ? AND status = ?",
			ctx.Repo.ID, ctx.TaskType.ID, ctx.Report.ID, models.StatusSuccess).
			Order("id desc").First(&baseReport).Error
		if err == nil && baseReport.ID > 0 {
			hasBaseReport = true
			basePath := baseReport.GetSynthesisJSONPath()
			if bBytes, bErr := os.ReadFile(basePath); bErr == nil {
				baseSynthesisBytes = bBytes
			}
		}
	}

	// 3. 执行纯函数报告间对账 (R2R Reconciliation)
	repoUnchanged := false
	if hasBaseReport && baseReport.HeadCommit != "" && ctx.Report.HeadCommit != "" {
		repoUnchanged = (baseReport.HeadCommit == ctx.Report.HeadCommit)
	}

	reconReq := &reconciliation.ReconcileRequest{
		RepoID:            ctx.Repo.ID,
		TaskTypeID:        ctx.TaskType.ID,
		TaskName:          ctx.TaskType.Name,
		CurrentReportID:   ctx.Report.ID,
		BaseReportID:      baseReport.ID,
		CurrentFindings:   allFindings,
		BaseSynthesisJSON: baseSynthesisBytes,
		RepoRoot:          ctx.CodesPath,
		BaseCommit:        baseReport.HeadCommit,
		HeadCommit:        ctx.Report.HeadCommit,
		RepoUnchanged:     repoUnchanged,
		GovernanceMode:    ctx.TaskType.GovernanceMode,
	}

	reconResult, reconErr := reconciliation.Reconcile(reconReq)
	if reconErr != nil {
		log.Printf("[Synthesis] Warning: Reconciliation failed, using raw findings: %v\n", reconErr)
	}

	// 4. 持久化完整问题台账 SSOT 与对账明细
	synthesisInputPath := filepath.Join(reportDir, fmt.Sprintf("report-%d-synthesis-%s.json", ctx.Report.ID, safeRepoName))
	if reconResult != nil {
		ledgerJSON, _ := json.MarshalIndent(reconResult.Ledger, "", "  ")
		if err := os.WriteFile(synthesisInputPath, ledgerJSON, 0644); err != nil {
			return fmt.Errorf("failed to write synthesis input: %w", err)
		}

		if hasBaseReport {
			reconPath := filepath.Join(reportDir, fmt.Sprintf("recon-%d-vs-%d.json", ctx.Report.ID, baseReport.ID))
			diffJSON, _ := json.MarshalIndent(reconResult.DiffPayload, "", "  ")
			_ = os.WriteFile(reconPath, diffJSON, 0644)
		}

		// 持久化到 DB
		if models.DB != nil && hasBaseReport {
			_ = models.DB.Create(&reconResult.Reconciliation).Error
			if reconResult.Reconciliation.ID > 0 && len(reconResult.Links) > 0 {
				for i := range reconResult.Links {
					reconResult.Links[i].ReconID = reconResult.Reconciliation.ID
				}
				_ = models.DB.Create(&reconResult.Links).Error
			}
			if len(reconResult.ResolvedByChange) > 0 {
				for _, rf := range reconResult.ResolvedByChange {
					_ = models.DB.Model(&models.DefectFingerprintRecord{}).
						Where("repo_id = ? AND task_type_id = ? AND fingerprint = ?", ctx.Repo.ID, ctx.TaskType.ID, rf.Fingerprint).
						Updates(map[string]interface{}{
							"status":             models.DiffStatusResolved,
							"resolved_at":        time.Now(),
							"resolved_diff_hunk": ctx.Report.HeadCommit,
						}).Error
				}
			}
		}
	} else {
		findingsJSON, _ := json.MarshalIndent(allFindings, "", "  ")
		if err := os.WriteFile(synthesisInputPath, findingsJSON, 0644); err != nil {
			return fmt.Errorf("failed to write synthesis input: %w", err)
		}
	}

	// 5. 提取供 AI 合成的活动条目集合
	var activeItems []models.AnalysisFinding
	if reconResult != nil && len(reconResult.Ledger.Items) > 0 {
		for _, it := range reconResult.Ledger.Items {
			f := it.Payload
			f.DiffStatus = it.DiffStatus
			if it.CoverageGap {
				f.DiffStatus = "COVERAGE_GAP"
			}
			activeItems = append(activeItems, f)
		}
	} else {
		activeItems = allFindings
	}

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

	if reconResult != nil && hasBaseReport {
		if reconResult.Ledger.Meta.GovernanceMode == models.GovernanceModeChangeFocus {
			suffixPrompt += fmt.Sprintf("\n\n【本次变更质量结论】：本次为变更增量卡点检视。涉及变动文件：%d 个，本次引入新缺陷：%d 条，顺带实质修复历史存量：%d 条。请在报告第一节清晰呈现变更质量结论！",
				len(reconResult.Ledger.Meta.ChangedFiles), reconResult.Ledger.Meta.NewIntroducedCount, reconResult.Ledger.Meta.ResolvedHistoryCount)
		} else {
			suffixPrompt += fmt.Sprintf("\n\n【跨轮对账与增量治理概要】：相比上一轮基线（报告 ID: %d），本次对账结论如下：真正新增缺陷 %d 条，确认存量缺陷 %d 条，本轮未复现覆盖缺口 %d 条（非代码修复，仍需持续关注），跨组件模板族 %d 处。请在报告中为相应问题条目标注对账徽标（如 [NEW]、[EXISTED]、[COVERAGE_GAP] 等）并在跨轮治理结论中体现！",
				baseReport.ID, reconResult.Reconciliation.NewCount, reconResult.Reconciliation.ExistedCount, reconResult.Reconciliation.VanishedCoverageGap, reconResult.Reconciliation.TemplateFamilyCount)
		}
	}

	// 7. 排序并执行 Top 60 截断保护 AI 上下文
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

	const maxFullDetailThreshold = 25
	const maxFindingsCap = 60
	if len(sortedFindings) > maxFindingsCap {
		log.Printf("[Synthesis] Active findings count %d exceeds cap %d. Truncating to top %d.\n",
			len(sortedFindings), maxFindingsCap, maxFindingsCap)
		sortedFindings = sortedFindings[:maxFindingsCap]
	}

	var aiFindings []models.AnalysisFinding
	if len(sortedFindings) <= maxFullDetailThreshold {
		aiFindings = sortedFindings
	} else {
		log.Printf("[Synthesis] Findings count %d exceeds %d. Generating simplified payload for AI synthesis...\n", len(sortedFindings), maxFullDetailThreshold)
		for i, f := range sortedFindings {
			if i < maxFullDetailThreshold {
				aiFindings = append(aiFindings, f)
			} else {
				f.CodeSnippet = ""
				f.Detail = "详细内容请查阅随附的完整版 JSON 发现清单附件。"
				f.Suggestion = "请查阅完整清单文件获取本项的具体修改建议。"
				aiFindings = append(aiFindings, f)
			}
		}
		suffixPrompt += fmt.Sprintf("\n\n此外，本次分析发现的问题总数较多（共 %d 处）。为了精炼报告，我们在输入的 JSON 中对排在第 %d 位以后的次要或低风险发现进行了简化。在生成『三、发现的问题』章节时，请仅对前 %d 个高风险问题进行详细罗列展示；对于其余的简化问题，请按照文件、分类或影响进行归聚归集，切勿逐个平铺列出。",
			len(sortedFindings), maxFullDetailThreshold+1, maxFullDetailThreshold)
	}

	// 8. 序列化简化后的 findings 为临时文件供 AI 输入
	aiFindingsJSON, _ := json.MarshalIndent(aiFindings, "", "  ")
	synthesisAIInputPath := filepath.Join(reportDir, fmt.Sprintf("report-%d-synthesis-%s-for-ai.json", ctx.Report.ID, safeRepoName))
	if err := os.WriteFile(synthesisAIInputPath, aiFindingsJSON, 0644); err != nil {
		return fmt.Errorf("failed to write AI synthesis input: %w", err)
	}
	defer os.Remove(synthesisAIInputPath)

	synthStart := time.Now()
	var lastErr error
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		ctx.Summary.Synthesis.Attempts = attempt + 1
		if attempt > 0 {
			log.Printf("[Synthesis] executeSynthesis failed (attempt %d/%d) for ReportID %d, retrying in %ds: %v\n",
				attempt, maxRetries, ctx.Report.ID, attempt*2, lastErr)
			time.Sleep(time.Duration(attempt*2) * time.Second)

			CleanSynthesisTempFiles(ctx.ReportPath)
		}

		err := ExecuteSynthesisOnce(ctx, synthesisAIInputPath, suffixPrompt)
		if err == nil {
			log.Printf("[Synthesis] Synthesis phase complete for ReportID %d\n", ctx.Report.ID)
			ctx.Summary.Synthesis.Status = "success"
			ctx.Summary.Synthesis.StartTime = synthStart
			ctx.Summary.Synthesis.EndTime = time.Now()
			ctx.Summary.Synthesis.DurationSeconds = ctx.Summary.Synthesis.EndTime.Sub(synthStart).Seconds()
			return nil
		}
		lastErr = err
	}

	ctx.Summary.Synthesis.Status = "failed"
	ctx.Summary.Synthesis.StartTime = synthStart
	ctx.Summary.Synthesis.EndTime = time.Now()
	ctx.Summary.Synthesis.DurationSeconds = ctx.Summary.Synthesis.EndTime.Sub(synthStart).Seconds()
	ctx.Summary.Synthesis.ErrorMessage = lastErr.Error()
	return fmt.Errorf("synthesis failed after %d retries: %w", maxRetries, lastErr)
}

// ExecuteSynthesisOnce 单次执行报告合成大模型调用
func ExecuteSynthesisOnce(ctx *TaskContext, synthesisInputPath string, suffixPrompt string) error {
	router := dispatcher.GetTierRouter()
	acq, err := router.AcquireTier(ctx.Ctx, "tier3_synthesis", "")
	if err != nil {
		return fmt.Errorf("failed to acquire tier3_synthesis compute resource: %w", err)
	}
	defer acq.Release()

	backend := acq.Backend
	modelName := acq.ModelName
	if backend == "" {
		backend = models.AppConfig.AI.Backend
	}

	tierCfg := models.AppConfig.GetTierConfig("tier3_synthesis")
	if modelName == "" {
		modelName = tierCfg.Model
	}

	promptMsg := "请基于以下 JSON 分析发现，生成综合 Markdown 报告"
	if suffixPrompt != "" {
		promptMsg += "\n\n" + suffixPrompt
	}

	absPrompt := models.AppConfig.GetAbsPath(ctx.TaskType.SynthesisPromptFile())
	aiInv := GetAIInvoker(backend)
	log.Printf("[Synthesis] Invoking Synthesis via %s (Model: %s, ReportID: %d, Output: %s)\n",
		aiInv.Name(), modelName, ctx.Report.ID, ctx.ReportPath)

	timeoutMin := ctx.TaskType.Timeout
	if tierCfg.TimeoutSeconds > 0 {
		timeoutMin = (tierCfg.TimeoutSeconds + 59) / 60
	}

	req := invoker.AIRequest{
		ParentContext:  ctx.Ctx,
		WorkDir:        ctx.CodesPath,
		PromptFile:     absPrompt,
		PromptMsg:      promptMsg,
		InputFiles:     []string{synthesisInputPath},
		OutputPath:     ctx.ReportPath,
		TimeoutMin:     timeoutMin,
		ModelName:      tierCfg.Model,
		ResponseFormat: "text",
		WorkContext: &invoker.LLMWorkContext{
			ReportID: ctx.Report.ID,
			RepoName: ctx.Repo.Name,
			TaskType: ctx.TaskType.DisplayName,
			Stage:    "Tier 3: 全仓态势汇总",
			SubTask:  "聚合分片发现并生成 Markdown 诊断报告",
		},
	}

	if err := aiInv.Invoke(req); err != nil {
		return err
	}

	reportBytes, err := os.ReadFile(ctx.ReportPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("report file %s was not generated", ctx.ReportPath)
		}
		return fmt.Errorf("failed to read report file: %w", err)
	}
	if len(bytes.TrimSpace(reportBytes)) == 0 {
		return fmt.Errorf("generated report file is empty")
	}

	cleanedReport := SanitizeMarkdownReport(reportBytes)
	if !bytes.Equal(cleanedReport, reportBytes) {
		if writeErr := os.WriteFile(ctx.ReportPath, cleanedReport, 0644); writeErr != nil {
			log.Printf("[Synthesis] Warning: failed to save sanitized markdown report: %v\n", writeErr)
		}
	}

	return nil
}
