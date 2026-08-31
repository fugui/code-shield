package models

import (
	"code-common/backend/gormdb"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

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
				"ai_backend":       taskType.AIBackend,
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
