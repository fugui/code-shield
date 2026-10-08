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
	// 创建 15 轮 TaskReport：
	// Report 1~5: 45 天前（超期，且在保底 10 轮之外，应该被清理）
	// Report 6~12: 40 天前（超期，但是在最新 10 轮之内，应该被保留！）
	// Report 13~15: 1 天前（未超期，在最新 10 轮之内，保留）
	var reports []models.TaskReport
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
		reports = append(reports, r)

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
	}

	// 执行清理：retentionDays=30, maxRetained=10
	purged, err := PurgeExpiredScanSnapshots(db, 30, 10)
	if err != nil {
		t.Fatalf("PurgeExpiredScanSnapshots failed: %v", err)
	}

	// 预期被清理的报告数：15 轮中最新 10 轮是 Report 6~15。
	// 排名在 10 之后的报告是 Report 1~5，且它们均在 45 天前 (>30天)，所以正好 5 轮被清理。
	if purged != 5 {
		t.Fatalf("expected 5 reports purged, got %d", purged)
	}

	// 验证 Report 1~5 的快照数据已被物理删除
	for i := 1; i <= 5; i++ {
		var scopeCount, obsCount, findCount int64
		db.Model(&models.ScanScopeEntry{}).Where("report_id = ?", i).Count(&scopeCount)
		db.Model(&models.DefectObservation{}).Where("report_id = ?", i).Count(&obsCount)
		db.Model(&models.AnalysisFinding{}).Where("task_report_id = ?", i).Count(&findCount)
		if scopeCount != 0 || obsCount != 0 || findCount != 0 {
			t.Fatalf("expected snapshot for report %d to be deleted, got scope=%d, obs=%d, finding=%d",
				i, scopeCount, obsCount, findCount)
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

	// 验证保底范围内的 Report 6~12（虽然超过 30 天，但在最新 10 轮基线内）快照完好无损
	for i := 6; i <= 15; i++ {
		var scopeCount int64
		db.Model(&models.ScanScopeEntry{}).Where("report_id = ?", i).Count(&scopeCount)
		if scopeCount != 1 {
			t.Fatalf("expected snapshot for report %d to be preserved, got count=%d", i, scopeCount)
		}
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
	if err != nil && !containsStr(err.Error(), expectedMsg) {
		t.Fatalf("expected error containing %q, got %v", expectedMsg, err)
	}
}

func TestPurgeOrphanManifestTempFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "manifest-test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldFile := filepath.Join(tempDir, "scope_manifest-tmp-12345.json.gz")
	newFile := filepath.Join(tempDir, "scope_manifest-tmp-67890.json.gz")
	otherFile := filepath.Join(tempDir, "scope_manifest-12345.json.gz") // 正式文件，不应被清理

	if err := os.WriteFile(oldFile, []byte("old content"), 0644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.WriteFile(newFile, []byte("new content"), 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.WriteFile(otherFile, []byte("regular file"), 0644); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	// 将 oldFile 的修改时间修改为 48 小时前
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	cleaned := PurgeOrphanManifestTempFiles(tempDir, 24*time.Hour)
	if cleaned != 1 {
		t.Fatalf("expected 1 cleaned file, got %d", cleaned)
	}

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected oldFile to be deleted")
	}
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		t.Fatalf("expected newFile to be preserved")
	}
	if _, err := os.Stat(otherFile); os.IsNotExist(err) {
		t.Fatalf("expected otherFile to be preserved")
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
