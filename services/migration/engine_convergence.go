package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"code-shield/models"
	"code-shield/services/engines/profile"

	"gorm.io/gorm"
)

const migrationPlannerVersion = "v1"

func RunEngineConvergence(db *gorm.DB, tasksRoot string) error {
	if db == nil {
		return fmt.Errorf("database is required for engine convergence migration")
	}
	if err := migrateDiskTaskTypes(tasksRoot); err != nil {
		return fmt.Errorf("migrate disk task types: %w", err)
	}
	if err := migrateDatabaseTaskTypes(db, tasksRoot); err != nil {
		return fmt.Errorf("migrate database task types: %w", err)
	}
	if err := backfillReportSnapshots(db, tasksRoot); err != nil {
		return fmt.Errorf("backfill report execution snapshots: %w", err)
	}
	if err := markLegacyReportSnapshots(db); err != nil {
		return fmt.Errorf("mark legacy report execution snapshots: %w", err)
	}
	if err := backfillAssessmentOutcomes(db); err != nil {
		return fmt.Errorf("backfill assessment outcomes: %w", err)
	}
	if err := BackfillCategorySemantics(db); err != nil {
		return fmt.Errorf("backfill category semantics: %w", err)
	}
	return verifyConvergence(db, tasksRoot)
}

func BackfillCategorySemantics(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required for category semantic backfill")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.AnalysisFinding{}).
			Where("category_code = '' AND category_status = ''").
			Update("category_status", "LEGACY").Error; err != nil {
			return err
		}
		if err := tx.Model(&models.AnalysisFinding{}).
			Where("category_code <> '' AND category_status = ''").
			Update("category_status", "VALID").Error; err != nil {
			return err
		}
		return nil
	})
}

func markLegacyReportSnapshots(db *gorm.DB) error {
	return db.Model(&models.TaskReport{}).
		Where("execution_snapshot_state NOT IN ? AND status IN ?",
			[]string{"complete", "legacy_readonly"}, models.TerminalTaskStatuses()).
		Update("execution_snapshot_state", "legacy_readonly").Error
}

func backfillReportSnapshots(db *gorm.DB, tasksRoot string) error {
	var taskTypes []models.TaskType
	if err := db.Find(&taskTypes).Error; err != nil {
		return err
	}
	for _, taskType := range taskTypes {
		configRaw, profileRaw, profileHash, err := taskTypeExecutionSnapshot(taskType, tasksRoot)
		if err != nil {
			return fmt.Errorf("task type %s: %w", taskType.Name, err)
		}
		if err := db.Model(&models.TaskReport{}).
			Where("task_type_id = ? AND engine_mode = ''", taskType.ID).
			Updates(map[string]any{
				"engine_mode":        taskType.EngineMode,
				"scan_profile":       profileRaw,
				"scan_profile_hash":  profileHash,
				"prompt_version":     "v1",
				"engine_config_hash": configHash(configRaw),
				"planner_version":    migrationPlannerVersion,
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateDiskTaskTypes(tasksRoot string) error {
	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		return fmt.Errorf("read task types: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		metaPath := filepath.Join(tasksRoot, entry.Name(), "meta.json")
		raw, err := os.ReadFile(metaPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(raw, &meta); err != nil {
			return fmt.Errorf("%s: parse meta: %w", entry.Name(), err)
		}
		configRaw, err := migrateTaskMeta(raw, entry.Name())
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if _, _, err := profile.Parse(configRaw); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		meta["engine_mode"] = json.RawMessage(`"debate_full"`)
		meta["engine_config"] = configRaw
		output, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(metaPath, output, 0644); err != nil {
			return err
		}
		log.Printf("[Migration] Converged task type %s", entry.Name())
	}
	return nil
}

func migrateDatabaseTaskTypes(db *gorm.DB, tasksRoot string) error {
	diskConfigs, err := diskTaskTypeConfigs(tasksRoot)
	if err != nil {
		return err
	}
	diskTaxonomies, err := diskTaskTypeTaxonomies(tasksRoot)
	if err != nil {
		return err
	}
	var taskTypes []models.TaskType
	if err := db.Find(&taskTypes).Error; err != nil {
		return err
	}
	for _, taskType := range taskTypes {
		configRaw, ok := diskConfigs[taskType.Name]
		if !ok {
			configRaw, err = migrateDatabaseOnlyTaskType(taskType)
			if err != nil {
				return fmt.Errorf("task type %s: %w", taskType.Name, err)
			}
		}
		if err := db.Model(&models.TaskType{}).Where("id = ?", taskType.ID).Updates(map[string]any{
			"engine_mode":            "debate_full",
			"engine_config":          configRaw,
			"taxonomy":               diskTaxonomies[taskType.Name],
			"assessment_config":      taskType.AssessmentConfig,
			"assessment_config_hash": models.HashAssessmentConfig(taskType.AssessmentConfig),
			"current_revision_id":    nil,
		}).Error; err != nil {
			return fmt.Errorf("update task type %s: %w", taskType.Name, err)
		}
	}
	return nil
}

func diskTaskTypeTaxonomies(tasksRoot string) (map[string]json.RawMessage, error) {
	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		return nil, fmt.Errorf("read task types: %w", err)
	}
	taxonomies := make(map[string]json.RawMessage)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		metaPath := filepath.Join(tasksRoot, entry.Name(), "meta.json")
		raw, err := os.ReadFile(metaPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var taskType models.TaskType
		if err := json.Unmarshal(raw, &taskType); err != nil {
			return nil, fmt.Errorf("parse %s: %w", metaPath, err)
		}
		taxonomies[taskType.Name] = json.RawMessage(taskType.Taxonomy)
	}
	return taxonomies, nil
}

func diskTaskTypeConfigs(tasksRoot string) (map[string]json.RawMessage, error) {
	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		return nil, err
	}
	configs := make(map[string]json.RawMessage)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		metaPath := filepath.Join(tasksRoot, entry.Name(), "meta.json")
		raw, err := os.ReadFile(metaPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var taskType models.TaskType
		if err := json.Unmarshal(raw, &taskType); err != nil {
			return nil, fmt.Errorf("parse %s: %w", metaPath, err)
		}
		configs[taskType.Name] = json.RawMessage(taskType.EngineConfig)
	}
	return configs, nil
}

func migrateDatabaseOnlyTaskType(taskType models.TaskType) (json.RawMessage, error) {
	configRaw := json.RawMessage(taskType.EngineConfig)
	if len(configRaw) == 0 {
		configRaw = json.RawMessage(`{}`)
	}
	if taskType.EngineMode == "debate_full" {
		if _, _, err := profile.Parse(configRaw); err == nil {
			return configRaw, nil
		}
	}
	wrapper := fmt.Sprintf(
		`{"name":%q,"engine_mode":%q,"engine_config":%s,"target_scope":%q,"governance_mode":%q}`,
		taskType.Name, taskType.EngineMode, string(configRaw), taskType.TargetScope, taskType.GovernanceMode,
	)
	return migrateTaskMeta([]byte(wrapper), taskType.Name)
}

func verifyConvergence(db *gorm.DB, tasksRoot string) error {
	var taskTypes []models.TaskType
	if err := db.Find(&taskTypes).Error; err != nil {
		return err
	}
	for _, taskType := range taskTypes {
		if taskType.EngineMode != "debate_full" {
			return fmt.Errorf("task type %s uses engine mode %q", taskType.Name, taskType.EngineMode)
		}
		if _, _, err := profile.Parse(json.RawMessage(taskType.EngineConfig)); err != nil {
			return fmt.Errorf("task type %s: %w", taskType.Name, err)
		}
	}
	var count int64
	if err := db.Model(&models.TaskReport{}).Where("engine_mode = ''").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%d reports still have an empty execution snapshot", count)
	}
	return nil
}

func taskTypeExecutionSnapshot(taskType models.TaskType, tasksRoot string) (json.RawMessage, json.RawMessage, string, error) {
	configRaw := json.RawMessage(taskType.EngineConfig)
	if len(configRaw) == 0 {
		dirName := strings.ReplaceAll(taskType.Name, "_", "-")
		raw, err := os.ReadFile(filepath.Join(tasksRoot, dirName, "meta.json"))
		if err != nil {
			return nil, nil, "", fmt.Errorf("read task meta: %w", err)
		}
		var diskTaskType models.TaskType
		if err := json.Unmarshal(raw, &diskTaskType); err != nil {
			return nil, nil, "", err
		}
		configRaw = json.RawMessage(diskTaskType.EngineConfig)
	}
	legacyConfig := configRaw
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(configRaw, &legacy); err == nil {
		if _, exists := legacy["scan_profile"]; !exists {
			migrated, err := migrateTaskMeta([]byte(fmt.Sprintf(`{"name":%q,"engine_mode":%q,"engine_config":%s,"target_scope":%q,"governance_mode":%q}`,
				taskType.Name, taskType.EngineMode, string(configRaw), taskType.TargetScope, taskType.GovernanceMode)), taskType.Name)
			if err != nil {
				return nil, nil, "", err
			}
			configRaw = migrated
		}
	}
	if _, _, err := profile.Parse(json.RawMessage(configRaw)); err != nil {
		return nil, nil, "", err
	}
	var parsedConfig struct {
		ScanProfile profile.ScanProfile `json:"scan_profile"`
	}
	if err := json.Unmarshal(configRaw, &parsedConfig); err != nil {
		return nil, nil, "", err
	}
	profileRaw, err := json.Marshal(parsedConfig.ScanProfile)
	if err != nil {
		return nil, nil, "", err
	}
	parsedProfile, profileHash, err := profile.Parse(json.RawMessage(configRaw))
	if err != nil {
		return nil, nil, "", err
	}
	_ = parsedProfile
	return json.RawMessage(legacyConfig), profileRaw, profileHash, nil
}

func migrateTaskMeta(raw []byte, taskName string) (json.RawMessage, error) {
	var taskType struct {
		Name             string          `json:"name"`
		EngineMode       string          `json:"engine_mode"`
		EngineConfig     json.RawMessage `json:"engine_config"`
		AssessmentConfig json.RawMessage `json:"assessment_config"`
		TargetScope      string          `json:"target_scope"`
		GovernanceMode   string          `json:"governance_mode"`
		EngineConfigNew  json.RawMessage `json:"-"`
	}
	if err := json.Unmarshal(raw, &taskType); err != nil {
		return nil, err
	}
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(taskType.EngineConfig, &legacy); err != nil {
		return nil, fmt.Errorf("invalid engine_config: %w", err)
	}
	if _, exists := legacy["scan_profile"]; exists {
		var scanProfile map[string]json.RawMessage
		if err := json.Unmarshal(taskType.EngineConfig, &scanProfile); err != nil {
			return nil, fmt.Errorf("invalid engine_config: %w", err)
		}
		var profileValue map[string]json.RawMessage
		if err := json.Unmarshal(scanProfile["scan_profile"], &profileValue); err != nil {
			return nil, fmt.Errorf("invalid scan_profile: %w", err)
		}
		var profileName string
		if err := json.Unmarshal(profileValue["name"], &profileName); err == nil {
			if taskType.Name == "change_review" && profileName == profile.NameFullReview {
				var scanProfile profile.ScanProfile
				if raw, err := json.Marshal(profileValue); err != nil {
					return nil, fmt.Errorf("encode scan_profile: %w", err)
				} else if err := json.Unmarshal(raw, &scanProfile); err != nil {
					return nil, fmt.Errorf("decode scan_profile: %w", err)
				}
				scanProfile = changeReviewProfile(scanProfile)
				encoded, err := json.Marshal(scanProfile)
				if err != nil {
					return nil, fmt.Errorf("encode scan_profile: %w", err)
				}
				var activated map[string]json.RawMessage
				if err := json.Unmarshal(encoded, &activated); err != nil {
					return nil, fmt.Errorf("encode scan_profile: %w", err)
				}
				profileValue = activated
			}
			switch profileName {
			case profile.NameKeywordReview:
				profileValue["name"] = json.RawMessage(`"occurrence_review"`)
			case profile.NameTestEntityReview:
				profileValue["name"] = json.RawMessage(`"entity_review"`)
				if _, exists := profileValue["entity_kind"]; !exists {
					profileValue["entity_kind"] = json.RawMessage(`"test_case"`)
				}
			}
			encoded, err := json.Marshal(profileValue)
			if err != nil {
				return nil, fmt.Errorf("encode scan_profile: %w", err)
			}
			scanProfile["scan_profile"] = encoded
			return json.Marshal(scanProfile)
		}
		return taskType.EngineConfig, nil
	}

	scanProfile := map[string]any{"version": 1}
	targetScope := taskType.TargetScope
	if targetScope != "" {
		scanProfile["target_scope"] = targetScope
	}
	if rawValue := legacy["exclude_paths"]; rawValue != nil {
		scanProfile["exclude_paths"] = rawValue
	}
	if rawValue := legacy["max_files"]; rawValue != nil {
		scanProfile["max_files"] = rawValue
	}
	if rawValue := legacy["depth"]; rawValue != nil {
		scanProfile["depth"] = rawValue
	}
	if rawValue := legacy["file_extensions"]; rawValue != nil {
		scanProfile["include_extensions"] = rawValue
	}

	switch {
	case taskType.Name == "change_review":
		scanProfile = changeReviewProfileValues(scanProfile)
	case taskType.GovernanceMode == models.GovernanceModeEntityAssessment && targetScope == "test":
		scanProfile["name"] = profile.NameEntityReview
		scanProfile["entity_kind"] = "test_case"
		delete(scanProfile, "include_extensions")
		if languages := languagesFromExtensions(legacy["file_extensions"]); len(languages) > 0 {
			scanProfile["languages"] = languages
		}
	case legacy["content_keywords"] != nil:
		scanProfile["name"] = profile.NameOccurrenceReview
		scanProfile["content_keywords"] = legacy["content_keywords"]
		if taskType.AssessmentConfig != nil {
			var assessmentConfig struct {
				Domain string `json:"domain"`
			}
			if err := json.Unmarshal(taskType.AssessmentConfig, &assessmentConfig); err == nil && assessmentConfig.Domain == "thread_creation" {
				scanProfile["primary_unit"] = profile.PrimaryUnitKeywordOccurrence
			}
		}
	default:
		scanProfile["name"] = profile.NameFullReview
	}

	config := map[string]any{"scan_profile": scanProfile}
	return json.Marshal(config)
}

func changeReviewProfile(scanProfile profile.ScanProfile) profile.ScanProfile {
	scanProfile.Version = 1
	scanProfile.Name = profile.NameChangeReview
	scanProfile.Languages = []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava}
	scanProfile.ScopePolicy = "changed_hunks"
	scanProfile.ContextPolicy = "changed_files"
	scanProfile.BasePolicy = &profile.BasePolicy{Strategy: "since_days", SinceDays: 7}
	scanProfile.TargetScope = ""
	return scanProfile
}

func changeReviewProfileValues(scanProfile map[string]any) map[string]any {
	scanProfile["version"] = 1
	scanProfile["name"] = profile.NameChangeReview
	scanProfile["languages"] = []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava}
	scanProfile["scope_policy"] = "changed_hunks"
	scanProfile["context_policy"] = "changed_files"
	scanProfile["base_policy"] = profile.BasePolicy{Strategy: "since_days", SinceDays: 7}
	delete(scanProfile, "target_scope")
	return scanProfile
}

func languagesFromExtensions(raw json.RawMessage) []string {
	var extensions []string
	if err := json.Unmarshal(raw, &extensions); err != nil {
		return nil
	}
	seen := map[string]bool{}
	languages := make([]string, 0)
	for _, extension := range extensions {
		language := ""
		switch strings.ToLower(extension) {
		case ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".hxx":
			language = profile.LanguageCPP
		case ".py":
			language = profile.LanguagePython
		case ".java":
			language = profile.LanguageJava
		case ".go":
			language = profile.LanguageGo
		}
		if language != "" && !seen[language] {
			seen[language] = true
			languages = append(languages, language)
		}
	}
	sort.Strings(languages)
	return languages
}

func configHash(raw json.RawMessage) string {
	var compact any
	if err := json.Unmarshal(raw, &compact); err != nil {
		sum := sha256.Sum256(raw)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	normalized, err := json.Marshal(compact)
	if err != nil {
		sum := sha256.Sum256(raw)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(normalized)
	return "sha256:" + hex.EncodeToString(sum[:])
}
