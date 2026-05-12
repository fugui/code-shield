package models

import (
	"log"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitDB() {
	var err error
	DB, err = gorm.Open(sqlite.Open("code_shield.db"), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	log.Println("AutoMigrating database schema (creates code_shield.db if it does not exist)...")

	// Drop old tables that are being replaced
	sqlDB, _ := DB.DB()
	sqlDB.Exec("DROP TABLE IF EXISTS review_reports")
	sqlDB.Exec("DROP TABLE IF EXISTS key_issues")
	sqlDB.Exec("DROP TABLE IF EXISTS task_execution_logs")

	// Auto Migrate
	err = DB.AutoMigrate(
		&User{},
		&Member{},
		&Team{},
		&Repository{},
		&TaskType{},
		&TaskReport{},
		&KeyIssue{},
		&AnalysisFinding{},
		&SystemConfig{},
		&ScheduleConfig{},
		&TaskExecutionLog{},
	)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// Migrate deprecated 'module' engine mode to 'chunked' (depth=1 is equivalent)
	if result := DB.Model(&TaskType{}).Where("engine_mode = ?", "module").
		Updates(map[string]interface{}{"engine_mode": "chunked", "engine_config": `{"max_files":50,"depth":1}`}); result.RowsAffected > 0 {
		log.Printf("Migrated %d task types from 'module' to 'chunked' engine", result.RowsAffected)
	}

	// Seed admin user if no users exist
	var count int64
	DB.Model(&User{}).Count(&count)
	if count == 0 {
		hashed, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		admin := User{
			Username: "admin@code-shield.com",
			Name:     "管理员",
			Password: string(hashed),
			IsAdmin:  true,
			IsActive: true,
		}
		if err := DB.Create(&admin).Error; err != nil {
			log.Printf("failed to seed admin user: %v", err)
		} else {
			log.Println("Admin user created (username: admin@code-shield.com, password: admin123)")
		}
	}

	// Seed built-in task types
	seedBuiltinTaskTypes()
}

func seedBuiltinTaskTypes() {
	builtins := []TaskType{
		{
			Name:            "code_review",
			DisplayName:     "代码检视",
			Description:     "对代码仓库进行全面的 AI 代码审查，检查多线程安全、内存泄漏、第三方库等问题",
			NotifyTemplate:  "【Code-Shield】{{.RepoName}} {{.TaskDisplayName}}报告",
			NotifyThreshold: 20,
			Timeout:         30,
			IsActive:        true,
			IsBuiltin:       true,
		},
		{
			Name:            "memory_leak",
			DisplayName:     "内存泄漏检测",
			Description:     "专项检测代码中的内存泄漏风险，包括未关闭资源、循环引用等",
			NotifyTemplate:  "【Code-Shield】{{.RepoName}} {{.TaskDisplayName}}报告",
			NotifyThreshold: 10,
			Timeout:         30,
			IsActive:        true,
			IsBuiltin:       true,
		},
	}

	for _, bt := range builtins {
		var existing TaskType
		if err := DB.Where("name = ?", bt.Name).First(&existing).Error; err != nil {
			if err := DB.Create(&bt).Error; err != nil {
				log.Printf("failed to seed task type %s: %v", bt.Name, err)
			} else {
				log.Printf("Built-in task type created: %s (%s)", bt.Name, bt.DisplayName)
			}
		}
	}
}
