package defectlifecycle

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/coverage"

	"gorm.io/gorm"
)

func setupBenchmarkEnv(t *testing.T, name string) (*gorm.DB, models.Repository, models.TaskType, string) {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, name,
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.TaskChunkExecution{},
		&models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: name + "-repo", URL: "https://example.com/" + name + ".git"}
	taskType := models.TaskType{Name: "coredump-risk", DisplayName: "Core Dump Risk", GovernanceMode: models.GovernanceModeFullLedger}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	root := t.TempDir()
	return db, repo, taskType, root
}

func writeBenchmarkFile(t *testing.T, root, relPath, content string) string {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", absPath, err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", absPath, err)
	}
	hash, err := ComputeFileSHA256(absPath)
	if err != nil {
		t.Fatalf("hash %s: %v", absPath, err)
	}
	return hash
}

func createBenchmarkReport(t *testing.T, db *gorm.DB, repoID, taskTypeID uint) models.TaskReport {
	t.Helper()
	report := models.TaskReport{
		RepoID:     repoID,
		TaskTypeID: taskTypeID,
		Status:     models.StatusAnalyzing,
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	return report
}

// TestGoldenBenchmarkA_ZeroChangeStability
// 基准 A：代码完全零变更，连续 10 轮随机丢弃 20% 告警的稳定度验证
func TestGoldenBenchmarkA_ZeroChangeStability(t *testing.T) {
	db, repo, taskType, root := setupBenchmarkEnv(t, "rfc006_bench_a")

	hashA := writeBenchmarkFile(t, root, "src/alpha.cpp", "void fnAlpha(int* ptrA) {\n  *ptrA = 1;\n}\n")
	hashB := writeBenchmarkFile(t, root, "src/beta.cpp", "void fnBeta(char* bufB) {\n  free(bufB);\n  bufB[0] = 'x';\n}\n")
	hashC := writeBenchmarkFile(t, root, "src/gamma.cpp", "void fnGamma(FILE* fp) {\n  if (!fp) return;\n  int arr[4];\n  arr[4] = 9;\n}\n")

	baseFindings := []models.AnalysisFinding{
		{FilePath: "src/alpha.cpp", LineNumber: "2", Category: "内存管理问题-空指针解引用", Severity: "致命", Title: "NPD alpha", TriggerLine: "*ptrA = 1;", ScopeSymbol: "fnAlpha"},
		{FilePath: "src/beta.cpp", LineNumber: "3", Category: "内存管理问题-释放后使用", Severity: "严重", Title: "UAF beta", TriggerLine: "bufB[0] = 'x';", ScopeSymbol: "fnBeta"},
		{FilePath: "src/gamma.cpp", LineNumber: "4", Category: "内存管理问题-越界访问/缓冲区溢出", Severity: "一般", Title: "OOB gamma", TriggerLine: "arr[4] = 9;", ScopeSymbol: "fnGamma"},
		{FilePath: "src/alpha.cpp", LineNumber: "1", Category: "逻辑与正确性-边界处理", Severity: "建议", Title: "Check alpha", TriggerLine: "void fnAlpha(int* ptrA) {", ScopeSymbol: "fnAlpha"},
		{FilePath: "src/beta.cpp", LineNumber: "2", Category: "内存管理问题-双重释放", Severity: "严重", Title: "DF beta", TriggerLine: "free(bufB);", ScopeSymbol: "fnBeta"},
	}

	cov := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/alpha.cpp", Hash: hashA, Status: coverage.StatusSuccess},
			{Path: "src/beta.cpp", Hash: hashB, Status: coverage.StatusSuccess},
			{Path: "src/gamma.cpp", Hash: hashC, Status: coverage.StatusSuccess},
		},
	}

	// 第 1 轮基线建立 5 个缺陷
	r1 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
	if _, err := PersistScanFacts(ScanInput{
		DB: db, Report: r1, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: append([]models.AnalysisFinding(nil), baseFindings...), Coverage: cov,
	}); err != nil {
		t.Fatalf("round 1 PersistScanFacts failed: %v", err)
	}
	var baselineDefects []models.Defect
	if err := db.Where("repo_id = ?", repo.ID).Order("id").Find(&baselineDefects).Error; err != nil {
		t.Fatalf("query baseline defects: %v", err)
	}
	if len(baselineDefects) != 5 {
		t.Fatalf("expected 5 baseline defects, got %d", len(baselineDefects))
	}
	baselineIDs := make(map[uint]bool, 5)
	for _, d := range baselineDefects {
		baselineIDs[d.ID] = true
	}

	// 第 2~11 轮（连续 10 轮），代码一行未动，随机丢弃 20% 发现（每轮随机移除 1 条）
	rng := rand.New(rand.NewSource(20261117))
	for round := 2; round <= 11; round++ {
		dropIdx := rng.Intn(len(baseFindings))
		subset := make([]models.AnalysisFinding, 0, len(baseFindings)-1)
		for idx, f := range baseFindings {
			if idx != dropIdx {
				subset = append(subset, f)
			}
		}
		rep := createBenchmarkReport(t, db, repo.ID, taskType.ID)
		res, err := PersistScanFacts(ScanInput{
			DB: db, Report: rep, Repo: repo, RepoRoot: root, TaskType: taskType,
			Findings: subset, Coverage: cov,
		})
		if err != nil {
			t.Fatalf("round %d PersistScanFacts failed: %v", round, err)
		}
		for _, obs := range res.Observations {
			if obs.DefectID == nil || !baselineIDs[*obs.DefectID] {
				t.Fatalf("round %d unexpected new/drifted defect in obs=%+v", round, obs)
			}
			if obs.Verdict != VerdictExisted {
				t.Fatalf("round %d expected EXISTED verdict, got %s (reason=%s)", round, obs.Verdict, obs.Reason)
			}
		}
	}

	// 验证连续 10 轮抖动后，0 条 RESOLVED/VERIFIED_PENDING/DORMANT/OBSOLETE，总数保持 5 条
	var finalDefects []models.Defect
	if err := db.Where("repo_id = ?", repo.ID).Order("id").Find(&finalDefects).Error; err != nil {
		t.Fatalf("query final defects: %v", err)
	}
	if len(finalDefects) != 5 {
		t.Fatalf("expected defect count to remain 5 after 10 flaky rounds, got %d", len(finalDefects))
	}
	for _, d := range finalDefects {
		if d.Status != DefectStatusActive {
			t.Fatalf("unchanged code defect %d decayed unexpectedly to status=%s missed=%d", d.ID, d.Status, d.MissedCount)
		}
	}

	// 第 12 轮全量回归，100% 匹配原 5 个 DefectID 且均为 EXISTED
	r12 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
	res12, err := PersistScanFacts(ScanInput{
		DB: db, Report: r12, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: append([]models.AnalysisFinding(nil), baseFindings...), Coverage: cov,
	})
	if err != nil {
		t.Fatalf("round 12 PersistScanFacts failed: %v", err)
	}
	for _, obs := range res12.Observations {
		if obs.Verdict != VerdictExisted || obs.DefectID == nil || !baselineIDs[*obs.DefectID] {
			t.Fatalf("round 12 expected 100%% EXISTED matched to original IDs, got %+v", obs)
		}
	}
}

// TestGoldenBenchmarkB_CodeEvolutionAnchoring
// 基准 B：代码增删位移 (+15行) + 函数内无关修改 + 多行表达式拆行后的跨轮锚定
func TestGoldenBenchmarkB_CodeEvolutionAnchoring(t *testing.T) {
	t.Run("ShiftAndInFunctionEdit", func(t *testing.T) {
		db, repo, taskType, root := setupBenchmarkEnv(t, "rfc006_bench_b_shift")
		src1 := `void ProcessRequest(Session* pSession) {
    if (!pSession) return;
    pSession->Release();
}
`
		hash1 := writeBenchmarkFile(t, root, "src/process.cpp", src1)
		r1 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
		res1, err := PersistScanFacts(ScanInput{
			DB: db, Report: r1, Repo: repo, RepoRoot: root, TaskType: taskType,
			Findings: []models.AnalysisFinding{{
				FilePath: "src/process.cpp", LineNumber: "3", Category: "内存管理问题-释放后使用",
				Severity: "严重", Title: "Release without reset", TriggerLine: "pSession->Release();", ScopeSymbol: "ProcessRequest",
			}},
			Coverage: &coverage.Coverage{
				PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
				Files: []coverage.File{{Path: "src/process.cpp", Hash: hash1, Status: coverage.StatusSuccess}},
			},
		})
		if err != nil || len(res1.Observations) != 1 || res1.Observations[0].DefectID == nil {
			t.Fatalf("round 1 failed: err=%v obs=%+v", err, res1)
		}
		origDefectID := *res1.Observations[0].DefectID

		// 第 2 轮：上方插入 15 行无关函数，并在 ProcessRequest 内触发行之前插入 3 行无关日志与变量
		var sb strings.Builder
		sb.WriteString("// Header comment\n")
		for i := 1; i <= 15; i++ {
			sb.WriteString(fmt.Sprintf("void Helper_%d() {}\n", i))
		}
		sb.WriteString(`void ProcessRequest(Session* pSession) {
    if (!pSession) return;
    int auditFlag = 1;
    auditFlag++;
    // extra comment
    pSession->Release();
}
`)
		hash2 := writeBenchmarkFile(t, root, "src/process.cpp", sb.String())
		r2 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
		res2, err := PersistScanFacts(ScanInput{
			DB: db, Report: r2, Repo: repo, RepoRoot: root, TaskType: taskType,
			Findings: []models.AnalysisFinding{{
				FilePath: "src/process.cpp", LineNumber: "22", Category: "内存管理问题-释放后使用",
				Severity: "严重", Title: "Release without reset renamed", TriggerLine: "pSession->Release();", ScopeSymbol: "ProcessRequest",
			}},
			Coverage: &coverage.Coverage{
				PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
				Files: []coverage.File{{Path: "src/process.cpp", Hash: hash2, Status: coverage.StatusSuccess}},
			},
		})
		if err != nil || len(res2.Observations) != 1 || res2.Observations[0].DefectID == nil {
			t.Fatalf("round 2 failed: err=%v obs=%+v", err, res2)
		}
		if *res2.Observations[0].DefectID != origDefectID {
			t.Fatalf("expected original defect %d across +15 line shift & in-function edits, got %d", origDefectID, *res2.Observations[0].DefectID)
		}
	})

	t.Run("MultilineWrapAndAdjacentEdit", func(t *testing.T) {
		// P1 目标 TDD 基准：+15行位移 + 多行表达式拆行 + 紧邻下方函数修改 + 同源归约分类变化
		db, repo, taskType, root := setupBenchmarkEnv(t, "rfc006_bench_b_multiline")
		src1 := `void Dispatch(Session* reqSession) {
    if (reqSession != nullptr && reqSession->GetReq() != nullptr) { reqSession->Execute(); }
}

void NeighborLower() {
    int unchanged = 1;
}
`
		hash1 := writeBenchmarkFile(t, root, "src/dispatch.cpp", src1)
		r1 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
		res1, err := PersistScanFacts(ScanInput{
			DB: db, Report: r1, Repo: repo, RepoRoot: root, TaskType: taskType,
			Findings: []models.AnalysisFinding{{
				FilePath: "src/dispatch.cpp", LineNumber: "2", Category: "空指针异常",
				Severity: "严重", Title: "reqSession check",
				TriggerLine: "if (reqSession != nullptr && reqSession->GetReq() != nullptr) { reqSession->Execute(); }",
				ScopeSymbol: "Dispatch", TargetSymbol: "reqSession",
			}},
			Coverage: &coverage.Coverage{
				PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
				Files: []coverage.File{{Path: "src/dispatch.cpp", Hash: hash1, Status: coverage.StatusSuccess}},
			},
		})
		if err != nil || len(res1.Observations) != 1 || res1.Observations[0].DefectID == nil {
			t.Fatalf("round 1 failed: err=%v", err)
		}
		origDefectID := *res1.Observations[0].DefectID

		// 第 2 轮：上方插入 15 行无关声明；条件拆分为多行；LLM 输出英文分类 & 仅给出拆行首行 TriggerLine
		var sb strings.Builder
		for i := 1; i <= 15; i++ {
			sb.WriteString(fmt.Sprintf("int pad_%d = %d;\n", i, i))
		}
		sb.WriteString(`void Dispatch(Session* reqSession) {
    if (reqSession != nullptr &&
        reqSession->GetReq() != nullptr) {
        reqSession->Execute();
    }
}

void NeighborLower() {
    int changedCompletely = 42;
    changedCompletely += 1;
}
`)
		hash2 := writeBenchmarkFile(t, root, "src/dispatch.cpp", sb.String())
		r2 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
		res2, err := PersistScanFacts(ScanInput{
			DB: db, Report: r2, Repo: repo, RepoRoot: root, TaskType: taskType,
			Findings: []models.AnalysisFinding{{
				FilePath: "src/dispatch.cpp", LineNumber: "17", Category: "CWE-476 Null Pointer",
				Severity: "严重", Title: "Null dereference on reqSession",
				TriggerLine: "if (reqSession != nullptr &&",
				ScopeSymbol: "Dispatch", TargetSymbol: "reqSession",
			}},
			Coverage: &coverage.Coverage{
				PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
				Files: []coverage.File{{Path: "src/dispatch.cpp", Hash: hash2, Status: coverage.StatusSuccess}},
			},
		})
		if err != nil || len(res2.Observations) != 1 || res2.Observations[0].DefectID == nil {
			t.Fatalf("round 2 failed: err=%v", err)
		}
		if *res2.Observations[0].DefectID != origDefectID {
			t.Fatalf("expected original defect %d after multiline wrap + shift + category synonym, got %d (verdict=%s reason=%s)",
				origDefectID, *res2.Observations[0].DefectID, res2.Observations[0].Verdict, res2.Observations[0].Reason)
		}
	})
}

// TestGoldenBenchmarkC_SameFunctionMultiInstance
// 基准 C：同文件同函数多实例精准区分（含相邻行不同指针 ptrA/ptrB 与短变量名 fd/buf）
func TestGoldenBenchmarkC_SameFunctionMultiInstance(t *testing.T) {
	db, repo, taskType, root := setupBenchmarkEnv(t, "rfc006_bench_c")
	src1 := `void multiLeakAndNPD(int* ptrA, int* ptrB, int fd, char* buf) {
    *ptrA = 1;
    *ptrB = 2;
    releaseHandle(fd);
    releaseHandle(buf);
}
`
	hash1 := writeBenchmarkFile(t, root, "src/multi.cpp", src1)
	findings1 := []models.AnalysisFinding{
		{FilePath: "src/multi.cpp", LineNumber: "2", Category: "内存管理问题-空指针解引用", Severity: "严重", Title: "ptrA npd", TriggerLine: "*ptrA = 1;", ScopeSymbol: "multiLeakAndNPD"},
		{FilePath: "src/multi.cpp", LineNumber: "3", Category: "内存管理问题-空指针解引用", Severity: "严重", Title: "ptrB npd", TriggerLine: "*ptrB = 2;", ScopeSymbol: "multiLeakAndNPD"},
		{FilePath: "src/multi.cpp", LineNumber: "4", Category: "性能与运行效率-资源未释放", Severity: "严重", Title: "fd leak", TriggerLine: "releaseHandle(fd);", ScopeSymbol: "multiLeakAndNPD", TargetSymbol: "fd"},
		{FilePath: "src/multi.cpp", LineNumber: "5", Category: "性能与运行效率-资源未释放", Severity: "严重", Title: "buf leak", TriggerLine: "releaseHandle(buf);", ScopeSymbol: "multiLeakAndNPD", TargetSymbol: "buf"},
	}

	cov1 := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{{Path: "src/multi.cpp", Hash: hash1, Status: coverage.StatusSuccess}},
	}
	r1 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
	res1, err := PersistScanFacts(ScanInput{
		DB: db, Report: r1, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: findings1, Coverage: cov1,
	})
	if err != nil {
		t.Fatalf("round 1 PersistScanFacts failed: %v", err)
	}
	if len(res1.Observations) != 4 {
		t.Fatalf("expected 4 distinct observation groups in same function, got %d", len(res1.Observations))
	}

	var defects1 []models.Defect
	if err := db.Where("repo_id = ?", repo.ID).Order("id").Find(&defects1).Error; err != nil {
		t.Fatalf("load defects round 1: %v", err)
	}
	if len(defects1) != 4 {
		t.Fatalf("expected 4 distinct ACTIVE defects, got %d", len(defects1))
	}

	defectByToken := make(map[string]uint)
	for _, d := range defects1 {
		switch {
		case strings.Contains(d.CleanToken, "ptra"):
			defectByToken["ptrA"] = d.ID
		case strings.Contains(d.CleanToken, "ptrb"):
			defectByToken["ptrB"] = d.ID
		case strings.Contains(d.CleanToken, "(fd)"):
			defectByToken["fd"] = d.ID
		case strings.Contains(d.CleanToken, "(buf)"):
			defectByToken["buf"] = d.ID
		}
	}
	if len(defectByToken) != 4 {
		t.Fatalf("expected all 4 instances mapped independently, got %+v", defectByToken)
	}

	// 第 2 轮仅修复 ptrA 和 fd，保留 ptrB 和 buf
	src2 := `void multiLeakAndNPD(int* ptrA, int* ptrB, int fd, char* buf) {
    if (ptrA) *ptrA = 1;
    *ptrB = 2;
    close(fd);
    releaseHandle(buf);
}
`
	hash2 := writeBenchmarkFile(t, root, "src/multi.cpp", src2)
	findings2 := []models.AnalysisFinding{
		{FilePath: "src/multi.cpp", LineNumber: "3", Category: "内存管理问题-空指针解引用", Severity: "严重", Title: "ptrB npd", TriggerLine: "*ptrB = 2;", ScopeSymbol: "multiLeakAndNPD"},
		{FilePath: "src/multi.cpp", LineNumber: "5", Category: "性能与运行效率-资源未释放", Severity: "严重", Title: "buf leak", TriggerLine: "releaseHandle(buf);", ScopeSymbol: "multiLeakAndNPD", TargetSymbol: "buf"},
	}
	r2 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
	res2, err := PersistScanFacts(ScanInput{
		DB: db, Report: r2, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: findings2,
		Coverage: &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{{Path: "src/multi.cpp", Hash: hash2, Status: coverage.StatusSuccess}},
		},
	})
	if err != nil {
		t.Fatalf("round 2 PersistScanFacts failed: %v", err)
	}
	if len(res2.Observations) != 2 {
		t.Fatalf("expected 2 surviving observations, got %d", len(res2.Observations))
	}
	matchedIDs := make(map[uint]bool)
	for _, obs := range res2.Observations {
		if obs.Verdict != VerdictExisted || obs.DefectID == nil {
			t.Fatalf("expected surviving observation to match as EXISTED, got %+v", obs)
		}
		matchedIDs[*obs.DefectID] = true
	}
	if !matchedIDs[defectByToken["ptrB"]] || !matchedIDs[defectByToken["buf"]] {
		t.Fatalf("expected surviving observations to match ptrB(%d) and buf(%d), got %+v", defectByToken["ptrB"], defectByToken["buf"], matchedIDs)
	}

	// 验证已修复的 ptrA 与 fd 进入待核验衰变状态，未影响 ptrB 与 buf 的 ACTIVE
	var ptrADefect, ptrBDefect models.Defect
	if err := db.First(&ptrADefect, defectByToken["ptrA"]).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&ptrBDefect, defectByToken["ptrB"]).Error; err != nil {
		t.Fatal(err)
	}
	if ptrADefect.Status == DefectStatusActive {
		t.Fatalf("expected fixed ptrA defect to leave ACTIVE, stayed ACTIVE")
	}
	if ptrBDefect.Status != DefectStatusActive {
		t.Fatalf("expected unfixed ptrB defect to stay ACTIVE, got %s", ptrBDefect.Status)
	}
}

// TestGoldenBenchmarkD_DenseFileHumanClosedBudget
// 基准 D：高密度单文件（15 条历史缺陷含 RESOLVED 与 HUMAN_CLOSED）全量召回与豁免保护
func TestGoldenBenchmarkD_DenseFileHumanClosedBudget(t *testing.T) {
	db, repo, taskType, root := setupBenchmarkEnv(t, "rfc006_bench_d")

	var srcLines []string
	srcLines = append(srcLines, "void denseFunction() {")
	for i := 1; i <= 15; i++ {
		srcLines = append(srcLines, fmt.Sprintf("    *ptr_%02d = %d;", i, i))
	}
	srcLines = append(srcLines, "}")
	hash := writeBenchmarkFile(t, root, "src/dense.cpp", strings.Join(srcLines, "\n")+"\n")

	allFindings := make([]models.AnalysisFinding, 15)
	for i := 0; i < 15; i++ {
		allFindings[i] = models.AnalysisFinding{
			FilePath:    "src/dense.cpp",
			LineNumber:  fmt.Sprintf("%d", i+2),
			Category:    "内存管理问题-空指针解引用",
			Severity:    "严重",
			Title:       fmt.Sprintf("dense npd %02d", i+1),
			TriggerLine: fmt.Sprintf("*ptr_%02d = %d;", i+1, i+1),
			ScopeSymbol: "denseFunction",
		}
	}

	cov := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{{Path: "src/dense.cpp", Hash: hash, Status: coverage.StatusSuccess}},
	}
	r1 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
	res1, err := PersistScanFacts(ScanInput{
		DB: db, Report: r1, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: allFindings, Coverage: cov,
	})
	if err != nil {
		t.Fatalf("round 1 dense PersistScanFacts failed: %v", err)
	}
	if len(res1.Observations) != 15 {
		t.Fatalf("expected 15 seeded observations, got %d", len(res1.Observations))
	}

	var seeded []models.Defect
	if err := db.Where("repo_id = ?", repo.ID).Order("id").Find(&seeded).Error; err != nil {
		t.Fatalf("load seeded defects: %v", err)
	}
	if len(seeded) != 15 {
		t.Fatalf("expected 15 seeded defects, got %d", len(seeded))
	}

	// 前 3 条置为 RESOLVED，第 4~5 条置为 HUMAN_CLOSED，其余 10 条保持 ACTIVE
	resolvedSet := map[uint]bool{}
	humanClosedSet := map[uint]bool{}
	activeSet := map[uint]bool{}
	for idx, d := range seeded {
		switch {
		case idx < 3:
			if err := db.Model(&models.Defect{}).Where("id = ?", d.ID).Update("status", DefectStatusResolved).Error; err != nil {
				t.Fatal(err)
			}
			resolvedSet[d.ID] = true
		case idx < 5:
			if err := db.Model(&models.Defect{}).Where("id = ?", d.ID).Update("status", DefectStatusHumanClosed).Error; err != nil {
				t.Fatal(err)
			}
			humanClosedSet[d.ID] = true
		default:
			activeSet[d.ID] = true
		}
	}

	// 第 2 轮发现 10 个 ACTIVE + 2 个 HUMAN_CLOSED（跳过前 3 个已解决的）
	round2Findings := allFindings[3:]
	r2 := createBenchmarkReport(t, db, repo.ID, taskType.ID)
	res2, err := PersistScanFacts(ScanInput{
		DB: db, Report: r2, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: round2Findings, Coverage: cov,
	})
	if err != nil {
		t.Fatalf("round 2 dense PersistScanFacts failed: %v", err)
	}
	if len(res2.Observations) != 12 {
		t.Fatalf("expected 12 observations in round 2, got %d", len(res2.Observations))
	}

	// 断言 0 新建缺陷，总数仍严格为 15
	var totalDefects int64
	if err := db.Model(&models.Defect{}).Where("repo_id = ?", repo.ID).Count(&totalDefects).Error; err != nil {
		t.Fatal(err)
	}
	if totalDefects != 15 {
		t.Fatalf("expected zero newly opened defects (total=15), got %d", totalDefects)
	}

	// 断言 10 个 ACTIVE -> EXISTED，2 个 HUMAN_CLOSED -> 精确命中并产生 EXEMPT_RECHECK
	existedCount := 0
	for _, obs := range res2.Observations {
		if obs.DefectID == nil {
			t.Fatalf("unexpected nil defect id in dense file obs: %+v", obs)
		}
		if obs.Verdict != VerdictExisted {
			t.Fatalf("expected EXISTED for dense file defect %d, got %s (reason=%s)", *obs.DefectID, obs.Verdict, obs.Reason)
		}
		existedCount++
	}
	if existedCount != 12 {
		t.Fatalf("expected 12 EXISTED verdicts, got %d", existedCount)
	}

	var exemptEvents int64
	if err := db.Model(&models.DefectEvent{}).Where("report_id = ? AND event_type = ?", r2.ID, EventTypeExemptRecheck).Count(&exemptEvents).Error; err != nil {
		t.Fatal(err)
	}
	if exemptEvents != 2 {
		t.Fatalf("expected 2 EXEMPT_RECHECK events for HUMAN_CLOSED defects, got %d", exemptEvents)
	}

	for id := range humanClosedSet {
		var d models.Defect
		if err := db.First(&d, id).Error; err != nil {
			t.Fatal(err)
		}
		if d.Status != DefectStatusHumanClosed {
			t.Fatalf("HUMAN_CLOSED defect %d should remain exempt, got %s", id, d.Status)
		}
	}
}
