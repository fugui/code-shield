package models

import (
	"code-common/backend/gormdb"
	"code-shield/services/engines/profile"
	"encoding/json"
	"fmt"
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
	if err := EnsureDefectSlotIndexes(DB); err != nil {
		log.Printf("[DB] EnsureDefectSlotIndexes notice: %v", err)
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
}

func seedBuiltinTaskTypes() {
	tasksDir := AppConfig.GetAbsPath("tasks")

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

		// 校验并规范化 engine_config，支持平铺格式无损自愈
		canonicalEngineConfig, _, parseErr := profile.Canonicalize(json.RawMessage(taskType.EngineConfig))
		if parseErr != nil {
			recoveredProfile, _, isRecovered, recErr := profile.ParseOrRecover(json.RawMessage(taskType.EngineConfig))
			if recErr != nil {
				log.Printf("Error: invalid engine_config in %s: %v, skipping", metaFilePath, parseErr)
				continue
			}
			if isRecovered {
				healedJSON, marshalErr := json.Marshal(profile.EngineConfig{ScanProfile: recoveredProfile})
				if marshalErr != nil {
					log.Printf("Error: failed to marshal auto-healed engine_config for %s: %v", taskType.Name, marshalErr)
					continue
				}
				taskType.EngineConfig = datatypes.JSON(healedJSON)
				log.Printf("[TaskType] Successfully auto-healed flattened engine_config for task %q from disk", taskType.Name)
				if err := SaveTaskTypeMetaFile(metaFilePath, taskType); err != nil {
					log.Printf("Warning: failed to rewrite healed meta.json for %s: %v", taskType.Name, err)
				}
			}
		} else {
			taskType.EngineConfig = datatypes.JSON(canonicalEngineConfig)
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

	// 存量 DB 任务类型规范化与平铺自愈兜底（针对可能缺失对应磁盘目录的 DB 存量数据）
	var dbTasksForHealing []TaskType
	if err := DB.Find(&dbTasksForHealing).Error; err == nil {
		for _, tt := range dbTasksForHealing {
			if _, _, parseErr := profile.Parse(json.RawMessage(tt.EngineConfig)); parseErr != nil {
				if recProfile, _, isRec, recErr := profile.ParseOrRecover(json.RawMessage(tt.EngineConfig)); recErr == nil && isRec {
					if healedJSON, marshalErr := json.Marshal(profile.EngineConfig{ScanProfile: recProfile}); marshalErr == nil {
						if err := DB.Model(&TaskType{}).Where("id = ?", tt.ID).Update("engine_config", datatypes.JSON(healedJSON)).Error; err == nil {
							log.Printf("[TaskType] Successfully auto-healed DB task type %q (id=%d) engine_config", tt.Name, tt.ID)
						}
					}
				}
			}
		}
	}
}

// migrateLegacyDynamicConfigsOnce 启动期自动无损迁移存量 dynamic config 记录
// TODO(cleanup): 存量环境升级完毕后统一清理本迁移函数
func migrateLegacyDynamicConfigsOnce(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var computeRecord SystemDynamicConfig
		hasCompute := tx.Where("category = ?", "compute").First(&computeRecord).Error == nil

		if hasCompute {
			// 若已存在 compute 权威记录，仅幂等清理可能残余的旧版 4 类配置记录
			if err := tx.Where("category IN ('llm', 'scanner', 'governance', 'notification')").Delete(&SystemDynamicConfig{}).Error; err != nil {
				log.Printf("[Migration] Warning: failed to clean up residual legacy configs: %v", err)
			}
			return nil
		}

		// 检测是否存在旧版记录 (llm 或 scanner)
		var legacyRecords []SystemDynamicConfig
		if err := tx.Where("category IN ('llm', 'scanner')").Find(&legacyRecords).Error; err != nil || len(legacyRecords) == 0 {
			// 全新部署环境，无需执行存量迁移
			return nil
		}

		log.Println("[Migration] Detecting legacy dynamic configs (llm/scanner), migrating to unified 'compute'...")

		var compute DynamicComputeConfig
		// 先使用当前全局默认值作为底座
		compute.LLM = AppConfig.LLM
		compute.Tiers = AppConfig.Scanner.Debate.Tiers
		compute.WorkerCount = AppConfig.Scanner.WorkerCount

		for _, rec := range legacyRecords {
			switch rec.Category {
			case "llm":
				var legacyLLM LLMConfig
				if err := json.Unmarshal(rec.Data, &legacyLLM); err == nil && len(legacyLLM.Resources) > 0 {
					compute.LLM = legacyLLM
				}
			case "scanner":
				var legacyScanner struct {
					WorkerCount int `json:"worker_count"`
					Debate      struct {
						Tiers DebateTiersConfig `json:"tiers"`
					} `json:"debate"`
				}
				if err := json.Unmarshal(rec.Data, &legacyScanner); err == nil {
					if legacyScanner.WorkerCount > 0 {
						compute.WorkerCount = legacyScanner.WorkerCount
					}
					if legacyScanner.Debate.Tiers.Tier1Hunter.HasConfig() ||
						legacyScanner.Debate.Tiers.Tier2Challenger.HasConfig() ||
						legacyScanner.Debate.Tiers.Tier3Judge.HasConfig() ||
						legacyScanner.Debate.Tiers.Tier4Synthesis.HasConfig() {
						compute.Tiers = legacyScanner.Debate.Tiers
					}
				}
			}
		}

		NormalizeComputeDefaults(&compute)

		rawJSON, err := json.Marshal(compute)
		if err != nil {
			return fmt.Errorf("failed to serialize migrated compute config: %w", err)
		}

		now := time.Now()
		migratedRecord := SystemDynamicConfig{
			Category:  "compute",
			Data:      datatypes.JSON(rawJSON),
			Version:   1,
			UpdatedBy: "system_migration",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.Create(&migratedRecord).Error; err != nil {
			return fmt.Errorf("failed to save migrated compute config: %w", err)
		}

		// 彻底清除历史旧记录，释放 YAML 对静态冷配置的绝对控制权
		if err := tx.Where("category IN ('llm', 'scanner', 'governance', 'notification')").Delete(&SystemDynamicConfig{}).Error; err != nil {
			return fmt.Errorf("failed to clean up legacy dynamic configs: %w", err)
		}

		log.Printf("[Migration] Successfully migrated legacy dynamic configs to category 'compute' (v1) and cleaned up legacy categories\n")
		return nil
	})
}

// InitDynamicCompute 启动期加载动态算力快照 (DB SSOT)，并执行 Seed-Once 检查
func InitDynamicCompute(seed DynamicComputeConfig) {
	if DB == nil {
		log.Println("[Config] WARNING: DB is nil, skipping dynamic compute initialization")
		return
	}

	// 1. 执行存量无损自动平滑迁移
	if err := migrateLegacyDynamicConfigsOnce(DB); err != nil {
		log.Printf("[Config] Warning: legacy dynamic config migration error: %v", err)
	}

	// 2. 查询当前唯一的 compute 动态配置记录
	var record SystemDynamicConfig
	res := DB.Where("category = ?", "compute").First(&record)
	if res.Error != nil {
		// 全新环境首次启动，执行 Seed-Once 写入
		NormalizeComputeDefaults(&seed)
		rawJSON, err := json.Marshal(seed)
		if err != nil {
			log.Printf("[Config] Failed to serialize seed compute config: %v", err)
			return
		}

		now := time.Now()
		record = SystemDynamicConfig{
			Category:  "compute",
			Data:      datatypes.JSON(rawJSON),
			Version:   1,
			UpdatedBy: "system_seed",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := DB.Create(&record).Error; err != nil {
			log.Printf("[Config] Failed to seed compute config into database: %v", err)
		} else {
			log.Println("[Config] Initialized seed compute config into database (version 1)")
			ApplyDynamicComputeConfig(seed)
		}
	} else {
		// 数据库已有 compute 记录，反序列化并原子覆盖内存中的动态算力快照
		var dbCompute DynamicComputeConfig
		if err := json.Unmarshal(record.Data, &dbCompute); err != nil {
			log.Printf("[Config] Error parsing compute config from DB: %v, falling back to seed", err)
			ApplyDynamicComputeConfig(seed)
		} else {
			NormalizeComputeDefaults(&dbCompute)
			ApplyDynamicComputeConfig(dbCompute)
			log.Printf("[Config Source] RUNTIME DYNAMIC COMPUTE LOADED FROM DATABASE (SSOT, version: %d, updated_by: %s)\n",
				record.Version, record.UpdatedBy)
		}
	}

	log.Println("================================================================================")
	log.Println("[Config Source] RUNTIME DYNAMIC COMPUTE LOADED FROM DATABASE (SSOT)")
	log.Println("Note: Static sections in config.yaml (governance, notification, scanner policies)")
	log.Println("      are 100% authoritative and effective upon service restart.")
	log.Println("To adjust LLM nodes, workers, or debate tiers, visit Web UI: /admin/config")
	log.Println("================================================================================")
}

// InitDynamicConfigs 向后兼容别名函数
func InitDynamicConfigs(seed Config) {
	InitDynamicCompute(DynamicComputeConfig{
		LLM:         seed.LLM,
		Tiers:       seed.Scanner.Debate.Tiers,
		WorkerCount: seed.Scanner.WorkerCount,
	})
}
