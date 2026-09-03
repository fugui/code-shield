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
		&TaskReport{},
		&KeyIssue{},
		&SystemConfig{},
		&SystemDynamicConfig{},
		&ScheduleConfig{},
		&TaskTriggerLog{},
		&TaskExecutionLog{},
		&AnalysisFinding{},
		&CampaignFinding{},
		&SysAuditLog{},
		&DefectFingerprintRecord{},
		&TaskDebateLog{},
		&RepoFeedbackRule{},
	)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// 确保任务类型 campaign_path 的部分唯一索引
	_ = DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_task_types_campaign_path_active ON task_types (campaign_path) WHERE is_campaign = true AND campaign_path != '';")

	// Seed admin user if no users exist
	seedDatabase()

	// Seed built-in task types
	seedBuiltinTaskTypes()
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
				"display_name":     taskType.DisplayName,
				"description":      taskType.Description,
				"engine_mode":      taskType.EngineMode,
				"engine_config":    taskType.EngineConfig,
				"target_scope":     taskType.TargetScope,
				"notify_template":  taskType.NotifyTemplate,
				"notify_threshold": taskType.NotifyThreshold,
				"notify_cc":        taskType.NotifyCc,
				"timeout":          taskType.Timeout,
				"is_active":        taskType.IsActive,
				"is_campaign":      taskType.IsCampaign,
				"campaign_path":    taskType.CampaignPath,
				"governance_mode":  taskType.GovernanceMode,
				"campaign_icon":    taskType.CampaignIcon,
				"campaign_config":  taskType.CampaignConfig,
				"categories":       taskType.Categories,
			}
			if err := DB.Model(&existing).Updates(updates).Error; err != nil {
				log.Printf("Error: failed to update task type %s in db: %v", taskType.Name, err)
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
	AppConfig.SyncLegacy()

	log.Println("================================================================================")
	log.Println("[Config Source] RUNTIME DYNAMIC CONFIG LOADED FROM DATABASE (SSOT)")
	log.Println("Note: Dynamic sections in config.yaml (llm, scanner, governance) are bypassed.")
	log.Println("To adjust LLM nodes, workers, or debate tiers, visit Web UI: /admin/config")
	log.Println("================================================================================")
}
