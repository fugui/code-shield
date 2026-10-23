package defectlifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-shield/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.TaskReport{},
		&models.Repository{},
		&models.TaskType{},
		&models.AnalysisFinding{},
		&models.ScanScopeEntry{},
		&models.Defect{},
		&models.DefectAlias{},
		&models.DefectObservation{},
		&models.DefectEvent{},
		&models.ArtifactRepairAudit{},
		&models.TaskDebateLog{},
		&models.CampaignFinding{},
	); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return db
}

func TestPurgeExpiredScanSnapshots(t *testing.T) {
	db := setupTestDB(t)

	repo := models.Repository{Name: "retention-demo", URL: "https://example.com/retention.git"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repo: %v", err)
	}
	taskType := models.TaskType{Name: "sec-scan", DisplayName: "Security Scan"}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}

	now := time.Now()
	// 创建 15 轮成功 TaskReport：
	// Report 1~5: 45 天前（超期，且在保底 10 轮之外，应该被清理）
	// Report 6~12: 40 天前（超期，但是在最新 10 轮之内，应该被保留！）
	// Report 13~15: 1 天前（未超期，在最新 10 轮之内，保留）
	for i := 1; i <= 15; i++ {
		createdAt := now.Add(-1 * time.Duration(24*time.Hour))
		if i <= 5 {
			createdAt = now.Add(-45 * 24 * time.Hour)
		} else if i <= 12 {
			createdAt = now.Add(-40 * 24 * time.Hour)
		}
		r := models.TaskReport{
			ID:         uint(i),
			RepoID:     repo.ID,
			TaskTypeID: taskType.ID,
			Status:     "success",
			CreatedAt:  createdAt,
		}
		if err := db.Create(&r).Error; err != nil {
			t.Fatalf("create report %d: %v", i, err)
		}

		// 为每轮创建测试数据
		entry := models.ScanScopeEntry{
			ReportID: r.ID,
			NormPath: fmt.Sprintf("src/file_%d.go", i),
			Outcome:  "SCANNED",
		}
		db.Create(&entry)

		obs := models.DefectObservation{
			ReportID:            r.ID,
			RepoID:              repo.ID,
			TaskTypeID:          taskType.ID,
			ObservationGroupUID: fmt.Sprintf("grp_%d", i),
			Verdict:             "EXISTED",
			MatchTier:           "EXACT",
		}
		db.Create(&obs)

		finding := models.AnalysisFinding{
			TaskReportID: r.ID,
			FilePath:     fmt.Sprintf("src/file_%d.go", i),
			Title:        fmt.Sprintf("Finding %d", i),
		}
		db.Create(&finding)

		sysEvent := models.DefectEvent{
			ReportID:  &r.ID,
			ActorType: "SYSTEM",
			EventType: "STATUS_CHANGED",
		}
		db.Create(&sysEvent)

		humanEvent := models.DefectEvent{
			ReportID:  &r.ID,
			ActorType: "HUMAN",
			EventType: "CONFIRMED",
		}
		db.Create(&humanEvent)

		repairAudit := models.ArtifactRepairAudit{
			ReportID: r.ID,
			RepoID:   repo.ID,
			BundleID: fmt.Sprintf("bundle_%d", i),
		}
		db.Create(&repairAudit)

		debateLog := models.TaskDebateLog{
			TaskReportID: r.ID,
			ChunkName:    fmt.Sprintf("chunk_%d", i),
			JudgeOutput:  []byte(`{}`),
			Verdict:      "CONFIRMED",
			CreatedAt:    createdAt,
		}
		db.Create(&debateLog)

		campFinding := models.CampaignFinding{
			TaskReportID: r.ID,
			TaskTypeID:   taskType.ID,
			RepoID:       repo.ID,
			FilePath:     fmt.Sprintf("src/file_%d.go", i),
			Title:        fmt.Sprintf("Camp %d", i),
			CreatedAt:    createdAt,
		}
		db.Create(&campFinding)
	}

	// 额外创建一轮 10 天前的 failed 任务（Report 16），测试非成功任务超期（默认 7 天）清理
	failedReport := models.TaskReport{
		ID:         16,
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "failed",
		CreatedAt:  now.Add(-10 * 24 * time.Hour),
	}
	db.Create(&failedReport)
	db.Create(&models.ScanScopeEntry{ReportID: failedReport.ID, NormPath: "src/failed.go"})
	db.Create(&models.ArtifactRepairAudit{ReportID: failedReport.ID, RepoID: repo.ID})
	db.Create(&models.TaskDebateLog{TaskReportID: failedReport.ID, ChunkName: "failed_chunk", JudgeOutput: []byte(`{}`)})

	// 执行清理：retentionDays=30, maxRetained=10, nonSuccessDays=7
	purged, err := PurgeExpiredScanSnapshots(db, 30, 10, 7)
	if err != nil {
		t.Fatalf("PurgeExpiredScanSnapshots failed: %v", err)
	}

	// 预期被清理的报告数：
	// 15 轮成功报告中清理超期且在 Top 10 之外的 5 轮（Report 1~5）
	// 加 1 轮超过 7 天的 failed 报告（Report 16），共计 6 轮
	if purged != 6 {
		t.Fatalf("expected 6 reports purged, got %d", purged)
	}

	// 验证 Report 1~5 的所有关联快照数据已被物理删除
	for i := 1; i <= 5; i++ {
		var scopeCount, obsCount, findCount, auditCount, debateCount, campCount int64
		db.Model(&models.ScanScopeEntry{}).Where("report_id = ?", i).Count(&scopeCount)
		db.Model(&models.DefectObservation{}).Where("report_id = ?", i).Count(&obsCount)
		db.Model(&models.AnalysisFinding{}).Where("task_report_id = ?", i).Count(&findCount)
		db.Model(&models.ArtifactRepairAudit{}).Where("report_id = ?", i).Count(&auditCount)
		db.Model(&models.TaskDebateLog{}).Where("task_report_id = ?", i).Count(&debateCount)
		db.Model(&models.CampaignFinding{}).Where("task_report_id = ?", i).Count(&campCount)

		if scopeCount != 0 || obsCount != 0 || findCount != 0 || auditCount != 0 || debateCount != 0 || campCount != 0 {
			t.Fatalf("expected snapshot for report %d to be deleted, got scope=%d, obs=%d, finding=%d, audit=%d, debate=%d, camp=%d",
				i, scopeCount, obsCount, findCount, auditCount, debateCount, campCount)
		}

		// 验证系统事件被清理，而 HUMAN 审计事件被保留
		var sysCount, humanCount int64
		db.Model(&models.DefectEvent{}).Where("report_id = ? AND actor_type = 'SYSTEM'", i).Count(&sysCount)
		db.Model(&models.DefectEvent{}).Where("report_id = ? AND actor_type = 'HUMAN'", i).Count(&humanCount)
		if sysCount != 0 {
			t.Fatalf("expected SYSTEM events for report %d to be deleted, got %d", i, sysCount)
		}
		if humanCount != 1 {
			t.Fatalf("expected HUMAN event for report %d to be preserved, got %d", i, humanCount)
		}
	}

	// 验证 Report 16 (failed) 的关联快照已被物理删除
	var failedScopeCount, failedAuditCount, failedDebateCount int64
	db.Model(&models.ScanScopeEntry{}).Where("report_id = ?", 16).Count(&failedScopeCount)
	db.Model(&models.ArtifactRepairAudit{}).Where("report_id = ?", 16).Count(&failedAuditCount)
	db.Model(&models.TaskDebateLog{}).Where("task_report_id = ?", 16).Count(&failedDebateCount)
	if failedScopeCount != 0 || failedAuditCount != 0 || failedDebateCount != 0 {
		t.Fatalf("expected failed report snapshots to be deleted, got scope=%d, audit=%d, debate=%d",
			failedScopeCount, failedAuditCount, failedDebateCount)
	}

	// 验证保底范围内的 Report 6~15（虽然部分超过 30 天，但在最新 10 轮基线内）快照完好无损
	for i := 6; i <= 15; i++ {
		var scopeCount int64
		db.Model(&models.ScanScopeEntry{}).Where("report_id = ?", i).Count(&scopeCount)
		if scopeCount != 1 {
			t.Fatalf("expected snapshot for report %d to be preserved, got count=%d", i, scopeCount)
		}
	}
}

func TestPurgeExpiredDebateLogs(t *testing.T) {
	db := setupTestDB(t)

	now := time.Now()
	// 插入 3 条 debate log：45 天前、35 天前、5 天前
	logs := []models.TaskDebateLog{
		{TaskReportID: 101, ChunkName: "old1", JudgeOutput: []byte(`{}`), CreatedAt: now.Add(-45 * 24 * time.Hour)},
		{TaskReportID: 102, ChunkName: "old2", JudgeOutput: []byte(`{}`), CreatedAt: now.Add(-35 * 24 * time.Hour)},
		{TaskReportID: 103, ChunkName: "recent", JudgeOutput: []byte(`{}`), CreatedAt: now.Add(-5 * 24 * time.Hour)},
	}
	for _, l := range logs {
		if err := db.Create(&l).Error; err != nil {
			t.Fatalf("create debate log: %v", err)
		}
	}

	purged, err := PurgeExpiredDebateLogs(db, 30)
	if err != nil {
		t.Fatalf("PurgeExpiredDebateLogs failed: %v", err)
	}
	if purged != 2 {
		t.Fatalf("expected 2 debate logs purged, got %d", purged)
	}

	var count int64
	db.Model(&models.TaskDebateLog{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 debate log remaining, got %d", count)
	}
}

func TestPurgeOrphanFindings(t *testing.T) {
	db := setupTestDB(t)

	// 创建真实 TaskReport (ID=200)
	r := models.TaskReport{ID: 200, Status: "success"}
	db.Create(&r)

	// 正常关联记录
	normal := models.CampaignFinding{
		TaskReportID: 200,
		FilePath:     "src/normal.go",
		Title:        "Normal Finding",
	}
	db.Create(&normal)

	// 孤儿记录（关联不存在的报告 ID 9999）
	orphan := models.CampaignFinding{
		TaskReportID: 9999,
		FilePath:     "src/orphan.go",
		Title:        "Orphan Finding",
	}
	db.Create(&orphan)

	purged, err := PurgeOrphanFindings(db)
	if err != nil {
		t.Fatalf("PurgeOrphanFindings failed: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 orphan purged, got %d", purged)
	}

	var remaining []models.CampaignFinding
	db.Find(&remaining)
	if len(remaining) != 1 || remaining[0].TaskReportID != 200 {
		t.Fatalf("expected only normal finding to remain, got %v", remaining)
	}
}

func TestRollbackReportLedgerRetentionGuard(t *testing.T) {
	db := setupTestDB(t)

	// 配置 30 天保留期
	models.AppConfig.Retention.LedgerRetentionDays = 30

	committed40DaysAgo := time.Now().Add(-40 * 24 * time.Hour)
	expiredReport := models.TaskReport{
		ID:                100,
		LedgerCommittedAt: &committed40DaysAgo,
	}

	err := rollbackReportLedger(db, expiredReport)
	if err == nil {
		t.Fatalf("expected error for expired rollback, got nil")
	}
	expectedMsg := "outside the snapshot retention window"
	if !containsStr(err.Error(), expectedMsg) {
		t.Fatalf("expected error containing %q, got %v", expectedMsg, err)
	}
}

func TestPurgeOrphanManifestTempFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "manifest-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	subDir := filepath.Join(tempDir, "task_type", "2026-10-23")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir subDir: %v", err)
	}

	// 1. 标准新格式临时文件（在深层子目录中，且过期）
	oldStandardTemp := filepath.Join(subDir, "report-123-scope-manifest.json.gz.tmp.1791766295073691915")
	// 2. 现网历史残留格式临时文件（.tmp. 格式，且过期）
	oldLegacyDotTemp := filepath.Join(tempDir, "scope_manifest-194.json.gz.tmp.1791766295073691915")
	// 3. 历史单元测试格式临时文件（-tmp- 格式，且过期）
	oldLegacyDashTemp := filepath.Join(tempDir, "scope_manifest-tmp-12345.json.gz")
	// 4. 新生成的未过期临时文件（不应被清理）
	newTempFile := filepath.Join(subDir, "report-456-scope-manifest.json.gz.tmp.1791766295073691999")
	// 5. 正式标准文件（不应被清理）
	regularFile := filepath.Join(subDir, "report-123-scope-manifest.json.gz")
	// 6. 正式遗留文件（不应被清理）
	legacyRegularFile := filepath.Join(tempDir, "scope_manifest-12345.json.gz")

	for _, f := range []string{oldStandardTemp, oldLegacyDotTemp, oldLegacyDashTemp, newTempFile, regularFile, legacyRegularFile} {
		if err := os.WriteFile(f, []byte("test content"), 0644); err != nil {
			t.Fatalf("write file %s: %v", f, err)
		}
	}

	// 将 3 个旧文件的修改时间修改为 48 小时前
	oldTime := time.Now().Add(-48 * time.Hour)
	for _, f := range []string{oldStandardTemp, oldLegacyDotTemp, oldLegacyDashTemp} {
		if err := os.Chtimes(f, oldTime, oldTime); err != nil {
			t.Fatalf("chtimes %s: %v", f, err)
		}
	}

	cleaned := PurgeOrphanManifestTempFiles(tempDir, 24*time.Hour)
	if cleaned != 3 {
		t.Fatalf("expected 3 cleaned files, got %d", cleaned)
	}

	for _, f := range []string{oldStandardTemp, oldLegacyDotTemp, oldLegacyDashTemp} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("expected old file %s to be deleted", f)
		}
	}
	for _, f := range []string{newTempFile, regularFile, legacyRegularFile} {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Fatalf("expected file %s to be preserved", f)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && len(sub) > 0 && stringContains(s, sub)))
}

func stringContains(s, substr string) bool {
	return filepath.Base(s) != "" && (s == substr || len(s) >= len(substr) && stringHasSub(s, substr))
}

func stringHasSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPurgeExpiredScanSnapshotsAtomicTaskDir(t *testing.T) {
	db := setupTestDB(t)

	tempDir := t.TempDir()
	oldDataDir := models.AppConfig.Server.DataDir
	models.AppConfig.Server.DataDir = tempDir
	t.Cleanup(func() { models.AppConfig.Server.DataDir = oldDataDir })

	repo := models.Repository{Name: "task-dir-repo", URL: "https://example.com/task-dir.git"}
	db.Create(&repo)
	taskType := models.TaskType{Name: "sec-scan", DisplayName: "Security Scan"}
	db.Create(&taskType)

	now := time.Now()
	// 创建一个 15 天前超期的 failed 任务 (ID 301)
	task301Dir := filepath.Join(tempDir, "reports", taskType.Name, "2026-10-09", "report-301-task-dir-repo")
	_ = os.MkdirAll(filepath.Join(task301Dir, "debate-chunks"), 0755)
	_ = os.WriteFile(filepath.Join(task301Dir, "report-301-report-task-dir-repo.md"), []byte("# Report"), 0644)
	_ = os.WriteFile(filepath.Join(task301Dir, "report-301-summary-task-dir-repo.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(task301Dir, "debate-chunks", "chunk-1.json"), []byte("{}"), 0644)

	rel301Path := filepath.Join("reports", taskType.Name, "2026-10-09", "report-301-task-dir-repo", "report-301-report-task-dir-repo.md")
	report301 := models.TaskReport{
		ID:         301,
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "failed",
		ReportPath: rel301Path,
		CreatedAt:  now.Add(-15 * 24 * time.Hour),
	}
	db.Create(&report301)

	// 创建一个未超期的任务 (ID 302)
	task302Dir := filepath.Join(tempDir, "reports", taskType.Name, "2026-10-23", "report-302-task-dir-repo")
	_ = os.MkdirAll(task302Dir, 0755)
	_ = os.WriteFile(filepath.Join(task302Dir, "report.md"), []byte("# Active Report"), 0644)
	rel302Path := filepath.Join("reports", taskType.Name, "2026-10-23", "report-302-task-dir-repo", "report.md")
	report302 := models.TaskReport{
		ID:         302,
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "success",
		ReportPath: rel302Path,
		CreatedAt:  now.Add(-1 * 24 * time.Hour),
	}
	db.Create(&report302)

	purged, err := PurgeExpiredScanSnapshots(db, 30, 10, 7)
	if err != nil {
		t.Fatalf("PurgeExpiredScanSnapshots failed: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 report purged, got %d", purged)
	}

	// 验证超期任务的整个专属目录被原子化物理清理
	if _, err := os.Stat(task301Dir); !os.IsNotExist(err) {
		t.Fatalf("expected expired task dir %s to be deleted completely", task301Dir)
	}

	// 验证未超期任务的专属目录完好无损
	if _, err := os.Stat(task302Dir); os.IsNotExist(err) {
		t.Fatalf("expected active task dir %s to be preserved", task302Dir)
	}
}
