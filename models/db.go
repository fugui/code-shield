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
		&TestCaseFinding{},
		&CoredumpFinding{},
		&FloatFinding{},
		&ThreadFinding{},
		&CjsonFinding{},
		&UnorderedCollectionFinding{},
		&DeepReviewFinding{},
		&AnalysisFinding{},
		&CampaignFinding{},
		&SysAuditLog{},
	)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// 自动幂等执行历史专项分表数据迁移至 campaign_findings
	if err := migrateLegacyCampaignTables(DB); err != nil {
		log.Printf("[DB] Warning: migrateLegacyCampaignTables returned error: %v", err)
	}

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

// migrateLegacyCampaignTables 自动幂等迁移历史 7 张分表数据至通用的 campaign_findings 表
func migrateLegacyCampaignTables(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// 1. 获取事务级全局排他咨询锁（事务结束自动提交/回滚时释放，防止多 Pod 并发竞态与死锁残留）
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('campaign_migration'))").Error; err != nil {
			log.Printf("[Migration] pg_advisory_xact_lock notice/warning (might not be postgres): %v", err)
		}

		type tableMigration struct {
			tableName      string
			taskTypeName   string
			campaignPath   string
			governanceMode string
			isUT           bool
		}

		migrations := []tableMigration{
			{tableName: "test_case_findings", taskTypeName: "ut_effectiveness", campaignPath: "ut", governanceMode: GovernanceModeEntityAssessment, isUT: true},
			{tableName: "coredump_findings", taskTypeName: "coredump_risk", campaignPath: "coredump", governanceMode: GovernanceModeDefectTracking, isUT: false},
			{tableName: "float_findings", taskTypeName: "float_comparison", campaignPath: "float", governanceMode: GovernanceModeDefectTracking, isUT: false},
			{tableName: "thread_findings", taskTypeName: "thread_create", campaignPath: "thread", governanceMode: GovernanceModeDefectTracking, isUT: false},
			{tableName: "cjson_findings", taskTypeName: "cjson_scan", campaignPath: "cjson", governanceMode: GovernanceModeDefectTracking, isUT: false},
			{tableName: "unordered_collection_findings", taskTypeName: "unordered_collection", campaignPath: "unordered-collection", governanceMode: GovernanceModeDefectTracking, isUT: false},
			{tableName: "deep_review_findings", taskTypeName: "deep_review", campaignPath: "deep-review", governanceMode: GovernanceModeDefectTracking, isUT: false},
		}

		for _, m := range migrations {
			// 更新/初始化 TaskType 的专项元数据
			tx.Model(&TaskType{}).Where("name = ?", m.taskTypeName).Updates(map[string]interface{}{
				"is_campaign":     true,
				"campaign_path":   m.campaignPath,
				"governance_mode": m.governanceMode,
			})

			// 检查旧表是否存在
			var exists bool
			checkSQL := "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = CURRENT_SCHEMA() AND table_name = ?)"
			if err := tx.Raw(checkSQL, m.tableName).Scan(&exists).Error; err != nil || !exists {
				continue
			}

			// 查询对应 task_type_id
			var taskType TaskType
			if err := tx.Where("name = ?", m.taskTypeName).First(&taskType).Error; err != nil {
				continue
			}

			var insertSQL string
			if m.isUT {
				insertSQL = `
INSERT INTO campaign_findings (
    task_type_id, repo_id, task_report_id, file_path, line_number,
    title, detail, severity, category, code_snippet, suggestion,
    status, assignee_id, status_log, feedback, created_at, updated_at
)
SELECT 
    ?, repo_id, task_report_id, file_path, line_number,
    test_case_name AS title, detail, severity, category, code_snippet, suggestion,
    status, assignee_id, status_log, feedback, created_at, updated_at
FROM ` + m.tableName + `
ON CONFLICT (task_type_id, repo_id, file_path, title) DO NOTHING;
`
			} else {
				insertSQL = `
INSERT INTO campaign_findings (
    task_type_id, repo_id, task_report_id, file_path, line_number,
    title, detail, severity, category, code_snippet, suggestion,
    status, assignee_id, status_log, feedback, created_at, updated_at
)
SELECT 
    ?, repo_id, task_report_id, file_path, line_number,
    title, detail, severity, category, code_snippet, suggestion,
    status, assignee_id, status_log, feedback, created_at, updated_at
FROM ` + m.tableName + `
ON CONFLICT (task_type_id, repo_id, file_path, title) DO NOTHING;
`
			}

			if err := tx.Exec(insertSQL, taskType.ID).Error; err != nil {
				log.Printf("[Migration] Error migrating table %s to campaign_findings: %v", m.tableName, err)
				return err
			}
		}

		log.Println("[Migration] Legacy campaign tables migration completed successfully.")
		return nil
	})
}

