package handlers

import (
	"code-shield/models"
	"code-shield/services/reports"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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
		comment := reports.ExtractLatestComment(f.StatusLog)
		if comment == "" && f.Feedback != "" {
			comment = f.Feedback
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
			Comment:    comment,
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
			statusCn := reports.GetStatusChinese(item.Status, true)
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
			statusCn := reports.GetStatusChinese(item.Status, false)
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

// ExportDynamicCampaignDepartments 导出部门排行榜及各部门下属代码仓二层表格至 Excel
func ExportDynamicCampaignDepartments(c *gin.Context) {
	taskTypeVal, exists := c.Get("taskType")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TaskType context missing"})
		return
	}
	tt := taskTypeVal.(*models.TaskType)
	isEntityMode := tt.GovernanceMode == models.GovernanceModeEntityAssessment

	// 1. 获取部门排行榜汇总数据
	deptSummaries, err := FetchCampaignDeptSummaries(tt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch department summaries: " + err.Error()})
		return
	}

	// 按照默认口径排序部门
	sort.Slice(deptSummaries, func(i, j int) bool {
		if isEntityMode {
			return deptSummaries[i].PassRate > deptSummaries[j].PassRate
		}
		if deptSummaries[i].OpenIssues != deptSummaries[j].OpenIssues {
			return deptSummaries[i].OpenIssues > deptSummaries[j].OpenIssues
		}
		return deptSummaries[i].FixRate < deptSummaries[j].FixRate
	})

	// 2. 获取全量代码仓明细数据
	repoSummaries, err := FetchCampaignRepoSummaries(tt, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch repo summaries: " + err.Error()})
		return
	}

	// 将代码仓按部门归类组织（保持部门排序）
	deptOrderMap := make(map[string]int)
	for idx, d := range deptSummaries {
		deptOrderMap[d.Department] = idx
	}

	sort.Slice(repoSummaries, func(i, j int) bool {
		orderI, okI := deptOrderMap[repoSummaries[i].Department]
		if !okI {
			orderI = 9999
		}
		orderJ, okJ := deptOrderMap[repoSummaries[j].Department]
		if !okJ {
			orderJ = 9999
		}
		if orderI != orderJ {
			return orderI < orderJ
		}
		if isEntityMode {
			return repoSummaries[i].PassRate > repoSummaries[j].PassRate
		}
		if repoSummaries[i].OpenIssues != repoSummaries[j].OpenIssues {
			return repoSummaries[i].OpenIssues > repoSummaries[j].OpenIssues
		}
		return repoSummaries[i].FixRate < repoSummaries[j].FixRate
	})

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

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

	centerDataStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: "334155", Size: 10},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})

	// ----------------------------------------------------
	// Sheet 1: 部门排行榜 (一级汇总表)
	// ----------------------------------------------------
	sheet1 := "部门治理排行榜"
	if isEntityMode {
		sheet1 = "部门评估排行榜"
	}
	sheet1Index, _ := f.NewSheet(sheet1)

	var deptHeaders []string
	if isEntityMode {
		deptHeaders = []string{"排名", "部门", "覆盖代码仓", "总评估用例数", "合格用例数", "用例合格率"}
	} else {
		deptHeaders = []string{"排名", "部门", "覆盖代码仓", "总累计缺陷数", "未整改缺陷", "缺陷整改率"}
	}

	for colIdx, h := range deptHeaders {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		_ = f.SetCellValue(sheet1, cell, h)
	}
	_ = f.SetRowStyle(sheet1, 1, 1, headerStyle)
	_ = f.SetRowHeight(sheet1, 1, 26)

	for rowIdx, d := range deptSummaries {
		r := rowIdx + 2
		_ = f.SetRowHeight(sheet1, r, 22)
		_ = f.SetRowStyle(sheet1, r, r, dataStyle)

		rateVal := d.FixRate
		if isEntityMode {
			rateVal = d.PassRate
		}

		_ = f.SetCellValue(sheet1, fmt.Sprintf("A%d", r), rowIdx+1)
		_ = f.SetCellValue(sheet1, fmt.Sprintf("B%d", r), d.Department)
		_ = f.SetCellValue(sheet1, fmt.Sprintf("C%d", r), fmt.Sprintf("%d/%d", d.ScannedRepos, d.TotalRepos))
		_ = f.SetCellValue(sheet1, fmt.Sprintf("D%d", r), d.TotalIssues)
		if isEntityMode {
			_ = f.SetCellValue(sheet1, fmt.Sprintf("E%d", r), d.PassCount)
		} else {
			_ = f.SetCellValue(sheet1, fmt.Sprintf("E%d", r), d.OpenIssues)
		}
		_ = f.SetCellValue(sheet1, fmt.Sprintf("F%d", r), fmt.Sprintf("%.1f%%", rateVal))
		_ = f.SetCellStyle(sheet1, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), centerDataStyle)
		_ = f.SetCellStyle(sheet1, fmt.Sprintf("C%d", r), fmt.Sprintf("F%d", r), centerDataStyle)
	}
	adjustCampaignColWidth(f, sheet1, len(deptHeaders))

	// ----------------------------------------------------
	// Sheet 2: 各部门代码仓明细 (二级明细表)
	// ----------------------------------------------------
	sheet2 := "各部门代码仓明细"
	f.NewSheet(sheet2)

	var repoHeaders []string
	if isEntityMode {
		repoHeaders = []string{"序号", "归属部门", "代码仓", "负责人", "用例总数", "合格用例", "待优化", "合格率", "最近扫描"}
	} else {
		repoHeaders = []string{"序号", "归属部门", "代码仓", "负责人", "跟踪缺陷数", "致命", "严重", "修复进度", "最近扫描"}
	}

	for colIdx, h := range repoHeaders {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		_ = f.SetCellValue(sheet2, cell, h)
	}
	_ = f.SetRowStyle(sheet2, 1, 1, headerStyle)
	_ = f.SetRowHeight(sheet2, 1, 26)

	for rowIdx, rItem := range repoSummaries {
		r := rowIdx + 2
		_ = f.SetRowHeight(sheet2, r, 22)
		_ = f.SetRowStyle(sheet2, r, r, dataStyle)

		rateVal := rItem.FixRate
		if isEntityMode {
			rateVal = rItem.PassRate
		}

		lastScanStr := "未扫描"
		if !rItem.LastScanTime.IsZero() && rItem.LastScanTime.Year() > 2000 {
			lastScanStr = rItem.LastScanTime.Format("2006-01-02 15:04")
		}

		_ = f.SetCellValue(sheet2, fmt.Sprintf("A%d", r), rowIdx+1)
		_ = f.SetCellValue(sheet2, fmt.Sprintf("B%d", r), rItem.Department)
		_ = f.SetCellValue(sheet2, fmt.Sprintf("C%d", r), rItem.RepoName)
		_ = f.SetCellValue(sheet2, fmt.Sprintf("D%d", r), rItem.OwnerName)
		if isEntityMode {
			_ = f.SetCellValue(sheet2, fmt.Sprintf("E%d", r), rItem.TotalEntities)
			_ = f.SetCellValue(sheet2, fmt.Sprintf("F%d", r), rItem.PassCount)
			_ = f.SetCellValue(sheet2, fmt.Sprintf("G%d", r), rItem.OpenIssues)
		} else {
			_ = f.SetCellValue(sheet2, fmt.Sprintf("E%d", r), rItem.OpenIssues)
			_ = f.SetCellValue(sheet2, fmt.Sprintf("F%d", r), rItem.Blocking)
			_ = f.SetCellValue(sheet2, fmt.Sprintf("G%d", r), rItem.Critical)
		}
		_ = f.SetCellValue(sheet2, fmt.Sprintf("H%d", r), fmt.Sprintf("%.0f%%", rateVal))
		_ = f.SetCellValue(sheet2, fmt.Sprintf("I%d", r), lastScanStr)

		_ = f.SetCellStyle(sheet2, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), centerDataStyle)
		_ = f.SetCellStyle(sheet2, fmt.Sprintf("E%d", r), fmt.Sprintf("I%d", r), centerDataStyle)
	}
	adjustCampaignColWidth(f, sheet2, len(repoHeaders))

	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(sheet1Index)

	campaignTitle := tt.DisplayName
	if campaignTitle == "" {
		campaignTitle = tt.Name
	}
	filename := fmt.Sprintf("专项治理_部门排行榜_%s_%s.xlsx", campaignTitle, time.Now().Format("2006-01-02"))
	encodedFilename := url.QueryEscape(filename)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", filename, encodedFilename))

	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "导出 Excel 文件写入失败: " + err.Error()})
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
