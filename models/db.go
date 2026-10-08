package models

import (
	"code-common/backend/gormdb"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitDB() {
	var err error

	DB, err = gormdb.Connect(AppConfig.Database, gormdb.Options{
		ServiceName: "Shield-DB",
	})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	log.Println("[DB] AutoMigrating database schema...")

	// Auto Migrate
	err = DB.AutoMigrate(
		&User{},
		&Department{},
		&Repository{},
		&TaskType{},
		&TaskTypeRevision{},
		&TaskReport{},
		&KeyIssue{},
		&SystemConfig{},
		&SystemDynamicConfig{},
		&ScheduleConfig{},
		&TaskTriggerLog{},
		&TaskExecutionLog{},
		&AnalysisFinding{},
		&ArtifactRepairAudit{},
		&ScanScopeEntry{},
		&TaskChunkExecution{},
		&Defect{},
		&DefectAlias{},
		&DefectObservation{},
		&DefectEvent{},
		&CampaignFinding{},
		&SysAuditLog{},
		&TaskDebateLog{},
		&RepoFeedbackRule{},
		&CategoryAliasUsage{},
		&CategoryRegressionSample{},
		&CategoryRegressionReview{},
		&CategoryConfusionMatrix{},
	)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}
	if err := DB.Exec("UPDATE analysis_findings SET category_status = 'LEGACY' WHERE category_status = ''").Error; err != nil {
		log.Fatalf("failed to mark legacy category status: %v", err)
	}

	// P0 removes all legacy cross-scan reconciliation storage. These tables and
	// denormalized lifecycle columns must not survive as a fallback state path.
	for _, table := range []string{
		"defect_fingerprint_records",
		"scan_reconciliations",
		"reconciliation_links",
		"defect_entities",
		"defect_entity_aliases",
		"defect_entity_relations",
		"defect_occurrences",
		"report_file_coverages",
		"defect_anchor_keys",
		"defect_entity_state_versions",
		"defect_entity_events",
		"defect_assignments",
	} {
		if err := DB.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			log.Fatalf("failed to drop legacy reconciliation table %s: %v", table, err)
		}
	}
	for _, column := range []string{"fingerprint", "diff_status", "anchor_state", "assignee_id", "assignee", "feedback", "feedback_at"} {
		if DB.Migrator().HasColumn(&AnalysisFinding{}, column) {
			if err := DB.Migrator().DropColumn(&AnalysisFinding{}, column); err != nil {
				log.Fatalf("failed to drop legacy analysis_findings column %s: %v", column, err)
			}
		}
	}
	for _, column := range []string{"new_defects_count", "existed_defects_count", "resolved_defects_count"} {
		if DB.Migrator().HasColumn(&TaskReport{}, column) {
			if err := DB.Migrator().DropColumn(&TaskReport{}, column); err != nil {
				log.Fatalf("failed to drop legacy task_reports column %s: %v", column, err)
			}
		}
	}

	if err := DB.Model(&TaskType{}).
		Where("is_campaign = ? AND governance_mode IN ?", true, []string{"", "defect_tracking"}).
		Update("governance_mode", GovernanceModeFullLedger).Error; err != nil {
		log.Fatalf("failed to normalize governance modes: %v", err)
	}

	if err := EnsureActiveTaskReportUniqueIndex(DB); err != nil {
		log.Fatalf("failed to create active task report unique index: %v", err)
	}

	if DB.Dialector.Name() == "postgres" {
		if err := ensureLedgerForeignKeys(DB); err != nil {
			log.Fatalf("failed to create ledger foreign keys: %v", err)
		}
		if err := DB.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS uq_alias_strong
			ON defect_aliases (repo_id, task_type_id, alias_type, alias_value)
			WHERE alias_class IN ('STRONG', 'HUMAN');
		`).Error; err != nil {
			log.Fatalf("failed to add strong defect alias constraint: %v", err)
		}
		if err := DB.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS uq_alias_bucket
			ON defect_aliases (repo_id, task_type_id, alias_type, alias_value, defect_id)
			WHERE alias_class = 'BUCKET';
		`).Error; err != nil {
			log.Fatalf("failed to add bucket defect alias constraint: %v", err)
		}

		// 会话级事务超时保护（08号设计 §4.4）
		DB.Exec("SET idle_in_transaction_session_timeout = '10000';") // 10秒空闲事务自动断开
		DB.Exec("SET statement_timeout = '60000';")                   // 60秒 SQL 语句兜底超时
		DB.Exec("SET lock_timeout = '5000';")                         // 5秒行锁等待超时
	}

	// The task queue relies on repository URLs. Persist the invariant in the
	// database as well so out-of-process imports cannot insert blank URLs.
	if err := DB.Exec(`
		ALTER TABLE repositories
		DROP CONSTRAINT IF EXISTS chk_repositories_url_not_blank
	`).Error; err != nil {
		log.Fatalf("failed to prepare repositories URL constraint: %v", err)
	}
	if err := DB.Exec(`
		ALTER TABLE repositories
		ADD CONSTRAINT chk_repositories_url_not_blank
		CHECK (url IS NOT NULL AND btrim(url) <> '')
	`).Error; err != nil {
		log.Fatalf("failed to add repositories URL constraint: %v", err)
	}

	// 确保任务类型 campaign_path 的部分唯一索引
	_ = DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_task_types_campaign_path_active ON task_types (campaign_path) WHERE is_campaign = true AND campaign_path != '';")

	// Seed admin user if no users exist
	seedDatabase()

	// Seed built-in task types
	seedBuiltinTaskTypes()
}

// EnsureActiveTaskReportUniqueIndex enforces at most one live report per scan identity.
func EnsureActiveTaskReportUniqueIndex(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uq_task_reports_active_repo_type
		ON task_reports (repo_id, task_type_id)
		WHERE status NOT IN ('success', 'degraded', 'failed', 'skipped');
	`).Error
}

func seedDatabase() {
	var count int64
	DB.Model(&User{}).Where("email = ?", "admin@code-shield.com").Count(&count)
	if count == 0 {
		hashed, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		admin := User{
			EmployeeID: "admin",
			Email:      "admin@code-shield.com",
			Name:       "管理员",
			Password:   string(hashed),
			Roles:      datatypes.JSON([]byte("[\"super_admin\"]")),
			IsActive:   true,
			IsAdmin:    true,
			RegMethod:  "local",
		}
		if err := DB.Create(&admin).Error; err != nil {
			log.Printf("failed to seed admin user: %v", err)
		} else {
			log.Println("Admin user created (email: admin@code-shield.com, password: admin123)")
		}
	}

	// Seed built-in task types
	seedBuiltinTaskTypes()
}

func seedBuiltinTaskTypes() {
	tasksDir := "tasks"

	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		log.Printf("Warning: failed to read tasks directory: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()
		metaFilePath := filepath.Join(tasksDir, dirName, "meta.json")

		if _, err := os.Stat(metaFilePath); os.IsNotExist(err) {
			continue
		}

		metaBytes, err := os.ReadFile(metaFilePath)
		if err != nil {
			log.Printf("Error: failed to read %s: %v", metaFilePath, err)
			continue
		}

		var taskType TaskType
		if err := json.Unmarshal(metaBytes, &taskType); err != nil {
			log.Printf("Error: failed to parse %s: %v", metaFilePath, err)
			continue
		}
		if taxonomy := taskType.GetCategoryTaxonomy(); taxonomy != nil {
			if taskType.TaxonomySchemaVersion == 0 {
				taskType.TaxonomySchemaVersion = taxonomy.SchemaVersion
			}
			taskType.TaxonomyHash = HashCategoryTaxonomy(taxonomy)
		}

		expectedDir := strings.ReplaceAll(taskType.Name, "_", "-")
		if dirName != expectedDir {
			log.Printf("Error: task name %q does not match its directory name %q (expected %q)",
				taskType.Name, dirName, expectedDir)
			continue
		}

		var existing TaskType
		if err := DB.Where("name = ?", taskType.Name).First(&existing).Error; err != nil {
			if err := DB.Create(&taskType).Error; err != nil {
				log.Printf("Error: failed to create task type %s in db: %v", taskType.Name, err)
			} else {
				log.Printf("Successfully loaded new task type from disk: %s (%s)", taskType.Name, taskType.DisplayName)
			}
		} else {
			updates := map[string]interface{}{
				"display_name":            taskType.DisplayName,
				"description":             taskType.Description,
				"engine_mode":             taskType.EngineMode,
				"engine_config":           taskType.EngineConfig,
				"assessment_config":       taskType.AssessmentConfig,
				"assessment_config_hash":  HashAssessmentConfig(taskType.AssessmentConfig),
				"current_revision_id":     nil,
				"target_scope":            taskType.TargetScope,
				"notify_template":         taskType.NotifyTemplate,
				"notify_threshold":        taskType.NotifyThreshold,
				"notify_cc":               taskType.NotifyCc,
				"timeout":                 taskType.Timeout,
				"is_active":               taskType.IsActive,
				"is_campaign":             taskType.IsCampaign,
				"campaign_path":           taskType.CampaignPath,
				"governance_mode":         taskType.GovernanceMode,
				"campaign_icon":           taskType.CampaignIcon,
				"campaign_config":         taskType.CampaignConfig,
				"categories":              taskType.Categories,
				"taxonomy":                taskType.Taxonomy,
				"taxonomy_schema_version": taskType.TaxonomySchemaVersion,
				"taxonomy_hash":           taskType.TaxonomyHash,
				"domain_family":           taskType.DomainFamily,
				"domain_label":            taskType.DomainLabel,
				"target_semantics":        taskType.TargetSemantics,
				"display_semantics":       taskType.DisplaySemantics,
			}
			if len(taskType.DefenseDimensions) > 0 {
				updates["defense_dimensions"] = taskType.DefenseDimensions
			}

			if err := DB.Model(&existing).Updates(updates).Error; err != nil {
				log.Printf("Error: failed to update task type %s in db: %v", taskType.Name, err)
			} else {
				log.Printf("Synced task type from disk: %s (%s)", taskType.Name, taskType.DisplayName)
			}
		}
	}

	// Clean up orphan tasks that exist in database but are missing on disk
	var dbTasks []TaskType
	if err := DB.Find(&dbTasks).Error; err == nil {
		for _, dbTask := range dbTasks {
			expectedDir := strings.ReplaceAll(dbTask.Name, "_", "-")
			dirPath := filepath.Join(tasksDir, expectedDir)

			if _, err := os.Stat(dirPath); os.IsNotExist(err) {
				var reportCount int64
				DB.Model(&TaskReport{}).Where("task_type_id = ?", dbTask.ID).Count(&reportCount)

				if reportCount == 0 {
					// Safe to physically delete if there are no historical execution reports
					DB.Unscoped().Where("task_type_id = ?", dbTask.ID).Delete(&TaskTypeRevision{})
					if err := DB.Unscoped().Delete(&dbTask).Error; err == nil {
						log.Printf("Orphan Cleanup: Successfully deleted unused task type %q from database", dbTask.Name)
					}
				} else if dbTask.IsActive {
					// Deactivate if there are execution reports to preserve GORM foreign keys
					dbTask.IsActive = false
					if err := DB.Model(&dbTask).Update("is_active", false).Error; err == nil {
						log.Printf("Orphan Cleanup: Successfully deactivated task type %q in database", dbTask.Name)
					}
				}
			}
		}
	}
}

// InitDynamicConfigs 检查并按 Category 执行 Seed-Once 导入，并将数据库持久化配置作为运行时 SSOT
func InitDynamicConfigs(seed Config) {
	if DB == nil {
		log.Println("[Config] WARNING: DB is nil, skipping dynamic config initialization")
		return
	}

	categories := []string{"llm", "scanner", "governance", "notification"}
	for _, cat := range categories {
		var record SystemDynamicConfig
		res := DB.Where("category = ?", cat).First(&record)
		if res.Error != nil {
			// 首次启动，未找到记录，执行 Seed-Once 写入
			var rawJSON []byte
			var err error
			switch cat {
			case "llm":
				rawJSON, err = json.Marshal(seed.LLM)
			case "scanner":
				rawJSON, err = json.Marshal(seed.Scanner)
			case "governance":
				rawJSON, err = json.Marshal(seed.Governance)
			case "notification":
				rawJSON, err = json.Marshal(seed.Notification)
			}
			if err != nil {
				log.Printf("[Config] Failed to serialize seed config for category %s: %v", cat, err)
				continue
			}

			record = SystemDynamicConfig{
				Category:  cat,
				Data:      datatypes.JSON(rawJSON),
				Version:   1,
				UpdatedBy: "system_seed",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			if err := DB.Create(&record).Error; err != nil {
				log.Printf("[Config] Failed to seed dynamic config for category %s: %v", cat, err)
			} else {
				log.Printf("[Config] Initialized seed config for category %s into database", cat)
			}
		} else {
			// 已存在记录，以数据库持久化数据为准反序列化覆盖至全局 AppConfig
			switch cat {
			case "llm":
				_ = json.Unmarshal(record.Data, &AppConfig.LLM)
			case "scanner":
				_ = json.Unmarshal(record.Data, &AppConfig.Scanner)
			case "governance":
				_ = json.Unmarshal(record.Data, &AppConfig.Governance)
			case "notification":
				_ = json.Unmarshal(record.Data, &AppConfig.Notification)
			}
		}
	}

	// 同步回旧字段，保证全系统向前与向后兼容
	AppConfig.Identity = AppConfig.Governance.Identity
	AppConfig.Arbitration = AppConfig.Governance.Arbitration
	AppConfig.Lifecycle = AppConfig.Governance.Lifecycle
	AppConfig.SyncLegacy()
	AppConfig.Scanner.NormalizeDefaults()

	log.Println("================================================================================")
	log.Println("[Config Source] RUNTIME DYNAMIC CONFIG LOADED FROM DATABASE (SSOT)")
	log.Println("Note: Dynamic sections in config.yaml (llm, scanner, governance) are bypassed.")
	log.Println("To adjust LLM nodes, workers, or debate tiers, visit Web UI: /admin/config")
	log.Println("================================================================================")
}
