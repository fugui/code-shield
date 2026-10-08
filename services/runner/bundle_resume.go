package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"code-shield/models"
)

// IsBundleResumeEngine 判断引擎模式是否支持 bundle 级 checkpoint 复用。
func IsBundleResumeEngine(engineMode string) bool {
	return engineMode == "debate_full"
}

// HasBundleResumeArtifacts 判断报告目录中是否存在可读取的 bundle checkpoint。
// 这里只做轻量存在性判断；真正的版本、报告和 bundle 匹配在引擎加载时执行。
func HasBundleResumeArtifacts(taskTypeName string, reportID uint, engineMode string) bool {
	return FindBundleResumeArtifacts(taskTypeName, engineMode, []uint{reportID})[reportID]
}

// FindBundleResumeArtifacts checks a set of reports with a single directory scan.
func FindBundleResumeArtifacts(taskTypeName string, engineMode string, reportIDs []uint) map[uint]bool {
	found := make(map[uint]bool)
	if !IsBundleResumeEngine(engineMode) || len(reportIDs) == 0 {
		return found
	}

	wanted := make(map[uint]struct{}, len(reportIDs))
	for _, reportID := range reportIDs {
		if reportID != 0 {
			wanted[reportID] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return found
	}

	root := filepath.Join(models.AppConfig.GetDataDir(), "reports", taskTypeName)
	if _, err := os.Stat(root); err != nil {
		return found
	}

	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.IsDir() {
			return nil
		}
		reportID, ok := bundleReportID(info.Name())
		if !ok {
			return nil
		}
		if _, exists := wanted[reportID]; !exists {
			return nil
		}

		matches, _ := filepath.Glob(filepath.Join(path, "resume-*.json"))
		if len(matches) == 0 {
			return nil
		}
		found[reportID] = true
		return filepath.SkipDir
	})
	return found
}

func bundleReportID(name string) (uint, bool) {
	rest, found := strings.CutPrefix(name, "debate-chunks-")
	if !found {
		return 0, false
	}
	idText, _, found := strings.Cut(rest, "-")
	if !found {
		return 0, false
	}
	reportID, err := strconv.ParseUint(idText, 10, 64)
	if err != nil || reportID == 0 || reportID > uint64(^uint(0)>>1) {
		return 0, false
	}
	return uint(reportID), true
}

// ResumeChunkedTask 是手动恢复与启动自动恢复共用的统一入口。
// 只有 debate_full 且存在 bundle checkpoint 时才允许复用执行进度；
// 其他情况必须走全新 RunTaskSync，不允许旧 chunk 恢复链路复活。
func ResumeChunkedTask(reportID uint) error {
	var report models.TaskReport
	if err := models.DB.Preload("Repo").Preload("TaskType").First(&report, reportID).Error; err != nil {
		return fmt.Errorf("report %d not found: %w", reportID, err)
	}

	if !IsBundleResumeEngine(report.TaskType.EngineMode) {
		err := fmt.Errorf("ENGINE_MODE_INVALID: engine mode %q is not registered", report.TaskType.EngineMode)
		markResumeRequiresRescan(report.ID, err)
		return err
	}

	if HasBundleResumeArtifacts(report.TaskType.Name, report.ID, report.TaskType.EngineMode) {
		// checkpoint 覆盖的结果会随本次恢复重新聚合；清零后可避免上一次
		// “已入库但未终态”的窗口导致 Token 或辩论日志重复。
		if _, err := models.UpdateActiveTaskReport(models.DB, report.ID, map[string]interface{}{
			"tier1_tokens": 0,
			"tier2_tokens": 0,
		}); err != nil {
			return fmt.Errorf("reset resume token counters: %w", err)
		}
		if err := models.DB.Where("task_report_id = ?", report.ID).Delete(&models.TaskDebateLog{}).Error; err != nil {
			return fmt.Errorf("replace resume debate logs: %w", err)
		}

		autoNotify := false
		runParams := models.RunParams{}
		var execLog models.TaskExecutionLog
		if err := models.DB.Preload("Schedule").Where("task_report_id = ?", reportID).First(&execLog).Error; err == nil {
			if execLog.Schedule != nil {
				autoNotify = execLog.Schedule.AutoNotify
				if len(execLog.Schedule.RunParams) > 0 {
					_ = json.Unmarshal(execLog.Schedule.RunParams, &runParams)
				}
			}
		}
		return RunTaskSync(reportID, report.Repo.URL, report.TaskTypeID, autoNotify, runParams)
	}

	markResumeRequiresRescan(report.ID, ErrResumeRequiresRescan)
	return ErrResumeRequiresRescan
}

func markResumeRequiresRescan(reportID uint, err error) {
	models.DB.Model(&models.TaskReport{}).
		Where("id = ? AND status NOT IN ?", reportID, models.TerminalTaskStatuses()).
		Updates(map[string]interface{}{
			"status":     models.StatusFailed,
			"ai_summary": fmt.Sprintf("【执行失败】%s", err.Error()),
		})
}
