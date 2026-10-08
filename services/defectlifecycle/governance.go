package defectlifecycle

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"code-shield/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GovernanceInput struct {
	RepoRoot string
	ActorID  uint
	Reason   string
}

func ConfirmProbable(db *gorm.DB, reportID uint, groupUID string, input GovernanceInput) (*models.Defect, error) {
	return mutateProbable(db, reportID, groupUID, input, nil)
}

func BindProbable(db *gorm.DB, reportID uint, groupUID string, defectID uint, input GovernanceInput) (*models.Defect, error) {
	return mutateProbable(db, reportID, groupUID, input, &defectID)
}

func mutateProbable(db *gorm.DB, reportID uint, groupUID string, input GovernanceInput, targetDefectID *uint) (*models.Defect, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	if reportID == 0 || groupUID == "" {
		return nil, errors.New("report and observation group are required")
	}

	var result *models.Defect
	err := db.Transaction(func(tx *gorm.DB) error {
		var report models.TaskReport
		if err := tx.First(&report, reportID).Error; err != nil {
			return fmt.Errorf("load report: %w", err)
		}
		var repo models.Repository
		if err := tx.First(&repo, report.RepoID).Error; err != nil {
			return fmt.Errorf("load repository: %w", err)
		}
		if input.RepoRoot == "" {
			input.RepoRoot = repositoryRoot(repo)
		}
		var taskType models.TaskType
		if err := tx.First(&taskType, report.TaskTypeID).Error; err != nil {
			return fmt.Errorf("load task type: %w", err)
		}

		var observation models.DefectObservation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("report_id = ? AND observation_group_uid = ?", reportID, groupUID).
			First(&observation).Error; err != nil {
			return fmt.Errorf("load probable observation: %w", err)
		}
		if observation.DefectID != nil {
			return fmt.Errorf("observation is already bound to defect %d", *observation.DefectID)
		}
		if observation.Verdict != VerdictProbable {
			return fmt.Errorf("observation %s is not PROBABLE", groupUID)
		}

		var findings []models.AnalysisFinding
		if err := tx.Where("task_report_id = ?", reportID).Order("id").Find(&findings).Error; err != nil {
			return fmt.Errorf("load findings: %w", err)
		}
		group := findObservationGroup(BuildObservationGroups(input.RepoRoot, reportID, findings), groupUID)
		if group == nil {
			return fmt.Errorf("rebuild observation group %s", groupUID)
		}
		group.Identity.RepoID = report.RepoID
		group.Identity.TaskTypeID = report.TaskTypeID
		now := time.Now()
		reason := input.Reason
		if reason == "" {
			reason = "human governance decision"
		}

		if targetDefectID != nil {
			defect, err := bindObservationToDefect(tx, *targetDefectID, report.RepoID, report.TaskTypeID, &observation, reason, input.ActorID, now)
			if err != nil {
				return err
			}
			result = defect
			return nil
		}

		scopeEntries := []models.ScanScopeEntry{}
		if err := tx.Where("report_id = ?", reportID).Find(&scopeEntries).Error; err != nil {
			return fmt.Errorf("load scope: %w", err)
		}
		var existing models.Defect
		fingerprint := ""
		fingerprint, _ = canonicalIdentity(group.Identity)
		err := tx.Where("repo_id = ? AND task_type_id = ? AND canonical_fingerprint = ?",
			report.RepoID, report.TaskTypeID, fingerprint).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			defect, createErr := createDefect(tx, LedgerInput{
				Report: report, Repo: repo, TaskType: taskType, Scope: scopeEntries, CommittedAt: now,
			}, group.Identity, now)
			if createErr != nil {
				return createErr
			}
			event := models.DefectEvent{
				DefectID: defect.ID, ReportID: &reportID, EventType: EventTypeCreated,
				FromStatus: "", ToStatus: defect.Status, ActorType: "HUMAN", ActorID: actorOrNil(input.ActorID),
				Reason: reason, Evidence: datatypes.JSON("{}"), CreatedAt: now,
			}
			if err := tx.Create(&event).Error; err != nil {
				return fmt.Errorf("create confirmed defect event: %w", err)
			}
			observation.DefectID = &defect.ID
			observation.Verdict = VerdictExisted
			observation.MatchTier = MatchHuman
			observation.Confidence = 1
			observation.Reason = reason
			result = &defect
		} else if err != nil {
			return fmt.Errorf("load existing defect: %w", err)
		} else {
			defect, bindErr := bindObservationToDefect(tx, existing.ID, report.RepoID, report.TaskTypeID, &observation, reason, input.ActorID, now)
			if bindErr != nil {
				return bindErr
			}
			result = defect
		}
		if err := tx.Save(&observation).Error; err != nil {
			return fmt.Errorf("save observation: %w", err)
		}
		if err := upsertDefectAliases(tx, LedgerInput{Repo: repo, TaskType: taskType}, result.ID, group.Identity, now); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

func repositoryRoot(repo models.Repository) string {
	parsed, err := url.Parse(repo.URL)
	if err != nil {
		return ""
	}
	rawPath := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/"), ".git")
	if rawPath == "" {
		return ""
	}
	return filepath.Join(models.AppConfig.GetDataDir(), "codes", rawPath)
}

func bindObservationToDefect(tx *gorm.DB, defectID, repoID, taskTypeID uint, observation *models.DefectObservation, reason string, actorID uint, now time.Time) (*models.Defect, error) {
	var defect models.Defect
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"id = ? AND repo_id = ? AND task_type_id = ?", defectID, repoID, taskTypeID,
	).First(&defect).Error; err != nil {
		return nil, fmt.Errorf("load target defect: %w", err)
	}
	status := defect.Status
	previousStatus := defect.Status
	switch defect.Status {
	case DefectStatusHumanClosed, DefectStatusMerged:
	default:
		status = DefectStatusActive
	}
	updates := map[string]interface{}{"row_version": gorm.Expr("row_version + 1"), "updated_at": now}
	if status != defect.Status {
		updates["status"] = status
		updates["status_reason"] = reason
	}
	result := tx.Model(&models.Defect{}).Where(
		"id = ? AND repo_id = ? AND task_type_id = ?", defect.ID, defect.RepoID, defect.TaskTypeID,
	).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("bind probable observation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("target defect %d not found during governance", defect.ID)
	}
	defect.Status = status
	defect.RowVersion++
	event := models.DefectEvent{
		DefectID: defect.ID, ReportID: &observation.ReportID, EventType: EventTypeStatusChanged,
		FromStatus: previousStatus, ToStatus: status, ActorType: "HUMAN", ActorID: actorOrNil(actorID),
		Reason: reason, Evidence: datatypes.JSON("{}"), CreatedAt: now,
	}
	if err := tx.Create(&event).Error; err != nil {
		return nil, fmt.Errorf("create bind event: %w", err)
	}
	observation.DefectID = &defect.ID
	observation.Verdict = VerdictExisted
	observation.MatchTier = MatchHuman
	observation.Confidence = 1
	observation.Reason = reason
	if err := tx.Save(observation).Error; err != nil {
		return nil, fmt.Errorf("save bound observation: %w", err)
	}
	return &defect, nil
}

func CreateHumanAlias(db *gorm.DB, defectID uint, aliasValue string, input GovernanceInput) error {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return errors.New("database is not initialized")
	}
	aliasValue = normalizeAliasValue(aliasValue)
	if aliasValue == "" {
		return errors.New("alias value is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var defect models.Defect
		if err := tx.First(&defect, defectID).Error; err != nil {
			return fmt.Errorf("load defect: %w", err)
		}
		now := time.Now()
		alias := models.DefectAlias{
			DefectID: defect.ID, RepoID: defect.RepoID, TaskTypeID: defect.TaskTypeID,
			AliasType: AliasHuman, AliasValue: aliasValue, AliasClass: AliasStrong,
			FirstReportID: defect.LastSeenReportID, LastReportID: defect.LastSeenReportID, HitCount: 0,
			CreatedAt: now, UpdatedAt: now,
		}
		var existing models.DefectAlias
		err := tx.Where("repo_id = ? AND task_type_id = ? AND alias_type = ? AND alias_value = ?",
			defect.RepoID, defect.TaskTypeID, AliasHuman, aliasValue).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&alias).Error; err != nil {
				return fmt.Errorf("create human alias: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("load human alias: %w", err)
		} else if existing.DefectID != defect.ID {
			return fmt.Errorf("human alias already belongs to defect %d", existing.DefectID)
		}
		event := models.DefectEvent{
			DefectID: defect.ID, EventType: "HUMAN_DECISION", FromStatus: defect.Status, ToStatus: defect.Status,
			ActorType: "HUMAN", ActorID: actorOrNil(input.ActorID), Reason: "human alias created",
			Evidence: datatypes.JSON(fmt.Sprintf(`{"alias":"%s"}`, aliasValue)), CreatedAt: now,
		}
		return tx.Create(&event).Error
	})
}

func MergeDefects(db *gorm.DB, sourceID, targetID uint, input GovernanceInput) (*models.Defect, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	if sourceID == targetID {
		return nil, errors.New("source and target defects are the same")
	}
	var target *models.Defect
	err := db.Transaction(func(tx *gorm.DB) error {
		var source models.Defect
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&source, sourceID).Error; err != nil {
			return fmt.Errorf("load source defect: %w", err)
		}
		var loadedTarget models.Defect
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&loadedTarget, targetID).Error; err != nil {
			return fmt.Errorf("load target defect: %w", err)
		}
		if source.RepoID != loadedTarget.RepoID || source.TaskTypeID != loadedTarget.TaskTypeID {
			return errors.New("source and target defects are in different namespaces")
		}
		if loadedTarget.Status == DefectStatusMerged {
			return errors.New("target defect is already merged")
		}
		now := time.Now()
		reason := input.Reason
		if reason == "" {
			reason = "human merge decision"
		}
		result := tx.Model(&models.Defect{}).Where("id = ?", source.ID).Updates(map[string]interface{}{
			"status": DefectStatusMerged, "status_reason": reason, "merged_into_id": loadedTarget.ID,
			"row_version": gorm.Expr("row_version + 1"), "updated_at": now,
		})
		if result.Error != nil {
			return fmt.Errorf("merge defect: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("source defect %d not found during merge", source.ID)
		}
		if err := transferAliases(tx, source.ID, loadedTarget.ID, now); err != nil {
			return err
		}
		source.RowVersion++
		event := models.DefectEvent{
			DefectID: source.ID, EventType: "HUMAN_DECISION", FromStatus: source.Status, ToStatus: DefectStatusMerged,
			ActorType: "HUMAN", ActorID: actorOrNil(input.ActorID), Reason: reason,
			Evidence: datatypes.JSON(fmt.Sprintf(`{"target_defect_id":%d}`, loadedTarget.ID)), CreatedAt: now,
		}
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("create merge event: %w", err)
		}
		loadedTarget.UpdatedAt = now
		target = &loadedTarget
		return nil
	})
	return target, err
}

func SplitDefect(db *gorm.DB, defectID, reportID uint, groupUID string, input GovernanceInput) (*models.Defect, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	var created *models.Defect
	err := db.Transaction(func(tx *gorm.DB) error {
		var source models.Defect
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&source, defectID).Error; err != nil {
			return fmt.Errorf("load source defect: %w", err)
		}
		var report models.TaskReport
		if err := tx.First(&report, reportID).Error; err != nil {
			return fmt.Errorf("load report: %w", err)
		}
		var observation models.DefectObservation
		if err := tx.Where("report_id = ? AND observation_group_uid = ? AND defect_id = ?", reportID, groupUID, defectID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&observation).Error; err != nil {
			return fmt.Errorf("load split observation: %w", err)
		}
		var findings []models.AnalysisFinding
		if err := tx.Where("task_report_id = ?", reportID).Order("id").Find(&findings).Error; err != nil {
			return fmt.Errorf("load findings: %w", err)
		}
		group := findObservationGroup(BuildObservationGroups(input.RepoRoot, reportID, findings), groupUID)
		if group == nil {
			return fmt.Errorf("rebuild observation group %s", groupUID)
		}
		group.Identity.RepoID = source.RepoID
		group.Identity.TaskTypeID = source.TaskTypeID
		var repo models.Repository
		if err := tx.First(&repo, source.RepoID).Error; err != nil {
			return fmt.Errorf("load repository: %w", err)
		}
		var taskType models.TaskType
		if err := tx.First(&taskType, source.TaskTypeID).Error; err != nil {
			return fmt.Errorf("load task type: %w", err)
		}
		now := time.Now()
		scopeEntries := []models.ScanScopeEntry{}
		if err := tx.Where("report_id = ?", reportID).Find(&scopeEntries).Error; err != nil {
			return fmt.Errorf("load scope: %w", err)
		}
		defect, err := createDefect(tx, LedgerInput{Report: report, Repo: repo, TaskType: taskType, Scope: scopeEntries, CommittedAt: now}, group.Identity, now)
		if err != nil {
			return err
		}
		observation.DefectID = &defect.ID
		observation.Verdict = VerdictExisted
		observation.MatchTier = MatchHuman
		observation.Confidence = 1
		observation.Reason = "human split decision"
		if err := tx.Save(&observation).Error; err != nil {
			return fmt.Errorf("save split observation: %w", err)
		}
		if err := upsertDefectAliases(tx, LedgerInput{Repo: repo, TaskType: taskType}, defect.ID, group.Identity, now); err != nil {
			return err
		}
		events := []models.DefectEvent{
			{DefectID: defect.ID, ReportID: &reportID, EventType: EventTypeCreated, ToStatus: defect.Status, ActorType: "HUMAN", ActorID: actorOrNil(input.ActorID), Reason: "human split decision", Evidence: datatypes.JSON("{}"), CreatedAt: now},
			{DefectID: source.ID, ReportID: &reportID, EventType: "HUMAN_DECISION", FromStatus: source.Status, ToStatus: source.Status, ActorType: "HUMAN", ActorID: actorOrNil(input.ActorID), Reason: "human split decision", Evidence: datatypes.JSON(fmt.Sprintf(`{"new_defect_id":%d}`, defect.ID)), CreatedAt: now},
		}
		if err := tx.Create(&events).Error; err != nil {
			return fmt.Errorf("create split events: %w", err)
		}
		created = &defect
		return nil
	})
	return created, err
}

func transferAliases(tx *gorm.DB, sourceID, targetID uint, now time.Time) error {
	var aliases []models.DefectAlias
	if err := tx.Where("defect_id = ?", sourceID).Find(&aliases).Error; err != nil {
		return fmt.Errorf("load aliases for merge: %w", err)
	}
	for _, alias := range aliases {
		var existing models.DefectAlias
		err := tx.Where("defect_id = ? AND repo_id = ? AND task_type_id = ? AND alias_type = ? AND alias_value = ?",
			targetID, alias.RepoID, alias.TaskTypeID, alias.AliasType, alias.AliasValue).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(&models.DefectAlias{}).Where("id = ?", alias.ID).Updates(map[string]interface{}{
				"defect_id": targetID, "updated_at": now,
			}).Error; err != nil {
				return fmt.Errorf("transfer alias: %w", err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("load target alias: %w", err)
		}
		if err := tx.Model(&models.DefectAlias{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
			"hit_count": existing.HitCount + alias.HitCount, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("merge alias hit count: %w", err)
		}
		if err := tx.Delete(&models.DefectAlias{}, alias.ID).Error; err != nil {
			return fmt.Errorf("delete duplicate alias: %w", err)
		}
	}
	return nil
}

func normalizeAliasValue(value string) string {
	return strings.TrimSpace(value)
}
