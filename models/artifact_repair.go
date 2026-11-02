package models

import (
	"errors"

	"gorm.io/gorm"
)

func CreateArtifactRepairAudit(db *gorm.DB, audit *ArtifactRepairAudit) error {
	if db == nil {
		return errors.New("database is not initialized")
	}
	return db.Create(audit).Error
}

// RecordArtifactRepairAudit 保存制品修复审计记录
func RecordArtifactRepairAudit(audit *ArtifactRepairAudit) error {
	if DB == nil || audit == nil {
		return nil
	}
	return CreateArtifactRepairAudit(DB, audit)
}

// HasDatabaseInitialized 判定全局数据库是否已初始化
func HasDatabaseInitialized() bool {
	return DB != nil
}

// SetTestDB 仅供单元测试注入与清理隔离数据库
func SetTestDB(db *gorm.DB) func() {
	prev := DB
	DB = db
	return func() {
		DB = prev
	}
}

// GetLatestArtifactRepairAudit 供测试获取最新制品修复审计
func GetLatestArtifactRepairAudit(dest *ArtifactRepairAudit) error {
	if DB == nil {
		return errors.New("database is not initialized")
	}
	return DB.Order("id DESC").First(dest).Error
}
