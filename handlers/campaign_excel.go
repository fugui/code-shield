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

// ExportDynamicCampaignDepartments 导出部门排行榜与下属代码仓单 Sheet 二层可折叠 Excel 数据表
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

	// 2. 获取全量代码仓明细数据并按部门组织
	repoSummaries, err := FetchCampaignRepoSummaries(tt, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch repo summaries: " + err.Error()})
		return
	}

	deptReposMap := make(map[string][]DynamicCampaignRepoSummary)
	for _, r := range repoSummaries {
		deptReposMap[r.Department] = append(deptReposMap[r.Department], r)
	}

	// 对各部门下属代码仓排序
	for deptName := range deptReposMap {
		list := deptReposMap[deptName]
		sort.Slice(list, func(i, j int) bool {
			if isEntityMode {
				return list[i].PassRate > list[j].PassRate
			}
			if list[i].OpenIssues != list[j].OpenIssues {
				return list[i].OpenIssues > list[j].OpenIssues
			}
			return list[i].FixRate < list[j].FixRate
		})
		deptReposMap[deptName] = list
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// 表头深色商务风格
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

	// 一级部门行样式 (淡灰背景、加粗文本)
	deptRowStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "0F172A", Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F1F5F9"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
		},
	})

	deptCenterStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "0F172A", Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F1F5F9"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
		},
	})

	// 二级代码仓行普通样式 (纯白背景、细字)
	repoRowStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: "334155", Size: 10},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})

	repoCenterStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: "334155", Size: 10},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})

	repoDangerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "DC2626", Size: 10},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})

	repoWarningStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "EA580C", Size: 10},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})

	sheet1 := "部门治理排行榜"
	if isEntityMode {
		sheet1 = "部门评估排行榜"
	}
	sheet1Index, _ := f.NewSheet(sheet1)

	// 配置大纲属性：折叠按钮置于父汇总行（上方）
	falseVal := false
	_ = f.SetSheetProps(sheet1, &excelize.SheetPropsOptions{
		OutlineSummaryBelow: &falseVal,
	})

	headers := []string{"序号/排名", "部门 / 代码仓", "负责人 / 覆盖仓", "跟踪缺陷数(未关闭)", "致命(未关闭)", "严重(未关闭)", "修复进度 / 整改率", "最近扫描"}
	if isEntityMode {
		headers = []string{"序号/排名", "部门 / 代码仓", "负责人 / 覆盖仓", "用例总数", "合格用例", "待优化", "用例合格率", "最近扫描"}
	}

	curRow := 1
	for colIdx, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, curRow)
		_ = f.SetCellValue(sheet1, cell, h)
	}
	_ = f.SetRowStyle(sheet1, curRow, curRow, headerStyle)
	_ = f.SetRowHeight(sheet1, curRow, 26)

	for deptIdx, d := range deptSummaries {
		curRow++
		deptRow := curRow
		_ = f.SetRowHeight(sheet1, deptRow, 24)

		subRepos := deptReposMap[d.Department]

		deptBlocking := 0
		deptCritical := 0
		for _, sr := range subRepos {
			deptBlocking += sr.Blocking
			deptCritical += sr.Critical
		}

		rateVal := d.FixRate
		if isEntityMode {
			rateVal = d.PassRate
		}

		_ = f.SetCellValue(sheet1, fmt.Sprintf("A%d", deptRow), deptIdx+1)
		_ = f.SetCellValue(sheet1, fmt.Sprintf("B%d", deptRow), d.Department)
		_ = f.SetCellValue(sheet1, fmt.Sprintf("C%d", deptRow), fmt.Sprintf("%d/%d 仓", d.ScannedRepos, d.TotalRepos))
		if isEntityMode {
			_ = f.SetCellValue(sheet1, fmt.Sprintf("D%d", deptRow), d.TotalIssues)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("E%d", deptRow), d.PassCount)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("F%d", deptRow), d.OpenIssues)
		} else {
			_ = f.SetCellValue(sheet1, fmt.Sprintf("D%d", deptRow), d.OpenIssues)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("E%d", deptRow), deptBlocking)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("F%d", deptRow), deptCritical)
		}
		_ = f.SetCellValue(sheet1, fmt.Sprintf("G%d", deptRow), fmt.Sprintf("%.1f%%", rateVal))
		_ = f.SetCellValue(sheet1, fmt.Sprintf("H%d", deptRow), "-")

		_ = f.SetRowStyle(sheet1, deptRow, deptRow, deptRowStyle)
		_ = f.SetCellStyle(sheet1, fmt.Sprintf("A%d", deptRow), fmt.Sprintf("A%d", deptRow), deptCenterStyle)
		_ = f.SetCellStyle(sheet1, fmt.Sprintf("C%d", deptRow), fmt.Sprintf("H%d", deptRow), deptCenterStyle)

		// 填充二级子代码仓（设置大纲层级为 1）
		if len(subRepos) == 0 {
			curRow++
			_ = f.SetRowHeight(sheet1, curRow, 20)
			_ = f.SetCellValue(sheet1, fmt.Sprintf("A%d", curRow), fmt.Sprintf("%d.1", deptIdx+1))
			_ = f.SetCellValue(sheet1, fmt.Sprintf("B%d", curRow), "    (暂无代码仓数据)")
			_ = f.SetRowStyle(sheet1, curRow, curRow, repoRowStyle)
			_ = f.SetCellStyle(sheet1, fmt.Sprintf("A%d", curRow), fmt.Sprintf("H%d", curRow), repoCenterStyle)
			_ = f.SetRowOutlineLevel(sheet1, curRow, 1)
		} else {
			for subIdx, rItem := range subRepos {
				curRow++
				subRow := curRow
				_ = f.SetRowHeight(sheet1, subRow, 20)

				rRateVal := rItem.FixRate
				if isEntityMode {
					rRateVal = rItem.PassRate
				}

				lastScanStr := "未扫描"
				if !rItem.LastScanTime.IsZero() && rItem.LastScanTime.Year() > 2000 {
					lastScanStr = rItem.LastScanTime.Format("2006-01-02 15:04")
				}

				_ = f.SetCellValue(sheet1, fmt.Sprintf("A%d", subRow), fmt.Sprintf("%d.%d", deptIdx+1, subIdx+1))
				_ = f.SetCellValue(sheet1, fmt.Sprintf("B%d", subRow), "    "+rItem.RepoName)
				_ = f.SetCellValue(sheet1, fmt.Sprintf("C%d", subRow), rItem.OwnerName)
				if isEntityMode {
					_ = f.SetCellValue(sheet1, fmt.Sprintf("D%d", subRow), rItem.TotalEntities)
					_ = f.SetCellValue(sheet1, fmt.Sprintf("E%d", subRow), rItem.PassCount)
					_ = f.SetCellValue(sheet1, fmt.Sprintf("F%d", subRow), rItem.OpenIssues)
				} else {
					_ = f.SetCellValue(sheet1, fmt.Sprintf("D%d", subRow), rItem.OpenIssues)
					_ = f.SetCellValue(sheet1, fmt.Sprintf("E%d", subRow), rItem.Blocking)
					_ = f.SetCellValue(sheet1, fmt.Sprintf("F%d", subRow), rItem.Critical)
				}
				_ = f.SetCellValue(sheet1, fmt.Sprintf("G%d", subRow), fmt.Sprintf("%.0f%%", rRateVal))
				_ = f.SetCellValue(sheet1, fmt.Sprintf("H%d", subRow), lastScanStr)

				_ = f.SetRowStyle(sheet1, subRow, subRow, repoRowStyle)
				_ = f.SetCellStyle(sheet1, fmt.Sprintf("A%d", subRow), fmt.Sprintf("A%d", subRow), repoCenterStyle)
				_ = f.SetCellStyle(sheet1, fmt.Sprintf("C%d", subRow), fmt.Sprintf("H%d", subRow), repoCenterStyle)
				if !isEntityMode {
					if rItem.Blocking > 0 {
						_ = f.SetCellStyle(sheet1, fmt.Sprintf("E%d", subRow), fmt.Sprintf("E%d", subRow), repoDangerStyle)
					}
					if rItem.Critical > 0 {
						_ = f.SetCellStyle(sheet1, fmt.Sprintf("F%d", subRow), fmt.Sprintf("F%d", subRow), repoWarningStyle)
					}
				}

				// 设置 Excel 行大纲折叠级别为 1
				_ = f.SetRowOutlineLevel(sheet1, subRow, 1)
			}
		}
	}

	adjustCampaignColWidth(f, sheet1, len(headers))

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
