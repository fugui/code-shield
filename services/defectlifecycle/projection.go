package defectlifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"code-shield/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ReportReconciliation struct {
	ReportID             uint                            `json:"report_id"`
	CoverageState        string                          `json:"coverage_state"`
	WorktreeClean        bool                            `json:"worktree_clean"`
	LedgerReady          bool                            `json:"ledger_ready"`
	AlgorithmVersion     string                          `json:"algorithm_version"`
	Stats                map[string]int                  `json:"stats"`
	Quality              ReconciliationQuality           `json:"quality"`
	Observations         []ObservationProjection         `json:"observations"`
	Events               []models.DefectEvent            `json:"events"`
	UnmatchedOpenDefects []UnmatchedOpenDefectProjection `json:"unmatched_open_defects"`
}

type ReconciliationQuality struct {
	CandidateBudgetExceeded int `json:"candidate_budget_exceeded"`
	AIUnavailable           int `json:"ai_unavailable"`
	IdentityMoved           int `json:"identity_moved"`
	GrayZoneAutoResolved    int `json:"gray_zone_auto_resolved"`
}

type ObservationProjection struct {
	ObservationGroupUID string                `json:"observation_group_uid"`
	DefectID            *uint                 `json:"defect_id"`
	Verdict             string                `json:"verdict"`
	MatchTier           string                `json:"match_tier"`
	Confidence          float64               `json:"confidence"`
	ScoreDetail         map[string]float64    `json:"score_detail"`
	Reason              string                `json:"reason"`
	SourceFindingIDs    []uint                `json:"source_finding_ids"`
	SourceFindings      []TaskFindingEvidence `json:"source_findings,omitempty"`
}

type TaskFindingEvidence struct {
	ID          uint   `json:"id"`
	Title       string `json:"title"`
	FilePath    string `json:"file_path"`
	LineNumber  string `json:"line_number"`
	ScopeSymbol string `json:"scope_symbol"`
	Severity    string `json:"severity,omitempty"`
	Category    string `json:"category,omitempty"`
	Detail      string `json:"detail,omitempty"`
	CodeSnippet string `json:"code_snippet,omitempty"`
	Suggestion  string `json:"suggestion,omitempty"`
}

type UnmatchedOpenDefectProjection struct {
	ID                  uint   `json:"id"`
	Title               string `json:"title,omitempty"`
	Detail              string `json:"detail,omitempty"`
	Category            string `json:"category,omitempty"`
	CodeSnippet         string `json:"code_snippet,omitempty"`
	Suggestion          string `json:"suggestion,omitempty"`
	Severity            string `json:"severity"`
	NormPath            string `json:"norm_path"`
	LineStart           *int   `json:"line_start"`
	LineEnd             *int   `json:"line_end"`
	Status              string `json:"status"`
	StatusReason        string `json:"status_reason"`
	MissedCount         int    `json:"missed_count"`
	DormantRounds       int    `json:"dormant_rounds"`
	LastMatchedReportID uint   `json:"last_matched_report_id"`
}

type DefectQuery struct {
	RepoID     uint
	TaskTypeID uint
	Statuses   []string
	Page       int
	PageSize   int
}

type DefectPage struct {
	Items      []models.Defect `json:"items"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalPages int             `json:"total_pages"`
}

func GetReportReconciliation(db *gorm.DB, reportID uint) (*ReportReconciliation, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	var report models.TaskReport
	if err := db.First(&report, reportID).Error; err != nil {
		return nil, fmt.Errorf("load report: %w", err)
	}
	var observations []models.DefectObservation
	if err := db.Where("report_id = ?", reportID).Order("id").Find(&observations).Error; err != nil {
		return nil, fmt.Errorf("load observations: %w", err)
	}
	var events []models.DefectEvent
	if err := db.Where("report_id = ?", reportID).Order("id").Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load events: %w", err)
	}

	stats := map[string]int{
		VerdictNew: 0, VerdictExisted: 0, VerdictReopened: 0,
		VerdictProbable: 0, VerdictCleared: 0, "RESOLVED": 0, "COVERAGE_GAP": 0,
	}
	projected := make([]ObservationProjection, 0, len(observations))
	for _, observation := range observations {
		if _, ok := stats[observation.Verdict]; ok {
			stats[observation.Verdict]++
		}
		projection := ObservationProjection{
			ObservationGroupUID: observation.ObservationGroupUID,
			DefectID:            observation.DefectID,
			Verdict:             observation.Verdict,
			MatchTier:           observation.MatchTier,
			Confidence:          observation.Confidence,
			Reason:              observation.Reason,
			SourceFindingIDs:    make([]uint, 0),
		}
		if len(observation.ScoreDetail) > 0 {
			_ = jsonUnmarshalScore(observation.ScoreDetail, &projection.ScoreDetail)
		}
		projection.SourceFindings = make([]TaskFindingEvidence, 0)
		projected = append(projected, projection)
	}
	var findings []models.AnalysisFinding
	if err := db.Select(
		"id", "title", "file_path", "line_number", "scope_symbol", "observation_group_uid",
		"severity", "category", "detail", "code_snippet", "suggestion",
	).
		Where("task_report_id = ?", reportID).Order("id").Find(&findings).Error; err == nil {
		index := make(map[string]int, len(projected))
		for i, item := range projected {
			index[item.ObservationGroupUID] = i
		}
		for _, finding := range findings {
			if position, ok := index[finding.ObservationGroupUID]; ok {
				projected[position].SourceFindingIDs = append(projected[position].SourceFindingIDs, finding.ID)
				projected[position].SourceFindings = append(projected[position].SourceFindings, TaskFindingEvidence{
					ID:          finding.ID,
					Title:       finding.Title,
					FilePath:    finding.FilePath,
					LineNumber:  finding.LineNumber,
					ScopeSymbol: finding.ScopeSymbol,
					Severity:    finding.Severity,
					Category:    finding.Category,
					Detail:      finding.Detail,
					CodeSnippet: finding.CodeSnippet,
					Suggestion:  finding.Suggestion,
				})
			}
		}
	}

	var resolvedCount int64
	_ = db.Model(&models.DefectEvent{}).
		Where("report_id = ? AND event_type = ? AND to_status = ?", reportID, EventTypeStatusChanged, DefectStatusResolved).
		Count(&resolvedCount).Error
	stats["RESOLVED"] = int(resolvedCount)
	var coverageGapCount int64
	_ = db.Model(&models.DefectEvent{}).
		Where("report_id = ? AND event_type = ? AND to_status = ?", reportID, EventTypeStatusChanged, DefectStatusCoverageGap).
		Count(&coverageGapCount).Error
	stats["COVERAGE_GAP"] = int(coverageGapCount)

	unmatchedOpenDefects := make([]UnmatchedOpenDefectProjection, 0)
	if report.LedgerCommittedAt != nil {
		var defects []models.Defect
		if err := db.Where(
			"repo_id = ? AND task_type_id = ? AND first_report_id <= ? AND last_matched_report_id < ? AND status IN ?",
			report.RepoID, report.TaskTypeID, report.ID, report.ID,
			[]string{DefectStatusActive, DefectStatusCoverageGap, DefectStatusVerifiedPending, DefectStatusDormant},
		).Order("id").Find(&defects).Error; err != nil {
			return nil, fmt.Errorf("load unmatched open defects: %w", err)
		}
		campaignItems, err := campaignFindings(db, defects)
		if err != nil {
			return nil, fmt.Errorf("project unmatched open defects: %w", err)
		}
		findingByDefectID := make(map[uint]CampaignFinding, len(campaignItems))
		for _, item := range campaignItems {
			findingByDefectID[item.ID] = item
		}
		unmatchedOpenDefects = make([]UnmatchedOpenDefectProjection, 0, len(defects))
		for _, defect := range defects {
			projection := UnmatchedOpenDefectProjection{
				ID:                  defect.ID,
				Severity:            defect.Severity,
				NormPath:            defect.NormPath,
				LineStart:           defect.LineStart,
				LineEnd:             defect.LineEnd,
				Status:              defect.Status,
				StatusReason:        defect.StatusReason,
				MissedCount:         defect.MissedCount,
				DormantRounds:       defect.DormantRounds,
				LastMatchedReportID: defect.LastMatchedReportID,
			}
			if finding, exists := findingByDefectID[defect.ID]; exists {
				projection.Title = finding.Title
				projection.Detail = finding.Detail
				projection.Category = finding.Category
				projection.CodeSnippet = finding.CodeSnippet
				projection.Suggestion = finding.Suggestion
			}
			unmatchedOpenDefects = append(unmatchedOpenDefects, projection)
		}
	}

	return &ReportReconciliation{
		ReportID:             report.ID,
		CoverageState:        report.CoverageState,
		WorktreeClean:        report.WorktreeClean,
		LedgerReady:          report.LedgerCommittedAt != nil,
		AlgorithmVersion:     report.LedgerAlgorithmVersion,
		Stats:                stats,
		Quality:              reconciliationQuality(projected, events),
		Observations:         projected,
		Events:               events,
		UnmatchedOpenDefects: unmatchedOpenDefects,
	}, nil
}

func reconciliationQuality(observations []ObservationProjection, events []models.DefectEvent) ReconciliationQuality {
	quality := ReconciliationQuality{}
	for _, observation := range observations {
		switch {
		case strings.HasPrefix(observation.Reason, reasonCandidateBudget):
			quality.CandidateBudgetExceeded++
		case strings.HasPrefix(observation.Reason, reasonAIUnavailable):
			quality.AIUnavailable++
		}
		if strings.HasPrefix(observation.Reason, reasonAutoMerge) || strings.HasPrefix(observation.Reason, reasonAutoNew) {
			quality.GrayZoneAutoResolved++
		}
	}
	for _, event := range events {
		if event.EventType == EventTypeIdentityMoved {
			quality.IdentityMoved++
		}
	}
	return quality
}

type CandidateProjection struct {
	DefectID            uint               `json:"defect_id"`
	Status              string             `json:"status"`
	NormPath            string             `json:"norm_path"`
	LineStart           *int               `json:"line_start"`
	LineEnd             *int               `json:"line_end"`
	MatchTier           string             `json:"match_tier"`
	Score               float64            `json:"score"`
	ScoreDetail         map[string]float64 `json:"score_detail"`
	Sources             []string           `json:"sources"`
	Title               string             `json:"title,omitempty"`
	Detail              string             `json:"detail,omitempty"`
	Category            string             `json:"category,omitempty"`
	CodeSnippet         string             `json:"code_snippet,omitempty"`
	Suggestion          string             `json:"suggestion,omitempty"`
	Severity            string             `json:"severity,omitempty"`
	SymbolPath          string             `json:"symbol_path,omitempty"`
	StatusReason        string             `json:"status_reason,omitempty"`
	FirstReportID       uint               `json:"first_report_id,omitempty"`
	LastSeenReportID    uint               `json:"last_seen_report_id,omitempty"`
	LastMatchedReportID uint               `json:"last_matched_report_id,omitempty"`
	MissedCount         int                `json:"missed_count,omitempty"`
	DormantRounds       int                `json:"dormant_rounds,omitempty"`
	HumanLocked         bool               `json:"human_locked,omitempty"`
	HumanDecision       string             `json:"human_decision,omitempty"`
}

func GetObservationCandidates(db *gorm.DB, reportID uint, groupUID string) ([]CandidateProjection, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	var report models.TaskReport
	if err := db.First(&report, reportID).Error; err != nil {
		return nil, fmt.Errorf("load report: %w", err)
	}
	var observation models.DefectObservation
	if err := db.Where("report_id = ? AND observation_group_uid = ?", reportID, groupUID).
		First(&observation).Error; err != nil {
		return nil, fmt.Errorf("load observation: %w", err)
	}
	if observation.Verdict != VerdictProbable {
		return []CandidateProjection{}, nil
	}

	var findings []models.AnalysisFinding
	if err := db.Where("task_report_id = ?", reportID).Order("id").Find(&findings).Error; err != nil {
		return nil, fmt.Errorf("load findings: %w", err)
	}
	group := findObservationGroup(BuildObservationGroups("", reportID, findings), groupUID)
	if group == nil {
		return nil, fmt.Errorf("rebuild observation group %s", groupUID)
	}
	group.Identity.RepoID = report.RepoID
	group.Identity.TaskTypeID = report.TaskTypeID

	var defects []models.Defect
	if err := db.Where("repo_id = ? AND task_type_id = ?", report.RepoID, report.TaskTypeID).
		Order("id").Find(&defects).Error; err != nil {
		return nil, fmt.Errorf("load defects: %w", err)
	}
	var aliases []models.DefectAlias
	if err := db.Where("repo_id = ? AND task_type_id = ?", report.RepoID, report.TaskTypeID).
		Order("id").Find(&aliases).Error; err != nil {
		return nil, fmt.Errorf("load aliases: %w", err)
	}

	projections := make([]CandidateProjection, 0)
	candidates, _ := retrieveCandidates(group.Identity, buildAliasIndex(aliases), MatchingInput{
		Defects:    defects,
		Budget:     RuntimeMatchBudget(),
		Thresholds: RuntimeMatchThresholds(),
	}, RuntimeMatchBudget().MaxCandidates)
	for _, candidate := range candidates {
		score, detail := scoreCandidate(group.Identity, candidate)
		if score < RuntimeMatchThresholds().RejectBelow {
			continue
		}
		sources := make([]string, 0, len(candidate.Sources))
		for source := range candidate.Sources {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		projections = append(projections, CandidateProjection{
			DefectID:    candidate.Defect.ID,
			Status:      candidate.Defect.Status,
			NormPath:    candidate.Defect.NormPath,
			LineStart:   candidate.Defect.LineStart,
			LineEnd:     candidate.Defect.LineEnd,
			MatchTier:   candidateMatchTier(candidate),
			Score:       roundScore(score),
			ScoreDetail: detail,
			Sources:     sources,
		})
	}
	sort.SliceStable(projections, func(left, right int) bool {
		return projections[left].Score > projections[right].Score
	})
	if len(projections) > 0 {
		candidateDefs := make([]models.Defect, 0, len(projections))
		projectionIndexByID := make(map[uint]int, len(projections))
		for index, projection := range projections {
			projectionIndexByID[projection.DefectID] = index
		}
		for _, candidate := range candidates {
			if _, exists := projectionIndexByID[candidate.Defect.ID]; exists {
				candidateDefs = append(candidateDefs, candidate.Defect)
			}
		}
		campaignItems, err := campaignFindings(db, candidateDefs)
		if err != nil {
			return nil, fmt.Errorf("project candidate findings: %w", err)
		}
		candidateItemByID := make(map[uint]CampaignFinding, len(campaignItems))
		for _, item := range campaignItems {
			candidateItemByID[item.ID] = item
		}
		for _, candidate := range candidates {
			index, exists := projectionIndexByID[candidate.Defect.ID]
			if !exists {
				continue
			}
			projection := &projections[index]
			if item, exists := candidateItemByID[candidate.Defect.ID]; exists {
				projection.Title = item.Title
				projection.Detail = item.Detail
				projection.Category = item.Category
				projection.CodeSnippet = item.CodeSnippet
				projection.Suggestion = item.Suggestion
				if item.Severity != "" {
					projection.Severity = item.Severity
				}
			}
			projection.SymbolPath = candidate.Defect.SymbolPath
			projection.StatusReason = candidate.Defect.StatusReason
			projection.FirstReportID = candidate.Defect.FirstReportID
			projection.LastSeenReportID = candidate.Defect.LastSeenReportID
			projection.LastMatchedReportID = candidate.Defect.LastMatchedReportID
			projection.MissedCount = candidate.Defect.MissedCount
			projection.DormantRounds = candidate.Defect.DormantRounds
			projection.HumanLocked = candidate.Defect.HumanLocked
			projection.HumanDecision = candidate.Defect.HumanDecision
		}
	}
	return projections, nil
}

func buildAliasIndex(aliases []models.DefectAlias) map[string][]models.DefectAlias {
	index := make(map[string][]models.DefectAlias)
	for _, alias := range aliases {
		if alias.RepoID == 0 && alias.TaskTypeID == 0 {
			index[alias.AliasValue] = append(index[alias.AliasValue], alias)
			continue
		}
		key := alias.AliasValue + "\x00" + fmt.Sprint(alias.RepoID) + "\x00" + fmt.Sprint(alias.TaskTypeID)
		index[key] = append(index[key], alias)
	}
	return index
}

func candidateMatchTier(candidate Candidate) string {
	switch {
	case hasSource(candidate, AliasHuman):
		return MatchHuman
	case hasSource(candidate, AliasK2) || hasSource(candidate, AliasF1):
		return MatchStrong
	case hasSource(candidate, AliasRename):
		return MatchStruct
	default:
		return MatchBucket
	}
}

func ListDefects(db *gorm.DB, query DefectQuery) (*DefectPage, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 50
	}
	tx := db.Model(&models.Defect{})
	if query.RepoID != 0 {
		tx = tx.Where("repo_id = ?", query.RepoID)
	}
	if query.TaskTypeID != 0 {
		tx = tx.Where("task_type_id = ?", query.TaskTypeID)
	}
	if len(query.Statuses) > 0 {
		tx = tx.Where("status IN ?", query.Statuses)
	} else {
		tx = tx.Where("status IN ?", []string{
			DefectStatusActive, DefectStatusCoverageGap, DefectStatusVerifiedPending,
		})
	}
	page := &DefectPage{Page: query.Page, PageSize: query.PageSize}
	if err := tx.Count(&page.Total).Error; err != nil {
		return nil, fmt.Errorf("count defects: %w", err)
	}
	offset := (query.Page - 1) * query.PageSize
	if err := tx.Order("id DESC").Offset(offset).Limit(query.PageSize).Find(&page.Items).Error; err != nil {
		return nil, fmt.Errorf("list defects: %w", err)
	}
	page.TotalPages = int((page.Total + int64(page.PageSize) - 1) / int64(page.PageSize))
	return page, nil
}

func GetDefectDetail(db *gorm.DB, defectID uint) (*models.Defect, []models.DefectObservation, []models.DefectEvent, []models.DefectAlias, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, nil, nil, nil, errors.New("database is not initialized")
	}
	var defect models.Defect
	if err := db.First(&defect, defectID).Error; err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load defect: %w", err)
	}
	var observations []models.DefectObservation
	if err := db.Where("defect_id = ?", defectID).Order("report_id DESC").Find(&observations).Error; err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load observations: %w", err)
	}
	var events []models.DefectEvent
	if err := db.Where("defect_id = ?", defectID).Order("id DESC").Find(&events).Error; err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load events: %w", err)
	}
	var aliases []models.DefectAlias
	if err := db.Where("defect_id = ?", defectID).Order("id").Find(&aliases).Error; err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load aliases: %w", err)
	}
	return &defect, observations, events, aliases, nil
}

func jsonUnmarshalScore(raw datatypes.JSON, target *map[string]float64) error {
	return json.Unmarshal(raw, target)
}
