package runner

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"code-shield/models"
)

// CheckPrecondition 执行任务特定的前置准入门禁脚本，决定是否准入执行
// 若脚本 exitCode == 1，代表无变更或不满足准入条件，返回 skipped = true
func CheckPrecondition(ctx context.Context, reportID uint, taskType models.TaskType, codesPath string) (bool, error) {
	scriptPath := taskType.PreconditionScript()
	if scriptPath == "" {
		return false, nil
	}

	absScript := models.AppConfig.GetAbsPath(scriptPath)
	if _, err := os.Stat(absScript); os.IsNotExist(err) {
		return false, nil
	}

	log.Printf("[Precondition] Running precondition: %s (ReportID: %d)\n", absScript, reportID)
	_ = os.Chmod(absScript, 0755)

	output, exitCode, err := ExecCommandWithProcessGroup(ctx, "", absScript, codesPath)
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		if exitCode == 1 {
			log.Printf("[Precondition] Skip condition met: %s\n", outputStr)
			if models.DB != nil {
				models.DB.Model(&models.TaskReport{}).Where("id = ?", reportID).Updates(map[string]interface{}{
					"status":     models.StatusSkipped,
					"ai_summary": outputStr,
					"created_at": time.Now(),
				})
			}
			return true, nil
		}
		return false, fmt.Errorf("precondition failed: %s", outputStr)
	}
	return false, nil
}
