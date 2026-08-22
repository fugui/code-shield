package reports

import (
	"archive/zip"
	"bytes"
	"code-shield/models"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type ArchiveExporter struct{}

func (a *ArchiveExporter) ContentType() string {
	return "application/zip"
}

func (a *ArchiveExporter) FileExtension() string {
	return "zip"
}

func (a *ArchiveExporter) Export(report *models.TaskReport, w io.Writer, opts ExportOptions) error {
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	// 1. 写入 summary.md
	summaryEntry, err := zipWriter.Create(fmt.Sprintf("report-%d-summary.md", report.ID))
	if err == nil {
		mdExp := &MarkdownExporter{}
		_ = mdExp.Export(report, summaryEntry, opts)
	}

	// 2. 写入 findings.xlsx
	excelEntry, err := zipWriter.Create(fmt.Sprintf("report-%d-findings.xlsx", report.ID))
	if err == nil {
		excelExp := &ExcelExporter{IsCSV: false}
		_ = excelExp.Export(report, excelEntry, opts)
	}

	// 3. 写入 findings.json
	jsonEntry, err := zipWriter.Create(fmt.Sprintf("report-%d-findings.json", report.ID))
	if err == nil {
		jsonExp := &JSONExporter{}
		_ = jsonExp.Export(report, jsonEntry, ExportOptions{Scope: "findings"})
	}

	// 4. 写入 diagnostics.json
	if diagBytes, err := os.ReadFile(report.GetSummaryJSONPath()); err == nil {
		if diagEntry, err := zipWriter.Create(fmt.Sprintf("report-%d-diagnostics.json", report.ID)); err == nil {
			_, _ = diagEntry.Write(diagBytes)
		}
	} else if diagDTO, err := GetReportDiagnostics(report.ID); err == nil {
		if diagEntry, err := zipWriter.Create(fmt.Sprintf("report-%d-diagnostics.json", report.ID)); err == nil {
			var buf bytes.Buffer
			_ = json.NewEncoder(&buf).Encode(diagDTO)
			_, _ = diagEntry.Write(buf.Bytes())
		}
	}

	// 5. 写入 execution.log
	if logBytes, err := os.ReadFile(report.GetExecutionLogPath()); err == nil && len(logBytes) > 0 {
		if logEntry, err := zipWriter.Create(fmt.Sprintf("report-%d-execution.log", report.ID)); err == nil {
			_, _ = logEntry.Write(logBytes)
		}
	}

	// 6. 写入 meta.json
	if metaEntry, err := zipWriter.Create(fmt.Sprintf("report-%d-meta.json", report.ID)); err == nil {
		meta := BuildMetaDTO(report)
		metaBytes, _ := json.MarshalIndent(meta, "", "  ")
		_, _ = metaEntry.Write(metaBytes)
	}

	// 7. 写入 README_ARCHIVE.txt 说明
	if readmeEntry, err := zipWriter.Create("README_ARCHIVE.txt"); err == nil {
		readmeText := fmt.Sprintf("Code-Shield 任务审计交付归档包\n"+
			"====================================\n"+
			"任务 ID: %d\n"+
			"代码仓库: %s\n"+
			"扫描分支: %s\n"+
			"任务类型: %s\n"+
			"综合评分: %d 分 (%s)\n"+
			"归档时间: %s\n\n"+
			"文件说明:\n"+
			"- report-%d-summary.md: AI 宏观分析概要与建议\n"+
			"- report-%d-findings.xlsx: 结构化问题清单与统计透视\n"+
			"- report-%d-findings.json: JSON 格式问题数据\n"+
			"- report-%d-diagnostics.json: 运行轨迹与分片时序\n"+
			"- report-%d-execution.log: AI CLI 原始执行日志\n",
			report.ID, report.Repo.Name, report.Repo.Branch, report.TaskType.DisplayName,
			report.Score, CalculateRating(report.Score), time.Now().Format("2006-01-02 15:04:05"),
			report.ID, report.ID, report.ID, report.ID, report.ID,
		)
		_, _ = readmeEntry.Write([]byte(readmeText))
	}

	return nil
}
