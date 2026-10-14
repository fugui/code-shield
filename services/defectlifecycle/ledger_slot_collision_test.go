package defectlifecycle

import (
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestUpdateMatchedDefectAutoMergesOnActiveSlotConflict(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_slot_collision",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	if err := models.EnsureDefectSlotIndexes(db); err != nil {
		t.Fatalf("EnsureDefectSlotIndexes failed: %v", err)
	}

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

	slotKey := "slot-pecstate-collision-123"
	// 1. 已有一个处于 ACTIVE 状态的活跃缺陷占用该槽位
	activeDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: slotKey, SlotKey: slotKey, CoreSlotKey: "core-slot-1",
		NormPath: "test/ut/pecstatetest.cpp", DefectClassMajor: "断言有效性-空测试",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	if err := db.Create(&activeDefect).Error; err != nil {
		t.Fatalf("create active defect: %v", err)
	}

	// 2. 数据库中有一个历史已修复 (RESOLVED) 的旧缺陷也带有该槽位
	resolvedDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusResolved,
		CanonicalFingerprint: slotKey, SlotKey: slotKey, CoreSlotKey: "core-slot-1",
		NormPath: "test/ut/pecstatetest.cpp", DefectClassMajor: "断言有效性-空测试",
		FirstReportID: report.ID - 1, LastSeenReportID: report.ID - 1, LastMatchedReportID: report.ID - 1,
	}
	if err := db.Create(&resolvedDefect).Error; err != nil {
		t.Fatalf("create resolved defect: %v", err)
	}

	// 3. 构造一条匹配到了 resolvedDefect 的观测，裁决意图为 REOPENED
	obsDefectID := resolvedDefect.ID
	observation := &models.DefectObservation{
		ReportID:            report.ID,
		RepoID:              repo.ID,
		TaskTypeID:          taskType.ID,
		ObservationGroupUID: "obs-group-1",
		DefectID:            &obsDefectID,
		Verdict:             VerdictReopened,
		MatchTier:           MatchAssign,
		Reason:              "GLOBAL_ASSIGNMENT",
	}

	input := LedgerInput{
		Report: report, Repo: repo, TaskType: taskType,
	}
	identity := Identity{
		NormPath:         resolvedDefect.NormPath,
		DefectClassMajor: resolvedDefect.DefectClassMajor,
		SlotKey:          slotKey,
	}

	now := time.Now()
	// 执行 updateMatchedDefect
	updated, event, changed, err := updateMatchedDefect(db, resolvedDefect, identity, input, observation, now)
	if err != nil {
		t.Fatalf("updateMatchedDefect should not fail with unique constraint error, got: %v", err)
	}

	// 验证自愈逻辑：resolvedDefect 应当被自动 MERGED 进 activeDefect，而非强行更新为 ACTIVE 导致冲突
	if updated.Status != DefectStatusMerged {
		t.Errorf("expected updated status %s, got %s", DefectStatusMerged, updated.Status)
	}
	if updated.MergedIntoID == nil || *updated.MergedIntoID != activeDefect.ID {
		t.Errorf("expected merged_into_id %d, got %v", activeDefect.ID, updated.MergedIntoID)
	}
	if observation.DefectID == nil || *observation.DefectID != activeDefect.ID {
		t.Errorf("expected observation defect_id redirected to %d, got %v", activeDefect.ID, observation.DefectID)
	}
	if !changed {
		t.Errorf("expected changed to be true")
	}
	if event.ToStatus != DefectStatusMerged {
		t.Errorf("expected event ToStatus %s, got %s", DefectStatusMerged, event.ToStatus)
	}

	// 从数据库重新加载验证
	var reloaded models.Defect
	if err := db.First(&reloaded, resolvedDefect.ID).Error; err != nil {
		t.Fatalf("reload defect from db: %v", err)
	}
	if reloaded.Status != DefectStatusMerged || reloaded.MergedIntoID == nil || *reloaded.MergedIntoID != activeDefect.ID {
		t.Errorf("db state invalid: status=%s, merged_into_id=%v", reloaded.Status, reloaded.MergedIntoID)
	}
}

func TestBindStrongAliasesAssignsEachDefectOnce(t *testing.T) {
	candDefect := models.Defect{ID: 100, SlotKey: "slot-100", Status: DefectStatusActive}
	cand := Candidate{Defect: candDefect, Sources: map[string]struct{}{AliasSlotPrecise: {}}}
	scored := scoredCandidate{candidate: cand, score: 0.95, detail: map[string]float64{}}

	edges := [][]scoredCandidate{
		{scored},
		{scored},
	}
	decisions := []ObservationDecision{
		{ObservationGroupUID: "obs-1"},
		{ObservationGroupUID: "obs-2"},
	}

	bindStrongAliases(edges, decisions, MatchThresholds{StrongSame: 0.90})

	if decisions[0].DefectID == nil || *decisions[0].DefectID != 100 {
		t.Fatalf("expected decision 0 to claim defect 100, got %v", decisions[0].DefectID)
	}
	if decisions[1].DefectID != nil {
		t.Fatalf("expected decision 1 to NOT claim defect 100, got %v", *decisions[1].DefectID)
	}
}

func TestCreateDefectReusesActiveSlotDefect(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_create_slot_reuse",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	if err := models.EnsureDefectSlotIndexes(db); err != nil {
		t.Fatalf("EnsureDefectSlotIndexes failed: %v", err)
	}

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	_ = db.Create(&repo)
	_ = db.Create(&taskType)
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	_ = db.Create(&report)

	slotKey := "slot-create-reuse-999"
	existing := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: slotKey, SlotKey: slotKey,
		NormPath: "src/a.cpp", DefectClassMajor: "SEC_INJECT",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	_ = db.Create(&existing)

	identity := Identity{
		NormPath:         "src/a.cpp",
		DefectClassMajor: "SEC_INJECT",
		SlotKey:          slotKey,
	}
	input := LedgerInput{Report: report, Repo: repo, TaskType: taskType}

	created, err := createDefect(db, input, identity, nil)
	if err != nil {
		t.Fatalf("createDefect failed: %v", err)
	}
	if created.ID != existing.ID {
		t.Fatalf("expected createDefect to reuse existing defect %d, got %d", existing.ID, created.ID)
	}
}
