package defectlifecycle

import (
	"errors"
	"fmt"
	"time"

	"code-shield/models"

	"gorm.io/gorm"
)

type WorkbenchQuery struct {
	UserID     uint
	RepoID     uint
	TaskTypeID uint
	Statuses   []string
	Page       int
	PageSize   int
}

type WorkbenchItem struct {
	ID               uint                 `json:"id"`
	TaskTypeID       uint                 `json:"task_type_id"`
	TypeName         string               `json:"type_name"`
	Type             string               `json:"type"`
	RepoID           uint                 `json:"repo_id"`
	RepoName         string               `json:"repo_name"`
	RepoURL          string               `json:"repo_url"`
	FilePath         string               `json:"file_path"`
	LineNumber       string               `json:"line_number"`
	Title            string               `json:"title"`
	Detail           string               `json:"detail"`
	Severity         string               `json:"severity"`
	Category         string               `json:"category"`
	CodeSnippet      string               `json:"code_snippet"`
	Suggestion       string               `json:"suggestion"`
	Status           string               `json:"status"`
	StatusLog        []models.DefectEvent `json:"status_log"`
	SourceFindingID  uint                 `json:"source_finding_id"`
	LastSeenReportID uint                 `json:"last_seen_report_id"`
	AssigneeID       *uint                `json:"assignee_id"`
	AssignedAt       *time.Time           `json:"assigned_at"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type WorkbenchPage struct {
	Items      []WorkbenchItem `json:"items"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalPages int             `json:"total_pages"`
}

func ListMyDefects(db *gorm.DB, query WorkbenchQuery) (*WorkbenchPage, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	if query.UserID == 0 {
		return nil, errors.New("user is required")
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 50
	}

	base := db.Model(&models.Defect{}).
		Where("defects.assignee_id = ?", query.UserID)
	if query.RepoID != 0 {
		base = base.Where("defects.repo_id = ?", query.RepoID)
	}
	if query.TaskTypeID != 0 {
		base = base.Where("defects.task_type_id = ?", query.TaskTypeID)
	}
	if len(query.Statuses) == 0 {
		query.Statuses = []string{DefectStatusActive, DefectStatusCoverageGap, DefectStatusVerifiedPending}
	}
	base = base.Where("defects.status IN ?", query.Statuses)

	page := &WorkbenchPage{Page: query.Page, PageSize: query.PageSize}
	if err := base.Count(&page.Total).Error; err != nil {
		return nil, fmt.Errorf("count workbench defects: %w", err)
	}
	rows := make([]models.Defect, 0)
	if err := base.Order("defects.updated_at DESC, defects.id DESC").
		Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list workbench defects: %w", err)
	}

	items := make([]WorkbenchItem, len(rows))
	defectIDs := make([]uint, 0, len(rows))
	ids := make(map[uint]struct{}, len(rows))
	for index, row := range rows {
		defectIDs = append(defectIDs, row.ID)
		ids[row.ID] = struct{}{}
		items[index] = WorkbenchItem{
			ID: row.ID, TaskTypeID: row.TaskTypeID, RepoID: row.RepoID,
			FilePath: row.NormPath, Severity: row.Severity, Status: campaignStatusFromLedger(row.Status),
			LastSeenReportID: row.LastSeenReportID, AssigneeID: row.AssigneeID,
			AssignedAt: row.AssignedAt,
			CreatedAt:  row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}
	if err := projectWorkbenchPayload(db, defectIDs, items); err != nil {
		return nil, err
	}
	if err := projectWorkbenchEvents(db, defectIDs, items); err != nil {
		return nil, err
	}
	page.TotalPages = int((page.Total + int64(query.PageSize) - 1) / int64(query.PageSize))
	return page, nil
}

func projectWorkbenchPayload(db *gorm.DB, defectIDs []uint, items []WorkbenchItem) error {
	if len(defectIDs) == 0 {
		return nil
	}
	type observationRow struct {
		DefectID            uint
		ReportID            uint
		ObservationGroupUID string
	}
	observations := make([]observationRow, 0)
	if err := db.Model(&models.DefectObservation{}).
		Select("defect_id, report_id, observation_group_uid").
		Where("defect_id IN ?", defectIDs).
		Order("report_id DESC, id DESC").Find(&observations).Error; err != nil {
		return fmt.Errorf("load workbench observations: %w", err)
	}
	representative := make(map[uint]observationRow, len(items))
	for _, observation := range observations {
		if _, exists := representative[observation.DefectID]; !exists {
			representative[observation.DefectID] = observation
		}
	}

	reportIDs := make([]uint, 0)
	groupUIDs := make([]string, 0)
	findingKey := make(map[uint]map[string]models.AnalysisFinding)
	for _, item := range items {
		observation, exists := representative[item.ID]
		if !exists {
			continue
		}
		if !containsUint(reportIDs, observation.ReportID) {
			reportIDs = append(reportIDs, observation.ReportID)
		}
		if !containsString(groupUIDs, observation.ObservationGroupUID) {
			groupUIDs = append(groupUIDs, observation.ObservationGroupUID)
		}
	}
	findings := make([]models.AnalysisFinding, 0)
	if len(reportIDs) > 0 && len(groupUIDs) > 0 {
		if err := db.Where("task_report_id IN ? AND observation_group_uid IN ?", reportIDs, groupUIDs).
			Order("id").Find(&findings).Error; err != nil {
			return fmt.Errorf("load workbench findings: %w", err)
		}
	}
	for _, finding := range findings {
		if findingKey[finding.TaskReportID] == nil {
			findingKey[finding.TaskReportID] = make(map[string]models.AnalysisFinding)
		}
		if _, exists := findingKey[finding.TaskReportID][finding.ObservationGroupUID]; !exists {
			findingKey[finding.TaskReportID][finding.ObservationGroupUID] = finding
		}
	}
	for index := range items {
		observation, exists := representative[items[index].ID]
		if !exists {
			continue
		}
		finding, exists := findingKey[observation.ReportID][observation.ObservationGroupUID]
		if !exists {
			continue
		}
		items[index].Title = finding.Title
		items[index].Detail = finding.Detail
		items[index].Category = finding.Category
		items[index].CodeSnippet = finding.CodeSnippet
		items[index].Suggestion = finding.Suggestion
		items[index].LineNumber = finding.LineNumber
		items[index].SourceFindingID = finding.ID
	}

	taskTypeIDs := make([]uint, 0)
	repoIDs := make([]uint, 0)
	for _, item := range items {
		if !containsUint(taskTypeIDs, item.TaskTypeID) {
			taskTypeIDs = append(taskTypeIDs, item.TaskTypeID)
		}
		if !containsUint(repoIDs, item.RepoID) {
			repoIDs = append(repoIDs, item.RepoID)
		}
	}
	taskTypes := make([]models.TaskType, 0)
	if len(taskTypeIDs) > 0 {
		if err := db.Select("id, name, display_name, campaign_path").
			Where("id IN ?", taskTypeIDs).Find(&taskTypes).Error; err != nil {
			return fmt.Errorf("load workbench task types: %w", err)
		}
	}
	repos := make([]models.Repository, 0)
	if len(repoIDs) > 0 {
		if err := db.Select("id, name, url").Where("id IN ?", repoIDs).Find(&repos).Error; err != nil {
			return fmt.Errorf("load workbench repositories: %w", err)
		}
	}
	taskTypeByID := make(map[uint]models.TaskType, len(taskTypes))
	for _, item := range taskTypes {
		taskTypeByID[item.ID] = item
	}
	repoByID := make(map[uint]models.Repository, len(repos))
	for _, item := range repos {
		repoByID[item.ID] = item
	}
	for index := range items {
		taskType := taskTypeByID[items[index].TaskTypeID]
		items[index].TypeName = taskType.DisplayName
		items[index].Type = taskType.CampaignPath
		if items[index].Type == "" {
			items[index].Type = taskType.Name
		}
		repo := repoByID[items[index].RepoID]
		items[index].RepoName = repo.Name
		items[index].RepoURL = repo.URL
	}
	return nil
}

func projectWorkbenchEvents(db *gorm.DB, defectIDs []uint, items []WorkbenchItem) error {
	if len(defectIDs) == 0 {
		return nil
	}
	events := make([]models.DefectEvent, 0)
	if err := db.Where("defect_id IN ?", defectIDs).Order("id DESC").Find(&events).Error; err != nil {
		return fmt.Errorf("load workbench events: %w", err)
	}
	eventByDefect := make(map[uint][]models.DefectEvent)
	for _, event := range events {
		eventByDefect[event.DefectID] = append(eventByDefect[event.DefectID], event)
	}
	for index := range items {
		items[index].StatusLog = eventByDefect[items[index].ID]
		if items[index].StatusLog == nil {
			items[index].StatusLog = make([]models.DefectEvent, 0)
		}
	}
	return nil
}

func AssignDefect(db *gorm.DB, defectID uint, assigneeID *uint, actorID uint) error {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return errors.New("database is not initialized")
	}
	return transactionWithConflictRetry(db, func(tx *gorm.DB) error {
		var defect models.Defect
		if err := tx.First(&defect, defectID).Error; err != nil {
			return fmt.Errorf("load defect: %w", err)
		}
		now := time.Now()
		if sameUintPtr(defect.AssigneeID, assigneeID) {
			return nil
		}
		updates := map[string]interface{}{
			"assignee_id": assigneeID,
			"assigned_at": now,
			"row_version": defect.RowVersion + 1,
		}
		result := tx.Model(&models.Defect{}).
			Where("id = ? AND row_version = ?", defectID, defect.RowVersion).
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("update defect assignment: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("defect %d changed during assignment: %w", defectID, ErrDefectChanged)
		}
		assigneeValue := uint(0)
		if assigneeID != nil {
			assigneeValue = *assigneeID
		}
		event := models.DefectEvent{
			DefectID: defectID, EventType: "HUMAN_DECISION", FromStatus: defect.Status,
			ToStatus: defect.Status, ActorType: "HUMAN", ActorID: &actorID,
			Reason:    "assignment changed",
			Evidence:  []byte(fmt.Sprintf(`{"assignee_id":%d}`, assigneeValue)),
			CreatedAt: now,
		}
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("create assignment event: %w", err)
		}
		return nil
	})
}

func sameUintPtr(left, right *uint) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func containsUint(values []uint, target uint) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
