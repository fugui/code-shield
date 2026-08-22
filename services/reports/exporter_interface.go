package reports

import (
	"code-shield/models"
	"fmt"
	"io"
	"strings"
	"time"
)

// ExportOptions 导出配置项
type ExportOptions struct {
	Format string // excel, csv, json, md, zip
	Scope  string // all, findings, summary
}

// ReportExporter 导出器接口
type ReportExporter interface {
	Export(report *models.TaskReport, w io.Writer, opts ExportOptions) error
	ContentType() string
	FileExtension() string
}

// BuildExportFilename 生成标准化的下载文件名 (遵循 {repo_name}_{task_id}_{task_type}_{yyyyMMdd}.{ext})
func BuildExportFilename(report *models.TaskReport, category, ext string) string {
	safeRepo := strings.ReplaceAll(report.Repo.Name, "/", "-")
	taskType := strings.ReplaceAll(report.TaskType.Name, "_", "-")
	dateStr := time.Now().Format("20060102")

	if category != "" {
		return fmt.Sprintf("%s_%d_%s_%s_%s.%s", safeRepo, report.ID, taskType, category, dateStr, ext)
	}
	return fmt.Sprintf("%s_%d_%s_%s.%s", safeRepo, report.ID, taskType, dateStr, ext)
}
