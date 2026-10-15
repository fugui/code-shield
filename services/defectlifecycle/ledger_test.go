package defectlifecycle

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"

	"gorm.io/gorm"
)

func TestAdvanceUnmatchedDefectsCountsScannedRoundWithoutReproduction(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_advance_unmatched",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	scannedDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fingerprint-scanned", IdentityKind: "K1",
		NormPath: "src/unchanged.cpp", Severity: "建议", BlobHash: "same-hash",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	uncoveredDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fingerprint-uncovered", IdentityKind: "K1",
		NormPath: "src/failed.cpp", Severity: "建议", BlobHash: "old-hash",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	for _, defect := range []*models.Defect{&scannedDefect, &uncoveredDefect} {
		if err := db.Create(defect).Error; err != nil {
			t.Fatalf("create defect %s: %v", defect.CanonicalFingerprint, err)
		}
	}

	input := LedgerInput{
		Report: report, Repo: repo, TaskType: taskType, CoverageState: CoverageChangeFocus,
		Scope: []models.ScanScopeEntry{
			{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: scannedDefect.NormPath, BlobHash: "same-hash", Outcome: ScopeScanned},
			{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: uncoveredDefect.NormPath, BlobHash: "new-hash", Outcome: ScopeFailed},
		},
		Lifecycle: LifecyclePolicy{
			HighRiskSeverities: []string{"致命", "严重"}, ResolvedRounds: 2,
			DormantThreshold: 2, ObsoleteAfterDormant: 8, RequireCoverage: true, RequireChange: true,
		},
	}
	defects := []models.Defect{scannedDefect, uncoveredDefect}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := advanceUnmatchedDefects(tx, defects, map[uint]bool{}, input, time.Now())
		return err
	})
	if err != nil {
		t.Fatalf("advance unmatched defects: %v", err)
	}

	var updatedScanned, updatedUncovered models.Defect
	if err := db.First(&updatedScanned, scannedDefect.ID).Error; err != nil {
		t.Fatalf("load scanned defect: %v", err)
	}
	if err := db.First(&updatedUncovered, uncoveredDefect.ID).Error; err != nil {
		t.Fatalf("load uncovered defect: %v", err)
	}
	if updatedScanned.MissedCount != 1 || updatedScanned.Status != DefectStatusCoverageGap {
		t.Fatalf("expected scanned defect to advance, got missed=%d status=%s", updatedScanned.MissedCount, updatedScanned.Status)
	}
	if updatedUncovered.MissedCount != 0 || updatedUncovered.Status != DefectStatusActive {
		t.Fatalf("expected uncovered defect to stay unchanged, got missed=%d status=%s", updatedUncovered.MissedCount, updatedUncovered.Status)
	}
}

func TestCommitLedgerAssignsSameFingerprintObservationsOnce(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_duplicate_identity",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	identity := Identity{
		RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: "src/a.cpp", ScopeKey: "scope",
		StmtShape: "shape", OccurrenceIndex: 1, DefectClassMajor: "memory_leak",
		LineStart: 158, LineEnd: 180, K2: "strong-key",
	}
	groups := []ObservationGroup{
		{UID: "group-1", Identity: identity},
		{UID: "group-2", Identity: identity},
	}
	decisions := []ObservationDecision{
		{ObservationGroupUID: groups[0].UID, Identity: identity, Verdict: VerdictNew},
		{ObservationGroupUID: groups[1].UID, Identity: identity, Verdict: VerdictNew},
	}
	input := LedgerInput{
		Report: report, Repo: repo, TaskType: taskType, Observations: groups, Decisions: decisions,
	}

	var result *LedgerResult
	err := db.Transaction(func(tx *gorm.DB) (txErr error) {
		result, txErr = commitLedger(tx, input)
		return txErr
	})
	if err != nil {
		t.Fatalf("commit ledger: %v", err)
	}
	if len(result.NewDefectIDs) != 1 {
		t.Fatalf("expected one new defect ID, got %+v", result.NewDefectIDs)
	}
	if len(result.Observations) != 2 || result.Observations[0].DefectID == nil || *result.Observations[0].DefectID != result.NewDefectIDs[0] {
		t.Fatalf("expected first observation bound to new defect, got %+v", result.Observations)
	}
	if result.Observations[1].DefectID == nil || *result.Observations[1].DefectID != result.NewDefectIDs[0] {
		t.Fatalf("expected same-fingerprint observations to share defect, got %+v", result.Observations)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected one event per new observation, got %+v", result.Events)
	}
	for _, event := range result.Events {
		if event.DefectID != result.NewDefectIDs[0] {
			t.Fatalf("expected event defect %d, got %d", result.NewDefectIDs[0], event.DefectID)
		}
	}
	var defectCount int64
	if err := db.Model(&models.Defect{}).Count(&defectCount).Error; err != nil {
		t.Fatalf("count defects: %v", err)
	}
	if defectCount != 1 {
		t.Fatalf("expected one defect, got %d", defectCount)
	}
}

func TestCommitLedgerReusesExistingActiveSlotDefectOnNewVerdict(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_slot_uniqueness",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report1 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&report1).Error; err != nil {
		t.Fatalf("create report 1: %v", err)
	}

	normPath := "test/scan-test.cc"
	scopeSymbol := "TEST"
	targetEntity := "scan_test"
	defectClass := "断言有效性-空测试"
	cleanToken := "test(scan_test,invalid_format){"
	stmtAnchorHash := ComputeStmtAnchorHash(cleanToken)
	coreSlotKey := ComputeCoreSlotKey(normPath, scopeSymbol, defectClass, targetEntity)
	slotKey := ComputePreciseSlotKey(coreSlotKey, stmtAnchorHash)

	// 存量库中已存在一条 ACTIVE 缺陷，拥有相同的 slot_key 但指纹为 old-fingerprint
	existingDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "old-fingerprint-k2", IdentityKind: "K2",
		NormPath: normPath, ScopeSymbol: scopeSymbol, TargetEntity: targetEntity,
		DefectClassMajor: defectClass, CleanToken: cleanToken,
		StmtAnchorHash: stmtAnchorHash, CoreSlotKey: coreSlotKey, SlotKey: slotKey,
		FirstReportID: report1.ID, LastSeenReportID: report1.ID, LastMatchedReportID: report1.ID,
		RowVersion: 1,
	}
	if err := db.Create(&existingDefect).Error; err != nil {
		t.Fatalf("create existing defect: %v", err)
	}

	// 模拟新一轮扫描
	report2 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report2).Error; err != nil {
		t.Fatalf("create report 2: %v", err)
	}

	// 新扫描中由于上下文变化指纹变为 new-fingerprint-k2，但槽位相同
	newIdentity := Identity{
		RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: normPath,
		SymbolPath: scopeSymbol, ScopeKey: "scope-key", StmtShape: "shape",
		DefectClassMajor: defectClass, CleanToken: cleanToken,
		TargetEntity: targetEntity, StmtAnchorHash: stmtAnchorHash,
		CoreSlotKey: coreSlotKey, SlotKey: slotKey,
		K2: "new-fingerprint-k2",
	}
	groups := []ObservationGroup{
		{UID: "group-scan-test", Identity: newIdentity},
	}
	// 假设由于极端原因该 observation 裁定为 VerdictNew
	decisions := []ObservationDecision{
		{ObservationGroupUID: groups[0].UID, Identity: newIdentity, Verdict: VerdictNew},
	}
	input := LedgerInput{
		Report: report2, Repo: repo, TaskType: taskType, Observations: groups, Decisions: decisions,
	}

	var result *LedgerResult
	err := db.Transaction(func(tx *gorm.DB) (txErr error) {
		result, txErr = commitLedger(tx, input)
		return txErr
	})
	if err != nil {
		t.Fatalf("commit ledger failed: %v", err)
	}

	// 验证未产生假新缺陷，且 Observation 正确复用了存量 defect ID
	if len(result.NewDefectIDs) != 0 {
		t.Fatalf("expected 0 new defect IDs, got %+v", result.NewDefectIDs)
	}
	if len(result.Observations) != 1 || result.Observations[0].DefectID == nil || *result.Observations[0].DefectID != existingDefect.ID {
		t.Fatalf("expected observation bound to existing defect %d, got %+v", existingDefect.ID, result.Observations)
	}

	// 数据库中 defect 总数依然为 1
	var count int64
	db.Model(&models.Defect{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 defect in db, got %d", count)
	}
}

func TestCommitLedgerDeduplicatesMultipleNewObservationsWithSameSlotKey(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_batch_slot_dedup",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	normPath := "test/scan-test.cc"
	scopeSymbol := "TEST"
	targetEntity := "scan_test"
	defectClass := "断言有效性-空测试"
	cleanToken := "test(scan_test,invalid_format){"
	stmtAnchorHash := ComputeStmtAnchorHash(cleanToken)
	coreSlotKey := ComputeCoreSlotKey(normPath, scopeSymbol, defectClass, targetEntity)
	slotKey := ComputePreciseSlotKey(coreSlotKey, stmtAnchorHash)

	// 同一批次中有两个 observation，指纹不同，但槽位相同
	identity1 := Identity{
		RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: normPath,
		SymbolPath: scopeSymbol, ScopeKey: "scope-1", StmtShape: "shape-1",
		DefectClassMajor: defectClass, CleanToken: cleanToken,
		TargetEntity: targetEntity, StmtAnchorHash: stmtAnchorHash,
		CoreSlotKey: coreSlotKey, SlotKey: slotKey,
		K2: "fingerprint-alpha",
	}
	identity2 := Identity{
		RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: normPath,
		SymbolPath: scopeSymbol, ScopeKey: "scope-2", StmtShape: "shape-2",
		DefectClassMajor: defectClass, CleanToken: cleanToken,
		TargetEntity: targetEntity, StmtAnchorHash: stmtAnchorHash,
		CoreSlotKey: coreSlotKey, SlotKey: slotKey,
		K2: "fingerprint-beta",
	}
	groups := []ObservationGroup{
		{UID: "group-1", Identity: identity1},
		{UID: "group-2", Identity: identity2},
	}
	decisions := []ObservationDecision{
		{ObservationGroupUID: groups[0].UID, Identity: identity1, Verdict: VerdictNew},
		{ObservationGroupUID: groups[1].UID, Identity: identity2, Verdict: VerdictNew},
	}
	input := LedgerInput{
		Report: report, Repo: repo, TaskType: taskType, Observations: groups, Decisions: decisions,
	}

	var result *LedgerResult
	err := db.Transaction(func(tx *gorm.DB) (txErr error) {
		result, txErr = commitLedger(tx, input)
		return txErr
	})
	if err != nil {
		t.Fatalf("commit ledger failed: %v", err)
	}

	if len(result.NewDefectIDs) != 1 {
		t.Fatalf("expected 1 new defect ID, got %+v", result.NewDefectIDs)
	}
	if len(result.Observations) != 2 ||
		*result.Observations[0].DefectID != result.NewDefectIDs[0] ||
		*result.Observations[1].DefectID != result.NewDefectIDs[0] {
		t.Fatalf("expected both observations to share defect ID %d, got %+v", result.NewDefectIDs[0], result.Observations)
	}
	var count int64
	db.Model(&models.Defect{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 defect in db, got %d", count)
	}
}

func TestAdvanceUnmatchedDefectsAutoResolvesMissingSourceFile(t *testing.T) {
	db := setupTestDB(t)

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	tempDir := t.TempDir()
	// 创建一个真实存在的文件
	existingFile := filepath.Join(tempDir, "src", "existing.cpp")
	if err := os.MkdirAll(filepath.Dir(existingFile), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(existingFile, []byte("int main(){}"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	missingDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fingerprint-missing", IdentityKind: "K1",
		NormPath: "src/removed.cpp", Severity: "严重",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
		RowVersion: 1,
	}
	existingDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fingerprint-existing", IdentityKind: "K1",
		NormPath: "src/existing.cpp", Severity: "严重",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
		RowVersion: 1,
	}
	for _, defect := range []*models.Defect{&missingDefect, &existingDefect} {
		if err := db.Create(defect).Error; err != nil {
			t.Fatalf("create defect: %v", err)
		}
	}

	input := LedgerInput{
		Report: report, Repo: repo, TaskType: taskType, RepoRoot: tempDir,
		Lifecycle: LifecyclePolicy{
			AutoResolveMissing: true,
			HighRiskSeverities: []string{"致命", "严重"},
			ResolvedRounds:     2,
		},
	}
	defects := []models.Defect{missingDefect, existingDefect}
	var events []models.DefectEvent
	err := db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		events, txErr = advanceUnmatchedDefects(tx, defects, map[uint]bool{}, input, time.Now())
		return txErr
	})
	if err != nil {
		t.Fatalf("advance unmatched defects: %v", err)
	}

	var updatedMissing, updatedExisting models.Defect
	if err := db.First(&updatedMissing, missingDefect.ID).Error; err != nil {
		t.Fatalf("load missing defect: %v", err)
	}
	if err := db.First(&updatedExisting, existingDefect.ID).Error; err != nil {
		t.Fatalf("load existing defect: %v", err)
	}

	// 缺失文件应直接置为 RESOLVED，且不会等待两轮 missed
	if updatedMissing.Status != DefectStatusResolved {
		t.Fatalf("expected missing file defect to be RESOLVED, got %s", updatedMissing.Status)
	}
	if updatedMissing.StatusReason != "source file does not exist" {
		t.Fatalf("expected status reason 'source file does not exist', got %s", updatedMissing.StatusReason)
	}

	// 现有物理文件不应被误关为 RESOLVED
	if updatedExisting.Status == DefectStatusResolved {
		t.Fatalf("expected existing file defect not to be RESOLVED")
	}

	// 验证事件记录
	foundEvent := false
	for _, ev := range events {
		if ev.DefectID == missingDefect.ID && ev.ToStatus == DefectStatusResolved {
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatalf("expected STATUS_CHANGED event for missing defect")
	}
}
