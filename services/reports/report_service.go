package reports

import (
	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines/assessment"
	assessmentprofiles "code-shield/services/engines/assessment/profiles"
	"code-shield/services/engines/planner"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	govMode := reportGovernanceMode(report)
	if govMode == "" {
		govMode = models.GovernanceModeFullLedger
	}

	var durationSec float64
	var scopeDecision *planner.ScopeDecision
	var planReconciliation *coverage.PlanReconciliation
	coverageComplete := report.CoverageComplete
	coverageDegraded := report.CoverageDegraded
	var coverageReasons []string
	var changedCoverageFiles []coverage.File
	var coverageSummary *coverage.ExecutionSummary
	if sumBytes, err := os.ReadFile(report.GetSummaryJSONPath()); err == nil {
		var summarySnapshot struct {
			DurationSeconds    float64                      `json:"duration_seconds"`
			ScopeDecision      *planner.ScopeDecision       `json:"scope_decision"`
			PlanReconciliation *coverage.PlanReconciliation `json:"plan_reconciliation"`
			CoverageSummary    *coverage.ExecutionSummary   `json:"coverage_summary"`
		}
		if err := json.Unmarshal(sumBytes, &summarySnapshot); err == nil {
			durationSec = summarySnapshot.DurationSeconds
			scopeDecision = summarySnapshot.ScopeDecision
			planReconciliation = summarySnapshot.PlanReconciliation
			coverageSummary = summarySnapshot.CoverageSummary
		}
	}
	if manifestBytes, err := os.ReadFile(filepath.Join(report.GetReportDir(), fmt.Sprintf("report-%d-file-manifest.json", report.ID))); err == nil {
		var manifest coverage.Coverage
		if err := json.Unmarshal(manifestBytes, &manifest); err == nil {
			summary := manifest.Summary()
			coverageComplete = summary.CoverageComplete
			coverageDegraded = summary.CoverageDegraded
			coverageReasons = summary.CoverageReasons
			for _, item := range manifest.Files {
				if item.DiffTouched || len(item.HunkRanges) > 0 {
					changedCoverageFiles = append(changedCoverageFiles, item)
				}
			}
		}
	}

	var scanProfile any
	if len(report.ScanProfile) > 0 {
		_ = json.Unmarshal(report.ScanProfile, &scanProfile)
	}

	return ReportMetaDTO{
		ID:                       report.ID,
		RepoID:                   report.RepoID,
		RepoName:                 report.Repo.Name,
		RepoURL:                  report.Repo.URL,
		Branch:                   report.Repo.Branch,
		TaskTypeID:               report.TaskTypeID,
		TaskTypeName:             report.TaskType.Name,
		TaskTypeDisplay:          report.TaskType.DisplayName,
		EngineMode:               report.EngineMode,
		ScopeDecision:            scopeDecision,
		PlanReconciliation:       planReconciliation,
		CoverageSummary:          coverageSummary,
		ScanProfile:              scanProfile,
		ScanProfileHash:          report.ScanProfileHash,
		PromptVersion:            report.PromptVersion,
		EngineConfigHash:         report.EngineConfigHash,
		PlannerVersion:           report.PlannerVersion,
		AssessmentConfigHash:     report.AssessmentConfigHash,
		PromptContentHash:        report.PromptContentHash,
		CategorySchemaHash:       report.CategorySchemaHash,
		TaxonomySchemaVersion:    report.TaxonomySchemaVersion,
		TaxonomyHash:             report.TaxonomyHash,
		DomainFamily:             report.DomainFamily,
		DomainLabel:              report.DomainLabel,
		DefenseDimensions:        json.RawMessage(report.DefenseDimensions),
		TargetSemantics:          json.RawMessage(report.TargetSemantics),
		DisplaySemantics:         json.RawMessage(report.DisplaySemantics),
		GovernanceMode:           govMode,
		ExecutionSnapshotVersion: report.ExecutionSnapshotVersion,
		ExecutionSnapshotState:   report.ExecutionSnapshotState,
		Status:                   report.Status,
		Score:                    report.Score,
		Rating:                   CalculateRating(report.Score),
		TotalChunks:              report.TotalChunks,
		ProcessedChunks:          report.ProcessedChunks,
		SuccessChunks:            report.SuccessChunks,
		CoverageNotApplicable:    report.CoverageNotApplicable,
		CoverageComplete:         coverageComplete,
		CoverageDegraded:         coverageDegraded,
		CoverageReasons:          coverageReasons,
		ChangedCoverageFiles:     changedCoverageFiles,
		DurationSeconds:          durationSec,
		BaseCommit:               report.BaseCommit,
		HeadCommit:               report.HeadCommit,
		Tier1Tokens:              report.Tier1Tokens,
		Tier2Tokens:              report.Tier2Tokens,
		CreatedAt:                report.CreatedAt,
	}
}

func reportGovernanceMode(report *models.TaskReport) string {
	if report.GovernanceMode != "" {
		return models.ResolveGovernanceMode(report.GovernanceMode)
	}
	if report.TaskType.GovernanceMode != "" {
		return models.ResolveGovernanceMode(report.TaskType.GovernanceMode)
	}
	return models.GovernanceModeFullLedger
}

// GetTaskReportEntity 加载包含 Repo 和 TaskType 的 TaskReport
func GetTaskReportEntity(taskID uint) (*models.TaskReport, error) {
	var report models.TaskReport
	if err := models.DB.Preload("Repo").Preload("TaskType").First(&report, taskID).Error; err != nil {
		return nil, fmt.Errorf("task report %d not found: %w", taskID, err)
	}
	return &report, nil
}

func campaignFindingToRawMap(dbf models.CampaignFinding) map[string]interface{} {
	return map[string]interface{}{
		"id":           float64(dbf.ID),
		"file_path":    dbf.FilePath,
		"line_number":  dbf.LineNumber,
		"title":        dbf.Title,
		"severity":     dbf.Severity,
		"category":     dbf.Category,
		"detail":       dbf.Detail,
		"suggestion":   dbf.Suggestion,
		"code_snippet": dbf.CodeSnippet,
	}
}

func analysisFindingToRawMap(dbf models.AnalysisFinding) map[string]interface{} {
	var assessmentArtifact map[string]interface{}
	if len(dbf.AssessmentArtifact) > 0 {
		_ = json.Unmarshal(dbf.AssessmentArtifact, &assessmentArtifact)
	}
	return map[string]interface{}{
		"id":                       float64(dbf.ID),
		"file_path":                dbf.FilePath,
		"line_number":              dbf.LineNumber,
		"title":                    dbf.Title,
		"severity":                 dbf.Severity,
		"category":                 dbf.Category,
		"category_code":            dbf.CategoryCode,
		"category_source":          dbf.CategorySource,
		"category_status":          dbf.CategoryStatus,
		"classification_rationale": dbf.ClassificationRationale,
		"taxonomy_hash":            dbf.TaxonomyHash,
		"detail":                   dbf.Detail,
		"suggestion":               dbf.Suggestion,
		"code_snippet":             dbf.CodeSnippet,
		"trigger_line":             dbf.TriggerLine,
		"scope_symbol":             dbf.ScopeSymbol,
		"observation_group_uid":    dbf.ObservationGroupUID,
		"hunter_claim":             dbf.HunterClaim,
		"challenger_arg":           dbf.ChallengerArg,
		"judge_verdict":            dbf.JudgeVerdict,
		"primary_unit_id":          dbf.PrimaryUnitID,
		"assessment_status":        dbf.AssessmentStatus,
		"assessment_outcome":       dbf.AssessmentOutcome,
		"assessment_artifact":      assessmentArtifact,
	}
}

// loadAllFindingsRaw 加载并归一化任务的所有结构化 Findings（融合文件与数据库流转字段）
func loadAllFindingsRaw(report *models.TaskReport) ([]FindingItemDTO, error) {
	isEntityMode := reportGovernanceMode(report) == models.GovernanceModeEntityAssessment
	useLedger := report.LedgerCommittedAt != nil && models.DB != nil

	var rawList []map[string]interface{}
	if useLedger {
		var dbFindings []models.AnalysisFinding
		if err := models.DB.Where("task_report_id = ?", report.ID).Order("id").Find(&dbFindings).Error; err == nil {
			for _, dbf := range dbFindings {
				rawList = append(rawList, analysisFindingToRawMap(dbf))
			}
		}
	}

	// 1. 读取 disk 上 synthesis findings json (若数据库中无 Findings，无论是未提交台账还是已受 TTL 清理，均透明回退读取磁盘 JSON 快照)
	if len(rawList) == 0 {
		jsonPath := report.GetSynthesisJSONPath()
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
	}

	// 2. 查询 DB 中的流转状态与责任人信息 (优先 CampaignFinding，回退 AnalysisFinding)
	statusMap := make(map[string]string)
	assigneeMap := make(map[string]string)
	assigneeIDMap := make(map[string]*uint)
	commentMap := make(map[string]string)
	createdAtMap := make(map[string]*time.Time)
	useCampaignFallback := isEntityMode && len(rawList) == 0
	useAnalysisFallback := len(rawList) == 0
	observationMap := make(map[string]models.DefectObservation)
	if report.LedgerCommittedAt != nil && models.DB != nil {
		var observations []models.DefectObservation
		if err := models.DB.Where("report_id = ?", report.ID).Find(&observations).Error; err == nil {
			for _, observation := range observations {
				observationMap[observation.ObservationGroupUID] = observation
			}
		}
	}

	if models.DB != nil && isEntityMode {
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

				if useCampaignFallback {
					rawList = append(rawList, campaignFindingToRawMap(dbf))
				}
			}
		}
	} else if models.DB != nil {
		var dbFindings []models.AnalysisFinding
		if err := models.DB.Where("task_report_id = ?", report.ID).Find(&dbFindings).Error; err == nil {
			for _, dbf := range dbFindings {
				k := makeFindingKey(dbf.FilePath, dbf.LineNumber, dbf.Title)
				statusMap[k] = "open"
				if observation, ok := observationMap[dbf.ObservationGroupUID]; ok && observation.DefectID != nil {
					var defect models.Defect
					if err := models.DB.Select("status", "status_reason", "assignee_id").
						First(&defect, *observation.DefectID).Error; err == nil {
						statusMap[k] = campaignDisplayStatus(defect.Status)
						commentMap[k] = defect.StatusReason
					}
				}
				t := dbf.CreatedAt
				createdAtMap[k] = &t

				if useAnalysisFallback {
					rawList = append(rawList, analysisFindingToRawMap(dbf))
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
		categoryCode := getMapString(raw, "category_code", "CategoryCode")
		categorySource := getMapString(raw, "category_source", "CategorySource")
		categoryStatus := getMapString(raw, "category_status", "CategoryStatus")
		classificationRationale := getMapString(raw, "classification_rationale", "ClassificationRationale")
		taxonomyHash := getMapString(raw, "taxonomy_hash", "TaxonomyHash")
		detail := getMapString(raw, "detail", "Detail")
		suggestion := getMapString(raw, "suggestion", "Suggestion")
		codeSnippet := getMapString(raw, "code_snippet", "CodeSnippet")

		// 智能体辩论与指纹增量字段
		triggerLine := getMapString(raw, "trigger_line", "TriggerLine")
		scopeSymbol := getMapString(raw, "scope_symbol", "ScopeSymbol")
		hunterClaim := getMapString(raw, "hunter_claim", "HunterClaim")
		challengerArg := getMapString(raw, "challenger_arg", "ChallengerArg")
		judgeVerdict := getMapString(raw, "judge_verdict", "JudgeVerdict")
		observationGroupUID := getMapString(raw, "observation_group_uid", "ObservationGroupUID")
		primaryUnitID := getMapString(raw, "primary_unit_id", "PrimaryUnitID")
		assessmentStatus := getMapString(raw, "assessment_status", "AssessmentStatus")
		assessmentOutcome := getMapString(raw, "assessment_outcome", "AssessmentOutcome")
		assessmentArtifact, _ := raw["assessment_artifact"].(map[string]interface{})
		if categoryStatus == "" {
			categoryStatus = "LEGACY"
		}

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
		if assessmentStatus != "" {
			outcome := assessment.AssessmentOutcome(assessmentOutcome)
			if outcome == "" {
				mappedOutcome, ok := assessmentprofiles.OutcomeForStatus(assessmentStatus)
				if ok {
					outcome = mappedOutcome
				}
			}
			if outcome != "" {
				switch outcome {
				case assessment.OutcomePass, assessment.OutcomeNotTarget:
					statusVal = "pass"
				case assessment.OutcomeDefect:
					statusVal = "open"
				case assessment.OutcomeNeedsHuman:
					statusVal = "analyzing"
				}
			}
		}

		statusDisplay := GetStatusChinese(statusVal, isEntityMode)

		idVal := uint(idx + 1)
		if rawID, ok := raw["id"].(float64); ok && rawID > 0 {
			idVal = uint(rawID)
		}

		note := getMapString(raw, "note", "Note")
		items = append(items, FindingItemDTO{
			ID:                      idVal,
			TaskReportID:            report.ID,
			TaskTypeID:              report.TaskTypeID,
			RepoID:                  report.RepoID,
			Severity:                canonicalSev,
			SeverityDisplay:         sevDisplay,
			Category:                category,
			CategoryCode:            categoryCode,
			CategorySource:          categorySource,
			CategoryStatus:          categoryStatus,
			ClassificationRationale: classificationRationale,
			TaxonomyHash:            taxonomyHash,
			FilePath:                filePath,
			LineNumber:              lineNumber,
			Title:                   title,
			Detail:                  detail,
			CodeSnippet:             codeSnippet,
			Suggestion:              suggestion,
			Status:                  statusVal,
			StatusDisplay:           statusDisplay,
			AssigneeID:              assigneeID,
			AssigneeName:            assigneeName,
			LatestComment:           latestComment,
			TriggerLine:             triggerLine,
			PrimaryUnitID:           primaryUnitID,
			AssessmentStatus:        assessmentStatus,
			AssessmentOutcome:       assessmentOutcome,
			AssessmentArtifact:      assessmentArtifact,
			ScopeSymbol:             scopeSymbol,
			HunterClaim:             hunterClaim,
			ChallengerArg:           challengerArg,
			JudgeVerdict:            judgeVerdict,
			ObservationGroupUID:     observationGroupUID,
			DefectID:                observationMap[observationGroupUID].DefectID,
			Verdict:                 observationMap[observationGroupUID].Verdict,
			MatchTier:               observationMap[observationGroupUID].MatchTier,
			Confidence:              observationMap[observationGroupUID].Confidence,
			Note:                    note,
			CreatedAt:               createdAt,
		})
	}

	return items, nil
}

func campaignDisplayStatus(status string) string {
	switch status {
	case "RESOLVED":
		return "resolved"
	case "HUMAN_CLOSED":
		return "closed"
	case "OBSOLETE":
		return "invalid"
	case "COVERAGE_GAP", "VERIFIED_PENDING", "DORMANT":
		return "analyzing"
	default:
		return "open"
	}
}

// computeKPIMetrics 从 items 计算统计分布指标
func computeKPIMetrics(items []FindingItemDTO, isEntityMode bool) KPIMetrics {
	metrics := KPIMetrics{
		TotalFindings:   len(items),
		CategoryStats:   make(map[string]int),
		StatusStats:     make(map[string]int),
		AssessmentStats: make(map[string]int),
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
		if item.AssessmentStatus != "" {
			if item.AssessmentOutcome != "" {
				metrics.AssessmentStats[item.AssessmentOutcome]++
			} else if outcome, ok := assessmentprofiles.OutcomeForStatus(item.AssessmentStatus); ok {
				metrics.AssessmentStats[string(outcome)]++
			} else {
				metrics.AssessmentStats[item.AssessmentStatus]++
			}
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

// CalculateRiskScoreFromFindings 根据归并后的 Findings 列表统一计算综合风险分估值
func CalculateRiskScoreFromFindings(items []FindingItemDTO) int {
	score := 0
	for _, it := range items {
		switch it.Severity {
		case SeverityFatal:
			score += 5
		case SeverityCritical:
			score += 4
		case SeverityMajor:
			score += 2
		case SeverityMinor:
			score += 1
		}
	}
	return score
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
	isEntityMode := reportGovernanceMode(report) == models.GovernanceModeEntityAssessment
	findings, _ := loadAllFindingsRaw(report)
	metrics := computeKPIMetrics(findings, isEntityMode)

	// 综合风险分必须基于归并后的全量条目总数动态校验并自愈数据库
	if len(findings) > 0 && !isEntityMode {
		calculatedScore := CalculateRiskScoreFromFindings(findings)
		if calculatedScore != meta.Score {
			meta.Score = calculatedScore
			meta.Rating = CalculateRating(calculatedScore)
			if models.DB != nil && report.ID > 0 && !models.IsTerminalTaskStatus(report.Status) {
				_, _ = models.UpdateActiveTaskReport(models.DB, report.ID, map[string]interface{}{
					"score": calculatedScore,
				})
			}
		}
	}

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

	isEntityMode := reportGovernanceMode(report) == models.GovernanceModeEntityAssessment
	allItems, err := loadAllFindingsRaw(report)
	if err != nil {
		return nil, err
	}

	metrics := computeKPIMetrics(allItems, isEntityMode)

	// 过滤
	var filtered []FindingItemDTO
	for _, item := range allItems {
		if !matchesReportQuery(item, query, isEntityMode) {
			continue
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

func matchesReportQuery(item FindingItemDTO, query FindingsQuery, isEntityMode bool) bool {
	if query.Severity != "" {
		if query.Severity == "__none__" {
			return false
		}
		matched := false
		for _, s := range strings.Split(query.Severity, ",") {
			s = strings.TrimSpace(s)
			if s != "" && item.Severity == NormalizeSeverity(s) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if query.Status != "" && !matchesFindingStatus(item, query.Status, isEntityMode) {
		return false
	}
	if query.Category != "" && !strings.EqualFold(item.Category, query.Category) {
		return false
	}
	if query.AssigneeID != "" {
		if assigneeID, _ := strconv.Atoi(query.AssigneeID); uint(assigneeID) != 0 {
			if item.AssigneeID == nil || *item.AssigneeID != uint(assigneeID) {
				return false
			}
		}
	}
	if query.Keyword != "" {
		keyword := strings.ToLower(query.Keyword)
		matched := strings.Contains(strings.ToLower(item.Title), keyword) ||
			strings.Contains(strings.ToLower(item.FilePath), keyword) ||
			strings.Contains(strings.ToLower(item.Detail), keyword) ||
			strings.Contains(strings.ToLower(item.Suggestion), keyword)
		if !matched {
			return false
		}
	}
	return true
}

func matchesFindingStatus(item FindingItemDTO, queryStatus string, isEntityMode bool) bool {
	status := strings.ToLower(strings.TrimSpace(queryStatus))
	if strings.EqualFold(item.Status, status) {
		return true
	}

	return isEntityMode && status == "fail" &&
		(item.AssessmentOutcome == string(assessment.OutcomeDefect) ||
			(item.AssessmentOutcome == "" && item.Status == "open"))
}

// GetReportDiagnostics 获取运行轨迹与诊断
func GetReportDiagnostics(taskID uint) (*DiagnosticsDTO, error) {
	report, err := GetTaskReportEntity(taskID)
	if err != nil {
		return nil, err
	}

	meta := BuildMetaDTO(report)
	dto := &DiagnosticsDTO{
		Meta:             meta,
		ArtifactComplete: true,
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
				if v, ok := analysis["attempts"].(float64); ok {
					dto.Attempts = int(v)
				}
				if v, ok := analysis["retries"].(float64); ok {
					dto.Retries = int(v)
				}
				if v, ok := analysis["contract_repairs"].(float64); ok {
					dto.ContractRepairs = int(v)
				}
				if v, ok := analysis["resource_failovers"].(float64); ok {
					dto.ResourceFailovers = int(v)
				}
				if v, ok := analysis["driver_failovers"].(float64); ok {
					dto.DriverFailovers = int(v)
				}
				if v, ok := analysis["split_invocations"].(float64); ok {
					dto.SplitInvocations = int(v)
				}
				if v, ok := analysis["recovered_chunks"].(float64); ok {
					dto.RecoveredChunks = int(v)
				}
				if v, ok := analysis["artifact_complete"].(bool); ok {
					dto.ArtifactComplete = v
				}
				if v, ok := analysis["artifact_state"].(string); ok {
					dto.ArtifactState = v
				}
				if v, ok := analysis["artifact_quality_degraded"].(bool); ok {
					dto.ArtifactQualityDegraded = v
				}
				if v, ok := analysis["unresolved_issue_count"].(float64); ok {
					dto.UnresolvedIssueCount = int(v)
				}
				if v, ok := analysis["normalized_issue_count"].(float64); ok {
					dto.NormalizedIssueCount = int(v)
				}
				if v, ok := analysis["schema_repair_attempts"].(float64); ok {
					dto.SchemaRepairAttempts = int(v)
				}
				if v, ok := analysis["schema_repair_successes"].(float64); ok {
					dto.SchemaRepairSuccesses = int(v)
				}
				if v, ok := analysis["json_syntax_repairs"].(float64); ok {
					dto.JSONSyntaxRepairs = int(v)
				}
				if v, ok := analysis["repair_baseline_known"].(bool); ok {
					dto.RepairBaselineKnown = v
				}
				if v, ok := analysis["repair_repaired_signature_known"].(bool); ok {
					dto.RepairRepairedKnown = v
				}
				if v, ok := analysis["repair_unverified"].(bool); ok {
					dto.RepairUnverified = v
				}
				if v, ok := analysis["repair_outcome"].(string); ok {
					dto.RepairOutcome = v
				}
				if v, ok := analysis["candidate_quarantine_count"].(float64); ok {
					dto.CandidateQuarantineCount = int(v)
				}
				if v, ok := analysis["artifact_schema_id"].(string); ok {
					dto.ArtifactSchemaID = v
				}
				if v, ok := analysis["artifact_schema_hash"].(string); ok {
					dto.ArtifactSchemaHash = v
				}
				if v, ok := analysis["response_format_mode"].(string); ok {
					dto.ResponseFormatMode = v
				}
				if v, ok := analysis["response_format_fallbacks"].(float64); ok {
					dto.ResponseFormatFallbacks = int(v)
				}
				if categoryRaw, ok := analysis["category"].(map[string]interface{}); ok {
					summary := &CategoryDiagnosticsSummary{}
					if hunterRaw, ok := categoryRaw["hunter"].(map[string]interface{}); ok {
						summary.Hunter = parseHunterCategoryDiagnostics(hunterRaw)
					}
					if judgeRaw, ok := categoryRaw["judge"].(map[string]interface{}); ok {
						summary.Judge = parseJudgeCategoryDiagnostics(judgeRaw)
					}
					dto.Category = summary
				}
				if reasonsRaw, ok := analysis["degraded_reasons"].([]interface{}); ok {
					for _, reasonRaw := range reasonsRaw {
						if reason, ok := reasonRaw.(string); ok {
							dto.DegradedReasons = append(dto.DegradedReasons, reason)
						}
					}
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
							errClass, _ := cMap["error_class"].(string)
							contractRepairs := 0
							if v, ok := cMap["contract_repairs"].(float64); ok {
								contractRepairs = int(v)
							}
							resourceFailovers := 0
							if v, ok := cMap["resource_failovers"].(float64); ok {
								resourceFailovers = int(v)
							}
							driverFailovers := 0
							if v, ok := cMap["driver_failovers"].(float64); ok {
								driverFailovers = int(v)
							}
							var resourceChain []string
							if rawChain, ok := cMap["resource_chain"].([]interface{}); ok {
								for _, item := range rawChain {
									if resourceID, ok := item.(string); ok {
										resourceChain = append(resourceChain, resourceID)
									}
								}
							}
							var errorClasses []string
							if rawClasses, ok := cMap["error_classes"].([]interface{}); ok {
								for _, item := range rawClasses {
									if class, ok := item.(string); ok {
										errorClasses = append(errorClasses, class)
									}
								}
							}
							var queueWaitMS []int64
							if rawQueueWaits, ok := cMap["queue_wait_ms"].([]interface{}); ok {
								for _, item := range rawQueueWaits {
									if value, ok := item.(float64); ok {
										queueWaitMS = append(queueWaitMS, int64(value))
									}
								}
							}
							var attemptDurations []float64
							if rawDurations, ok := cMap["attempt_duration_seconds"].([]interface{}); ok {
								for _, item := range rawDurations {
									if value, ok := item.(float64); ok {
										attemptDurations = append(attemptDurations, value)
									}
								}
							}
							splitDepth := 0
							if v, ok := cMap["split_depth"].(float64); ok {
								splitDepth = int(v)
							}
							splitCount := 0
							if v, ok := cMap["split_count"].(float64); ok {
								splitCount = int(v)
							}
							resumed := false
							if v, ok := cMap["resumed"].(bool); ok {
								resumed = v
							}
							artifactComplete := true
							if v, ok := cMap["artifact_complete"].(bool); ok {
								artifactComplete = v
							}
							artifactState, _ := cMap["artifact_state"].(string)
							artifactQualityDegraded, _ := cMap["artifact_quality_degraded"].(bool)
							unresolvedIssueCount := 0
							if v, ok := cMap["unresolved_issue_count"].(float64); ok {
								unresolvedIssueCount = int(v)
							}
							normalizedIssueCount := 0
							if v, ok := cMap["normalized_issue_count"].(float64); ok {
								normalizedIssueCount = int(v)
							}
							schemaRepairAttempts := 0
							if v, ok := cMap["schema_repair_attempts"].(float64); ok {
								schemaRepairAttempts = int(v)
							}
							schemaRepairSuccesses := 0
							if v, ok := cMap["schema_repair_successes"].(float64); ok {
								schemaRepairSuccesses = int(v)
							}
							jsonSyntaxRepairs := 0
							if v, ok := cMap["json_syntax_repairs"].(float64); ok {
								jsonSyntaxRepairs = int(v)
							}
							repairBaselineKnown := false
							if v, ok := cMap["repair_baseline_known"].(bool); ok {
								repairBaselineKnown = v
							}
							repairRepairedKnown := false
							if v, ok := cMap["repair_repaired_signature_known"].(bool); ok {
								repairRepairedKnown = v
							}
							repairUnverified := false
							if v, ok := cMap["repair_unverified"].(bool); ok {
								repairUnverified = v
							}
							repairOutcome, _ := cMap["repair_outcome"].(string)
							candidateQuarantineCount := 0
							if v, ok := cMap["candidate_quarantine_count"].(float64); ok {
								candidateQuarantineCount = int(v)
							}
							var schemaRepairIssues []string
							if v, ok := cMap["schema_repair_issues"].([]interface{}); ok {
								for _, item := range v {
									if text, ok := item.(string); ok {
										schemaRepairIssues = append(schemaRepairIssues, text)
									}
								}
							}
							artifactSchemaID, _ := cMap["artifact_schema_id"].(string)
							artifactSchemaHash, _ := cMap["artifact_schema_hash"].(string)
							responseFormatMode, _ := cMap["response_format_mode"].(string)
							responseFormatFallbacks := 0
							if v, ok := cMap["response_format_fallbacks"].(float64); ok {
								responseFormatFallbacks = int(v)
							}

							dto.Chunks = append(dto.Chunks, ChunkDiagnosticDetail{
								ChunkName:                cName,
								Status:                   status,
								DurationSeconds:          dur,
								Attempts:                 attempts,
								FilesCount:               filesCount,
								FindingsCount:            findingsCount,
								ErrorMessage:             errMsg,
								ErrorClass:               errClass,
								ContractRepairs:          contractRepairs,
								ResourceFailovers:        resourceFailovers,
								DriverFailovers:          driverFailovers,
								ResourceChain:            resourceChain,
								ErrorClasses:             errorClasses,
								QueueWaitMS:              queueWaitMS,
								AttemptDurationSeconds:   attemptDurations,
								SplitDepth:               splitDepth,
								SplitCount:               splitCount,
								Resumed:                  resumed,
								ArtifactComplete:         artifactComplete,
								ArtifactState:            artifactState,
								ArtifactQualityDegraded:  artifactQualityDegraded,
								UnresolvedIssueCount:     unresolvedIssueCount,
								NormalizedIssueCount:     normalizedIssueCount,
								SchemaRepairAttempts:     schemaRepairAttempts,
								SchemaRepairSuccesses:    schemaRepairSuccesses,
								JSONSyntaxRepairs:        jsonSyntaxRepairs,
								RepairBaselineKnown:      repairBaselineKnown,
								RepairRepairedKnown:      repairRepairedKnown,
								RepairUnverified:         repairUnverified,
								RepairOutcome:            repairOutcome,
								CandidateQuarantineCount: candidateQuarantineCount,
								SchemaRepairIssues:       schemaRepairIssues,
								ArtifactSchemaID:         artifactSchemaID,
								ArtifactSchemaHash:       artifactSchemaHash,
								ResponseFormatMode:       responseFormatMode,
								ResponseFormatFallbacks:  responseFormatFallbacks,
								Files:                    files,
							})
						}
					}
				}

				// Older debate summaries dropped chunk.Files during runner
				// conversion. Recover the authoritative mapping from the
				// coverage manifest so historical diagnostics remain useful.
				if len(dto.Chunks) > 0 {
					manifestPath := filepath.Join(report.GetReportDir(), fmt.Sprintf("report-%d-file-manifest.json", report.ID))
					if manifestBytes, err := os.ReadFile(manifestPath); err == nil {
						var manifest coverage.Coverage
						if err := json.Unmarshal(manifestBytes, &manifest); err == nil {
							manifestFiles := make(map[string][]string)
							for _, item := range manifest.Files {
								if item.Path == "" || item.ChunkUID == "" {
									continue
								}
								manifestFiles[item.ChunkUID] = append(manifestFiles[item.ChunkUID], item.Path)
							}
							for i := range dto.Chunks {
								if len(dto.Chunks[i].Files) > 0 {
									continue
								}
								dto.Chunks[i].Files = manifestFiles[dto.Chunks[i].ChunkName]
								dto.Chunks[i].FilesCount = len(dto.Chunks[i].Files)
							}
						}
					}
				}
			}

			dto.Synthesis = parseSynthesisDiagnostics(summaryMap)

			// 构建时序步骤 (完全基于实际执行记录，杜绝硬编码虚构耗时)
			step1Name := "代码静态分析"
			if report.EngineMode == "debate_full" {
				step1Name = "智能体对抗辩论 (Hunter ➜ Challenger ➜ Judge)"
			} else if report.EngineMode == "debate_selective" {
				step1Name = "选择性智能体辩论初筛与仲裁"
			} else if report.EngineMode == "chunked_fast" {
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

func parseSynthesisDiagnostics(summaryMap map[string]interface{}) *SynthesisDiagnosticSummary {
	raw, ok := summaryMap["synthesis"].(map[string]interface{})
	if !ok {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	summary := &SynthesisDiagnosticSummary{}
	if err := json.Unmarshal(encoded, summary); err != nil {
		return nil
	}
	return summary
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
