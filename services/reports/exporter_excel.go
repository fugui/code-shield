package reports

import (
	"code-shield/models"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/xuri/excelize/v2"
)

type ExcelExporter struct {
	IsCSV bool
}

func (e *ExcelExporter) ContentType() string {
	if e.IsCSV {
		return "text/csv; charset=utf-8"
	}
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}

func (e *ExcelExporter) FileExtension() string {
	if e.IsCSV {
		return "csv"
	}
	return "xlsx"
}

func (e *ExcelExporter) Export(report *models.TaskReport, w io.Writer, opts ExportOptions) error {
	items, err := loadAllFindingsRaw(report)
	if err != nil {
		return err
	}

	isEntityMode := report.TaskType.GovernanceMode == models.GovernanceModeEntityAssessment

	if e.IsCSV {
		return exportCSV(report, items, isEntityMode, w)
	}
	return exportXLSX(report, items, isEntityMode, w)
}

func exportCSV(report *models.TaskReport, items []FindingItemDTO, isEntityMode bool, w io.Writer) error {
	// 写入 UTF-8 BOM，保证 Excel 打开不乱码
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	writer := csv.NewWriter(w)
	defer writer.Flush()

	if isEntityMode {
		_ = writer.Write([]string{"序号", "评估结论", "评估项分类", "文件路径", "行号", "用例/实体名称", "详细描述与断言", "流转状态", "复核人", "跟踪意见"})
		for idx, item := range items {
			_ = writer.Write([]string{
				strconv.Itoa(idx + 1),
				item.SeverityDisplay,
				item.Category,
				item.FilePath,
				item.LineNumber,
				item.Title,
				item.Detail,
				item.StatusDisplay,
				item.AssigneeName,
				item.LatestComment,
			})
		}
	} else {
		_ = writer.Write([]string{"序号", "严重等级", "缺陷分类", "文件路径", "行号", "问题标题", "详细描述", "修复建议", "流转状态", "责任人", "跟踪意见"})
		for idx, item := range items {
			_ = writer.Write([]string{
				strconv.Itoa(idx + 1),
				item.SeverityDisplay,
				item.Category,
				item.FilePath,
				item.LineNumber,
				item.Title,
				item.Detail,
				item.Suggestion,
				item.StatusDisplay,
				item.AssigneeName,
				item.LatestComment,
			})
		}
	}

	return nil
}

func exportXLSX(report *models.TaskReport, items []FindingItemDTO, isEntityMode bool, w io.Writer) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	metrics := computeKPIMetrics(items, isEntityMode)

	// ── Sheet 1: 概览与透视 ──
	overviewSheet := "治理概览"
	f.SetSheetName("Sheet1", overviewSheet)

	// 样式定义
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14, Color: "0F172A"},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	sectionHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1E293B"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	labelStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10, Color: "475569"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F1F5F9"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
		},
	})
	valStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "0F172A"},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
		},
	})

	// 标题
	_ = f.SetCellValue(overviewSheet, "A1", fmt.Sprintf("Code-Shield 任务审计报告 - %s", report.Repo.Name))
	_ = f.SetCellStyle(overviewSheet, "A1", "A1", titleStyle)
	_ = f.SetRowHeight(overviewSheet, 1, 30)

	// 任务基本信息块
	_ = f.SetCellValue(overviewSheet, "A3", "任务基本信息")
	_ = f.SetCellStyle(overviewSheet, "A3", "D3", sectionHeaderStyle)
	_ = f.MergeCell(overviewSheet, "A3", "D3")

	infoRows := [][]string{
		{"代码仓库", report.Repo.Name, "扫描分支", report.Repo.Branch},
		{"任务类型", report.TaskType.DisplayName, "综合评分", fmt.Sprintf("%d 分 (%s)", report.Score, CalculateRating(report.Score))},
		{"治理模式", getGovModeChinese(report.TaskType.GovernanceMode), "完成时间", report.CreatedAt.Format("2006-01-02 15:04:05")},
	}
	for i, row := range infoRows {
		r := 4 + i
		_ = f.SetCellValue(overviewSheet, fmt.Sprintf("A%d", r), row[0])
		_ = f.SetCellValue(overviewSheet, fmt.Sprintf("B%d", r), row[1])
		_ = f.SetCellValue(overviewSheet, fmt.Sprintf("C%d", r), row[2])
		_ = f.SetCellValue(overviewSheet, fmt.Sprintf("D%d", r), row[3])
		_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), labelStyle)
		_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("B%d", r), fmt.Sprintf("B%d", r), valStyle)
		_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("C%d", r), fmt.Sprintf("C%d", r), labelStyle)
		_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("D%d", r), fmt.Sprintf("D%d", r), valStyle)
	}

	// 统计指标透视块
	_ = f.SetCellValue(overviewSheet, "A8", "统计分布透视")
	_ = f.SetCellStyle(overviewSheet, "A8", "D8", sectionHeaderStyle)
	_ = f.MergeCell(overviewSheet, "A8", "D8")

	if isEntityMode {
		statRows := [][]interface{}{
			{"评估实体/用例总数", metrics.TotalFindings, "合格实体数", metrics.PassCount},
			{"综合达标率", fmt.Sprintf("%.1f%%", metrics.PassRate), "待整改/不合格数", metrics.TotalFindings - metrics.PassCount},
		}
		for i, row := range statRows {
			r := 9 + i
			_ = f.SetCellValue(overviewSheet, fmt.Sprintf("A%d", r), row[0])
			_ = f.SetCellValue(overviewSheet, fmt.Sprintf("B%d", r), row[1])
			_ = f.SetCellValue(overviewSheet, fmt.Sprintf("C%d", r), row[2])
			_ = f.SetCellValue(overviewSheet, fmt.Sprintf("D%d", r), row[3])
			_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), labelStyle)
			_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("B%d", r), fmt.Sprintf("B%d", r), valStyle)
			_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("C%d", r), fmt.Sprintf("C%d", r), labelStyle)
			_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("D%d", r), fmt.Sprintf("D%d", r), valStyle)
		}
	} else {
		statRows := [][]interface{}{
			{"检出缺陷总数", metrics.TotalFindings, "致命缺陷 (P0)", metrics.FatalCount},
			{"严重缺陷 (P1)", metrics.CriticalCount, "一般缺陷 (P2)", metrics.MajorCount},
			{"轻微提示 (P3)", metrics.MinorCount, "改进建议", metrics.SuggestionCount},
		}
		for i, row := range statRows {
			r := 9 + i
			_ = f.SetCellValue(overviewSheet, fmt.Sprintf("A%d", r), row[0])
			_ = f.SetCellValue(overviewSheet, fmt.Sprintf("B%d", r), row[1])
			_ = f.SetCellValue(overviewSheet, fmt.Sprintf("C%d", r), row[2])
			_ = f.SetCellValue(overviewSheet, fmt.Sprintf("D%d", r), row[3])
			_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), labelStyle)
			_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("B%d", r), fmt.Sprintf("B%d", r), valStyle)
			_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("C%d", r), fmt.Sprintf("C%d", r), labelStyle)
			_ = f.SetCellStyle(overviewSheet, fmt.Sprintf("D%d", r), fmt.Sprintf("D%d", r), valStyle)
		}
	}

	_ = f.SetColWidth(overviewSheet, "A", "A", 20)
	_ = f.SetColWidth(overviewSheet, "B", "B", 28)
	_ = f.SetColWidth(overviewSheet, "C", "C", 20)
	_ = f.SetColWidth(overviewSheet, "D", "D", 28)

	// ── Sheet 2: 详细明细清单 ──
	detailSheet := "详细清单"
	if isEntityMode {
		detailSheet = "实体评估明细"
	}
	_, _ = f.NewSheet(detailSheet)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"334155"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
		},
	})
	cellStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "1E293B"},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})
	fatalSevStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10, Color: "DC2626"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FEE2E2"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	criticalSevStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10, Color: "EA580C"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FFEDD5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	majorSevStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10, Color: "CA8A04"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FEF9C3"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	passSevStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10, Color: "16A34A"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"DCFCE7"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	var headers []string
	if isEntityMode {
		headers = []string{"序号", "评估结论", "评估项分类", "文件路径", "行号", "用例/实体名称", "详细描述与断言", "流转状态", "复核人", "跟踪意见"}
	} else {
		headers = []string{"序号", "严重等级", "缺陷分类", "文件路径", "行号", "问题标题", "详细描述", "修复建议", "流转状态", "责任人", "跟踪意见"}
	}

	for colIdx, h := range headers {
		colName, _ := excelize.ColumnNumberToName(colIdx + 1)
		cell := fmt.Sprintf("%s1", colName)
		_ = f.SetCellValue(detailSheet, cell, h)
		_ = f.SetCellStyle(detailSheet, cell, cell, headerStyle)
	}
	_ = f.SetRowHeight(detailSheet, 1, 26)

	for rowIdx, item := range items {
		rowNum := rowIdx + 2
		_ = f.SetRowHeight(detailSheet, rowNum, 22)

		var rowVals []interface{}
		if isEntityMode {
			rowVals = []interface{}{
				rowIdx + 1,
				item.SeverityDisplay,
				item.Category,
				item.FilePath,
				item.LineNumber,
				item.Title,
				item.Detail,
				item.StatusDisplay,
				item.AssigneeName,
				item.LatestComment,
			}
		} else {
			rowVals = []interface{}{
				rowIdx + 1,
				item.SeverityDisplay,
				item.Category,
				item.FilePath,
				item.LineNumber,
				item.Title,
				item.Detail,
				item.Suggestion,
				item.StatusDisplay,
				item.AssigneeName,
				item.LatestComment,
			}
		}

		for colIdx, val := range rowVals {
			colName, _ := excelize.ColumnNumberToName(colIdx + 1)
			cell := fmt.Sprintf("%s%d", colName, rowNum)
			_ = f.SetCellValue(detailSheet, cell, val)
			_ = f.SetCellStyle(detailSheet, cell, cell, cellStyle)

			// 严重度特殊高亮
			if colIdx == 1 {
				switch item.Severity {
				case SeverityFatal:
					_ = f.SetCellStyle(detailSheet, cell, cell, fatalSevStyle)
				case SeverityCritical:
					_ = f.SetCellStyle(detailSheet, cell, cell, criticalSevStyle)
				case SeverityMajor:
					_ = f.SetCellStyle(detailSheet, cell, cell, majorSevStyle)
				case SeverityPass:
					_ = f.SetCellStyle(detailSheet, cell, cell, passSevStyle)
				}
			}
		}
	}

	// 启用 AutoFilter 与列宽设置
	lastCol, _ := excelize.ColumnNumberToName(len(headers))
	_ = f.AutoFilter(detailSheet, fmt.Sprintf("A1:%s%d", lastCol, len(items)+1), nil)

	_ = f.SetColWidth(detailSheet, "A", "A", 8)
	_ = f.SetColWidth(detailSheet, "B", "B", 14)
	_ = f.SetColWidth(detailSheet, "C", "C", 16)
	_ = f.SetColWidth(detailSheet, "D", "D", 28)
	_ = f.SetColWidth(detailSheet, "E", "E", 10)
	_ = f.SetColWidth(detailSheet, "F", "F", 28)
	_ = f.SetColWidth(detailSheet, "G", "G", 36)
	_ = f.SetColWidth(detailSheet, "H", "H", 32)
	_ = f.SetColWidth(detailSheet, "I", "I", 14)
	_ = f.SetColWidth(detailSheet, "J", "J", 14)
	_ = f.SetColWidth(detailSheet, "K", "K", 24)

	return f.Write(w)
}

func getGovModeChinese(mode string) string {
	switch mode {
	case models.GovernanceModeEntityAssessment:
		return "全量实体评估模式"
	default:
		return "缺陷攻关治理模式"
	}
}
