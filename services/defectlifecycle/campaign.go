package defectlifecycle

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"code-shield/models"

	"gorm.io/gorm"
)

var (
	ErrDefectChanged = errors.New("defect changed; retry")
)

const defectUpdateRetryLimit = 3

func transactionWithConflictRetry(db *gorm.DB, operation func(tx *gorm.DB) error) error {
	for attempt := 0; attempt < defectUpdateRetryLimit; attempt++ {
		if err := db.Transaction(operation); err == nil {
			return nil
		} else if !errors.Is(err, ErrDefectChanged) {
			return err
		}
	}
	return ErrDefectChanged
}

type CampaignQuery struct {
	TaskTypeID uint
	RepoID     uint
	Severity   string
	Status     string
	Category   string
	Keyword    string
	Page       int
	PageSize   int
}

type CampaignFinding struct {
	ID            uint                     `json:"id"`
	TaskTypeID    uint                     `json:"task_type_id"`
	RepoID        uint                     `json:"repo_id"`
	RepoName      string                   `json:"repo_name"`
	RepoURL       string                   `json:"repo_url"`
	RepoBranch    string                   `json:"repo_branch,omitempty"`
	FilePath      string                   `json:"file_path"`
	LineNumber    string                   `json:"line_number"`
	Title         string                   `json:"title"`
	Detail        string                   `json:"detail"`
	HunterClaim   string                   `json:"hunter_claim,omitempty"`
	ChallengerArg string                   `json:"challenger_arg,omitempty"`
	JudgeVerdict  string                   `json:"judge_verdict,omitempty"`
	Severity      string                   `json:"severity"`
	Category      string                   `json:"category"`
	CodeSnippet   string                   `json:"code_snippet"`
	TriggerLine   string                   `json:"trigger_line,omitempty"`
	ScopeSymbol   string                   `json:"scope_symbol,omitempty"`
	Suggestion    string                   `json:"suggestion"`
	Status        string                   `json:"status"`
	StatusLog     []map[string]interface{} `json:"status_log"`
	Feedback      string                   `json:"feedback"`
	AssigneeID    *uint                    `json:"assignee_id"`
	Assignee      *CampaignUser            `json:"assignee,omitempty"`
	CreatedAt     time.Time                `json:"created_at"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

type CampaignUser struct {
	ID    uint   `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type CampaignPage struct {
	Total         int64             `json:"total"`
	Page          int               `json:"page"`
	PageSize      int               `json:"page_size"`
	Items         []CampaignFinding `json:"items"`
	SeverityStats map[string]int    `json:"severity_stats"`
	StatusStats   map[string]int    `json:"status_stats"`
	CategoryStats map[string]int    `json:"category_stats"`
	Categories    []string          `json:"categories"`
}

type CampaignRepoSummary struct {
	RepoID         uint      `json:"repo_id"`
	RepoName       string    `json:"repo_name"`
	RepoURL        string    `json:"repo_url"`
	Department     string    `json:"department"`
	OwnerName      string    `json:"owner_name"`
	TotalIssues    int       `json:"total_issues"`
	TotalDefects   int       `json:"total_defects"`
	TotalEntities  int       `json:"total_entities"`
	PassCount      int       `json:"pass_count"`
	PassRate       float64   `json:"pass_rate"`
	Blocking       int       `json:"blocking"`
	Critical       int       `json:"critical"`
	Major          int       `json:"major"`
	Hint           int       `json:"hint"`
	Suggestion     int       `json:"suggestion"`
	OpenIssues     int       `json:"open_issues"`
	ResolvedIssues int       `json:"resolved_issues"`
	FixRate        float64   `json:"fix_rate"`
	LastScanTime   time.Time `json:"last_scan_time"`
}

type CampaignTrendPoint struct {
	Date           string  `json:"date"`
	TotalIssues    int     `json:"total_issues"`
	OpenIssues     int     `json:"open_issues"`
	ResolvedIssues int     `json:"resolved_issues,omitempty"`
	PassCount      int     `json:"pass_count,omitempty"`
	PassRate       float64 `json:"pass_rate,omitempty"`
	FixRate        float64 `json:"fix_rate"`
}

type DefectWorkflowInput struct {
	Status      string
	Feedback    string
	AssigneeID  *uint
	AssigneeSet bool
	ActorID     uint
}

func ListCampaignDefects(db *gorm.DB, query CampaignQuery) (*CampaignPage, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	if query.TaskTypeID == 0 {
		return nil, errors.New("task type is required")
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 || query.PageSize > 500 {
		query.PageSize = 25
	}

	var defects []models.Defect
	tx := db.Where("task_type_id = ?", query.TaskTypeID)
	if query.RepoID != 0 {
		tx = tx.Where("repo_id = ?", query.RepoID)
	}
	if err := tx.Order("id DESC").Find(&defects).Error; err != nil {
		return nil, fmt.Errorf("load campaign defects: %w", err)
	}

	items, err := campaignFindings(db, defects)
	if err != nil {
		return nil, err
	}
	filteredItems := filterCampaignFindings(items, query)

	page := &CampaignPage{
		Total:         int64(len(filteredItems)),
		Page:          query.Page,
		PageSize:      query.PageSize,
		Items:         make([]CampaignFinding, 0),
		SeverityStats: make(map[string]int),
		StatusStats:   make(map[string]int),
		CategoryStats: make(map[string]int),
		Categories:    make([]string, 0),
	}
	for _, item := range items {
		page.SeverityStats[item.Severity]++
		page.StatusStats[item.Status]++
		if item.Category != "" {
			if page.CategoryStats[item.Category] == 0 {
				page.Categories = append(page.Categories, item.Category)
			}
			page.CategoryStats[item.Category]++
		}
	}
	start := (query.Page - 1) * query.PageSize
	end := start + query.PageSize
	if start > len(filteredItems) {
		start = len(filteredItems)
	}
	if end > len(filteredItems) {
		end = len(filteredItems)
	}
	if start < end {
		page.Items = filteredItems[start:end]
	}
	return page, nil
}

func GetCampaignDefect(db *gorm.DB, taskTypeID, defectID uint) (*CampaignFinding, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	var defect models.Defect
	if err := db.Where("id = ? AND task_type_id = ?", defectID, taskTypeID).First(&defect).Error; err != nil {
		return nil, fmt.Errorf("load campaign defect: %w", err)
	}
	items, err := campaignFindings(db, []models.Defect{defect})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("campaign finding not found")
	}
	return &items[0], nil
}

func UpdateDefectWorkflow(db *gorm.DB, defectID uint, input DefectWorkflowInput) (*CampaignFinding, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	err := transactionWithConflictRetry(db, func(tx *gorm.DB) error {
		var defect models.Defect
		if err := tx.First(&defect, defectID).Error; err != nil {
			return fmt.Errorf("load defect: %w", err)
		}
		now := time.Now()
		nextStatus := defect.Status
		if input.Status != "" {
			status, ok := campaignStatusToLedger(input.Status)
			if !ok {
				return fmt.Errorf("invalid status: %s", input.Status)
			}
			nextStatus = status
		}
		rowVersion := defect.RowVersion
		if nextStatus != defect.Status || input.Feedback != "" {
			rowVersion++
			updates := map[string]interface{}{
				"status":         nextStatus,
				"status_reason":  input.Feedback,
				"human_locked":   true,
				"human_decision": input.Status,
				"row_version":    rowVersion,
			}
			result := tx.Model(&models.Defect{}).
				Where("id = ? AND row_version = ?", defectID, defect.RowVersion).
				Updates(updates)
			if result.Error != nil {
				return fmt.Errorf("update defect workflow: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return fmt.Errorf("defect %d changed during workflow update: %w", defectID, ErrDefectChanged)
			}
			fromStatus := campaignStatusFromLedger(defect.Status)
			toStatus := campaignStatusFromLedger(nextStatus)
			event := models.DefectEvent{
				DefectID: defectID, EventType: EventTypeStatusChanged, FromStatus: fromStatus,
				ToStatus: toStatus, ActorType: "HUMAN", ActorID: actorOrNil(input.ActorID),
				Reason: input.Feedback, Evidence: []byte("{}"), CreatedAt: now,
			}
			if err := tx.Create(&event).Error; err != nil {
				return fmt.Errorf("create workflow event: %w", err)
			}
			defect.RowVersion = rowVersion
		}
		if input.AssigneeSet {
			if sameUintPtr(defect.AssigneeID, input.AssigneeID) {
				return nil
			}
			rowVersion = defect.RowVersion + 1
			result := tx.Model(&models.Defect{}).
				Where("id = ? AND row_version = ?", defectID, defect.RowVersion).
				Updates(map[string]interface{}{"assignee_id": input.AssigneeID, "assigned_at": now, "row_version": rowVersion})
			if result.Error != nil {
				return fmt.Errorf("update defect assignment: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return fmt.Errorf("defect %d changed during assignment: %w", defectID, ErrDefectChanged)
			}
			assigneeValue := uint(0)
			if input.AssigneeID != nil {
				assigneeValue = *input.AssigneeID
			}
			event := models.DefectEvent{
				DefectID: defectID, EventType: "HUMAN_DECISION", FromStatus: nextStatus,
				ToStatus: nextStatus, ActorType: "HUMAN", ActorID: actorOrNil(input.ActorID),
				Reason:   "assignment changed",
				Evidence: []byte(fmt.Sprintf(`{"assignee_id":%d}`, assigneeValue)), CreatedAt: now,
			}
			if err := tx.Create(&event).Error; err != nil {
				return fmt.Errorf("create assignment event: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var defect models.Defect
	if err := db.First(&defect, defectID).Error; err != nil {
		return nil, fmt.Errorf("load updated defect: %w", err)
	}
	items, err := campaignFindings(db, []models.Defect{defect})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("campaign finding not found")
	}
	return &items[0], nil
}

func ListLedgerRepoSummaries(db *gorm.DB, taskTypeID uint) ([]CampaignRepoSummary, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	var repos []models.Repository
	if err := db.Preload("Owner").Preload("Department").Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("load campaign repositories: %w", err)
	}
	var defects []models.Defect
	if err := db.Where("task_type_id = ?", taskTypeID).Find(&defects).Error; err != nil {
		return nil, fmt.Errorf("load ledger defects: %w", err)
	}
	type scanTimeRow struct {
		RepoID    uint
		CreatedAt *time.Time
	}
	scanTimes := make([]scanTimeRow, 0)
	if err := db.Model(&models.TaskReport{}).
		Select("repo_id, max(created_at) as created_at").
		Where("task_type_id = ? AND status IN ?", taskTypeID, []string{"success", "skipped"}).
		Group("repo_id").Scan(&scanTimes).Error; err != nil {
		return nil, fmt.Errorf("load ledger scan times: %w", err)
	}
	lastScan := make(map[uint]time.Time, len(scanTimes))
	for _, item := range scanTimes {
		if item.CreatedAt != nil {
			lastScan[item.RepoID] = *item.CreatedAt
		}
	}

	type aggregate struct {
		Open, Resolved, Blocking, Critical, Major, Suggestion int
	}
	aggregates := make(map[uint]*aggregate)
	for _, defect := range defects {
		if defect.Status == DefectStatusMerged {
			continue
		}
		item := aggregates[defect.RepoID]
		if item == nil {
			item = &aggregate{}
			aggregates[defect.RepoID] = item
		}
		if isLedgerDefectResolved(defect.Status) {
			item.Resolved++
		} else {
			item.Open++
		}
		switch defect.Severity {
		case "致命", "阻塞":
			item.Blocking++
		case "严重":
			item.Critical++
		case "一般", "主要", "提示":
			item.Major++
		case "建议":
			item.Suggestion++
		}
	}

	summaries := make([]CampaignRepoSummary, 0, len(repos))
	for _, repo := range repos {
		counts := aggregates[repo.ID]
		if counts == nil {
			counts = &aggregate{}
		}
		total := counts.Open + counts.Resolved
		fixRate := 100.0
		if total > 0 {
			fixRate = float64(counts.Resolved) / float64(total) * 100
		}
		ownerName := repo.Owner.Name
		if ownerName == "" {
			ownerName = "已离职/未知"
		}
		summaries = append(summaries, CampaignRepoSummary{
			RepoID: repo.ID, RepoName: repo.Name, RepoURL: repo.URL,
			Department: repo.Department.Name, OwnerName: ownerName,
			TotalIssues: counts.Open, TotalDefects: total,
			PassRate: 0, Blocking: counts.Blocking, Critical: counts.Critical,
			Major: counts.Major, Suggestion: counts.Suggestion,
			OpenIssues: counts.Open, ResolvedIssues: counts.Resolved,
			FixRate: fixRate, LastScanTime: lastScan[repo.ID],
		})
	}
	return summaries, nil
}

func LedgerTrend(db *gorm.DB, taskTypeID uint, repoIDs []uint) ([]CampaignTrendPoint, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	if len(repoIDs) == 0 {
		return emptyTrend(), nil
	}
	var defects []models.Defect
	if err := db.Where("task_type_id = ? AND repo_id IN ?", taskTypeID, repoIDs).
		Order("id").Find(&defects).Error; err != nil {
		return nil, fmt.Errorf("load ledger trend defects: %w", err)
	}
	defectIDs := make([]uint, 0, len(defects))
	for _, defect := range defects {
		defectIDs = append(defectIDs, defect.ID)
	}
	events := make([]models.DefectEvent, 0)
	if len(defectIDs) > 0 {
		if err := db.Where("defect_id IN ?", defectIDs).Order("created_at, id").Find(&events).Error; err != nil {
			return nil, fmt.Errorf("load ledger trend events: %w", err)
		}
	}
	eventsByDefect := make(map[uint][]models.DefectEvent)
	for _, event := range events {
		eventsByDefect[event.DefectID] = append(eventsByDefect[event.DefectID], event)
	}

	now := time.Now()
	trend := emptyTrend()
	for dayIndex := 29; dayIndex >= 0; dayIndex-- {
		endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location()).AddDate(0, 0, dayIndex-29)
		date := endOfDay.Format("2006-01-02")
		position := 29 - dayIndex
		trend[position].Date = date
		for _, defect := range defects {
			if defect.CreatedAt.After(endOfDay) {
				continue
			}
			status := DefectStatusActive
			for _, event := range eventsByDefect[defect.ID] {
				if event.CreatedAt.After(endOfDay) {
					continue
				}
				if event.EventType == EventTypeStatusChanged || event.EventType == "HUMAN_DECISION" {
					status = event.ToStatus
				}
			}
			trend[position].TotalIssues++
			if isLedgerDefectResolved(status) {
				trend[position].ResolvedIssues++
			} else {
				trend[position].OpenIssues++
			}
		}
		if trend[position].TotalIssues > 0 {
			trend[position].FixRate = float64(trend[position].ResolvedIssues) / float64(trend[position].TotalIssues) * 100
		}
	}
	return trend, nil
}

func campaignFindings(db *gorm.DB, defects []models.Defect) ([]CampaignFinding, error) {
	items := make([]CampaignFinding, 0, len(defects))
	if len(defects) == 0 {
		return items, nil
	}
	defectIDs := make([]uint, 0, len(defects))
	for _, defect := range defects {
		defectIDs = append(defectIDs, defect.ID)
	}
	observations := make([]models.DefectObservation, 0)
	if err := db.Where("defect_id IN ?", defectIDs).Order("report_id DESC, id DESC").Find(&observations).Error; err != nil {
		return nil, fmt.Errorf("load campaign observations: %w", err)
	}
	representative := make(map[uint]models.DefectObservation)
	for _, observation := range observations {
		if observation.DefectID == nil {
			continue
		}
		if _, exists := representative[*observation.DefectID]; !exists {
			representative[*observation.DefectID] = observation
		}
	}

	reportIDs := make([]uint, 0)
	groupUIDs := make([]string, 0)
	for _, observation := range representative {
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
			return nil, fmt.Errorf("load campaign findings: %w", err)
		}
	}
	findingByKey := make(map[string]models.AnalysisFinding)
	for _, finding := range findings {
		key := fmt.Sprintf("%d:%s", finding.TaskReportID, finding.ObservationGroupUID)
		if _, exists := findingByKey[key]; !exists {
			findingByKey[key] = finding
		}
	}

	userIDs := make([]uint, 0)
	repoIDs := make([]uint, 0)
	for _, defect := range defects {
		if defect.AssigneeID != nil && !containsUint(userIDs, *defect.AssigneeID) {
			userIDs = append(userIDs, *defect.AssigneeID)
		}
		if !containsUint(repoIDs, defect.RepoID) {
			repoIDs = append(repoIDs, defect.RepoID)
		}
	}
	events := make([]models.DefectEvent, 0)
	if len(defectIDs) > 0 {
		if err := db.Where("defect_id IN ?", defectIDs).Order("id DESC").Find(&events).Error; err != nil {
			return nil, fmt.Errorf("load campaign events: %w", err)
		}
	}
	for _, event := range events {
		if event.ActorType == "HUMAN" && event.ActorID != nil && !containsUint(userIDs, *event.ActorID) {
			userIDs = append(userIDs, *event.ActorID)
		}
	}
	users := make([]models.User, 0)
	if len(userIDs) > 0 {
		if err := db.Select("id, name, employee_id, email").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return nil, fmt.Errorf("load campaign users: %w", err)
		}
	}
	repos := make([]models.Repository, 0)
	if len(repoIDs) > 0 {
		if err := db.Select("id, name, url, branch").Where("id IN ?", repoIDs).Find(&repos).Error; err != nil {
			return nil, fmt.Errorf("load campaign repositories: %w", err)
		}
	}
	userByID := make(map[uint]models.User)
	for _, user := range users {
		userByID[user.ID] = user
	}
	repoByID := make(map[uint]models.Repository)
	for _, repo := range repos {
		repoByID[repo.ID] = repo
	}
	eventsByDefect := make(map[uint][]models.DefectEvent)
	for _, event := range events {
		eventsByDefect[event.DefectID] = append(eventsByDefect[event.DefectID], event)
	}

	for _, defect := range defects {
		item := CampaignFinding{
			ID: defect.ID, TaskTypeID: defect.TaskTypeID, RepoID: defect.RepoID,
			Severity: defect.Severity, Status: campaignStatusFromLedger(defect.Status),
			StatusLog: make([]map[string]interface{}, 0), Feedback: defect.StatusReason,
			AssigneeID: defect.AssigneeID, CreatedAt: defect.CreatedAt, UpdatedAt: defect.UpdatedAt,
			FilePath: defect.NormPath,
		}
		if defect.AssigneeID != nil {
			user := userByID[*defect.AssigneeID]
			name := user.Name
			if name == "" {
				name = user.EmployeeID
			}
			item.Assignee = &CampaignUser{ID: user.ID, Name: name, Email: user.Email}
		}
		repo := repoByID[defect.RepoID]
		item.RepoName = repo.Name
		item.RepoURL = repo.URL
		item.RepoBranch = repo.Branch
		if observation, exists := representative[defect.ID]; exists {
			finding, exists := findingByKey[fmt.Sprintf("%d:%s", observation.ReportID, observation.ObservationGroupUID)]
			if exists {
				item.FilePath = finding.FilePath
				item.LineNumber = finding.LineNumber
				item.Title = finding.Title
				item.Detail = finding.Detail
				item.HunterClaim = finding.HunterClaim
				item.ChallengerArg = finding.ChallengerArg
				item.JudgeVerdict = finding.JudgeVerdict
				item.Severity = finding.Severity
				item.Category = finding.Category
				item.CodeSnippet = finding.CodeSnippet
				item.TriggerLine = finding.TriggerLine
				item.ScopeSymbol = finding.ScopeSymbol
				item.Suggestion = finding.Suggestion
			}
		}
		if item.Title == "" && defect.Title != "" {
			item.Title = defect.Title
			item.Detail = defect.DetailSummary
			item.Category = defect.Category
			item.CodeSnippet = defect.CodeSnippet
			item.Suggestion = defect.Suggestion
		}
		for _, event := range eventsByDefect[defect.ID] {
			if event.EventType != EventTypeStatusChanged && event.EventType != "HUMAN_DECISION" {
				continue
			}
			actor := "SYSTEM"
			if event.ActorType == "HUMAN" {
				actor = "未知用户"
				if event.ActorID != nil {
					if user, exists := userByID[*event.ActorID]; exists {
						actor = user.Name
						if actor == "" {
							actor = user.EmployeeID
						}
					} else {
						actor = fmt.Sprintf("%d", *event.ActorID)
					}
				}
			}
			item.StatusLog = append(item.StatusLog, map[string]interface{}{
				"status": campaignStatusFromLedger(event.ToStatus), "time": event.CreatedAt.Format("2006-01-02 15:04:05"),
				"user": actor, "comment": event.Reason, "reason": event.Reason,
			})
		}
		items = append(items, item)
	}
	return items, nil
}

func filterCampaignFindings(items []CampaignFinding, query CampaignQuery) []CampaignFinding {
	filtered := make([]CampaignFinding, 0, len(items))
	keyword := strings.ToLower(query.Keyword)
	for _, item := range items {
		if query.Severity != "" && item.Severity != query.Severity {
			continue
		}
		if query.Status != "" && item.Status != query.Status {
			continue
		}
		if query.Category != "" && item.Category != query.Category {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(item.FilePath), keyword) &&
			!strings.Contains(strings.ToLower(item.Title), keyword) &&
			!strings.Contains(strings.ToLower(item.Detail), keyword) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func campaignStatusFromLedger(status string) string {
	switch status {
	case DefectStatusResolved:
		return "resolved"
	case DefectStatusHumanClosed:
		return "closed"
	case DefectStatusObsolete:
		return "invalid"
	case DefectStatusCoverageGap, DefectStatusVerifiedPending, DefectStatusDormant:
		return "analyzing"
	default:
		return "open"
	}
}

func campaignStatusToLedger(status string) (string, bool) {
	switch status {
	case "open":
		return DefectStatusActive, true
	case "analyzing":
		return DefectStatusVerifiedPending, true
	case "resolved":
		return DefectStatusResolved, true
	case "closed":
		return DefectStatusHumanClosed, true
	case "invalid":
		return DefectStatusObsolete, true
	default:
		return "", false
	}
}

func isLedgerDefectResolved(status string) bool {
	switch status {
	case DefectStatusResolved, DefectStatusHumanClosed, DefectStatusObsolete:
		return true
	default:
		return false
	}
}

func emptyTrend() []CampaignTrendPoint {
	now := time.Now()
	trend := make([]CampaignTrendPoint, 30)
	for index := range trend {
		trend[index].Date = now.AddDate(0, 0, index-29).Format("2006-01-02")
		trend[index].FixRate = 100
	}
	return trend
}

func actorOrNil(actorID uint) *uint {
	if actorID == 0 {
		return nil
	}
	return &actorID
}
