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

// CalculateRating 返回风险分估值说明（风险分仅为参考估值，不进行好坏等级评价）
func CalculateRating(score int) string {
	return "风险估值"
}

// BuildMetaDTO 构建任务元数据 DTO
func BuildMetaDTO(report *models.TaskReport) ReportMetaDTO {
	govMode := report.TaskType.GovernanceMode
	if govMode == "" {
		govMode = models.GovernanceModeDefectTracking
	}

	var durationSec float64
	if sumBytes, err := os.ReadFile(report.GetSummaryJSONPath()); err == nil {
		var summaryMap map[string]interface{}
		if err := json.Unmarshal(sumBytes, &summaryMap); err == nil {
			if dur, ok := summaryMap["duration_seconds"].(float64); ok {
				durationSec = dur
			}
		}
	}

	var baseReportID uint
	var vanishedGap, templateFam, activeWork, dormantArch, resolvedChg int
	if models.DB != nil {
		var recon models.ScanReconciliation
		if err := models.DB.Where("current_report_id = ?", report.ID).Order("id desc").First(&recon).Error; err == nil {
			baseReportID = recon.BaseReportID
			vanishedGap = recon.VanishedCoverageGap
			templateFam = recon.TemplateFamilyCount
			activeWork = recon.ActiveWorkingCount
			dormantArch = recon.DormantArchivedCount
			resolvedChg = recon.ResolvedByChangeCount
		}
	}

	return ReportMetaDTO{
		ID:                    report.ID,
		RepoID:                report.RepoID,
		RepoName:              report.Repo.Name,
		RepoURL:               report.Repo.URL,
		Branch:                report.Repo.Branch,
		TaskTypeID:            report.TaskTypeID,
		TaskTypeName:          report.TaskType.Name,
		TaskTypeDisplay:       report.TaskType.DisplayName,
		EngineMode:            report.TaskType.EngineMode,
		GovernanceMode:        govMode,
		Status:                report.Status,
		Score:                 report.Score,
		Rating:                CalculateRating(report.Score),
		TotalChunks:           report.TotalChunks,
		ProcessedChunks:       report.ProcessedChunks,
		SuccessChunks:         report.SuccessChunks,
		DurationSeconds:       durationSec,
		BaseCommit:            report.BaseCommit,
		HeadCommit:            report.HeadCommit,
		NewDefectsCount:       report.NewDefectsCount,
		ExistedDefectsCount:   report.ExistedDefectsCount,
		ResolvedDefectsCount:  report.ResolvedDefectsCount,
		BaseReportID:          baseReportID,
		VanishedCoverageGap:   vanishedGap,
		TemplateFamilyCount:   templateFam,
		ActiveWorkingCount:    activeWork,
		DormantArchivedCount:  dormantArch,
		ResolvedByChangeCount: resolvedChg,
		Tier1Tokens:           report.Tier1Tokens,
		Tier2Tokens:           report.Tier2Tokens,
		CreatedAt:             report.CreatedAt,
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

	// 1. 读取 disk 上 synthesis findings json (支持新版台账 SSOT 与旧版平铺数组双模解析)
	jsonPath := report.GetSynthesisJSONPath()
	var rawList []map[string]interface{}

	if jsonBytes, err := os.ReadFile(jsonPath); err == nil {
		if errUnmarshal := json.Unmarshal(jsonBytes, &rawList); errUnmarshal != nil || len(rawList) == 0 {
			var ledgerMap map[string]interface{}
			if errLedger := json.Unmarshal(jsonBytes, &ledgerMap); errLedger == nil {
				if itemsRaw, ok := ledgerMap["items"].([]interface{}); ok {
					for _, it := range itemsRaw {
						if itm, ok := it.(map[string]interface{}); ok {
							flattened := make(map[string]interface{})
							if p, ok := itm["payload"].(map[string]interface{}); ok {
								for k, v := range p {
									flattened[k] = v
								}
							}
							for k, v := range itm {
								if k != "payload" {
									flattened[k] = v
								}
							}
							rawList = append(rawList, flattened)
						}
					}
				}
			}
		}
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
				comment := ExtractLatestComment(dbf.StatusLog)
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

		// 智能体辩论与指纹增量字段
		fingerprint := getMapString(raw, "fingerprint", "Fingerprint")
		diffStatus := getMapString(raw, "diff_status", "DiffStatus")
		triggerLine := getMapString(raw, "trigger_line", "TriggerLine")
		scopeSymbol := getMapString(raw, "scope_symbol", "ScopeSymbol")
		hunterClaim := getMapString(raw, "hunter_claim", "HunterClaim")
		challengerArg := getMapString(raw, "challenger_arg", "ChallengerArg")
		judgeVerdict := getMapString(raw, "judge_verdict", "JudgeVerdict")

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

		// 报告对账与台账治理字段
		itemUID := getMapString(raw, "item_uid", "ItemUID")
		lifecycleStatus := getMapString(raw, "lifecycle_status", "LifecycleStatus")
		templateFamilyID := getMapString(raw, "template_family_id", "TemplateFamilyID")
		reconRelation := getMapString(raw, "recon_relation", "ReconRelation")
		severityRange := getMapString(raw, "severity_range", "SeverityRange")
		note := getMapString(raw, "note", "Note")
		coverageGap := false
		if cg, ok := raw["coverage_gap"].(bool); ok {
			coverageGap = cg
		}
		severityTriage := false
		if st, ok := raw["severity_triage"].(bool); ok {
			severityTriage = st
		}

		items = append(items, FindingItemDTO{
			ID:               idVal,
			TaskReportID:     report.ID,
			TaskTypeID:       report.TaskTypeID,
			RepoID:           report.RepoID,
			Severity:         canonicalSev,
			SeverityDisplay:  sevDisplay,
			Category:         category,
			FilePath:         filePath,
			LineNumber:       lineNumber,
			Title:            title,
			Detail:           detail,
			CodeSnippet:      codeSnippet,
			Suggestion:       suggestion,
			Status:           statusVal,
			StatusDisplay:    statusDisplay,
			AssigneeID:       assigneeID,
			AssigneeName:     assigneeName,
			LatestComment:    latestComment,
			Fingerprint:      fingerprint,
			DiffStatus:       diffStatus,
			TriggerLine:      triggerLine,
			ScopeSymbol:      scopeSymbol,
			HunterClaim:      hunterClaim,
			ChallengerArg:    challengerArg,
			JudgeVerdict:     judgeVerdict,
			ItemUID:          itemUID,
			LifecycleStatus:  lifecycleStatus,
			CoverageGap:      coverageGap,
			TemplateFamilyID: templateFamilyID,
			ReconRelation:    reconRelation,
			SeverityRange:    severityRange,
			SeverityTriage:   severityTriage,
			Note:             note,
			CreatedAt:        createdAt,
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
		if query.Severity != "" {
			sevs := strings.Split(query.Severity, ",")
			matched := false
			for _, s := range sevs {
				s = strings.TrimSpace(s)
				if s != "" && item.Severity == NormalizeSeverity(s) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
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

			// 构建时序步骤 (完全基于实际执行记录，杜绝硬编码虚构耗时)
			step1Name := "代码静态分析"
			if report.TaskType.EngineMode == "debate_full" {
				step1Name = "智能体对抗辩论 (Hunter ➜ Challenger ➜ Judge)"
			} else if report.TaskType.EngineMode == "debate_selective" {
				step1Name = "选择性智能体辩论初筛与仲裁"
			} else if report.TaskType.EngineMode == "chunked_fast" {
				step1Name = "语义分片与确定性规则初筛"
			}

			dto.PipelineSteps = []PipelineStep{
				{Name: step1Name, Status: getStepStatus(summaryMap, "analysis"), DurationSeconds: dto.AnalysisDuration},
				{Name: "确定性校准与综合报告生成", Status: getStepStatus(summaryMap, "synthesis"), DurationSeconds: getStepDuration(summaryMap, "synthesis")},
				{Name: "缺陷指纹对比与闭环入库", Status: getStepStatus(summaryMap, "merging"), DurationSeconds: getStepDuration(summaryMap, "merging")},
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

	diagnostics, diagErr := GetReportDiagnostics(taskID)
	if diagErr != nil || diagnostics == nil {
		diagnostics = &DiagnosticsDTO{
			Meta: summary.Meta,
		}
	}

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

// ExtractLatestComment 从 StatusLog 字节数据中提取最近一次有效的跟踪意见或原因
func ExtractLatestComment(statusLogBytes []byte) string {
	if len(statusLogBytes) == 0 {
		return ""
	}
	var logs []map[string]interface{}
	if err := json.Unmarshal(statusLogBytes, &logs); err != nil || len(logs) == 0 {
		return ""
	}
	// 优先从后往前查找最近一次填写的 comment
	for i := len(logs) - 1; i >= 0; i-- {
		if comment, ok := logs[i]["comment"].(string); ok && strings.TrimSpace(comment) != "" {
			return comment
		}
	}
	// 若无 comment，再从后往前查找最近一次的原因记录 (例如系统自动识别/关闭原因)
	for i := len(logs) - 1; i >= 0; i-- {
		if reason, ok := logs[i]["reason"].(string); ok && strings.TrimSpace(reason) != "" {
			return reason
		}
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
