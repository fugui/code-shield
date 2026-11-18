package defectlifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/coverage"
)

func TestCompareReports_LedgerSSOTAllBucketsAndManifest(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "rfc006_compare_ssot",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.TaskChunkExecution{},
		&models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	origDataDir := models.AppConfig.Server.DataDir
	tempDataDir := t.TempDir()
	models.AppConfig.Server.DataDir = tempDataDir
	t.Cleanup(func() { models.AppConfig.Server.DataDir = origDataDir })

	origSparse := models.AppConfig.Retention.EnableSparseScope
	sparseEnabled := true
	models.AppConfig.Retention.EnableSparseScope = &sparseEnabled
	t.Cleanup(func() { models.AppConfig.Retention.EnableSparseScope = origSparse })

	repo := models.Repository{Name: "compare-demo", URL: "https://example.com/compare.git", Branch: "main"}
	otherRepo := models.Repository{Name: "other-repo", URL: "https://example.com/other.git", Branch: "main"}
	taskType := models.TaskType{Name: "coredump-risk", DisplayName: "Core Dump Risk", GovernanceMode: models.GovernanceModeFullLedger}
	otherTaskType := models.TaskType{Name: "memory-leak", DisplayName: "Memory Leak", GovernanceMode: models.GovernanceModeFullLedger}
	for _, m := range []any{&repo, &otherRepo, &taskType, &otherTaskType} {
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
	}

	root := t.TempDir()
	hKeep := writeBenchmarkFile(t, root, "src/keep.cpp", "void keep() { *p = 1; }\n")
	hChanged1 := writeBenchmarkFile(t, root, "src/changed.cpp", "void changed() { *c = 1; }\n")
	hUnchanged := writeBenchmarkFile(t, root, "src/unchanged.cpp", "void unchanged() { *u = 1; }\n")
	hGap := writeBenchmarkFile(t, root, "src/gap.cpp", "void gap() { *g = 1; }\n")

	rA := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing, Branch: "main"}
	if err := db.Create(&rA).Error; err != nil {
		t.Fatal(err)
	}
	covA := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/keep.cpp", Hash: hKeep, Status: coverage.StatusSuccess},
			{Path: "src/changed.cpp", Hash: hChanged1, Status: coverage.StatusSuccess},
			{Path: "src/unchanged.cpp", Hash: hUnchanged, Status: coverage.StatusSuccess},
			{Path: "src/gap.cpp", Hash: hGap, Status: coverage.StatusSuccess},
		},
	}
	resA, err := PersistScanFacts(ScanInput{
		DB: db, Report: rA, Repo: repo, RepoRoot: root, TaskType: taskType, Coverage: covA,
		Findings: []models.AnalysisFinding{
			{FilePath: "src/keep.cpp", LineNumber: "1", Category: "空指针解引用", Severity: "严重", Title: "keep npd", TriggerLine: "*p = 1;", ScopeSymbol: "keep"},
			{FilePath: "src/changed.cpp", LineNumber: "1", Category: "空指针解引用", Severity: "严重", Title: "changed npd", TriggerLine: "*c = 1;", ScopeSymbol: "changed"},
			{FilePath: "src/unchanged.cpp", LineNumber: "1", Category: "空指针解引用", Severity: "严重", Title: "unchanged npd", TriggerLine: "*u = 1;", ScopeSymbol: "unchanged"},
			{FilePath: "src/gap.cpp", LineNumber: "1", Category: "空指针解引用", Severity: "严重", Title: "gap npd", TriggerLine: "*g = 1;", ScopeSymbol: "gap"},
		},
	})
	if err != nil {
		t.Fatalf("PersistScanFacts A failed: %v", err)
	}

	// 找到 src/keep.cpp 的 defectID 并制造一条合并后的别名/历史缺陷验证 MERGED 解析链路
	var keepDefect models.Defect
	if err := db.Where("repo_id = ? AND norm_path = ?", repo.ID, "src/keep.cpp").First(&keepDefect).Error; err != nil {
		t.Fatal(err)
	}
	var changedDefect models.Defect
	if err := db.Where("repo_id = ? AND norm_path = ?", repo.ID, "src/changed.cpp").First(&changedDefect).Error; err != nil {
		t.Fatal(err)
	}

	// 插入一条 A 报告里的旧缺陷观测指向待合并源缺陷 mergedSource，验证 CompareReports 解析 merged_into_id
	mergedSource := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusMerged, MergedIntoID: &keepDefect.ID,
		CanonicalFingerprint: "merged-src", IdentityKind: "SLOT", NormPath: "src/keep.cpp",
		DefectClassMajor: ClassNPD, Severity: "严重", FirstReportID: rA.ID, LastSeenReportID: rA.ID, LastMatchedReportID: rA.ID,
	}
	if err := db.Create(&mergedSource).Error; err != nil {
		t.Fatal(err)
	}

	// 第 B 轮：src/changed.cpp 改变；src/unchanged.cpp 未变且无告警（验证从稀疏 gzip manifest 读取完整覆盖！）；
	// src/gap.cpp 未进入覆盖范围（excluded）；src/newfile.cpp 新增问题。
	hChanged2 := writeBenchmarkFile(t, root, "src/changed.cpp", "void changed() { int* c = new int(1); if (c) *c = 1; }\n")
	hNew := writeBenchmarkFile(t, root, "src/newfile.cpp", "void newFn() { *n = 1; }\n")

	rB := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing, Branch: "main"}
	if err := db.Create(&rB).Error; err != nil {
		t.Fatal(err)
	}
	covB := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/keep.cpp", Hash: hKeep, Status: coverage.StatusSuccess},
			{Path: "src/changed.cpp", Hash: hChanged2, Status: coverage.StatusSuccess, DiffTouched: true},
			{Path: "src/unchanged.cpp", Hash: hUnchanged, Status: coverage.StatusSuccess}, // 无变更无告警：Sparse 模式下不会落 DB 行，仅在 gzip manifest 中！
			{Path: "src/gap.cpp", Hash: hGap, Status: coverage.StatusExcluded},
			{Path: "src/newfile.cpp", Hash: hNew, Status: coverage.StatusSuccess, DiffTouched: true},
		},
	}
	resB, err := PersistScanFacts(ScanInput{
		DB: db, Report: rB, Repo: repo, RepoRoot: root, TaskType: taskType, Coverage: covB,
		Findings: []models.AnalysisFinding{
			{FilePath: "src/keep.cpp", LineNumber: "1", Category: "空指针解引用", Severity: "严重", Title: "keep npd", TriggerLine: "*p = 1;", ScopeSymbol: "keep"},
			{FilePath: "src/newfile.cpp", LineNumber: "1", Category: "空指针解引用", Severity: "严重", Title: "new npd", TriggerLine: "*n = 1;", ScopeSymbol: "newFn"},
		},
	})
	if err != nil {
		t.Fatalf("PersistScanFacts B failed: %v", err)
	}
	_ = resA

	// 确认 Sparse 确实跳过落库 src/unchanged.cpp DB 行，而 gzip manifest 文件存在
	var unchangedDBRows int64
	if err := db.Model(&models.ScanScopeEntry{}).Where("report_id = ? AND norm_path = ?", rB.ID, "src/unchanged.cpp").Count(&unchangedDBRows).Error; err != nil {
		t.Fatal(err)
	}
	if unchangedDBRows != 0 {
		t.Fatalf("expected sparse mode to omit clean DB row for src/unchanged.cpp, got %d", unchangedDBRows)
	}
	if _, statErr := os.Stat(rB.GetScopeManifestPath()); statErr != nil {
		t.Fatalf("expected gzip scope manifest file at %s: %v", rB.GetScopeManifestPath(), statErr)
	}

	// 将 A 中的一条额外观测改为指向 mergedSource（-> keepDefect.ID），测试 MERGED 去重归一
	if err := db.Model(&models.DefectObservation{}).
		Where("report_id = ? AND defect_id = ?", rA.ID, keepDefect.ID).
		Update("defect_id", mergedSource.ID).Error; err != nil {
		t.Fatal(err)
	}

	cmp, err := CompareReports(db, rA.ID, rB.ID)
	if err != nil {
		t.Fatalf("CompareReports failed: %v", err)
	}
	if cmp.DataSource != CompareDataSourceLedger {
		t.Fatalf("expected data_source=%s, got %s", CompareDataSourceLedger, cmp.DataSource)
	}
	if cmp.Summary.CommonPersisted != 1 || cmp.Summary.OnlyACodeChanged != 1 ||
		cmp.Summary.OnlyACodeUnchanged != 1 || cmp.Summary.OnlyANotCovered != 1 || cmp.Summary.OnlyBNew != 1 {
		t.Fatalf("unexpected compare summary counts: %+v", cmp.Summary)
	}
	if cmp.CommonPersisted[0].DefectID == nil || *cmp.CommonPersisted[0].DefectID != keepDefect.ID {
		t.Fatalf("expected merged chain resolved to keepDefect %d in COMMON_PERSISTED, got %+v", keepDefect.ID, cmp.CommonPersisted)
	}
	if cmp.OnlyACodeChanged[0].NormPath != "src/changed.cpp" || *cmp.OnlyACodeChanged[0].DefectID != changedDefect.ID {
		t.Fatalf("unexpected ONLY_A_CODE_CHANGED: %+v", cmp.OnlyACodeChanged)
	}
	if cmp.OnlyACodeUnchanged[0].NormPath != "src/unchanged.cpp" {
		t.Fatalf("expected src/unchanged.cpp in ONLY_A_CODE_UNCHANGED via gzip manifest, got %+v", cmp.OnlyACodeUnchanged)
	}
	if cmp.OnlyANotCovered[0].NormPath != "src/gap.cpp" {
		t.Fatalf("expected src/gap.cpp in ONLY_A_NOT_COVERED, got %+v", cmp.OnlyANotCovered)
	}
	if cmp.OnlyBNew[0].NormPath != "src/newfile.cpp" || cmp.OnlyBNew[0].DefectID == nil {
		t.Fatalf("unexpected ONLY_B_NEW: %+v", cmp.OnlyBNew)
	}

	// 验证 SSOT 同源一致性：CompareReports 中属于 B 的缺陷 ID 集合与 GetReportReconciliation(B) 100% 一致
	reconB, err := GetReportReconciliation(db, rB.ID)
	if err != nil {
		t.Fatal(err)
	}
	reconBIDs := map[uint]bool{}
	for _, obs := range reconB.Observations {
		if obs.DefectID != nil {
			reconBIDs[*obs.DefectID] = true
		}
	}
	cmpBIDs := map[uint]bool{}
	for _, item := range cmp.CommonPersisted {
		if item.DefectID != nil {
			cmpBIDs[*item.DefectID] = true
		}
	}
	for _, item := range cmp.OnlyBNew {
		if item.DefectID != nil {
			cmpBIDs[*item.DefectID] = true
		}
	}
	if len(reconBIDs) != len(cmpBIDs) {
		t.Fatalf("SSOT mismatch between CompareReports and GetReportReconciliation: cmp=%v recon=%v", cmpBIDs, reconBIDs)
	}
	for id := range reconBIDs {
		if !cmpBIDs[id] {
			t.Fatalf("defect %d present in GetReportReconciliation but missing from CompareReports", id)
		}
	}
	_ = resB

	// 验证跨仓库 / 跨任务类型校验错误
	rOtherRepo := models.TaskReport{RepoID: otherRepo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&rOtherRepo).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := CompareReports(db, rA.ID, rOtherRepo.ID); !errors.Is(err, ErrCompareRepoMismatch) {
		t.Fatalf("expected ErrCompareRepoMismatch, got %v", err)
	}
	rOtherType := models.TaskReport{RepoID: repo.ID, TaskTypeID: otherTaskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&rOtherType).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := CompareReports(db, rA.ID, rOtherType.ID); !errors.Is(err, ErrCompareTaskTypeMismatch) {
		t.Fatalf("expected ErrCompareTaskTypeMismatch, got %v", err)
	}
	if _, err := CompareReports(db, 0, rB.ID); !errors.Is(err, ErrCompareInvalidArgs) {
		t.Fatalf("expected ErrCompareInvalidArgs, got %v", err)
	}
	if _, err := CompareReports(db, rA.ID, 999999); !errors.Is(err, ErrCompareReportNotFound) {
		t.Fatalf("expected ErrCompareReportNotFound, got %v", err)
	}
	_ = filepath.Base
}

func TestCompareReports_EntityAssessmentFallback(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "rfc006_compare_entity_mode",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.TaskChunkExecution{},
		&models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "ut-compare", URL: "https://example.com/ut-compare.git"}
	tt := models.TaskType{Name: "ut-effectiveness", DisplayName: "单测有效性", GovernanceMode: models.GovernanceModeEntityAssessment}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&tt).Error; err != nil {
		t.Fatal(err)
	}
	rA := models.TaskReport{RepoID: repo.ID, TaskTypeID: tt.ID, Status: models.StatusAnalyzing}
	rB := models.TaskReport{RepoID: repo.ID, TaskTypeID: tt.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&rA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&rB).Error; err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	_, err := PersistScanFacts(ScanInput{
		DB: db, Report: rA, Repo: repo, RepoRoot: root, TaskType: tt,
		Findings: []models.AnalysisFinding{
			{FilePath: "tests/a_test.cpp", LineNumber: "10", Title: "空测试 A", Severity: "严重", Category: "断言有效性-空测试", TriggerLine: "TEST_F(S, A) {}", ScopeSymbol: "TEST_F"},
			{FilePath: "tests/b_test.cpp", LineNumber: "20", Title: "永真断言 B", Severity: "一般", Category: "断言有效性-永真断言", TriggerLine: "EXPECT_TRUE(true);", ScopeSymbol: "TEST_F"},
			{FilePath: "tests/c_test.cpp", LineNumber: "30", Title: "有效测试 C", Severity: "合格", Category: "无问题", AssessmentStatus: "pass", TriggerLine: "EXPECT_EQ(x, 1);"},
		},
		Coverage: &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{
				{Path: "tests/a_test.cpp", Hash: "h1", Status: coverage.StatusSuccess},
				{Path: "tests/b_test.cpp", Hash: "h2", Status: coverage.StatusSuccess},
				{Path: "tests/c_test.cpp", Hash: "h3", Status: coverage.StatusSuccess},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = PersistScanFacts(ScanInput{
		DB: db, Report: rB, Repo: repo, RepoRoot: root, TaskType: tt,
		Findings: []models.AnalysisFinding{
			{FilePath: "tests/a_test.cpp", LineNumber: "10", Title: "空测试 A", Severity: "严重", Category: "断言有效性-空测试", TriggerLine: "TEST_F(S, A) {}", ScopeSymbol: "TEST_F"},
			{FilePath: "tests/d_test.cpp", LineNumber: "15", Title: "静默跳过 D", Severity: "严重", Category: "提前返回-静默跳过", TriggerLine: "return;", ScopeSymbol: "TEST_F"},
		},
		Coverage: &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{
				{Path: "tests/a_test.cpp", Hash: "h1", Status: coverage.StatusSuccess},
				{Path: "tests/b_test.cpp", Hash: "h2-changed", Status: coverage.StatusSuccess, DiffTouched: true},
				{Path: "tests/d_test.cpp", Hash: "h4", Status: coverage.StatusSuccess, DiffTouched: true},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cmp, err := CompareReports(db, rA.ID, rB.ID)
	if err != nil {
		t.Fatalf("CompareReports entity_assessment failed: %v", err)
	}
	if cmp.DataSource != CompareDataSourceEntityAssessment {
		t.Fatalf("expected data_source=%s, got %s", CompareDataSourceEntityAssessment, cmp.DataSource)
	}
	if cmp.Summary.CommonPersisted != 1 || cmp.Summary.OnlyACodeChanged != 1 || cmp.Summary.OnlyBNew != 1 {
		t.Fatalf("unexpected entity compare summary: %+v", cmp.Summary)
	}

	// 回归：B 报告前置插入额外条目引发全局索引偏移时，compareByEntitySlots 不得因全局下标错位
	rBShift := models.TaskReport{RepoID: repo.ID, TaskTypeID: tt.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&rBShift).Error; err != nil {
		t.Fatal(err)
	}
	_, err = PersistScanFacts(ScanInput{
		DB: db, Report: rBShift, Repo: repo, RepoRoot: root, TaskType: tt,
		Findings: []models.AnalysisFinding{
			{FilePath: "tests/d_test.cpp", LineNumber: "15", Title: "前置新增项", Severity: "严重", Category: "提前返回-静默跳过", TriggerLine: "return;", ScopeSymbol: "TEST_F"},
			{FilePath: "tests/a_test.cpp", LineNumber: "18", Title: "空测试 A", Severity: "严重", Category: "断言有效性-空测试", TriggerLine: "TEST_F(S, A) {}", ScopeSymbol: "TEST_F"},
			{FilePath: "tests/b_test.cpp", LineNumber: "20", Title: "永真断言 B", Severity: "一般", Category: "断言有效性-永真断言", TriggerLine: "EXPECT_TRUE(true);", ScopeSymbol: "TEST_F"},
			{FilePath: "tests/c_test.cpp", LineNumber: "31", Title: "退化告警 C", Severity: "严重", Category: "提前返回-静默跳过", TriggerLine: "return;", ScopeSymbol: "TEST_F"},
		},
		Coverage: &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{
				{Path: "tests/a_test.cpp", Hash: "h1", Status: coverage.StatusSuccess},
				{Path: "tests/b_test.cpp", Hash: "h2", Status: coverage.StatusSuccess},
				{Path: "tests/c_test.cpp", Hash: "h3-new", Status: coverage.StatusSuccess, DiffTouched: true},
				{Path: "tests/d_test.cpp", Hash: "h4", Status: coverage.StatusSuccess, DiffTouched: true},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmpShift, err := CompareReports(db, rA.ID, rBShift.ID)
	if err != nil {
		t.Fatalf("CompareReports entity shifted failed: %v", err)
	}
	if cmpShift.Summary.CommonPersisted != 2 || cmpShift.Summary.OnlyBNew != 2 {
		t.Fatalf("expected CommonPersisted=2 OnlyBNew=2 after index shift, got %+v", cmpShift.Summary)
	}
	var foundA bool
	for _, item := range cmpShift.CommonPersisted {
		if item.NormPath == "tests/a_test.cpp" {
			foundA = true
			if item.BaseLineStart == nil || *item.BaseLineStart != 10 || item.LineStart == nil || *item.LineStart != 18 {
				t.Fatalf("expected BaseLineStart=10 & LineStart=18 preserved on COMMON_PERSISTED, got base=%v target=%v", item.BaseLineStart, item.LineStart)
			}
		}
	}
	if !foundA {
		t.Fatalf("expected tests/a_test.cpp in CommonPersisted")
	}
	// 验证 ONLY_B_NEW 在已有文件上也能从 scopeA 带出 base_blob_hash
	var foundCNew bool
	for _, item := range cmpShift.OnlyBNew {
		if item.NormPath == "tests/c_test.cpp" {
			foundCNew = true
			if item.BaseBlobHash != "h3" || item.TargetBlobHash != "h3-new" {
				t.Fatalf("expected ONLY_B_NEW c_test base=h3 target=h3-new, got base=%q target=%q", item.BaseBlobHash, item.TargetBlobHash)
			}
		}
	}
	if !foundCNew {
		t.Fatalf("expected tests/c_test.cpp in OnlyBNew, got %+v", cmpShift.OnlyBNew)
	}

	// 验证 RecordSlotAliasSelfHealing 使用 AliasStrong 类别
	healedDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: tt.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fp-alias-strong", IdentityKind: "SLOT",
		NormPath: "src/alias.cpp", CoreSlotKey: "core-a", SlotKey: "slot-new-a",
		FirstReportID: rA.ID, LastSeenReportID: rA.ID, LastMatchedReportID: rA.ID, RowVersion: 1,
	}
	if err := db.Create(&healedDefect).Error; err != nil {
		t.Fatal(err)
	}
	if err := RecordSlotAliasSelfHealing(db, healedDefect.ID, repo.ID, tt.ID, rBShift.ID, "legacy-slot-1", AliasSlotHealed); err != nil {
		t.Fatalf("RecordSlotAliasSelfHealing failed: %v", err)
	}
	var aliasRec models.DefectAlias
	if err := db.Where("alias_value = ?", "legacy-slot-1").First(&aliasRec).Error; err != nil {
		t.Fatal(err)
	}
	if aliasRec.AliasClass != AliasStrong {
		t.Fatalf("expected AliasClass=%s, got %q", AliasStrong, aliasRec.AliasClass)
	}
}
