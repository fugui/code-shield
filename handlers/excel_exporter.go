package handlers

import (
	"fmt"
	"log"
	"net/http"
	"reflect"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"gorm.io/datatypes"
)

// ExcelFindingItem 定义 Excel 导出通用的缺陷属性结构
type ExcelFindingItem struct {
	ID            uint
	Severity      string
	Category      string
	FilePath      string
	LineNumber    string
	Title         string
	Detail        string
	Suggestion    string
	Status        string
	AssigneeName  string
	LatestComment string
	CreatedAt     time.Time
}

// convertToExcelItems 使用反射将各种类型的 Finding 切片（如 TestCaseFinding, CoredumpFinding 等）统一转换为 ExcelFindingItem 切片
func convertToExcelItems(sliceVal reflect.Value, isUT bool) []ExcelFindingItem {
	if sliceVal.Kind() != reflect.Slice {
		return nil
	}
	n := sliceVal.Len()
	items := make([]ExcelFindingItem, n)
	for i := 0; i < n; i++ {
		v := sliceVal.Index(i)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}

		getFieldValueReflect := func(fieldName string) reflect.Value {
			return v.FieldByName(fieldName)
		}

		getStringField := func(fieldName string) string {
			f := getFieldValueReflect(fieldName)
			if f.IsValid() && f.Kind() == reflect.String {
				return f.String()
			}
			return ""
		}

		getUintField := func(fieldName string) uint {
			f := getFieldValueReflect(fieldName)
			if f.IsValid() {
				if f.Kind() == reflect.Uint || f.Kind() == reflect.Uint64 || f.Kind() == reflect.Uint32 || f.Kind() == reflect.Uint16 || f.Kind() == reflect.Uint8 {
					return uint(f.Uint())
				}
			}
			return 0
		}

		getTimeField := func(fieldName string) time.Time {
			f := getFieldValueReflect(fieldName)
			if f.IsValid() {
				if t, ok := f.Interface().(time.Time); ok {
					return t
				}
			}
			return time.Time{}
		}

		var title string
		if isUT {
			title = getStringField("TestCaseName")
		} else {
			title = getStringField("Title")
		}

		// 负责人映射
		var assigneeName string
		assigneeField := getFieldValueReflect("Assignee")
		if assigneeField.IsValid() && !assigneeField.IsNil() {
			assigneeElem := assigneeField.Elem()
			nameField := assigneeElem.FieldByName("Name")
			if nameField.IsValid() && nameField.Kind() == reflect.String {
				assigneeName = nameField.String()
			}
		}

		// 最新评论提取
		var latestComment string
		statusLogField := getFieldValueReflect("StatusLog")
		if statusLogField.IsValid() {
			if bytes, ok := statusLogField.Interface().(datatypes.JSON); ok && len(bytes) > 0 {
				latestComment = getLatestComment(bytes)
			} else if bytes, ok := statusLogField.Interface().([]byte); ok && len(bytes) > 0 {
				latestComment = getLatestComment(bytes)
			}
		}

		// 兜底处理：部分旧格式可能会存在在 Feedback 中
		if latestComment == "" {
			feedbackField := getFieldValueReflect("Feedback")
			if feedbackField.IsValid() && feedbackField.Kind() == reflect.String {
				latestComment = feedbackField.String()
			}
		}

		items[i] = ExcelFindingItem{
			ID:            getUintField("ID"),
			Severity:      getStringField("Severity"),
			Category:      getStringField("Category"),
			FilePath:      getStringField("FilePath"),
			LineNumber:    getStringField("LineNumber"),
			Title:         title,
			Detail:        getStringField("Detail"),
			Suggestion:    getStringField("Suggestion"),
			Status:        getStringField("Status"),
			AssigneeName:  assigneeName,
			LatestComment: latestComment,
			CreatedAt:     getTimeField("CreatedAt"),
		}
	}
	return items
}

// generateCampaignExcel 生成带有双 Sheet 以及解决率透视与原生饼图的 Excel 文件
func generateCampaignExcel(c *gin.Context, repoName string, campaignTitle string, items []ExcelFindingItem, isUT bool) {
	f := excelize.NewFile()
	defer func() {
		_ = f.Close()
	}()

	// Sheet 1: 缺陷明细
	sheet1 := "缺陷明细"
	index1, err := f.NewSheet(sheet1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建工作表失败: " + err.Error()})
		return
	}

	// 统一的高级表头样式（深 Slate 背景，白色粗体字）
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"334155"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建 Excel 样式失败: " + err.Error()})
		return
	}

	// 数据行样式
	dataStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 10, Color: "334155"},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建 Excel 样式失败: " + err.Error()})
		return
	}

	// 写入 Sheet 1 表头
	headers := []string{"序号 (ID)", "严重程度", "缺陷分类", "文件路径", "行号", "问题标题/用例名称", "详细描述", "修复建议", "状态", "责任人", "最新跟踪意见", "发现时间"}
	for colIdx, name := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		f.SetCellValue(sheet1, cell, name)
	}
	f.SetRowStyle(sheet1, 1, 1, headerStyle)
	f.SetRowHeight(sheet1, 1, 28)

	// 写入 Sheet 1 数据
	for idx, item := range items {
		row := idx + 2
		f.SetCellValue(sheet1, fmt.Sprintf("A%d", row), item.ID)
		f.SetCellValue(sheet1, fmt.Sprintf("B%d", row), item.Severity)
		f.SetCellValue(sheet1, fmt.Sprintf("C%d", row), item.Category)
		f.SetCellValue(sheet1, fmt.Sprintf("D%d", row), item.FilePath)
		f.SetCellValue(sheet1, fmt.Sprintf("E%d", row), item.LineNumber)
		f.SetCellValue(sheet1, fmt.Sprintf("F%d", row), item.Title)
		f.SetCellValue(sheet1, fmt.Sprintf("G%d", row), item.Detail)
		f.SetCellValue(sheet1, fmt.Sprintf("H%d", row), item.Suggestion)
		f.SetCellValue(sheet1, fmt.Sprintf("I%d", row), getStatusChinese(item.Status, isUT))
		f.SetCellValue(sheet1, fmt.Sprintf("J%d", row), item.AssigneeName)
		f.SetCellValue(sheet1, fmt.Sprintf("K%d", row), item.LatestComment)
		f.SetCellValue(sheet1, fmt.Sprintf("L%d", row), item.CreatedAt.Format("2006-01-02 15:04:05"))
		f.SetRowHeight(sheet1, row, 22)
		f.SetRowStyle(sheet1, row, row, dataStyle)
	}

	// Sheet 2: 数据统计与图表
	sheet2 := "数据统计与图表"
	_, err = f.NewSheet(sheet2)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建统计工作表失败: " + err.Error()})
		return
	}

	// 缺陷数据分类统计
	statusCounts := map[string]int{"open": 0, "analyzing": 0, "resolved": 0, "closed": 0, "invalid": 0}
	severityCounts := map[string]int{"致命": 0, "阻塞": 0, "严重": 0, "一般": 0, "主要": 0, "提示": 0, "建议": 0, "合格": 0}
	assigneeStatsMap := make(map[string]*struct{ Total, Resolved int })
	categoryCounts := make(map[string]int)

	for _, item := range items {
		statusCounts[item.Status]++
		severityCounts[item.Severity]++
		categoryCounts[item.Category]++

		assignee := item.AssigneeName
		if assignee == "" {
			assignee = "未分配"
		}
		if _, ok := assigneeStatsMap[assignee]; !ok {
			assigneeStatsMap[assignee] = &struct{ Total, Resolved int }{}
		}
		assigneeStatsMap[assignee].Total++
		if item.Status == "resolved" || item.Status == "closed" {
			assigneeStatsMap[assignee].Resolved++
		}
	}

	// 页面标题
	titleStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 14, Color: "0F172A"},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err == nil {
		f.SetCellValue(sheet2, "A1", fmt.Sprintf("【%s】专题分析缺陷解决透视图 - %s", campaignTitle, repoName))
		f.SetRowStyle(sheet2, 1, 1, titleStyle)
		f.SetRowHeight(sheet2, 1, 32)
	}

	// 统计表表头与合计行的样式
	tblHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF", Size: 10},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"475569"}, Pattern: 1}, // Muted Slate
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
		},
	})

	totalStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 10, Color: "1E293B"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"F1F5F9"}, Pattern: 1},
		Border: []excelize.Border{
			{Type: "top", Color: "94A3B8", Style: 1},
			{Type: "bottom", Color: "94A3B8", Style: 6}, // 双底边线
		},
	})

	pctStyle, _ := f.NewStyle(&excelize.Style{
		NumFmt: 9, // "0%" 百分比格式
		Font: &excelize.Font{Size: 10, Color: "334155"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "E2E8F0", Style: 1},
			{Type: "top", Color: "E2E8F0", Style: 1},
			{Type: "bottom", Color: "E2E8F0", Style: 1},
			{Type: "right", Color: "E2E8F0", Style: 1},
		},
	})

	// --- 1. 缺陷状态统计表 (A3:B9) ---
	f.SetCellValue(sheet2, "A3", "缺陷状态")
	f.SetCellValue(sheet2, "B3", "数量")
	f.SetRowStyle(sheet2, 3, 3, tblHeaderStyle)

	statusNames := []string{"open", "analyzing", "resolved", "closed", "invalid"}
	for idx, s := range statusNames {
		row := idx + 4
		f.SetCellValue(sheet2, fmt.Sprintf("A%d", row), getStatusChinese(s, isUT))
		f.SetCellValue(sheet2, fmt.Sprintf("B%d", row), statusCounts[s])
		f.SetRowStyle(sheet2, row, row, dataStyle)
	}
	f.SetCellValue(sheet2, "A9", "合计")
	f.SetCellFormula(sheet2, "B9", "=SUM(B4:B8)")
	f.SetRowStyle(sheet2, 9, 9, totalStyle)

	// --- 2. 严重程度统计表 (D3:E9) ---
	f.SetCellValue(sheet2, "D3", "严重程度")
	f.SetCellValue(sheet2, "E3", "数量")
	f.SetRowStyle(sheet2, 3, 3, tblHeaderStyle)

	severityNames := []string{"致命", "严重", "一般", "建议", "合格"}
	for idx, sev := range severityNames {
		row := idx + 4
		cnt := severityCounts[sev]
		if sev == "致命" {
			cnt += severityCounts["阻塞"]
		} else if sev == "一般" {
			cnt += severityCounts["主要"] + severityCounts["提示"]
		}
		f.SetCellValue(sheet2, fmt.Sprintf("D%d", row), sev)
		f.SetCellValue(sheet2, fmt.Sprintf("E%d", row), cnt)
		f.SetRowStyle(sheet2, row, row, dataStyle)
	}
	f.SetCellValue(sheet2, "D9", "合计")
	f.SetCellFormula(sheet2, "E9", "=SUM(E4:E8)")
	f.SetRowStyle(sheet2, 9, 9, totalStyle)

	// --- 3. 责任人解决进度统计表 (G3:J...) ---
	f.SetCellValue(sheet2, "G3", "责任人")
	f.SetCellValue(sheet2, "H3", "总缺陷数")
	f.SetCellValue(sheet2, "I3", "已解决数")
	f.SetCellValue(sheet2, "J3", "解决率")
	f.SetRowStyle(sheet2, 3, 3, tblHeaderStyle)

	var assignees []string
	for k := range assigneeStatsMap {
		assignees = append(assignees, k)
	}
	sort.Strings(assignees)

	for idx, ass := range assignees {
		row := idx + 4
		stats := assigneeStatsMap[ass]
		f.SetCellValue(sheet2, fmt.Sprintf("G%d", row), ass)
		f.SetCellValue(sheet2, fmt.Sprintf("H%d", row), stats.Total)
		f.SetCellValue(sheet2, fmt.Sprintf("I%d", row), stats.Resolved)
		f.SetCellFormula(sheet2, fmt.Sprintf("J%d", row), fmt.Sprintf("=IF(H%d>0, I%d/H%d, 0)", row, row, row))
		f.SetRowStyle(sheet2, row, row, dataStyle)
		f.SetCellStyle(sheet2, fmt.Sprintf("J%d", row), fmt.Sprintf("J%d", row), pctStyle)
	}
	totalAssigneesRow := len(assignees) + 4
	f.SetCellValue(sheet2, fmt.Sprintf("G%d", totalAssigneesRow), "合计")
	f.SetCellFormula(sheet2, fmt.Sprintf("H%d", totalAssigneesRow), fmt.Sprintf("=SUM(H4:H%d)", totalAssigneesRow-1))
	f.SetCellFormula(sheet2, fmt.Sprintf("I%d", totalAssigneesRow), fmt.Sprintf("=SUM(I4:I%d)", totalAssigneesRow-1))
	f.SetCellFormula(sheet2, fmt.Sprintf("J%d", totalAssigneesRow), fmt.Sprintf("=IF(H%d>0, I%d/H%d, 0)", totalAssigneesRow, totalAssigneesRow, totalAssigneesRow))
	f.SetRowStyle(sheet2, totalAssigneesRow, totalAssigneesRow, totalStyle)
	f.SetCellStyle(sheet2, fmt.Sprintf("J%d", totalAssigneesRow), fmt.Sprintf("J%d", totalAssigneesRow), pctStyle)

	// --- 4. 缺陷分类统计表 (L3:M...) ---
	f.SetCellValue(sheet2, "L3", "缺陷分类")
	f.SetCellValue(sheet2, "M3", "数量")
	f.SetRowStyle(sheet2, 3, 3, tblHeaderStyle)

	var categories []string
	for k := range categoryCounts {
		if k != "" {
			categories = append(categories, k)
		}
	}
	sort.Strings(categories)
	if len(categories) == 0 {
		categories = append(categories, "常规检测")
	}

	for idx, cat := range categories {
		row := idx + 4
		cnt := categoryCounts[cat]
		if cat == "常规检测" {
			cnt = categoryCounts[""]
		}
		f.SetCellValue(sheet2, fmt.Sprintf("L%d", row), cat)
		f.SetCellValue(sheet2, fmt.Sprintf("M%d", row), cnt)
		f.SetRowStyle(sheet2, row, row, dataStyle)
	}
	totalCategoriesRow := len(categories) + 4
	f.SetCellValue(sheet2, fmt.Sprintf("L%d", totalCategoriesRow), "合计")
	f.SetCellFormula(sheet2, fmt.Sprintf("M%d", totalCategoriesRow), fmt.Sprintf("=SUM(M4:M%d)", totalCategoriesRow-1))
	f.SetRowStyle(sheet2, totalCategoriesRow, totalCategoriesRow, totalStyle)

	// 设置统一的行高以体现美感
	maxStatsRow := totalAssigneesRow
	if totalCategoriesRow > maxStatsRow {
		maxStatsRow = totalCategoriesRow
	}
	if maxStatsRow < 9 {
		maxStatsRow = 9
	}
	for r := 2; r <= maxStatsRow; r++ {
		f.SetRowHeight(sheet2, r, 20)
	}

	// --- 5. 插入原生 Excel 状态饼图 ---
	chartCell := "O2"
	err = f.AddChart(sheet2, chartCell, &excelize.Chart{
		Type: excelize.Pie,
		Series: []excelize.ChartSeries{
			{
				Name:       fmt.Sprintf("'%s'!$B$3", sheet2),
				Categories: fmt.Sprintf("'%s'!$A$4:$A$8", sheet2),
				Values:     fmt.Sprintf("'%s'!$B$4:$B$8", sheet2),
			},
		},
		Format: excelize.GraphicOptions{
			ScaleX: 0.9,
			ScaleY: 0.9,
		},
		Legend: excelize.ChartLegend{
			Position:      "right",
			ShowLegendKey: false,
		},
		Title: excelize.ChartTitle{
			Paragraph: []excelize.RichTextRun{
				{
					Text: "缺陷状态分布图",
					Font: &excelize.Font{Bold: true, Color: "0F172A", Size: 11},
				},
			},
		},
		PlotArea: excelize.ChartPlotArea{
			ShowPercent: true,
			ShowVal:     false,
		},
	})
	if err != nil {
		log.Printf("[ExcelExporter] Failed to add status distribution chart: %v", err)
	}

	// 删除系统默认创建的无用工作表，并设明细为首位活跃 Sheet
	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(index1)

	// 自动适配列宽以优化可读性
	adjustColWidth(f, sheet1, len(headers))
	adjustColWidth(f, sheet2, 13) // 给统计表分配较宽的列间距

	// 导出 HTTP 头配置，文件名格式：synthesis_[专题名称]_YYYY-MM-DD.xlsx
	filename := fmt.Sprintf("synthesis_%s_%s.xlsx", campaignTitle, time.Now().Format("2006-01-02"))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "导出 Excel 文件写入失败: " + err.Error()})
	}
}

// adjustColWidth 自适应优化调整 Excel 工作表列宽
func adjustColWidth(f *excelize.File, sheetName string, colCount int) {
	for colIdx := 1; colIdx <= colCount; colIdx++ {
		colName, _ := excelize.ColumnNumberToName(colIdx)
		cols, _ := f.GetCols(sheetName)
		if len(cols) < colIdx {
			continue
		}
		maxLen := 10 // 最小宽度为 10
		for _, val := range cols[colIdx-1] {
			actualLen := 0
			for _, r := range val {
				if r > 127 {
					actualLen += 2 // 中文字符加权占位宽度
				} else {
					actualLen += 1
				}
			}
			if actualLen > maxLen {
				maxLen = actualLen
			}
		}
		_ = f.SetColWidth(sheetName, colName, colName, float64(maxLen+4))
	}
}
