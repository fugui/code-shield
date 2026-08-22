package reports

import (
	"code-shield/models"
	"io"
	"os"
)

type MarkdownExporter struct{}

func (m *MarkdownExporter) ContentType() string {
	return "text/markdown; charset=utf-8"
}

func (m *MarkdownExporter) FileExtension() string {
	return "md"
}

func (m *MarkdownExporter) Export(report *models.TaskReport, w io.Writer, opts ExportOptions) error {
	mdContent := ""
	if absMd := report.GetAbsReportPath(); absMd != "" {
		if content, err := os.ReadFile(absMd); err == nil {
			mdContent = string(content)
		}
	}
	if mdContent == "" && report.AISummary != "" {
		mdContent = report.AISummary
	}
	if mdContent == "" {
		mdContent = "# 任务审计报告\n\n*暂无报告正文内容*"
	}

	_, err := w.Write([]byte(mdContent))
	return err
}
