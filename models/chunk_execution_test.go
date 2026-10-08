package models

import (
	"testing"
	"time"

	"code-common/backend/testdb"
)

func TestTaskChunkExecutionUniqueConstraint(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_task_chunk_execution", &TaskChunkExecution{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}

	execution := TaskChunkExecution{
		ReportID:   1,
		RepoID:     2,
		TaskTypeID: 3,
		ChunkUID:   "chunk-stable",
		Stage:      "analysis",
		Attempt:    1,
		Status:     StatusFailed,
		ErrorClass: "timeout",
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := execution
	duplicate.ID = 0
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("expected report/chunk/stage/attempt unique constraint to reject duplicate")
	}
}

func TestTaskChunkExecutionFinalAttemptIsLatest(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_task_chunk_execution_final", &TaskChunkExecution{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}

	now := time.Now()
	rows := []TaskChunkExecution{
		{ReportID: 1, RepoID: 2, TaskTypeID: 3, ChunkUID: "chunk", Stage: "analysis", Attempt: 1, AttemptKind: "invocation", Status: StatusFailed, ErrorClass: "timeout", IsFinal: false, StartedAt: now},
		{ReportID: 1, RepoID: 2, TaskTypeID: 3, ChunkUID: "chunk", Stage: "analysis", Attempt: 2, AttemptKind: "fresh_retry", Status: StatusFailed, ErrorClass: "rate_limited", IsFinal: false, StartedAt: now},
		{ReportID: 1, RepoID: 2, TaskTypeID: 3, ChunkUID: "chunk", Stage: "analysis", Attempt: 3, AttemptKind: "cross_resource_failover", Status: StatusSuccess, IsFinal: true, StartedAt: now},
	}
	for i := range rows {
		if err := UpsertChunkExecution(db, &rows[i]); err != nil {
			t.Fatal(err)
		}
	}

	var final TaskChunkExecution
	if err := db.Where("report_id = ? AND chunk_uid = ? AND stage = ? AND is_final = ?", 1, "chunk", "analysis", true).
		Order("attempt DESC").First(&final).Error; err != nil {
		t.Fatal(err)
	}
	if final.Attempt != 3 || final.Status != StatusSuccess || final.AttemptKind != "cross_resource_failover" {
		t.Fatalf("unexpected final attempt: %+v", final)
	}
}
