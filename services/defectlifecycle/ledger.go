package defectlifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DefectStatusActive          = "ACTIVE"
	DefectStatusCoverageGap     = "COVERAGE_GAP"
	DefectStatusVerifiedPending = "VERIFIED_PENDING"
	DefectStatusResolved        = "RESOLVED"
	DefectStatusDormant         = "DORMANT"
	DefectStatusObsolete        = "OBSOLETE"
	DefectStatusHumanClosed     = "HUMAN_CLOSED"
	DefectStatusMerged          = "MERGED"

	EventTypeCreated       = "CREATED"
	EventTypeStatusChanged = "STATUS_CHANGED"
	EventTypeIdentityMoved = "IDENTITY_MOVED"
	EventTypeExemptRecheck = "EXEMPT_RECHECK"
)

type DefectSnapshot struct {
	Status              string `json:"status"`
	StatusReason        string `json:"status_reason"`
	NormPath            string `json:"norm_path"`
	ScopeKey            string `json:"scope_key"`
	SymbolPath          string `json:"symbol_path"`
	StmtShape           string `json:"stmt_shape"`
	CleanToken          string `json:"clean_token"`
	PrevShape           string `json:"prev_shape"`
	NextShape           string `json:"next_shape"`
	OccurrenceIndex     int    `json:"occurrence_index"`
	DefectClassMajor    string `json:"defect_class_major"`
	Severity            string `json:"severity"`
	LineStart           *int   `json:"line_start"`
	LineEnd             *int   `json:"line_end"`
	BlobHash            string `json:"blob_hash"`
	ScopeBodyHash       string `json:"scope_body_hash"`
	LastSeenReportID    uint   `json:"last_seen_report_id"`
	LastMatchedReportID uint   `json:"last_matched_report_id"`
	ResolvedReportID    *uint  `json:"resolved_report_id,omitempty"`
	MissedCount         int    `json:"missed_count"`
	DormantRounds       int    `json:"dormant_rounds"`
	RowVersion          int    `json:"row_version"`
}

func newDefectSnapshot(defect models.Defect) DefectSnapshot {
	return DefectSnapshot{
		Status: defect.Status, StatusReason: defect.StatusReason,
		NormPath: defect.NormPath, ScopeKey: defect.ScopeKey, SymbolPath: defect.SymbolPath,
		StmtShape: defect.StmtShape, CleanToken: defect.CleanToken, PrevShape: defect.PrevShape,
		NextShape: defect.NextShape, OccurrenceIndex: defect.OccurrenceIndex,
		DefectClassMajor: defect.DefectClassMajor, Severity: defect.Severity,
		LineStart: defect.LineStart, LineEnd: defect.LineEnd, BlobHash: defect.BlobHash,
		ScopeBodyHash: defect.ScopeBodyHash, LastSeenReportID: defect.LastSeenReportID,
		LastMatchedReportID: defect.LastMatchedReportID, ResolvedReportID: defect.ResolvedReportID,
		MissedCount: defect.MissedCount, DormantRounds: defect.DormantRounds, RowVersion: defect.RowVersion,
	}
}

func evidenceWithSnapshot(value interface{}) (datatypes.JSON, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(raw), nil
}

type LedgerInput struct {
	DB               *gorm.DB
	Report           models.TaskReport
	Repo             models.Repository
	TaskType         models.TaskType
	Observations     []ObservationGroup
	Decisions        []ObservationDecision
	Scope            []models.ScanScopeEntry
	Evaluator        ScopeCoverageEvaluator
	Coverage         *coverage.Coverage
	CoverageState    string
	AlgorithmVersion string
	RenameTargets    map[string]string
	Lifecycle        LifecyclePolicy
	CommittedAt      time.Time
}

type LedgerResult struct {
	Observations []models.DefectObservation
	Events       []models.DefectEvent
	NewDefectIDs []uint
	CommittedAt  time.Time
}

func CommitScanLedger(input LedgerInput) (*LedgerResult, error) {
	if input.DB == nil {
		return nil, errors.New("database is not initialized")
	}
	if input.Report.ID == 0 || input.Repo.ID == 0 || input.TaskType.ID == 0 {
		return nil, errors.New("ledger commit requires report, repository and task type")
	}
	if input.AlgorithmVersion == "" {
		input.AlgorithmVersion = AlgorithmVersion
	}
	if input.CommittedAt.IsZero() {
		input.CommittedAt = time.Now()
	}

	var result *LedgerResult
	err := input.DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		result, txErr = commitLedger(tx, input)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func commitLedger(tx *gorm.DB, input LedgerInput) (*LedgerResult, error) {
	defects, err := loadNamespaceDefects(tx, input.Repo.ID, input.TaskType.ID)
	if err != nil {
		return nil, err
	}
	defectByID := make(map[uint]models.Defect, len(defects))
	for _, defect := range defects {
		defectByID[defect.ID] = defect
	}

	result := &LedgerResult{
		Observations: make([]models.DefectObservation, 0, len(input.Decisions)),
		Events:       make([]models.DefectEvent, 0),
		CommittedAt:  input.CommittedAt,
	}
	matchedDefects := make(map[uint]bool)
	now := input.CommittedAt

	newDefectByFingerprint := make(map[string]uint)
	for _, decision := range committableDecisions(input) {
		observation := models.DefectObservation{
			ReportID:            input.Report.ID,
			RepoID:              input.Repo.ID,
			TaskTypeID:          input.TaskType.ID,
			ObservationGroupUID: decision.ObservationGroupUID,
			DefectID:            decision.DefectID,
			Verdict:             decision.Verdict,
			MatchTier:           decision.MatchTier,
			Confidence:          decision.Confidence,
			ScoreDetail:         marshalScoreDetail(decision.ScoreDetail),
			Reason:              decision.Reason,
			CreatedAt:           now,
		}

		if decision.Verdict == VerdictNew {
			group := findObservationGroup(input.Observations, decision.ObservationGroupUID)
			identity := decision.Identity
			if group != nil {
				identity = group.Identity
			}
			fingerprint, _ := canonicalIdentity(identity)
			defectID, exists := newDefectByFingerprint[fingerprint]
			if !exists {
				var err error
				var finding *models.AnalysisFinding
				if group != nil {
					finding = &group.Representative
				}
				createdDefect, err := createDefect(tx, input, identity, finding, now)
				if err != nil {
					return nil, err
				}
				defectID = createdDefect.ID
				newDefectByFingerprint[fingerprint] = defectID
				defectByID[defectID] = createdDefect
				result.NewDefectIDs = append(result.NewDefectIDs, defectID)
			}
			observation.DefectID = &defectID
			createdDefect := defectByID[defectID]
			createdEvent := models.DefectEvent{
				DefectID: createdDefect.ID, ReportID: &input.Report.ID, EventType: EventTypeCreated,
				FromStatus: "", ToStatus: createdDefect.Status, ActorType: "SYSTEM", Reason: "NEW observation",
				Evidence: datatypes.JSON("{}"), CreatedAt: now,
			}
			result.Events = append(result.Events, createdEvent)
		}
		if observation.DefectID != nil {
			if source, exists := defectByID[*observation.DefectID]; exists &&
				source.Status == DefectStatusMerged && source.MergedIntoID != nil {
				observation.DefectID = source.MergedIntoID
				decision.DefectID = source.MergedIntoID
				if target, targetExists := defectByID[*source.MergedIntoID]; targetExists {
					if decision.Verdict != VerdictCleared {
						observation.Verdict = matchedVerdict(target.Status)
					}
				}
			}
			matchedDefects[*observation.DefectID] = true
		}
		result.Observations = append(result.Observations, observation)
	}

	if err := upsertObservations(tx, result.Observations); err != nil {
		return nil, err
	}
	for observationIndex := range result.Observations {
		observation := &result.Observations[observationIndex]
		if observation.DefectID == nil || observation.Verdict == VerdictNew {
			continue
		}
		group := findObservationGroup(input.Observations, observation.ObservationGroupUID)
		if group == nil {
			continue
		}
		identity := group.Identity
		defect, statusEvent, changed, err := updateMatchedDefect(tx, defectByID[*observation.DefectID], identity, input, observation, now)
		if err != nil {
			return nil, err
		}
		defectByID[defect.ID] = defect
		if changed {
			result.Events = append(result.Events, statusEvent)
		}
	}

	unmatchedEvents, err := advanceUnmatchedDefects(tx, defects, matchedDefects, input, now)
	if err != nil {
		return nil, err
	}
	result.Events = append(result.Events, unmatchedEvents...)

	for _, observation := range result.Observations {
		if observation.DefectID == nil {
			continue
		}
		group := findObservationGroup(input.Observations, observation.ObservationGroupUID)
		if group == nil {
			continue
		}
		if err := upsertDefectAliases(tx, input, *observation.DefectID, group.Identity, now); err != nil {
			return nil, err
		}
	}
	if len(result.Events) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&result.Events).Error; err != nil {
			return nil, fmt.Errorf("persist defect events: %w", err)
		}
	}

	if _, err := models.UpdateActiveTaskReport(tx, input.Report.ID, map[string]interface{}{
		"ledger_committed_at":      input.CommittedAt,
		"ledger_algorithm_version": input.AlgorithmVersion,
	}); err != nil {
		return nil, fmt.Errorf("persist ledger metadata: %w", err)
	}
	return result, nil
}

func prepareLedgerCommit(tx *gorm.DB, input LedgerInput) error {
	if tx.Dialector.Name() == "postgres" {
		lockKey := fmt.Sprintf("%d:%d", input.Repo.ID, input.TaskType.ID)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return fmt.Errorf("acquire ledger lock: %w", err)
		}
	}
	if err := validateLatestLedgerReport(tx, input.Report); err != nil {
		return err
	}
	return rollbackReportLedger(tx, input.Report)
}

func validateLatestLedgerReport(tx *gorm.DB, report models.TaskReport) error {
	var latest struct {
		ID uint
	}
	err := tx.Model(&models.TaskReport{}).
		Select("id").
		Where("repo_id = ? AND task_type_id = ? AND ledger_committed_at IS NOT NULL", report.RepoID, report.TaskTypeID).
		Order("id DESC").First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve latest ledger report: %w", err)
	}
	if report.ID < latest.ID {
		return fmt.Errorf("%w: report %d is not the latest committed ledger report (latest is %d)", ErrSupersededReport, report.ID, latest.ID)
	}
	return nil
}

func rollbackReportLedger(tx *gorm.DB, report models.TaskReport) error {
	retentionDays := models.AppConfig.Retention.LedgerRetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}
	if report.LedgerCommittedAt != nil &&
		time.Since(*report.LedgerCommittedAt) > time.Duration(retentionDays)*24*time.Hour {
		return fmt.Errorf("report %d is outside the snapshot retention window (%d days); rollback rejected", report.ID, retentionDays)
	}

	var observations []models.DefectObservation
	if err := tx.Where("report_id = ?", report.ID).Find(&observations).Error; err != nil {
		return fmt.Errorf("load observations for retry: %w", err)
	}
	var events []models.DefectEvent
	if err := tx.Where("report_id = ? AND actor_type = ?", report.ID, "SYSTEM").Order("id DESC").Find(&events).Error; err != nil {
		return fmt.Errorf("load events for retry: %w", err)
	}

	affected := make(map[uint]models.DefectEvent)
	for _, event := range events {
		if event.EventType == EventTypeStatusChanged {
			affected[event.DefectID] = event
		}
	}
	if err := tx.Where("report_id = ?", report.ID).Delete(&models.DefectObservation{}).Error; err != nil {
		return fmt.Errorf("delete observations for retry: %w", err)
	}
	if err := tx.Where("report_id = ? AND actor_type = ?", report.ID, "SYSTEM").Delete(&models.DefectEvent{}).Error; err != nil {
		return fmt.Errorf("delete events for retry: %w", err)
	}

	for _, observation := range observations {
		if observation.DefectID == nil {
			continue
		}
		defectID := *observation.DefectID
		var otherCount int64
		if err := tx.Model(&models.DefectObservation{}).
			Where("defect_id = ? AND report_id <> ?", defectID, report.ID).
			Count(&otherCount).Error; err != nil {
			return fmt.Errorf("count sibling observations: %w", err)
		}
		var defect models.Defect
		if err := tx.First(&defect, defectID).Error; err != nil {
			continue
		}
		if otherCount == 0 && defect.FirstReportID == report.ID {
			if err := tx.Where("defect_id = ?", defectID).Delete(&models.DefectAlias{}).Error; err != nil {
				return fmt.Errorf("delete aliases for rolled-back defect: %w", err)
			}
			if err := tx.Where("defect_id = ?", defectID).Delete(&models.DefectEvent{}).Error; err != nil {
				return fmt.Errorf("delete events for rolled-back defect: %w", err)
			}
			if err := tx.Unscoped().Delete(&models.Defect{}, defectID).Error; err != nil {
				return fmt.Errorf("rollback defect: %w", err)
			}
			continue
		}
		if event, ok := affected[defectID]; ok {
			var snapshot DefectSnapshot
			if err := json.Unmarshal(event.Evidence, &snapshot); err != nil {
				return fmt.Errorf("parse defect rollback snapshot: %w", err)
			}
			updates := map[string]interface{}{
				"status": snapshot.Status, "status_reason": snapshot.StatusReason,
				"norm_path": snapshot.NormPath, "scope_key": snapshot.ScopeKey,
				"symbol_path": snapshot.SymbolPath, "stmt_shape": snapshot.StmtShape,
				"clean_token": snapshot.CleanToken, "prev_shape": snapshot.PrevShape,
				"next_shape": snapshot.NextShape, "occurrence_index": snapshot.OccurrenceIndex,
				"defect_class_major": snapshot.DefectClassMajor, "severity": snapshot.Severity,
				"line_start": snapshot.LineStart, "line_end": snapshot.LineEnd,
				"blob_hash": snapshot.BlobHash, "scope_body_hash": snapshot.ScopeBodyHash,
				"last_seen_report_id": snapshot.LastSeenReportID, "last_matched_report_id": snapshot.LastMatchedReportID,
				"resolved_report_id": snapshot.ResolvedReportID,
				"missed_count":       snapshot.MissedCount, "dormant_rounds": snapshot.DormantRounds,
				"row_version": gorm.Expr("row_version + 1"), "updated_at": time.Now(),
			}
			if err := tx.Model(&models.Defect{}).Where("id = ?", defectID).Updates(updates).Error; err != nil {
				return fmt.Errorf("restore defect snapshot: %w", err)
			}
		}
	}
	return recomputeAffectedAliases(tx, report.ID)
}

func recomputeAffectedAliases(tx *gorm.DB, reportID uint) error {
	var aliases []models.DefectAlias
	if err := tx.Where("last_report_id = ?", reportID).Find(&aliases).Error; err != nil {
		return fmt.Errorf("load aliases for retry: %w", err)
	}
	for _, alias := range aliases {
		var stats struct {
			Count int64
			First uint
			Last  uint
		}
		err := tx.Model(&models.DefectObservation{}).
			Select("COUNT(*) AS count, COALESCE(MIN(report_id), 0) AS first, COALESCE(MAX(report_id), 0) AS last").
			Where("defect_id = ?", alias.DefectID).Scan(&stats).Error
		if err != nil {
			return fmt.Errorf("recompute alias stats: %w", err)
		}
		if stats.Count == 0 {
			if err := tx.Delete(&models.DefectAlias{}, alias.ID).Error; err != nil {
				return fmt.Errorf("delete empty alias: %w", err)
			}
			continue
		}
		if err := tx.Model(&models.DefectAlias{}).Where("id = ?", alias.ID).Updates(map[string]interface{}{
			"first_report_id": stats.First, "last_report_id": stats.Last, "hit_count": stats.Count, "updated_at": time.Now(),
		}).Error; err != nil {
			return fmt.Errorf("update alias stats: %w", err)
		}
	}
	return nil
}

func loadNamespaceDefects(tx *gorm.DB, repoID, taskTypeID uint) ([]models.Defect, error) {
	var defects []models.Defect
	if err := tx.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).Order("id").Find(&defects).Error; err != nil {
		return nil, fmt.Errorf("load defects: %w", err)
	}
	return defects, nil
}

func findObservationGroup(groups []ObservationGroup, uid string) *ObservationGroup {
	for index := range groups {
		if groups[index].UID == uid {
			return &groups[index]
		}
	}
	return nil
}

func committableDecisions(input LedgerInput) []ObservationDecision {
	decisions := make([]ObservationDecision, 0, len(input.Decisions))
	for _, decision := range input.Decisions {
		group := findObservationGroup(input.Observations, decision.ObservationGroupUID)
		if group == nil {
			if decision.Verdict == VerdictCleared {
				continue
			}
			decisions = append(decisions, decision)
			continue
		}
		if isPassOnlyObservationGroup(*group) && decision.DefectID == nil {
			continue
		}
		if decision.Verdict == VerdictCleared && !passObservationCovered(input, group.Identity) {
			continue
		}
		decisions = append(decisions, decision)
	}
	return decisions
}

func passObservationCovered(input LedgerInput, identity Identity) bool {
	if input.CoverageState != CoverageComplete && input.CoverageState != CoverageNotApplicable {
		return false
	}
	if input.Evaluator != nil {
		return input.Evaluator.IsCovered(identity.NormPath)
	}
	scope := scopeEntryByPath(input.Scope, identity.NormPath)
	return scope != nil && scope.Outcome == ScopeScanned
}

func createDefect(tx *gorm.DB, input LedgerInput, identity Identity, finding *models.AnalysisFinding, now time.Time) (models.Defect, error) {
	fingerprint, fingerprintKind := canonicalIdentity(identity)
	if tx.Dialector.Name() == "postgres" {
		lockKey := fmt.Sprintf("%d:%d", input.Repo.ID, input.TaskType.ID)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return models.Defect{}, fmt.Errorf("acquire defect namespace lock: %w", err)
		}
	}
	var existing models.Defect
	err := tx.Where("repo_id = ? AND task_type_id = ? AND canonical_fingerprint = ?",
		input.Repo.ID, input.TaskType.ID, fingerprint).First(&existing).Error
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Defect{}, fmt.Errorf("check existing defect: %w", err)
	}
	scope := scopeEntryByPath(input.Scope, identity.NormPath)
	lineStart, lineEnd := identity.LineStart, identity.LineEnd
	defect := models.Defect{
		RepoID: input.Repo.ID, TaskTypeID: input.TaskType.ID,
		Status: DefectStatusActive, StatusReason: "created from NEW observation",
		CanonicalFingerprint: fingerprint, IdentityKind: fingerprintKind,
		NormPath: identity.NormPath, ScopeKey: identity.ScopeKey,
		SymbolPath: identity.SymbolPath, StmtShape: identity.StmtShape,
		CleanToken: identity.CleanToken, PrevShape: identity.PrevShape, NextShape: identity.NextShape,
		OccurrenceIndex: identity.OccurrenceIndex, DefectClassMajor: identity.DefectClassMajor,
		Severity: identity.Severity, LineStart: &lineStart, LineEnd: &lineEnd,
		ScopeBodyHash: identity.ScopeBodyHash,
		FirstReportID: input.Report.ID, LastSeenReportID: input.Report.ID,
		LastMatchedReportID: input.Report.ID, RowVersion: 1,
	}
	if finding != nil {
		defect.Title = finding.Title
		defect.Category = finding.Category
		defect.CodeSnippet = finding.CodeSnippet
		defect.Suggestion = finding.Suggestion
		defect.DetailSummary = finding.Detail
	}
	if scope != nil {
		defect.BlobHash = scope.BlobHash
	}
	if err := tx.Create(&defect).Error; err != nil {
		return models.Defect{}, fmt.Errorf("create defect: %w", err)
	}
	return defect, nil
}

func canonicalIdentity(identity Identity) (string, string) {
	switch {
	case identity.K2 != "":
		return identity.K2, "K2"
	case identity.F1 != "":
		return identity.F1, "F1"
	case identity.K1 != "":
		return identity.K1, "K1"
	case identity.F2 != "":
		return identity.F2, "F2"
	default:
		return identityHash(identity.NormPath, identity.ScopeKey, identity.StmtShape, fmt.Sprint(identity.OccurrenceIndex), identity.DefectClassMajor), "K1"
	}
}

func marshalScoreDetail(detail map[string]float64) datatypes.JSON {
	if detail == nil {
		detail = map[string]float64{}
	}
	raw, _ := json.Marshal(detail)
	return datatypes.JSON(raw)
}

func scopeEntryByPath(entries []models.ScanScopeEntry, path string) *models.ScanScopeEntry {
	for index := range entries {
		if entries[index].NormPath == path {
			return &entries[index]
		}
	}
	return nil
}

func upsertObservations(tx *gorm.DB, observations []models.DefectObservation) error {
	if len(observations) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "report_id"}, {Name: "observation_group_uid"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"defect_id", "verdict", "match_tier", "confidence", "score_detail", "reason",
		}),
	}).Create(&observations).Error
}

func updateMatchedDefect(tx *gorm.DB, defect models.Defect, identity Identity, input LedgerInput, observation *models.DefectObservation, now time.Time) (models.Defect, models.DefectEvent, bool, error) {
	if defect.Status == DefectStatusHumanClosed {
		return defect, models.DefectEvent{
			DefectID: defect.ID, ReportID: &input.Report.ID, EventType: EventTypeExemptRecheck,
			FromStatus: defect.Status, ToStatus: defect.Status, ActorType: "SYSTEM",
			Reason: observation.Reason, Evidence: datatypes.JSON("{}"), CreatedAt: now,
		}, true, nil
	}
	if defect.Status == DefectStatusMerged {
		return defect, models.DefectEvent{
			DefectID: defect.ID, ReportID: &input.Report.ID, EventType: EventTypeExemptRecheck,
			FromStatus: defect.Status, ToStatus: defect.Status, ActorType: "SYSTEM",
			Reason: "merged defect matched; observation transferred to target", Evidence: datatypes.JSON("{}"),
			CreatedAt: now,
		}, true, nil
	}
	scope := scopeEntryByPath(input.Scope, identity.NormPath)
	status := defect.Status
	switch observation.Verdict {
	case VerdictExisted, VerdictReopened:
		status = DefectStatusActive
	case VerdictCleared:
		switch defect.Status {
		case DefectStatusActive, DefectStatusCoverageGap, DefectStatusVerifiedPending, DefectStatusDormant, DefectStatusResolved:
			status = DefectStatusResolved
		}
	}
	lineStart, lineEnd := identity.LineStart, identity.LineEnd
	snapshot := newDefectSnapshot(defect)
	updates := map[string]interface{}{
		"norm_path": identity.NormPath, "scope_key": identity.ScopeKey,
		"symbol_path": identity.SymbolPath, "stmt_shape": identity.StmtShape,
		"clean_token": identity.CleanToken, "prev_shape": identity.PrevShape,
		"next_shape": identity.NextShape, "occurrence_index": identity.OccurrenceIndex,
		"defect_class_major": identity.DefectClassMajor, "severity": identity.Severity,
		"line_start": lineStart, "line_end": lineEnd,
		"scope_body_hash":     identity.ScopeBodyHash,
		"last_seen_report_id": input.Report.ID, "last_matched_report_id": input.Report.ID,
		"row_version": gorm.Expr("row_version + 1"), "updated_at": now,
	}
	if scope != nil {
		updates["blob_hash"] = scope.BlobHash
	}
	if defect.Title == "" {
		if group := findObservationGroup(input.Observations, observation.ObservationGroupUID); group != nil {
			updates["title"] = group.Representative.Title
			updates["category"] = group.Representative.Category
			updates["code_snippet"] = group.Representative.CodeSnippet
			updates["suggestion"] = group.Representative.Suggestion
			updates["detail_summary"] = group.Representative.Detail
		}
	}
	if status != defect.Status {
		updates["status"] = status
		updates["status_reason"] = observation.Reason
	}
	if status == DefectStatusResolved {
		updates["resolved_report_id"] = input.Report.ID
	}
	query := tx.Model(&models.Defect{}).Where("id = ? AND row_version = ?", defect.ID, defect.RowVersion)
	if err := query.Updates(updates).Error; err != nil {
		return models.Defect{}, models.DefectEvent{}, false, fmt.Errorf("update matched defect: %w", err)
	}
	if query.RowsAffected == 0 {
		return models.Defect{}, models.DefectEvent{}, false, fmt.Errorf("defect %d changed during ledger commit", defect.ID)
	}
	previousStatus := defect.Status
	defect.Status = status
	defect.LastSeenReportID = input.Report.ID
	defect.LastMatchedReportID = input.Report.ID
	defect.RowVersion++
	event := models.DefectEvent{}
	changed := false
	if status != previousStatus || identityChanged(defect, identity) {
		evidence, evidenceErr := evidenceWithSnapshot(snapshot)
		if evidenceErr != nil {
			return models.Defect{}, models.DefectEvent{}, false, fmt.Errorf("serialize matched defect snapshot: %w", evidenceErr)
		}
		event = models.DefectEvent{
			DefectID: defect.ID, ReportID: &input.Report.ID, EventType: EventTypeStatusChanged,
			FromStatus: previousStatus, ToStatus: status, ActorType: "SYSTEM",
			Reason: observation.Reason, Evidence: evidence, CreatedAt: now,
		}
		changed = true
	}
	return defect, event, changed, nil
}

func identityChanged(defect models.Defect, identity Identity) bool {
	lineStartChanged := (defect.LineStart == nil && identity.LineStart != 0) ||
		(defect.LineStart != nil && *defect.LineStart != identity.LineStart)
	lineEndChanged := (defect.LineEnd == nil && identity.LineEnd != 0) ||
		(defect.LineEnd != nil && *defect.LineEnd != identity.LineEnd)
	return defect.NormPath != identity.NormPath || defect.ScopeKey != identity.ScopeKey ||
		defect.SymbolPath != identity.SymbolPath || defect.StmtShape != identity.StmtShape ||
		defect.CleanToken != identity.CleanToken || defect.PrevShape != identity.PrevShape ||
		defect.NextShape != identity.NextShape || defect.OccurrenceIndex != identity.OccurrenceIndex ||
		defect.DefectClassMajor != identity.DefectClassMajor || defect.Severity != identity.Severity ||
		lineStartChanged || lineEndChanged || defect.ScopeBodyHash != identity.ScopeBodyHash
}

func advanceUnmatchedDefects(tx *gorm.DB, defects []models.Defect, matched map[uint]bool, input LedgerInput, now time.Time) ([]models.DefectEvent, error) {
	events := make([]models.DefectEvent, 0)
	coverageBlocked := coverageLifecycleBlocked(input.Coverage)
	for _, defect := range defects {
		if matched[defect.ID] {
			continue
		}
		switch defect.Status {
		case DefectStatusActive, DefectStatusCoverageGap, DefectStatusVerifiedPending, DefectStatusDormant:
		default:
			continue
		}
		covered := false
		if input.Evaluator != nil {
			covered = input.Evaluator.IsCovered(defect.NormPath)
		} else {
			scope := scopeEntryByPath(input.Scope, defect.NormPath)
			covered = scope != nil && scope.Outcome == ScopeScanned
		}
		status := defect.Status
		missed := defect.MissedCount
		dormant := defect.DormantRounds
		reason := ""

		if coverageBlocked {
			if defect.Status == DefectStatusActive {
				status = DefectStatusCoverageGap
				reason = "coverage degraded; lifecycle advancement withheld"
				updates := map[string]interface{}{
					"row_version": gorm.Expr("row_version + 1"), "updated_at": now,
					"status": status, "status_reason": reason,
				}
				query := tx.Model(&models.Defect{}).Where("id = ? AND row_version = ?", defect.ID, defect.RowVersion)
				if err := query.Updates(updates).Error; err != nil {
					return nil, fmt.Errorf("withhold lifecycle advancement: %w", err)
				}
				if query.RowsAffected == 0 {
					return nil, fmt.Errorf("defect %d changed while withholding lifecycle advancement", defect.ID)
				}
				defect.RowVersion++
				defect.Status = status
				evidence, evidenceErr := evidenceWithSnapshot(newDefectSnapshot(defect))
				if evidenceErr != nil {
					return nil, fmt.Errorf("serialize coverage gap snapshot: %w", evidenceErr)
				}
				events = append(events, models.DefectEvent{
					DefectID: defect.ID, ReportID: &input.Report.ID, EventType: EventTypeStatusChanged,
					FromStatus: DefectStatusActive, ToStatus: status, ActorType: "SYSTEM",
					Reason: reason, Evidence: evidence, CreatedAt: now,
				})
			}
			continue
		}

		if input.Lifecycle.RequireCoverage && !covered {
			if status == DefectStatusActive {
				status = DefectStatusCoverageGap
				reason = "file is not successfully covered"
			}
		} else {
			missed++
			severity := severityWeight(defect.Severity)
			switch {
			case containsString(input.Lifecycle.HighRiskSeverities, defect.Severity):
				status = DefectStatusVerifiedPending
				reason = "file scanned but defect not reproduced"
			case severity >= 3:
				if missed >= input.Lifecycle.ResolvedRounds {
					status = DefectStatusResolved
					reason = "file scanned but defect not reproduced for two rounds"
				} else {
					status = DefectStatusCoverageGap
					reason = "file scanned but defect not reproduced"
				}
			default:
				if missed >= input.Lifecycle.DormantThreshold {
					status = DefectStatusDormant
					reason = "file scanned but defect not reproduced"
				} else {
					status = DefectStatusCoverageGap
					reason = "file scanned but defect not reproduced"
				}
			}
		}
		if status == DefectStatusDormant && covered {
			dormant++
			if dormant >= input.Lifecycle.ObsoleteAfterDormant {
				status = DefectStatusObsolete
				reason = fmt.Sprintf("dormant for %d covered rounds", dormant)
			}
		}

		updates := map[string]interface{}{
			"row_version": gorm.Expr("row_version + 1"), "updated_at": now,
			"missed_count": missed, "dormant_rounds": dormant,
		}
		if status != defect.Status {
			updates["status"] = status
			updates["status_reason"] = reason
		}
		query := tx.Model(&models.Defect{}).Where("id = ? AND row_version = ?", defect.ID, defect.RowVersion)
		if err := query.Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("advance unmatched defect: %w", err)
		}
		if query.RowsAffected == 0 {
			return nil, fmt.Errorf("defect %d changed during lifecycle advance", defect.ID)
		}
		defect.RowVersion++
		if status != defect.Status || missed != defect.MissedCount || dormant != defect.DormantRounds {
			evidence, err := evidenceWithSnapshot(newDefectSnapshot(defect))
			if err != nil {
				return nil, fmt.Errorf("serialize unmatched defect snapshot: %w", err)
			}
			events = append(events, models.DefectEvent{
				DefectID: defect.ID, ReportID: &input.Report.ID, EventType: EventTypeStatusChanged,
				FromStatus: defect.Status, ToStatus: status, ActorType: "SYSTEM",
				Reason: reason, Evidence: evidence, CreatedAt: now,
			})
			defect.Status = status
		}
	}
	return events, nil
}

func upsertDefectAliases(tx *gorm.DB, input LedgerInput, defectID uint, identity Identity, now time.Time) error {
	values := []struct {
		aliasType string
		value     string
		class     string
	}{
		{AliasK2, identity.K2, AliasStrong},
		{AliasF1, identity.F1, AliasStrong},
		{AliasK1, identity.K1, AliasBucket},
		{AliasF2, identity.F2, AliasBucket},
		{AliasF3, identity.F3, AliasBucket},
		{AliasPath, identity.NormPath, AliasBucket},
		{AliasRoot, identity.RootFamily, AliasBucket},
		{AliasResource, identity.ResourceIdentity, AliasBucket},
		{AliasChain, identity.ValidationChain, AliasBucket},
	}
	if input.RenameTargets != nil {
		if target, ok := input.RenameTargets[identity.NormPath]; ok && target != "" {
			values = append(values, struct {
				aliasType string
				value     string
				class     string
			}{AliasRename, target, AliasStrong})
		}
	}
	for _, item := range values {
		if item.value == "" {
			continue
		}
		var alias models.DefectAlias
		aliasKey := func() *gorm.DB {
			query := tx.Where("repo_id = ? AND task_type_id = ? AND alias_type = ? AND alias_value = ?",
				input.Repo.ID, input.TaskType.ID, item.aliasType, item.value)
			if item.class == AliasBucket {
				query = query.Where("defect_id = ?", defectID)
			}
			return query
		}
		err := aliasKey().First(&alias).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			alias = models.DefectAlias{
				DefectID: defectID, RepoID: input.Repo.ID, TaskTypeID: input.TaskType.ID,
				AliasType: item.aliasType, AliasValue: item.value, AliasClass: item.class,
				FirstReportID: input.Report.ID, LastReportID: input.Report.ID, HitCount: 1,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&alias).Error; err != nil {
				err = aliasKey().First(&alias).Error
				if err != nil {
					return fmt.Errorf("create defect alias: %w", err)
				}
				if alias.DefectID != defectID {
					continue
				}
			} else {
				continue
			}
		}
		if err != nil {
			return fmt.Errorf("load defect alias: %w", err)
		}
		updates := map[string]interface{}{
			"last_report_id": input.Report.ID, "hit_count": gorm.Expr("hit_count + 1"), "updated_at": now,
		}
		if err := tx.Model(&models.DefectAlias{}).Where("id = ?", alias.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("update defect alias: %w", err)
		}
	}
	return nil
}
