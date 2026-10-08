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
