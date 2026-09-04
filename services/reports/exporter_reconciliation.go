package reports

import (
	"code-shield/models"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReconciliationExporter 导出对账过程明细数据 (Scope="reconcile")
type ReconciliationExporter struct{}

func (r *ReconciliationExporter) ContentType() string {
	return "application/json; charset=utf-8"
}

func (r *ReconciliationExporter) FileExtension() string {
	return "json"
}

func (r *ReconciliationExporter) Export(report *models.TaskReport, w io.Writer, opts ExportOptions) error {
	reportDir := report.GetReportDir()

	// 1. 尝试直接读取落盘的 recon-*.json
	matches, err := filepath.Glob(filepath.Join(reportDir, fmt.Sprintf("recon-%d-vs-*.json", report.ID)))
	if err == nil && len(matches) > 0 {
		data, readErr := os.ReadFile(matches[0])
		if readErr == nil {
			_, err = w.Write(data)
			return err
		}
	}

	// 2. 回退查询数据库中的 ScanReconciliation 与 ReconciliationLink
	if models.DB != nil {
		var recon models.ScanReconciliation
		if dbErr := models.DB.Where("current_report_id = ?", report.ID).Order("id desc").First(&recon).Error; dbErr == nil {
			var links []models.ReconciliationLink
			_ = models.DB.Where("recon_id = ?", recon.ID).Find(&links).Error

			type ReconExportDTO struct {
				Session models.ScanReconciliation   `json:"session"`
				Links   []models.ReconciliationLink `json:"links"`
			}
			dto := ReconExportDTO{
				Session: recon,
				Links:   links,
			}
			data, _ := json.MarshalIndent(dto, "", "  ")
			_, err = w.Write(data)
			return err
		}
	}

	return fmt.Errorf("reconciliation audit data not found for report %d", report.ID)
}
