package handlers

import (
	"code-shield/models"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type ExcelFindingItem struct {
	ID         string
	Severity   string
	Category   string
	FilePath   string
	LineNumber string
	Title      string
	Detail     string
	Suggestion string
	Status     string
	Assignee   string
	Comment    string
}

func convertCampaignFindingsToExcelItems(dbFindings []models.CampaignFinding) []ExcelFindingItem {
	var items []ExcelFindingItem
	for _, f := range dbFindings {
		assigneeName := ""
		if f.Assignee != nil {
			assigneeName = f.Assignee.Name
			if assigneeName == "" {
				assigneeName = f.Assignee.EmployeeID
			}
		}
		items = append(items, ExcelFindingItem{
			ID:         fmt.Sprintf("%d", f.ID),
			Severity:   f.Severity,
			Category:   f.Category,
			FilePath:   f.FilePath,
			LineNumber: f.LineNumber,
			Title:      f.Title,
			Detail:     f.Detail,
			Suggestion: f.Suggestion,
			Status:     f.Status,
			Assignee:   assigneeName,
			Comment:    f.Feedback,
		})
	}
	return items
}

func generateCampaignExcel(c *gin.Context, repoName, campaignTitle string, items []ExcelFindingItem, isEntityMode bool) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet1 := "缺陷清单明细"
	if isEntityMode {
		sheet1 = "用例清单明细"
	}
	index1, _ := f.NewSheet(sheet1)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1E293B"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
		},
	})

	dataStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: "334155", Size: 10},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})

	headers := []string{"缺陷ID", "严重程度", "缺陷分类", "文件路径", "行号", "问题标题", "详细描述", "修复建议", "状态", "责任人", "跟踪意见"}
	if isEntityMode {
		headers = []string{"用例ID", "评估结论", "分类", "文件路径", "行号", "测试用例/实体名称", "详细描述/断言", "流转状态", "复核责任人", "跟踪意见"}
	}

	for colIdx, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		_ = f.SetCellValue(sheet1, cell, h)
	}
	_ = f.SetRowStyle(sheet1, 1, 1, headerStyle)
	_ = f.SetRowHeight(sheet1, 1, 26)

	for rowIdx, item := range items {
		r := rowIdx + 2
		_ = f.SetRowHeight(sheet1, r, 22)
		_ = f.SetRowStyle(sheet1, r, r, dataStyle)

		if isEntityMode {
			statusCn := getCampaignStatusChinese(item.Status, true)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("A%d", r), item.ID)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("B%d", r), item.Severity)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("C%d", r), item.Category)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("D%d", r), item.FilePath)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("E%d", r), item.LineNumber)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("F%d", r), item.Title)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("G%d", r), item.Detail)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("H%d", r), statusCn)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("I%d", r), item.Assignee)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("J%d", r), item.Comment)
		} else {
			statusCn := getCampaignStatusChinese(item.Status, false)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("A%d", r), item.ID)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("B%d", r), item.Severity)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("C%d", r), item.Category)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("D%d", r), item.FilePath)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("E%d", r), item.LineNumber)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("F%d", r), item.Title)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("G%d", r), item.Detail)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("H%d", r), item.Suggestion)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("I%d", r), statusCn)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("J%d", r), item.Assignee)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("K%d", r), item.Comment)
		}
	}

	// 自动适配列宽
	adjustCampaignColWidth(f, sheet1, len(headers))

	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(index1)

	filename := fmt.Sprintf("campaign_%s_%s.xlsx", campaignTitle, time.Now().Format("2006-01-02"))
	encodedFilename := url.QueryEscape(filename)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", filename, encodedFilename))

	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "导出 Excel 文件写入失败: " + err.Error()})
	}
}

func getCampaignStatusChinese(status string, isUT bool) string {
	switch status {
	case "open":
		if isUT {
			return "待复核"
		}
		return "待处理"
	case "analyzing":
		return "问题分析"
	case "resolved":
		if isUT {
			return "已整改"
		}
		return "已解决"
	case "closed":
		return "已关闭"
	case "invalid":
		if isUT {
			return "无效问题"
		}
		return "忽略/误报"
	default:
		return status
	}
}

func adjustCampaignColWidth(f *excelize.File, sheetName string, colCount int) {
	cols, err := f.GetCols(sheetName)
	if err != nil || len(cols) == 0 {
		return
	}
	for colIdx := 1; colIdx <= colCount && colIdx <= len(cols); colIdx++ {
		colName, _ := excelize.ColumnNumberToName(colIdx)
		maxLen := 10
		for _, val := range cols[colIdx-1] {
			actualLen := 0
			for _, r := range val {
				if r > 127 {
					actualLen += 2
				} else {
					actualLen += 1
				}
				if actualLen >= 50 {
					break
				}
			}
			if actualLen > maxLen {
				maxLen = actualLen
			}
		}
		if maxLen > 50 {
			maxLen = 50
		}
		_ = f.SetColWidth(sheetName, colName, colName, float64(maxLen+4))
	}
}
