package reports

import (
	"code-shield/models"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CalculateRating 根据分数计算优/良/中/差评级
func CalculateRating(score int) string {
	if score >= 90 {
		return "优"
	} else if score >= 75 {
		return "良"
	} else if score >= 60 {
		return "中"
	}
	return "差"
}

// BuildMetaDTO 构建任务元数据 DTO
func BuildMetaDTO(report *models.TaskReport) ReportMetaDTO {
	govMode := report.TaskType.GovernanceMode
	if govMode == "" {
		govMode = models.GovernanceModeDefectTracking
	}

	return ReportMetaDTO{
		ID:              report.ID,
		RepoID:          report.RepoID,
		RepoName:        report.Repo.Name,
		RepoURL:         report.Repo.URL,
		Branch:          report.Repo.Branch,
		TaskTypeID:      report.TaskTypeID,
		TaskTypeName:    report.TaskType.Name,
		TaskTypeDisplay: report.TaskType.DisplayName,
		EngineMode:      report.TaskType.EngineMode,
		GovernanceMode:  govMode,
		Status:          report.Status,
		Score:           report.Score,
		Rating:          CalculateRating(report.Score),
		TotalChunks:     report.TotalChunks,
		ProcessedChunks: report.ProcessedChunks,
		SuccessChunks:   report.SuccessChunks,
		BaseCommit:      report.BaseCommit,
		HeadCommit:      report.HeadCommit,
		CreatedAt:       report.CreatedAt,
	}
}

// GetTaskReportEntity 加载包含 Repo 和 TaskType 的 TaskReport
func GetTaskReportEntity(taskID uint) (*models.TaskReport, error) {
	var report models.TaskReport
	if err := models.DB.Preload("Repo").Preload("TaskType").First(&report, taskID).Error; err != nil {
		return nil, fmt.Errorf("task report %d not found: %w", taskID, err)
	}
	return &report, nil
}

// loadAllFindingsRaw 加载并归一化任务的所有结构化 Findings（融合文件与数据库流转字段）
func loadAllFindingsRaw(report *models.TaskReport) ([]FindingItemDTO, error) {
	isEntityMode := report.TaskType.GovernanceMode == models.GovernanceModeEntityAssessment

	// 1. 读取 disk 上 synthesis findings json
	jsonPath := report.GetSynthesisJSONPath()
	var rawList []map[string]interface{}

	if jsonBytes, err := os.ReadFile(jsonPath); err == nil {
		_ = json.Unmarshal(jsonBytes, &rawList)
	}

	// 2. 查询 DB 中的流转状态与责任人信息 (优先 CampaignFinding，回退 AnalysisFinding)
	statusMap := make(map[string]string)
	assigneeMap := make(map[string]string)
	assigneeIDMap := make(map[string]*uint)
	commentMap := make(map[string]string)
	createdAtMap := make(map[string]*time.Time)

	if models.DB != nil {
		var dbCampaignFindings []models.CampaignFinding
		if err := models.DB.Preload("Assignee").Where("task_report_id = ?", report.ID).Find(&dbCampaignFindings).Error; err == nil && len(dbCampaignFindings) > 0 {
			for _, dbf := range dbCampaignFindings {
				k1 := makeFindingKey(dbf.FilePath, dbf.LineNumber, dbf.Title)
				k2 := makeTestCaseKey(dbf.FilePath, dbf.Title)
				statusMap[k1] = dbf.Status
				statusMap[k2] = dbf.Status
				if dbf.Assignee != nil {
					assigneeMap[k1] = dbf.Assignee.Name
					assigneeMap[k2] = dbf.Assignee.Name
					assigneeIDMap[k1] = dbf.AssigneeID
					assigneeIDMap[k2] = dbf.AssigneeID
				}
				comment := extractLatestComment(dbf.StatusLog)
				if comment == "" && dbf.Feedback != "" {
					comment = dbf.Feedback
				}
				commentMap[k1] = comment
				commentMap[k2] = comment
				t := dbf.CreatedAt
				createdAtMap[k1] = &t
				createdAtMap[k2] = &t
			}
		} else {
			var dbFindings []models.AnalysisFinding
			if err := models.DB.Preload("Assignee").Where("task_report_id = ?", report.ID).Find(&dbFindings).Error; err == nil {
				for _, dbf := range dbFindings {
					k := makeFindingKey(dbf.FilePath, dbf.LineNumber, dbf.Title)
					statusMap[k] = "open"
					if dbf.Assignee != nil {
						assigneeMap[k] = dbf.Assignee.Name
						assigneeIDMap[k] = dbf.AssigneeID
					}
					commentMap[k] = dbf.Feedback
					t := dbf.CreatedAt
					createdAtMap[k] = &t
				}
			}
		}
	}

	items := make([]FindingItemDTO, 0, len(rawList))
	for idx, raw := range rawList {
		filePath := getMapString(raw, "file_path", "FilePath")
		lineNumber := getMapString(raw, "line_number", "LineNumber")
		title := getMapString(raw, "title", "Title")
		rawSev := getMapString(raw, "severity", "Severity")
		category := getMapString(raw, "category", "Category")
		detail := getMapString(raw, "detail", "Detail")
		suggestion := getMapString(raw, "suggestion", "Suggestion")
		codeSnippet := getMapString(raw, "code_snippet", "CodeSnippet")

		canonicalSev := NormalizeSeverity(rawSev)
		sevDisplay := GetSeverityChinese(canonicalSev)

		var statusVal, assigneeName, latestComment string
		var assigneeID *uint
		var createdAt *time.Time

		if isEntityMode {
			k := makeTestCaseKey(filePath, title)
			statusVal = statusMap[k]
			assigneeName = assigneeMap[k]
			assigneeID = assigneeIDMap[k]
			latestComment = commentMap[k]
			createdAt = createdAtMap[k]
		} else {
			k := makeFindingKey(filePath, lineNumber, title)
			statusVal = statusMap[k]
			assigneeName = assigneeMap[k]
			assigneeID = assigneeIDMap[k]
			latestComment = commentMap[k]
			createdAt = createdAtMap[k]
		}

		if statusVal == "" {
			if isEntityMode {
				statusVal = "pass"
				if canonicalSev == SeverityFatal || canonicalSev == SeverityCritical || canonicalSev == SeverityMajor {
					statusVal = "fail"
				}
			} else {
				statusVal = "open"
			}
		}

		statusDisplay := GetStatusChinese(statusVal, isEntityMode)

		idVal := uint(idx + 1)
		if rawID, ok := raw["id"].(float64); ok && rawID > 0 {
			idVal = uint(rawID)
		}

		items = append(items, FindingItemDTO{
			ID:              idVal,
			TaskReportID:    report.ID,
			TaskTypeID:      report.TaskTypeID,
			RepoID:          report.RepoID,
			Severity:        canonicalSev,
			SeverityDisplay: sevDisplay,
			Category:        category,
			FilePath:        filePath,
			LineNumber:      lineNumber,
			Title:           title,
			Detail:          detail,
			CodeSnippet:     codeSnippet,
			Suggestion:      suggestion,
			Status:          statusVal,
			StatusDisplay:   statusDisplay,
			AssigneeID:      assigneeID,
			AssigneeName:    assigneeName,
			LatestComment:   latestComment,
			CreatedAt:       createdAt,
		})
	}

	return items, nil
}

// computeKPIMetrics 从 items 计算统计分布指标
func computeKPIMetrics(items []FindingItemDTO, isEntityMode bool) KPIMetrics {
	metrics := KPIMetrics{
		TotalFindings: len(items),
		CategoryStats: make(map[string]int),
		StatusStats:   make(map[string]int),
	}

	for _, item := range items {
		switch item.Severity {
		case SeverityFatal:
			metrics.FatalCount++
		case SeverityCritical:
			metrics.CriticalCount++
		case SeverityMajor:
			metrics.MajorCount++
		case SeverityMinor:
			metrics.MinorCount++
		case SeveritySuggestion:
			metrics.SuggestionCount++
		case SeverityPass:
			metrics.PassCount++
		}

		if item.Category != "" {
			metrics.CategoryStats[item.Category]++
		}
		if item.Status != "" {
			metrics.StatusStats[item.Status]++
		}
	}

	if isEntityMode && metrics.TotalFindings > 0 {
		passCount := metrics.PassCount
		if passCount == 0 && metrics.StatusStats["pass"] > 0 {
			passCount = metrics.StatusStats["pass"]
		}
		metrics.PassRate = float64(passCount) / float64(metrics.TotalFindings) * 100
	}

	return metrics
}

// GetReportSummary 获取轻量级总结概览
func GetReportSummary(taskID uint) (*ReportSummaryDTO, error) {
	report, err := GetTaskReportEntity(taskID)
	if err != nil {
		return nil, err
	}

	meta := BuildMetaDTO(report)

	// 读取 Markdown 报告内容
	mdContent := ""
	if absMd := report.GetAbsReportPath(); absMd != "" {
		if content, err := os.ReadFile(absMd); err == nil {
			mdContent = string(content)
		}
	}
	if mdContent == "" && report.AISummary != "" {
		mdContent = report.AISummary
	}

	// 读取所有 findings 用于快速计算轻量级 KPI
	isEntityMode := report.TaskType.GovernanceMode == models.GovernanceModeEntityAssessment
	findings, _ := loadAllFindingsRaw(report)
	metrics := computeKPIMetrics(findings, isEntityMode)

	return &ReportSummaryDTO{
		Meta:            meta,
		MarkdownContent: mdContent,
		Metrics:         metrics,
	}, nil
}

// GetReportFindings 获取按需过滤与分页的详细清单
func GetReportFindings(taskID uint, query FindingsQuery) (*FindingsPageDTO, error) {
	report, err := GetTaskReportEntity(taskID)
	if err != nil {
		return nil, err
	}

	isEntityMode := report.TaskType.GovernanceMode == models.GovernanceModeEntityAssessment
	allItems, err := loadAllFindingsRaw(report)
	if err != nil {
		return nil, err
	}

	metrics := computeKPIMetrics(allItems, isEntityMode)

	// 过滤
	var filtered []FindingItemDTO
	for _, item := range allItems {
		if query.Severity != "" && item.Severity != NormalizeSeverity(query.Severity) {
			continue
		}
		if query.Status != "" && !strings.EqualFold(item.Status, query.Status) {
			continue
		}
		if query.Category != "" && !strings.EqualFold(item.Category, query.Category) {
			continue
		}
		if query.AssigneeID != "" {
			if aID, _ := strconv.Atoi(query.AssigneeID); uint(aID) != 0 {
				if item.AssigneeID == nil || *item.AssigneeID != uint(aID) {
					continue
				}
			}
		}
		if query.Keyword != "" {
			kw := strings.ToLower(query.Keyword)
			match := strings.Contains(strings.ToLower(item.Title), kw) ||
				strings.Contains(strings.ToLower(item.FilePath), kw) ||
				strings.Contains(strings.ToLower(item.Detail), kw) ||
				strings.Contains(strings.ToLower(item.Suggestion), kw)
			if !match {
				continue
			}
		}
		filtered = append(filtered, item)
	}

	// 排序
	sortFindings(filtered, query.SortField, query.SortOrder)

	total := int64(len(filtered))
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	if end > len(filtered) {
		end = len(filtered)
	}

	pagedItems := filtered[start:end]

	return &FindingsPageDTO{
		Items:      pagedItems,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		Metrics:    metrics,
	}, nil
}

// GetReportDiagnostics 获取运行轨迹与诊断
func GetReportDiagnostics(taskID uint) (*DiagnosticsDTO, error) {
	report, err := GetTaskReportEntity(taskID)
	if err != nil {
		return nil, err
	}

	meta := BuildMetaDTO(report)
	dto := &DiagnosticsDTO{
		Meta: meta,
	}

	// 读取 diagnostics.json 或 summary json
	sumPath := report.GetSummaryJSONPath()
	if sumBytes, err := os.ReadFile(sumPath); err == nil {
		var summaryMap map[string]interface{}
		if err := json.Unmarshal(sumBytes, &summaryMap); err == nil {
			if dur, ok := summaryMap["duration_seconds"].(float64); ok {
				dto.TotalDuration = dur
			}

			if analysis, ok := summaryMap["analysis"].(map[string]interface{}); ok {
				if aDur, ok := analysis["duration_seconds"].(float64); ok {
					dto.AnalysisDuration = aDur
				}
				if chunksRaw, ok := analysis["chunks"].([]interface{}); ok {
					for _, cRaw := range chunksRaw {
						if cMap, ok := cRaw.(map[string]interface{}); ok {
							cName, _ := cMap["chunk_name"].(string)
							status, _ := cMap["status"].(string)
							dur, _ := cMap["duration_seconds"].(float64)
							attempts := 1
							if att, ok := cMap["attempts"].(float64); ok {
								attempts = int(att)
							}
							filesCount := 0
							var files []string
							if fl, ok := cMap["files"].([]interface{}); ok {
								filesCount = len(fl)
								for _, f := range fl {
									if s, ok := f.(string); ok {
										files = append(files, s)
									}
								}
							}
							findingsCount := 0
							if fc, ok := cMap["findings_count"].(float64); ok {
								findingsCount = int(fc)
							}
							errMsg, _ := cMap["error_message"].(string)

							dto.Chunks = append(dto.Chunks, ChunkDiagnosticDetail{
								ChunkName:       cName,
								Status:          status,
								DurationSeconds: dur,
								Attempts:        attempts,
								FilesCount:      filesCount,
								FindingsCount:   findingsCount,
								ErrorMessage:    errMsg,
								Files:           files,
							})
						}
					}
				}
			}

			// 构建时序步骤
			dto.PipelineSteps = []PipelineStep{
				{Name: "初始化克隆", Status: "success", DurationSeconds: 1.0},
				{Name: "前置检查", Status: "success", DurationSeconds: 0.5},
				{Name: "代码静态分析", Status: getStepStatus(summaryMap, "analysis"), DurationSeconds: dto.AnalysisDuration},
				{Name: "综合报告生成", Status: getStepStatus(summaryMap, "synthesis"), DurationSeconds: getStepDuration(summaryMap, "synthesis")},
				{Name: "缺陷归并与入库", Status: getStepStatus(summaryMap, "merging"), DurationSeconds: getStepDuration(summaryMap, "merging")},
			}
		}
	}

	// 读取执行输出日志（截取尾部 200 行）
	logPath := report.GetExecutionLogPath()
	if logBytes, err := os.ReadFile(logPath); err == nil {
		lines := strings.Split(string(logBytes), "\n")
		dto.TotalLogLines = len(lines)
		if len(lines) > 200 {
			dto.LogTruncated = true
			dto.RawOutputLog = strings.Join(lines[len(lines)-200:], "\n")
		} else {
			dto.RawOutputLog = string(logBytes)
		}
	}

	if report.Status == "failed" && len(dto.Chunks) > 0 {
		for _, ch := range dto.Chunks {
			if ch.Status == "failed" && ch.ErrorMessage != "" {
				dto.ErrorMessage = fmt.Sprintf("分片 %s 执行失败: %s", ch.ChunkName, ch.ErrorMessage)
				break
			}
		}
	}

	return dto, nil
}

// GetReportAggregate 获取一站式全量聚合数据
func GetReportAggregate(taskID uint) (*ReportAggregateDTO, error) {
	summary, err := GetReportSummary(taskID)
	if err != nil {
		return nil, err
	}

	findingsPage, err := GetReportFindings(taskID, FindingsQuery{Page: 1, PageSize: 500})
	if err != nil {
		return nil, err
	}

	diagnostics, _ := GetReportDiagnostics(taskID)

	return &ReportAggregateDTO{
		Meta:        summary.Meta,
		Summary:     *summary,
		Findings:    findingsPage.Items,
		Diagnostics: *diagnostics,
	}, nil
}

// 辅助函数
func getStepStatus(m map[string]interface{}, key string) string {
	if step, ok := m[key].(map[string]interface{}); ok {
		if s, ok := step["status"].(string); ok && s != "" {
			return s
		}
	}
	return "success"
}

func getStepDuration(m map[string]interface{}, key string) float64 {
	if step, ok := m[key].(map[string]interface{}); ok {
		if dur, ok := step["duration_seconds"].(float64); ok {
			return dur
		}
	}
	return 0.0
}

func sortFindings(items []FindingItemDTO, field, order string) {
	if field == "" {
		field = "severity"
	}
	isDesc := strings.ToLower(order) != "asc"

	sevWeight := map[string]int{
		SeverityFatal:      100,
		SeverityCritical:   80,
		SeverityMajor:      60,
		SeverityMinor:      40,
		SeveritySuggestion: 20,
		SeverityPass:       0,
	}

	sort.SliceStable(items, func(i, j int) bool {
		var less bool
		switch field {
		case "severity":
			w1 := sevWeight[items[i].Severity]
			w2 := sevWeight[items[j].Severity]
			less = w1 < w2
		case "file_path":
			less = items[i].FilePath < items[j].FilePath
		case "status":
			less = items[i].Status < items[j].Status
		case "id":
			less = items[i].ID < items[j].ID
		default:
			w1 := sevWeight[items[i].Severity]
			w2 := sevWeight[items[j].Severity]
			less = w1 < w2
		}

		if isDesc {
			return !less
		}
		return less
	})
}

func makeFindingKey(filePath, lineNumber, title string) string {
	return filePath + "|" + lineNumber + "|" + title
}

func makeTestCaseKey(filePath, testCaseName string) string {
	return filePath + "||" + testCaseName
}

func extractLatestComment(statusLogBytes []byte) string {
	if len(statusLogBytes) == 0 {
		return ""
	}
	var logs []map[string]interface{}
	if err := json.Unmarshal(statusLogBytes, &logs); err != nil || len(logs) == 0 {
		return ""
	}
	lastEntry := logs[len(logs)-1]
	if comment, ok := lastEntry["comment"].(string); ok {
		return comment
	}
	if reason, ok := lastEntry["reason"].(string); ok {
		return reason
	}
	return ""
}

func getMapString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if val, ok := m[k]; ok && val != nil {
			if s, ok := val.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}
