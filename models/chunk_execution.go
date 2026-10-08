package models

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func UpsertChunkExecution(db *gorm.DB, execution *TaskChunkExecution) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "report_id"},
			{Name: "chunk_uid"},
			{Name: "stage"},
			{Name: "attempt"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"repo_id", "task_type_id", "chunk_name", "primary_unit_id", "file_path",
			"attempt_kind", "is_final", "status", "error_class", "error_message",
			"driver", "backend", "resource_id", "model_name", "session_id",
			"prompt_hash", "artifact_hash", "artifact_path", "queue_wait_ms",
			"duration_ms", "token_usage", "started_at", "finished_at", "updated_at",
		}),
	}).Create(execution).Error
}
