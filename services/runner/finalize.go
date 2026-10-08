package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/governance"
	"code-shield/services/invoker"
)

// UpdateTaskStatus 原子同步更新 TaskReport 和关联 TaskExecutionLog 的运行状态
func UpdateTaskStatus(reportID uint, status string) {
	if models.DB == nil || reportID == 0 {
		return
	}
	if _, err := models.UpdateActiveTaskReport(models.DB, reportID, map[string]interface{}{
		"status": status,
	}); err != nil && !errors.Is(err, models.ErrTaskReportImmutable) {
		log.Printf("[Finalize] Failed to update task status for report %d: %v", reportID, err)
	}
	models.DB.Model(&models.TaskExecutionLog{}).Where("task_report_id = ?", reportID).Updates(map[string]interface{}{
		"status":          status,
		"status_priority": models.GetStatusPriority(status),
	})
}

// UpdateTaskProgress 原子同步更新任务分片处理微进度与运行状态，内建克隆/前检滞后自愈逻辑
func UpdateTaskProgress(reportID uint, total, processed, success int, currentChunk string) {
	if models.DB == nil || reportID == 0 {
		return
	}

	reportUpdates := map[string]interface{}{
		"total_chunks":     total,
		"processed_chunks": processed,
		"success_chunks":   success,
	}

	// 状态自愈：若当前状态仍停留在克隆、门禁前检或排队，收到分析微进度时自动跃迁为 analyzing
	var curr models.TaskReport
	if err := models.DB.Select("status").First(&curr, reportID).Error; err == nil {
		if curr.Status == models.StatusCloning || curr.Status == models.StatusPreProcessing || curr.Status == models.StatusPending || curr.Status == models.StatusQueued || curr.Status == "" {
			reportUpdates["status"] = models.StatusAnalyzing
		}
	}

	if _, err := models.UpdateActiveTaskReport(models.DB, reportID, reportUpdates); err != nil {
		if errors.Is(err, models.ErrTaskReportImmutable) {
			return
		}
		log.Printf("[Finalize] Failed to update task progress for report %d: %v", reportID, err)
	}

	logUpdates := map[string]interface{}{
		"status":          models.StatusAnalyzing,
		"status_priority": models.GetStatusPriority(models.StatusAnalyzing),
	}
	models.DB.Model(&models.TaskExecutionLog{}).Where("task_report_id = ?", reportID).Updates(logUpdates)
}

// WriteSummaryReport 将当前任务汇总度量指标写入统一的 summary JSON 文件
func WriteSummaryReport(ctx *TaskContext) {
	if ctx.JsonPath == "" {
		return
	}
	ctx.Summary.EndTime = time.Now()
	ctx.Summary.DurationSeconds = ctx.Summary.EndTime.Sub(ctx.Summary.StartTime).Seconds()
	ctx.Summary.CoverageSummary = buildCoverageExecutionSummary(ctx)

	if ctx.Summary.Status == "" {
		if ctx.Summary.Analysis.Status == "failed" || ctx.Summary.Synthesis.Status == "failed" {
			ctx.Summary.Status = "failed"
		} else {
			ctx.Summary.Status = "success"
		}
	}

	reportData, err := json.MarshalIndent(ctx.Summary, "", "  ")
	if err != nil {
		log.Printf("[Finalize] Failed to marshal task summary report: %v\n", err)
		return
	}

	if err := os.WriteFile(ctx.JsonPath, reportData, 0644); err != nil {
		log.Printf("[Finalize] Failed to write task summary report to %s: %v\n", ctx.JsonPath, err)
	} else {
		log.Printf("[Finalize] Successfully saved task summary report to %s\n", ctx.JsonPath)
	}
}

// Finalize 最终化任务：执行专项治理 Hook、更新数据库状态与指标，并按需触发通告
func Finalize(ctx *TaskContext, result TaskResult) error {
	metricsJSON, _ := json.Marshal(result.Metrics)

	if analysis := ctx.Summary.Analysis; analysis.TotalChunks > 0 &&
		analysis.FailedChunks == analysis.TotalChunks {
		MarkFailed(ctx, "all primary chunks failed")
		return nil
	}

	coverageSummary := buildCoverageExecutionSummary(ctx)
	terminalStatus := coverage.TerminalTaskStatus(*coverageSummary, false)
	relReportPath := ctx.ReportPath
	if rel, err := filepath.Rel(models.AppConfig.GetDataDir(), ctx.ReportPath); err == nil {
		relReportPath = rel
	}

	// 1. 先将任务状态置为 "merging"（问题归并中）
	UpdateTaskStatus(ctx.Report.ID, models.StatusMerging)

	// 2. 初始化归并阶段统计并刷新 summary
	ctx.Summary.Merging.Status = "active"
	ctx.Summary.Merging.StartTime = time.Now()
	WriteSummaryReport(ctx)

	// 3. 执行专项治理归并（若启用 IsCampaign）
	if ctx.TaskType.IsCampaign {
		backend := models.AppConfig.AI.ToolBackends.FindingMatch
		if backend == "" {
			backend = "native"
		}
		if !invoker.IsValidAIBackend(backend) {
			backend = models.AppConfig.AI.Backend
		}
		if backend == "" {
			backend = "claude"
		}

		inv := GetAIInvoker(backend)

		campCtx := &governance.CampaignContext{
			Ctx:             ctx.Ctx,
			TaskType:        ctx.TaskType,
			Repo:            ctx.Repo,
			Report:          ctx.Report,
			CodesPath:       ctx.CodesPath,
			HasFailedChunks: ctx.HasFailedChunks,
			Invoker:         inv,
		}

		if err := governance.HandleGenericCampaign(campCtx, ctx.Findings); err != nil {
			log.Printf("[Finalize] Generic campaign hook failed: %v", err)
		} else {
			// 若专项攻关治理归并成功，以归并后的最新全量 Findings 重算综合指标与评分
			effectiveFindings := GetEffectiveFindings(ctx)
			result = RunPostProcess(effectiveFindings, ctx.TaskType)
			metricsJSON, _ = json.Marshal(result.Metrics)
		}
	}

	// 4. 归并完毕，更新归并耗时指标
	ctx.Summary.Merging.Status = "success"
	ctx.Summary.Merging.EndTime = time.Now()
	ctx.Summary.Merging.DurationSeconds = ctx.Summary.Merging.EndTime.Sub(ctx.Summary.Merging.StartTime).Seconds()

	// 5. 最终状态更新为 "success"
	var err error
	if models.DB != nil {
		affected, updateErr := models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, map[string]interface{}{
			"status":                  terminalStatus,
			"report_path":             relReportPath,
			"ai_summary":              result.Summary,
			"score":                   result.Score,
			"metrics":                 string(metricsJSON),
			"created_at":              time.Now(),
			"coverage_complete":       coverageSummary.CoverageComplete,
			"coverage_degraded":       coverageSummary.CoverageDegraded,
			"coverage_not_applicable": coverageSummary.CoverageNotApplicable,
			"coverage_state":          coverageSummary.CoverageState,
		})
		err = updateErr
		if errors.Is(updateErr, models.ErrTaskReportImmutable) {
			return updateErr
		}
		if affected == 0 && updateErr == nil {
			err = models.ErrTaskReportImmutable
		}
	}

	degradedReasons := make([]string, 0)
	ctx.Summary.Status = models.StatusSuccess
	if !ctx.Summary.Analysis.ArtifactComplete {
		degradedReasons = append(degradedReasons, "ARTIFACT_INCOMPLETE")
	}
	if ctx.Summary.Analysis.CandidateQuarantineCount > 0 {
		degradedReasons = append(degradedReasons, "CANDIDATE_QUARANTINED")
	}
	if len(degradedReasons) > 0 {
		if models.DB != nil {
			_, _ = models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, map[string]interface{}{
				"status": models.StatusDegraded,
			})
		}
		ctx.Summary.Status = models.StatusDegraded
	}
	if ctx.Coverage != nil {
		coverageFacts := ctx.Coverage.Summary()
		if coverageFacts.CoverageDegraded || len(coverageFacts.FailedChunkItems) > 0 || len(coverageFacts.MissingAssessments) > 0 {
			degradedReasons = append(degradedReasons, coverageFacts.CoverageReasons...)
			if models.DB != nil {
				_, _ = models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, map[string]interface{}{
					"status": models.StatusDegraded,
				})
			}
			ctx.Summary.Status = models.StatusDegraded
		} else if coverageFacts.CoverageNotApplicable {
			if models.DB != nil {
				_, _ = models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, map[string]interface{}{
					"coverage_not_applicable": true,
				})
			}
		}
	}
	if ctx.Summary.Status == models.StatusDegraded {
		ctx.Summary.DegradedReasons = degradedReasons
	} else {
		ctx.Summary.DegradedReasons = nil
	}
	WriteSummaryReport(ctx)

	if ctx.AutoNotify && result.Score >= ctx.TaskType.NotifyThreshold {
		NotifyTaskResult(ctx.Repo, ctx.TaskType, result, "", ctx.Report.ID, ctx.ReportPath)
	}

	return err
}

// MarkFailed 任务失败处理：更新失败状态、错误原因并记录输出日志
func MarkFailed(ctx *TaskContext, errMsg string) error {
	updates := map[string]interface{}{
		"status":         models.StatusFailed,
		"ai_summary":     fmt.Sprintf("【执行失败】%s", errMsg),
		"coverage_state": coverage.StateFailed,
	}
	if ctx.ReportPath != "" {
		relPath := ctx.ReportPath
		if rel, err := filepath.Rel(models.AppConfig.GetDataDir(), ctx.ReportPath); err == nil {
			relPath = rel
		}
		updates["report_path"] = relPath
	}
	if models.DB != nil {
		if _, err := models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, updates); err != nil {
			return err
		}
	}

	ctx.Summary.Status = "failed"
	if ctx.Summary.Analysis.Status == "" {
		ctx.Summary.Analysis.Status = "failed"
	}
	WriteSummaryReport(ctx)

	if ctx.ReportPath != "" {
		cliOutputPath := ctx.ReportPath + ".output.txt"
		alreadyLogged := false
		if contentBytes, err := os.ReadFile(cliOutputPath); err == nil {
			content := string(contentBytes)
			if strings.Contains(content, "[Code-Shield Error]") && strings.Contains(content, errMsg) {
				alreadyLogged = true
			}
		}

		if !alreadyLogged {
			var f *os.File
			var openErr error
			if _, statErr := os.Stat(cliOutputPath); os.IsNotExist(statErr) {
				f, openErr = os.Create(cliOutputPath)
			} else {
				f, openErr = os.OpenFile(cliOutputPath, os.O_APPEND|os.O_WRONLY, 0644)
			}

			if openErr == nil {
				defer f.Close()
				_, _ = f.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution failed: %s\n", errMsg))
			} else {
				log.Printf("[Finalize] Failed to write error to output.txt: %v\n", openErr)
			}
		}
	}
	return nil
}
