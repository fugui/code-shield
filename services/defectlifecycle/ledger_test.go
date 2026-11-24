package defectlifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/coverage"

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
		NormPath: "src/changed.cpp", Severity: "建议", BlobHash: "old-hash",
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
		Report: report, Repo: repo, TaskType: taskType, CoverageState: CoverageComplete,
		Coverage: &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{
				{Path: scannedDefect.NormPath, Hash: "new-hash", Status: coverage.StatusSuccess},
			},
		},
		Scope: []models.ScanScopeEntry{
			{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: scannedDefect.NormPath, BlobHash: "new-hash", Outcome: ScopeScanned},
			{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: uncoveredDefect.NormPath, BlobHash: "new-hash-2", Outcome: ScopeFailed},
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
	if updatedUncovered.MissedCount != 0 || updatedUncovered.Status != DefectStatusCoverageGap {
		t.Fatalf("expected uncovered defect to move to COVERAGE_GAP with missed=0, got missed=%d status=%s", updatedUncovered.MissedCount, updatedUncovered.Status)
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

func TestAdvanceUnmatchedDefectsFreezesDecayWhenRequireChangeUnchanged(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_freeze_unchanged",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "freeze-demo", URL: "https://example.com/freeze.git"}
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
	defect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fp-freeze", IdentityKind: "K1",
		NormPath: "src/unchanged.cpp", Severity: "严重", BlobHash: "blob-same-123",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
		RowVersion: 1,
	}
	if err := db.Create(&defect).Error; err != nil {
		t.Fatalf("create defect: %v", err)
	}
	scopeEntries := []models.ScanScopeEntry{
		{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: defect.NormPath, BlobHash: "blob-same-123", Outcome: ScopeScanned},
	}
	input := LedgerInput{
		Report:        report,
		Repo:          repo,
		TaskType:      taskType,
		CoverageState: CoverageComplete,
		Coverage: &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{{Path: defect.NormPath, Hash: "blob-same-123", Status: coverage.StatusSuccess}},
		},
		Evaluator: NewMemoryScopeEvaluator(scopeEntries),
		Lifecycle: LifecyclePolicy{
			HighRiskSeverities: []string{"致命", "严重"}, ResolvedRounds: 2,
			DormantThreshold: 2, ObsoleteAfterDormant: 8,
			RequireCoverage: true, RequireChange: true,
		},
	}
	var events []models.DefectEvent
	err := db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		events, txErr = advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, input, time.Now())
		return txErr
	})
	if err != nil {
		t.Fatalf("advanceUnmatchedDefects() error = %v", err)
	}
	var reload models.Defect
	if err := db.First(&reload, defect.ID).Error; err != nil {
		t.Fatalf("reload defect: %v", err)
	}
	if reload.Status != DefectStatusActive || reload.MissedCount != 0 || len(events) != 0 {
		t.Fatalf("expected unchanged file defect to freeze at ACTIVE missed=0 events=0, got status=%s missed=%d events=%d",
			reload.Status, reload.MissedCount, len(events))
	}
}

func TestUpdateMatchedDefectResetsMissedCountAndPreservesHumanClosed(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_reset_missed_exempt",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "reset-demo", URL: "https://example.com/reset.git"}
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

	pendingDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusVerifiedPending,
		CanonicalFingerprint: "fp-pending", IdentityKind: "K1",
		NormPath: "src/pending.cpp", ScopeKey: "scope-p", SymbolPath: "fnP",
		CleanToken: "delete p", DefectClassMajor: "memory_leak", Severity: "严重",
		MissedCount: 2, DormantRounds: 3, RowVersion: 1,
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	exemptDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusHumanClosed,
		CanonicalFingerprint: "fp-exempt", IdentityKind: "K1",
		NormPath: "src/exempt.cpp", ScopeKey: "scope-e", SymbolPath: "fnE",
		CleanToken: "delete e", DefectClassMajor: "memory_leak", Severity: "严重",
		MissedCount: 0, RowVersion: 1,
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	for _, d := range []*models.Defect{&pendingDefect, &exemptDefect} {
		if err := db.Create(d).Error; err != nil {
			t.Fatalf("create defect: %v", err)
		}
	}

	pendingID := pendingDefect.ID
	pendingObs := models.DefectObservation{DefectID: &pendingID, Verdict: VerdictExisted, Reason: "SLOT_PRECISE"}
	exemptID := exemptDefect.ID
	exemptObs := models.DefectObservation{DefectID: &exemptID, Verdict: VerdictExisted, Reason: "SLOT_PRECISE"}

	input := LedgerInput{Report: report, Repo: repo, TaskType: taskType}
	err := db.Transaction(func(tx *gorm.DB) error {
		updatedPending, _, _, uErr := updateMatchedDefect(tx, pendingDefect, Identity{
			NormPath: "src/pending.cpp", ScopeKey: "scope-p", SymbolPath: "fnP",
			CleanToken: "delete p", DefectClassMajor: "memory_leak", Severity: "严重",
		}, input, &pendingObs, time.Now())
		if uErr != nil {
			return uErr
		}
		if updatedPending.MissedCount != 0 || updatedPending.DormantRounds != 0 || updatedPending.Status != DefectStatusActive {
			t.Fatalf("expected active & zero counters, got status=%s missed=%d dormant=%d",
				updatedPending.Status, updatedPending.MissedCount, updatedPending.DormantRounds)
		}

		updatedExempt, exemptEvent, changed, eErr := updateMatchedDefect(tx, exemptDefect, Identity{
			NormPath: "src/exempt.cpp", ScopeKey: "scope-e", SymbolPath: "fnE",
			CleanToken: "delete e", DefectClassMajor: "memory_leak", Severity: "严重",
		}, input, &exemptObs, time.Now())
		if eErr != nil {
			return eErr
		}
		if updatedExempt.Status != DefectStatusHumanClosed || !changed || exemptEvent.EventType != EventTypeExemptRecheck {
			t.Fatalf("expected HUMAN_CLOSED + EXEMPT_RECHECK event, got status=%s changed=%v event=%s",
				updatedExempt.Status, changed, exemptEvent.EventType)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	var reload models.Defect
	if err := db.First(&reload, pendingDefect.ID).Error; err != nil {
		t.Fatalf("reload pending defect: %v", err)
	}
	if reload.MissedCount != 0 || reload.DormantRounds != 0 || reload.Status != DefectStatusActive {
		t.Fatalf("expected DB missed_count=0 & dormant_rounds=0 & ACTIVE, got missed=%d dormant=%d status=%s",
			reload.MissedCount, reload.DormantRounds, reload.Status)
	}
}

func TestPersistScanFactsHotUpgradesV1DefectToV2AndPreservesLegacyAlias(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "src", "upgrade.cpp")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "void Dispatch(Session* reqSession) {\n    if (reqSession != nullptr) {\n        reqSession->Execute();\n    }\n}\n"
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_v1_to_v2_upgrade",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{}, &models.TaskChunkExecution{},
	)
	repo := models.Repository{Name: "upgrade-demo", URL: "https://example.com/upgrade.git"}
	taskType := models.TaskType{Name: "coredump-risk", DisplayName: "Core Dump Risk", GovernanceMode: models.GovernanceModeFullLedger}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	r1 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&r1).Error; err != nil {
		t.Fatal(err)
	}

	// 模拟历史 v1 算法留存的数据库记录（v1 原始分类 + v1 单行槽位）
	v1Class := "内存管理问题"
	v1CoreSlot := ComputeCoreSlotKey("src/upgrade.cpp", "Dispatch", v1Class, "reqSession")
	v1StmtHash := ComputeStmtAnchorHash("reqSession->Execute();")
	v1Slot := ComputePreciseSlotKey(v1CoreSlot, v1StmtHash)
	lineStart := 3
	legacyDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: v1Slot, IdentityKind: "SLOT",
		NormPath: "src/upgrade.cpp", ScopeKey: "legacy-scope-key",
		ScopeSymbol: "Dispatch", SymbolPath: "Dispatch", TargetEntity: "reqSession",
		StmtAnchorHash: v1StmtHash, CoreSlotKey: v1CoreSlot, SlotKey: v1Slot,
		CleanToken: "reqsession->execute()", Category: "内存管理问题-空指针解引用", DefectClassMajor: v1Class, Severity: "严重",
		LineStart: &lineStart, LineEnd: &lineStart, BlobHash: "hash-1",
		FirstReportID: r1.ID, LastSeenReportID: r1.ID, LastMatchedReportID: r1.ID, RowVersion: 1,
	}
	if err := db.Create(&legacyDefect).Error; err != nil {
		t.Fatal(err)
	}

	r2 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&r2).Error; err != nil {
		t.Fatal(err)
	}
	v2Finding := models.AnalysisFinding{
		FilePath: "src/upgrade.cpp", LineNumber: "2", Category: "内存管理问题-空指针解引用",
		Severity: "严重", Title: "Unchecked reqSession",
		TriggerLine: "if (reqSession != nullptr) {", ScopeSymbol: "Dispatch", TargetSymbol: "reqSession",
	}
	cov := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{{Path: "src/upgrade.cpp", Hash: "hash-1", Status: coverage.StatusSuccess}},
	}
	res, err := PersistScanFacts(ScanInput{
		DB: db, Report: r2, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: []models.AnalysisFinding{v2Finding}, Coverage: cov, AlgorithmVersion: "v2",
	})
	if err != nil {
		t.Fatalf("v2 PersistScanFacts failed: %v", err)
	}
	if len(res.Observations) != 1 || res.Observations[0].DefectID == nil || *res.Observations[0].DefectID != legacyDefect.ID {
		t.Fatalf("expected v1 defect %d matched seamlessly by v2 CORE_SLOT_SINGLETON/slot, got %+v", legacyDefect.ID, res.Observations)
	}
	if res.Observations[0].Verdict != VerdictExisted {
		t.Fatalf("expected EXISTED verdict, got %s", res.Observations[0].Verdict)
	}

	var count int64
	if err := db.Model(&models.Defect{}).Where("repo_id = ?", repo.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected 0 duplicate defects (total=1), got %d err=%v", count, err)
	}

	var upgraded models.Defect
	if err := db.First(&upgraded, legacyDefect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if upgraded.DefectClassMajor != ClassNPD || upgraded.SlotKey == v1Slot {
		t.Fatalf("expected defect hot-upgraded to v2 ClassNPD and new SlotKey, got class=%q slot=%q", upgraded.DefectClassMajor, upgraded.SlotKey)
	}

	var aliasCount int64
	if err := db.Model(&models.DefectAlias{}).
		Where("defect_id = ? AND alias_type = ? AND alias_value = ?", legacyDefect.ID, AliasSlotHealed, v1Slot).
		Count(&aliasCount).Error; err != nil || aliasCount != 1 {
		t.Fatalf("expected legacy v1 SlotKey preserved in defect_aliases, got count=%d err=%v", aliasCount, err)
	}
}

func TestAdvanceUnmatchedDefectsWithholdsOnNonDefaultBranchOrScopedOverride(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_branch_scope_guard",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "branch-guard-demo", URL: "https://example.com/bg.git", Branch: "main"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review", TargetScope: "business"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess, Branch: "feature/exp"}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	defect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fp-bg", IdentityKind: "K1",
		NormPath: "src/changed.cpp", Severity: "一般", BlobHash: "old-hash",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
		RowVersion: 1,
	}
	if err := db.Create(&defect).Error; err != nil {
		t.Fatal(err)
	}

	cov := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{{Path: defect.NormPath, Hash: "new-hash", Status: coverage.StatusSuccess}},
	}
	// 1. 非默认分支扫描应冻结存量衰变
	err := db.Transaction(func(tx *gorm.DB) error {
		events, txErr := advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, LedgerInput{
			Report: report, Repo: repo, TaskType: taskType, CoverageState: CoverageComplete, Coverage: cov,
			Scope: []models.ScanScopeEntry{{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: defect.NormPath, BlobHash: "new-hash", Outcome: ScopeScanned}},
			Lifecycle: LifecyclePolicy{
				ResolvedRounds: 1, RequireCoverage: true, RequireChange: false, OnlyAdvanceOnDefaultBranch: true,
			},
		}, time.Now())
		if len(events) != 0 {
			t.Fatalf("expected 0 events on non-default branch, got %d", len(events))
		}
		return txErr
	})
	if err != nil {
		t.Fatalf("branch guard transaction failed: %v", err)
	}

	// 2. 主干分支但 TargetScope 覆盖（只测单测 test != business）同样应冻结存量衰变
	reportScoped := models.TaskReport{ID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, Branch: "main", TargetScope: "test"}
	err = db.Transaction(func(tx *gorm.DB) error {
		events, txErr := advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, LedgerInput{
			Report: reportScoped, Repo: repo, TaskType: taskType, CoverageState: CoverageComplete, Coverage: cov,
			Scope: []models.ScanScopeEntry{{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: defect.NormPath, BlobHash: "new-hash", Outcome: ScopeScanned}},
			Lifecycle: LifecyclePolicy{
				ResolvedRounds: 1, RequireCoverage: true, RequireChange: false, OnlyAdvanceOnDefaultBranch: true,
			},
		}, time.Now())
		if len(events) != 0 {
			t.Fatalf("expected 0 events on scoped override, got %d", len(events))
		}
		return txErr
	})
	if err != nil {
		t.Fatalf("scoped override transaction failed: %v", err)
	}

	var reload models.Defect
	if err := db.First(&reload, defect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reload.Status != DefectStatusActive || reload.MissedCount != 0 {
		t.Fatalf("expected defect untouched by non-default branch & scoped override, got status=%s missed=%d", reload.Status, reload.MissedCount)
	}

	// 3. 验证 Repo.Branch 为空时默认回退到 master：feature 分支应被拦截，master 分支才放行
	repoEmptyBranch := repo
	repoEmptyBranch.Branch = ""
	err = db.Transaction(func(tx *gorm.DB) error {
		events, txErr := advanceUnmatchedDefects(tx, []models.Defect{reload}, map[uint]bool{}, LedgerInput{
			Report: report, Repo: repoEmptyBranch, TaskType: taskType, CoverageState: CoverageComplete, Coverage: cov,
			Scope: []models.ScanScopeEntry{{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: reload.NormPath, BlobHash: "new-hash", Outcome: ScopeScanned}},
			Lifecycle: LifecyclePolicy{
				ResolvedRounds: 1, RequireCoverage: true, RequireChange: false, OnlyAdvanceOnDefaultBranch: true,
			},
		}, time.Now())
		if len(events) != 0 {
			t.Fatalf("expected feature branch withheld when default branch falls back to master, got %d", len(events))
		}
		return txErr
	})
	if err != nil {
		t.Fatalf("empty default branch fallback transaction failed: %v", err)
	}
}

func TestAdvanceUnmatchedDefects_SingleCommitFixAndHighRiskConverge(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_require_change_high_risk",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "rc-demo", URL: "https://example.com/rc.git", Branch: "main"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	r0 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess, Branch: "main"}
	if err := db.Create(&r0).Error; err != nil {
		t.Fatal(err)
	}

	normalDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fp-normal", IdentityKind: "SLOT",
		NormPath: "src/fix.cpp", Severity: "一般", BlobHash: "h1",
		FirstReportID: r0.ID, LastSeenReportID: r0.ID, LastMatchedReportID: r0.ID, RowVersion: 1,
	}
	highRiskDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fp-high", IdentityKind: "SLOT",
		NormPath: "src/fix.cpp", Severity: "严重", BlobHash: "h1",
		FirstReportID: r0.ID, LastSeenReportID: r0.ID, LastMatchedReportID: r0.ID, RowVersion: 1,
	}
	noBaselineDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fp-legacy", IdentityKind: "SLOT",
		NormPath: "src/unchanged.cpp", Severity: "一般", BlobHash: "",
		FirstReportID: r0.ID, LastSeenReportID: r0.ID, LastMatchedReportID: r0.ID, RowVersion: 1,
	}
	for _, d := range []*models.Defect{&normalDefect, &highRiskDefect, &noBaselineDefect} {
		if err := db.Create(d).Error; err != nil {
			t.Fatal(err)
		}
	}

	policy := LifecyclePolicy{
		ResolvedRounds:             3,
		RequireCoverage:            true,
		RequireChange:              true,
		OnlyAdvanceOnDefaultBranch: true,
		HighRiskSeverities:         []string{"致命", "严重"},
	}

	// 第 1 轮（无代码变更 h1 -> h1）：应冻结 normal/highRisk 衰变，并静默回填 noBaselineDefect 基线而不衰变
	r1 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess, Branch: "main"}
	if err := db.Create(&r1).Error; err != nil {
		t.Fatal(err)
	}
	covSame := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/fix.cpp", Hash: "h1", Status: coverage.StatusSuccess},
			{Path: "src/unchanged.cpp", Hash: "u1", Status: coverage.StatusSuccess},
		},
	}
	evalSame := NewMemoryScopeEvaluator([]models.ScanScopeEntry{
		{NormPath: "src/fix.cpp", BlobHash: "h1", Outcome: ScopeScanned, DiffTouched: false},
		{NormPath: "src/unchanged.cpp", BlobHash: "u1", Outcome: ScopeScanned, DiffTouched: false},
	})
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, txErr := advanceUnmatchedDefects(tx, []models.Defect{normalDefect, highRiskDefect, noBaselineDefect}, map[uint]bool{}, LedgerInput{
			Report: r1, Repo: repo, TaskType: taskType, CoverageState: CoverageComplete, Coverage: covSame, Evaluator: evalSame, Lifecycle: policy,
		}, time.Now())
		return txErr
	}); err != nil {
		t.Fatalf("unchanged round failed: %v", err)
	}

	var backfilled models.Defect
	if err := db.First(&backfilled, noBaselineDefect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if backfilled.BlobHash != "u1" || backfilled.MissedCount != 0 || backfilled.Status != DefectStatusActive {
		t.Fatalf("expected silent blob hash backfill without decay, got blob=%q missed=%d status=%s", backfilled.BlobHash, backfilled.MissedCount, backfilled.Status)
	}

	// 第 2~5 轮：单次提交修复（h1 -> h2），之后保持 h2 不再改动；不应因覆盖 BlobHash 而冻结在 missed=1
	var finalReportID uint
	for round := 1; round <= 4; round++ {
		rep := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess, Branch: "main"}
		if err := db.Create(&rep).Error; err != nil {
			t.Fatal(err)
		}
		finalReportID = rep.ID
		var currentDefects []models.Defect
		if err := db.Where("id IN ?", []uint{normalDefect.ID, highRiskDefect.ID}).Order("id").Find(&currentDefects).Error; err != nil {
			t.Fatal(err)
		}
		covFix := &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{
				{Path: "src/fix.cpp", Hash: "h2", Status: coverage.StatusSuccess, DiffTouched: round == 1},
			},
		}
		evalFix := NewMemoryScopeEvaluator([]models.ScanScopeEntry{
			{NormPath: "src/fix.cpp", BlobHash: "h2", Outcome: ScopeScanned, DiffTouched: round == 1},
		})
		if err := db.Transaction(func(tx *gorm.DB) error {
			_, txErr := advanceUnmatchedDefects(tx, currentDefects, map[uint]bool{}, LedgerInput{
				Report: rep, Repo: repo, TaskType: taskType, CoverageState: CoverageComplete, Coverage: covFix, Evaluator: evalFix, Lifecycle: policy,
			}, time.Now())
			return txErr
		}); err != nil {
			t.Fatalf("post-fix round %d failed: %v", round, err)
		}
	}

	var resolvedNormal, resolvedHigh models.Defect
	if err := db.First(&resolvedNormal, normalDefect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&resolvedHigh, highRiskDefect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resolvedNormal.Status != DefectStatusResolved || resolvedNormal.MissedCount != 3 || resolvedNormal.ResolvedReportID == nil {
		t.Fatalf("expected normal defect resolved at missed=3 with resolved_report_id, got %+v", resolvedNormal)
	}
	if resolvedNormal.StatusReason != "file scanned but defect not reproduced for 3 rounds" {
		t.Fatalf("unexpected normal status reason: %q", resolvedNormal.StatusReason)
	}
	if resolvedHigh.Status != DefectStatusResolved || resolvedHigh.MissedCount != 4 || resolvedHigh.ResolvedReportID == nil || *resolvedHigh.ResolvedReportID != finalReportID {
		t.Fatalf("expected high-risk defect converged to RESOLVED on round 4 (report %d), got %+v", finalReportID, resolvedHigh)
	}
}

func runGitTestCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\noutput: %s", args, err, string(out))
	}
	return string(out)
}

func initGitTestRepo(t *testing.T, dir string) {
	t.Helper()
	runGitTestCmd(t, dir, "init")
	runGitTestCmd(t, dir, "config", "user.email", "test@example.com")
	runGitTestCmd(t, dir, "config", "user.name", "Test User")
}

func TestAdvanceUnmatchedDefectsDoesNotFalseResolveUppercasePath(t *testing.T) {
	runCase := func(t *testing.T, withGit bool) {
		t.Helper()
		db := testdb.SetupIsolatedDB(t, "defectlifecycle_uppercase_path_"+map[bool]string{true: "git", false: "fs"}[withGit],
			&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
			&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
		)
		repo := models.Repository{Name: "upper-demo", URL: "https://example.com/upper.git", Branch: "master"}
		taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
		if err := db.Create(&repo).Error; err != nil {
			t.Fatalf("create repo: %v", err)
		}
		if err := db.Create(&taskType).Error; err != nil {
			t.Fatalf("create task type: %v", err)
		}

		tempDir := t.TempDir()
		upperFile := filepath.Join(tempDir, "src", "OrderService.cpp")
		if err := os.MkdirAll(filepath.Dir(upperFile), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(upperFile, []byte("void process() { int* p = nullptr; *p = 1; }\n"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		var headCommit string
		if withGit {
			initGitTestRepo(t, tempDir)
			runGitTestCmd(t, tempDir, "add", ".")
			runGitTestCmd(t, tempDir, "commit", "-m", "initial")
			headCommit = strings.TrimSpace(runGitTestCmd(t, tempDir, "rev-parse", "HEAD"))
		}

		report1 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess, Branch: "master", HeadCommit: headCommit}
		if err := db.Create(&report1).Error; err != nil {
			t.Fatalf("create report1: %v", err)
		}
		report2 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess, Branch: "master", HeadCommit: headCommit}
		if err := db.Create(&report2).Error; err != nil {
			t.Fatalf("create report2: %v", err)
		}

		defect := models.Defect{
			RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
			CanonicalFingerprint: "fp-upper-case", IdentityKind: "SLOT",
			NormPath: "src/orderservice.cpp", Severity: "严重", BlobHash: "blob-upper-1",
			FirstReportID: report1.ID, LastSeenReportID: report1.ID, LastMatchedReportID: report1.ID,
			RowVersion: 1,
		}
		if err := db.Create(&defect).Error; err != nil {
			t.Fatalf("create defect: %v", err)
		}

		cov := &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{
				{Path: "src/OrderService.cpp", Hash: "blob-upper-1", Status: coverage.StatusSuccess},
			},
		}
		eval := NewMemoryScopeEvaluator([]models.ScanScopeEntry{
			{NormPath: "src/orderservice.cpp", BlobHash: "blob-upper-1", Outcome: ScopeScanned},
		})
		input := LedgerInput{
			Report: report2, Repo: repo, TaskType: taskType, RepoRoot: tempDir,
			CoverageState: CoverageComplete,
			Coverage:      cov,
			Evaluator:     eval,
			Lifecycle: LifecyclePolicy{
				AutoResolveMissing: true,
				RequireCoverage:    true,
				RequireChange:      true,
				HighRiskSeverities: []string{"致命", "严重"},
				ResolvedRounds:     2,
			},
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			_, txErr := advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, input, time.Now())
			return txErr
		})
		if err != nil {
			t.Fatalf("advanceUnmatchedDefects() error = %v", err)
		}

		var updated models.Defect
		if err := db.First(&updated, defect.ID).Error; err != nil {
			t.Fatalf("reload defect: %v", err)
		}
		if updated.Status != DefectStatusActive {
			t.Fatalf("uppercase existing file defect must stay ACTIVE, got status=%s reason=%q", updated.Status, updated.StatusReason)
		}

		// 即使 Evaluator 为空且仅其他文件在覆盖清单中，也不能把存在的大写路径误判为文件不存在
		covOther := &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{{Path: "src/Other.cpp", Hash: "blob-other-1", Status: coverage.StatusSuccess}},
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			_, txErr := advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, LedgerInput{
				Report: report2, Repo: repo, TaskType: taskType, RepoRoot: tempDir,
				CoverageState: CoverageComplete,
				Coverage:      covOther,
				Lifecycle:     input.Lifecycle,
			}, time.Now())
			return txErr
		})
		if err != nil {
			t.Fatalf("advanceUnmatchedDefects(no evaluator) error = %v", err)
		}
		if err := db.First(&updated, defect.ID).Error; err != nil {
			t.Fatalf("reload defect: %v", err)
		}
		if updated.Status == DefectStatusResolved || updated.StatusReason == "source file does not exist" {
			t.Fatalf("uppercase existing file must never resolve as missing without Evaluator, got status=%s reason=%q", updated.Status, updated.StatusReason)
		}
	}

	t.Run("non-git directory", func(t *testing.T) { runCase(t, false) })
	t.Run("git commit tree", func(t *testing.T) { runCase(t, true) })
}

func TestPersistScanFactsUnchangedUppercaseFileNotAutoClosedThenResolvesOnGitRm(t *testing.T) {
	origDataDir := models.AppConfig.Server.DataDir
	models.AppConfig.Server.DataDir = t.TempDir()
	t.Cleanup(func() {
		models.AppConfig.Server.DataDir = origDataDir
	})
	models.AppConfig.Governance.NormalizeDefaults()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	upperSource := "int calculate(int* ptr) {\n    *ptr = 42;\n    return *ptr;\n}\n"
	keepSource := "int ok() { return 0; }\n"
	if err := os.WriteFile(filepath.Join(root, "src", "OrderService.cpp"), []byte(upperSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "KeepHelper.cpp"), []byte(keepSource), 0644); err != nil {
		t.Fatal(err)
	}
	initGitTestRepo(t, root)
	runGitTestCmd(t, root, "add", ".")
	runGitTestCmd(t, root, "commit", "-m", "commit c1")
	commit1 := strings.TrimSpace(runGitTestCmd(t, root, "rev-parse", "HEAD"))

	db := testdb.SetupIsolatedDB(t, "defectlifecycle_persist_uppercase_e2e",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.User{}, &models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{}, &models.TaskChunkExecution{},
	)
	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git", Branch: "master"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}

	// Round 1: 识别出 src/OrderService.cpp 中的缺陷
	report1 := models.TaskReport{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing,
		Branch: "master", HeadCommit: commit1,
	}
	if err := db.Create(&report1).Error; err != nil {
		t.Fatal(err)
	}
	finding1 := models.AnalysisFinding{
		FilePath: "src/OrderService.cpp", LineNumber: "2", Title: "null pointer deref", Severity: "严重",
		TriggerLine: "*ptr = 42;", ScopeSymbol: "calculate",
	}
	cov1 := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/OrderService.cpp", Hash: "hash-order-1", Status: coverage.StatusSuccess},
			{Path: "src/KeepHelper.cpp", Hash: "hash-keep-1", Status: coverage.StatusSuccess},
		},
	}
	res1, err := PersistScanFacts(ScanInput{
		DB: db, Report: report1, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: []models.AnalysisFinding{finding1}, Coverage: cov1,
	})
	if err != nil {
		t.Fatalf("round 1 PersistScanFacts: %v", err)
	}
	if len(res1.NewDefectIDs) != 1 {
		t.Fatalf("expected 1 new defect, got %+v", res1.NewDefectIDs)
	}
	defectID := res1.NewDefectIDs[0]

	// Round 2: 代码未变化，相同 commit，模拟大模型概率漏报（Findings 为空）
	report2 := models.TaskReport{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing,
		Branch: "master", HeadCommit: commit1,
	}
	if err := db.Create(&report2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := PersistScanFacts(ScanInput{
		DB: db, Report: report2, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: nil, Coverage: cov1,
	}); err != nil {
		t.Fatalf("round 2 PersistScanFacts: %v", err)
	}

	var afterRound2 models.Defect
	if err := db.First(&afterRound2, defectID).Error; err != nil {
		t.Fatal(err)
	}
	if afterRound2.Status != DefectStatusActive {
		t.Fatalf("round 2 unchanged code must keep defect ACTIVE, got status=%s reason=%q", afterRound2.Status, afterRound2.StatusReason)
	}

	// Round 3: 真正 git rm 删除 src/OrderService.cpp 并提交新 commit C2
	runGitTestCmd(t, root, "rm", "src/OrderService.cpp")
	runGitTestCmd(t, root, "commit", "-m", "remove OrderService.cpp")
	commit2 := strings.TrimSpace(runGitTestCmd(t, root, "rev-parse", "HEAD"))

	report3 := models.TaskReport{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing,
		Branch: "master", HeadCommit: commit2,
	}
	if err := db.Create(&report3).Error; err != nil {
		t.Fatal(err)
	}
	cov3 := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/KeepHelper.cpp", Hash: "hash-keep-1", Status: coverage.StatusSuccess},
		},
	}
	if _, err := PersistScanFacts(ScanInput{
		DB: db, Report: report3, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: nil, Coverage: cov3,
	}); err != nil {
		t.Fatalf("round 3 PersistScanFacts: %v", err)
	}

	var afterRound3 models.Defect
	if err := db.First(&afterRound3, defectID).Error; err != nil {
		t.Fatal(err)
	}
	if afterRound3.Status != DefectStatusResolved || afterRound3.StatusReason != "source file does not exist" {
		t.Fatalf("round 3 after git rm expected RESOLVED with 'source file does not exist', got status=%s reason=%q",
			afterRound3.Status, afterRound3.StatusReason)
	}
	if afterRound3.ResolvedReportID == nil || *afterRound3.ResolvedReportID != report3.ID {
		t.Fatalf("expected resolved_report_id=%d, got %v", report3.ID, afterRound3.ResolvedReportID)
	}
}

func TestAdvanceUnmatchedDefectsAutoResolveMissingGuards(t *testing.T) {
	setup := func(t *testing.T, name string) (*gorm.DB, models.Repository, models.TaskType, models.TaskReport, models.TaskReport) {
		t.Helper()
		db := testdb.SetupIsolatedDB(t, name,
			&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
			&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
		)
		repo := models.Repository{Name: "guard-demo", URL: "https://example.com/guard.git", Branch: "master"}
		taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
		if err := db.Create(&repo).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&taskType).Error; err != nil {
			t.Fatal(err)
		}
		r1 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess, Branch: "master", HeadCommit: "commit-111"}
		r2 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess, Branch: "master", HeadCommit: "commit-111"}
		if err := db.Create(&r1).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&r2).Error; err != nil {
			t.Fatal(err)
		}
		return db, repo, taskType, r1, r2
	}

	policy := LifecyclePolicy{
		AutoResolveMissing: true,
		RequireCoverage:    true,
		RequireChange:      true,
		HighRiskSeverities: []string{"致命", "严重"},
		ResolvedRounds:     2,
	}

	t.Run("non-existent RepoRoot must fail closed", func(t *testing.T) {
		db, repo, tt, r1, r2 := setup(t, "guard_bad_root")
		defect := models.Defect{
			RepoID: repo.ID, TaskTypeID: tt.ID, Status: DefectStatusActive,
			CanonicalFingerprint: "fp-1", IdentityKind: "K1", NormPath: "src/a.cpp", Severity: "严重",
			FirstReportID: r1.ID, LastSeenReportID: r1.ID, LastMatchedReportID: r1.ID, RowVersion: 1,
		}
		if err := db.Create(&defect).Error; err != nil {
			t.Fatal(err)
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			_, txErr := advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, LedgerInput{
				Report: r2, Repo: repo, TaskType: tt,
				RepoRoot:  filepath.Join(t.TempDir(), "does-not-exist"),
				Lifecycle: policy,
			}, time.Now())
			return txErr
		})
		if err != nil {
			t.Fatalf("advanceUnmatchedDefects error = %v", err)
		}
		var reload models.Defect
		if err := db.First(&reload, defect.ID).Error; err != nil {
			t.Fatal(err)
		}
		if reload.Status == DefectStatusResolved {
			t.Fatalf("must not resolve when RepoRoot does not exist")
		}
	})

	t.Run("degraded coverage must block auto resolve missing", func(t *testing.T) {
		db, repo, tt, r1, r2 := setup(t, "guard_degraded_cov")
		tempDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tempDir, "other.cpp"), []byte("int x=1;"), 0644); err != nil {
			t.Fatal(err)
		}
		defect := models.Defect{
			RepoID: repo.ID, TaskTypeID: tt.ID, Status: DefectStatusActive,
			CanonicalFingerprint: "fp-2", IdentityKind: "K1", NormPath: "src/missing.cpp", Severity: "严重",
			FirstReportID: r1.ID, LastSeenReportID: r1.ID, LastMatchedReportID: r1.ID, RowVersion: 1,
		}
		if err := db.Create(&defect).Error; err != nil {
			t.Fatal(err)
		}
		degradedCov := &coverage.Coverage{
			PolicyVersion:      "v1",
			PlanReconciliation: &coverage.PlanReconciliation{MissingUnits: []string{"chunk-1"}},
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			_, txErr := advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, LedgerInput{
				Report: r2, Repo: repo, TaskType: tt, RepoRoot: tempDir,
				Coverage: degradedCov, Lifecycle: policy,
			}, time.Now())
			return txErr
		})
		if err != nil {
			t.Fatalf("advanceUnmatchedDefects error = %v", err)
		}
		var reload models.Defect
		if err := db.First(&reload, defect.ID).Error; err != nil {
			t.Fatal(err)
		}
		if reload.Status == DefectStatusResolved {
			t.Fatalf("must not resolve missing during degraded coverage, got %+v", reload)
		}
	})

	t.Run("same HeadCommit as last matched report must block auto resolve missing", func(t *testing.T) {
		db, repo, tt, r1, r2 := setup(t, "guard_same_commit")
		tempDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tempDir, "other.cpp"), []byte("int x=1;"), 0644); err != nil {
			t.Fatal(err)
		}
		defect := models.Defect{
			RepoID: repo.ID, TaskTypeID: tt.ID, Status: DefectStatusActive,
			CanonicalFingerprint: "fp-3", IdentityKind: "K1", NormPath: "src/missing.cpp", Severity: "严重",
			FirstReportID: r1.ID, LastSeenReportID: r1.ID, LastMatchedReportID: r1.ID, RowVersion: 1,
		}
		if err := db.Create(&defect).Error; err != nil {
			t.Fatal(err)
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			_, txErr := advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, LedgerInput{
				Report: r2, Repo: repo, TaskType: tt, RepoRoot: tempDir,
				RepoFiles: &repoFileIndex{source: "fs_walk", files: map[string]bool{"other.cpp": true}},
				Lifecycle: policy,
			}, time.Now())
			return txErr
		})
		if err != nil {
			t.Fatalf("advanceUnmatchedDefects error = %v", err)
		}
		var reload models.Defect
		if err := db.First(&reload, defect.ID).Error; err != nil {
			t.Fatal(err)
		}
		if reload.Status == DefectStatusResolved {
			t.Fatalf("must not auto-resolve missing when HeadCommit equals last matched report's HeadCommit")
		}
	})

	t.Run("file present in Evaluator must block auto resolve missing", func(t *testing.T) {
		db, repo, tt, r1, r2 := setup(t, "guard_evaluator")
		r2.HeadCommit = "commit-222"
		if err := db.Save(&r2).Error; err != nil {
			t.Fatal(err)
		}
		defect := models.Defect{
			RepoID: repo.ID, TaskTypeID: tt.ID, Status: DefectStatusActive,
			CanonicalFingerprint: "fp-4", IdentityKind: "K1", NormPath: "src/planned.cpp", Severity: "严重",
			FirstReportID: r1.ID, LastSeenReportID: r1.ID, LastMatchedReportID: r1.ID, RowVersion: 1,
		}
		if err := db.Create(&defect).Error; err != nil {
			t.Fatal(err)
		}
		eval := NewMemoryScopeEvaluator([]models.ScanScopeEntry{
			{NormPath: "src/planned.cpp", Outcome: ScopeFailed, FailReason: "timeout"},
		})
		err := db.Transaction(func(tx *gorm.DB) error {
			_, txErr := advanceUnmatchedDefects(tx, []models.Defect{defect}, map[uint]bool{}, LedgerInput{
				Report: r2, Repo: repo, TaskType: tt,
				Evaluator: eval,
				RepoFiles: &repoFileIndex{source: "git_tree", files: map[string]bool{"other.cpp": true}},
				Lifecycle: policy,
			}, time.Now())
			return txErr
		})
		if err != nil {
			t.Fatalf("advanceUnmatchedDefects error = %v", err)
		}
		var reload models.Defect
		if err := db.First(&reload, defect.ID).Error; err != nil {
			t.Fatal(err)
		}
		if reload.Status == DefectStatusResolved {
			t.Fatalf("must not auto-resolve missing when Evaluator knows file is in planned scan scope")
		}
	})
}
