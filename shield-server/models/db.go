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
	// Auto Migrate
	err = DB.AutoMigrate(
		&User{},
		&Member{},
		&Team{},
		&Repository{},
		&ReviewReport{},
		&KeyIssue{},
		&SystemConfig{},
		&ScheduleConfig{},
		&TaskExecutionLog{},
	)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// Seed admin user if no users exist
	var count int64
	DB.Model(&User{}).Count(&count)
	if count == 0 {
		hashed, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		admin := User{
			Username: "admin",
			Password: string(hashed),
			IsAdmin:  true,
			IsActive: true,
		}
		if err := DB.Create(&admin).Error; err != nil {
			log.Printf("failed to seed admin user: %v", err)
		} else {
			log.Println("Admin user created (username: admin, password: admin123)")
		}
	}
}
