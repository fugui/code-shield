package defectlifecycle

import (
	"testing"

	"code-shield/models"
)

// 1. 同 Commit 连续扫描零增量验收（零假 NEW）
func Test_SameCommit_ZeroNewDefects(t *testing.T) {
	normPath := "src/core/buffer.cpp"
	scope := "Buffer::Allocate#1"
	classMajor := ClassNPD
	targetEntity := "pBuf"
	stmtHash := "a1b2c3d4e5f60718"

	coreSlot := ComputeCoreSlotKey(normPath, scope, classMajor, targetEntity)
	slotKey := ComputePreciseSlotKey(coreSlot, stmtHash)

	// 存量活跃缺陷
	activeDefects := []models.Defect{
		{
			ID:               101,
			RepoID:           1,
			TaskTypeID:       1,
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     targetEntity,
			StmtAnchorHash:   stmtHash,
			CoreSlotKey:      coreSlot,
			SlotKey:          slotKey,
			Status:           "ACTIVE",
		},
	}

	// 连续扫描上报完全一致的 Finding
	findings := []StabilizedFindingDTO{
		{
			FindingID:        "f1",
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     targetEntity,
			StmtAnchorHash:   stmtHash,
			CoreSlotKey:      coreSlot,
			SlotKey:          slotKey,
			Confidence:       ConfidenceHigh,
		},
	}

	res := MatchBatchObservations(findings, activeDefects, nil, nil)
	if res.NewCount != 0 {
		t.Fatalf("expected 0 new defects, got %d", res.NewCount)
	}
	if res.ExistCount != 1 {
		t.Fatalf("expected 1 exist defect, got %d", res.ExistCount)
	}
	if res.Decisions[0].Tier != "SLOT_PRECISE" || res.Decisions[0].DefectID != 101 {
		t.Fatalf("unexpected decision: %+v", res.Decisions[0])
	}
}

// 2. 零吞噬验收：同函数新增真实独立缺陷
func Test_SameScope_AddRealNewDefect(t *testing.T) {
	normPath := "src/core/buffer.cpp"
	scope := "Buffer::Allocate#1"
	classMajor := ClassNPD

	// 存量缺陷: 变量 pOld
	coreSlot1 := ComputeCoreSlotKey(normPath, scope, classMajor, "pOld")
	slotKey1 := ComputePreciseSlotKey(coreSlot1, "hash1111")
	activeDefects := []models.Defect{
		{
			ID:               201,
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     "pOld",
			StmtAnchorHash:   "hash1111",
			CoreSlotKey:      coreSlot1,
			SlotKey:          slotKey1,
			Status:           "ACTIVE",
		},
	}

	// 本轮扫描上报 2 个 Finding: 一个存量 pOld，一个真实新增 pNew
	coreSlot2 := ComputeCoreSlotKey(normPath, scope, classMajor, "pNew")
	slotKey2 := ComputePreciseSlotKey(coreSlot2, "hash2222")

	findings := []StabilizedFindingDTO{
		{
			FindingID:        "f_old",
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     "pOld",
			StmtAnchorHash:   "hash1111",
			CoreSlotKey:      coreSlot1,
			SlotKey:          slotKey1,
			Confidence:       ConfidenceHigh,
		},
		{
			FindingID:        "f_new",
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     "pNew",
			StmtAnchorHash:   "hash2222",
			CoreSlotKey:      coreSlot2,
			SlotKey:          slotKey2,
			Confidence:       ConfidenceHigh,
		},
	}

	res := MatchBatchObservations(findings, activeDefects, nil, nil)
	if res.ExistCount != 1 || res.NewCount != 1 {
		t.Fatalf("expected 1 EXISTED and 1 NEW, got exist=%d, new=%d", res.ExistCount, res.NewCount)
	}
	if res.Decisions[0].Verdict != "EXISTED" || res.Decisions[0].DefectID != 201 {
		t.Fatalf("decision 0 mismatch: %+v", res.Decisions[0])
	}
	if res.Decisions[1].Verdict != "NEW" {
		t.Fatalf("decision 1 mismatch: %+v", res.Decisions[1])
	}
}

// 3. 同变量多缺陷生命周期独立演进验收（杜绝 Index Shifting 状态倒挂）
func Test_MultiDefects_IndependentLifecycle(t *testing.T) {
	normPath := "src/core/session.cpp"
	scope := "Session::Process#1"
	classMajor := ClassNPD
	targetEntity := "ptr"

	coreSlot := ComputeCoreSlotKey(normPath, scope, classMajor, targetEntity)
	slotKeyA := ComputePreciseSlotKey(coreSlot, "anchor_hash_line30")
	slotKeyB := ComputePreciseSlotKey(coreSlot, "anchor_hash_line85")

	// 存量库中有 2 处同名变量 ptr 的空指针缺陷
	line30 := 30
	line85 := 85
	activeDefects := []models.Defect{
		{
			ID:               301, // Defect A (第 30 行)
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     targetEntity,
			StmtAnchorHash:   "anchor_hash_line30",
			CoreSlotKey:      coreSlot,
			SlotKey:          slotKeyA,
			LineStart:        &line30,
			Status:           "ACTIVE",
		},
		{
			ID:               302, // Defect B (第 85 行)
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     targetEntity,
			StmtAnchorHash:   "anchor_hash_line85",
			CoreSlotKey:      coreSlot,
			SlotKey:          slotKeyB,
			LineStart:        &line85,
			Status:           "ACTIVE",
		},
	}

	// 场景：开发者修复了第 30 行的 Defect A，本轮扫描只报出第 85 行的 Defect B
	findings := []StabilizedFindingDTO{
		{
			FindingID:        "f_remain",
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     targetEntity,
			StmtAnchorHash:   "anchor_hash_line85", // 绝对静止锚点匹配 Defect B
			CoreSlotKey:      coreSlot,
			SlotKey:          slotKeyB,
			LineStart:        85,
			Confidence:       ConfidenceHigh,
		},
	}

	res := MatchBatchObservations(findings, activeDefects, nil, nil)
	if res.ExistCount != 1 || res.NewCount != 0 {
		t.Fatalf("match failed: exist=%d, new=%d", res.ExistCount, res.NewCount)
	}

	// 必须精准认领 Defect B (ID 302)，绝对不能发生 Index Shifting 认领 Defect A (ID 301)
	if res.Decisions[0].DefectID != 302 {
		t.Fatalf("INDEX SHIFTING DETECTED! Expected defect 302, claimed %d", res.Decisions[0].DefectID)
	}

	// 验证生命周期结算：Defect A 应被判定修复 RESOLVED，Defect B 维持 ACTIVE
	claimedMap := map[uint]bool{302: true}
	evaluator := &MemoryScopeEvaluator{
		plannedFiles: map[string]bool{normPath: true},
	}

	transitions := SettleLedgerLifecycle(activeDefects, claimedMap, evaluator)
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(transitions))
	}

	for _, tr := range transitions {
		if tr.DefectID == 301 && tr.NewStatus != "RESOLVED" {
			t.Errorf("Defect 301 should be RESOLVED, got %s", tr.NewStatus)
		}
		if tr.DefectID == 302 && tr.NewStatus != "ACTIVE" {
			t.Errorf("Defect 302 should be ACTIVE, got %s", tr.NewStatus)
		}
	}
}

// 4. 单缺陷代码格式化抗扰动验收（Tier 0.5 秒配与原地自愈）
func Test_CodeFormat_AutoSelfHealing(t *testing.T) {
	normPath := "src/core/parser.cpp"
	scope := "Parser::ParseHeader#1"
	classMajor := ClassNPD
	targetEntity := "pHeader"

	coreSlot := ComputeCoreSlotKey(normPath, scope, classMajor, targetEntity)
	oldStmtHash := "old_hash_unformatted"
	oldSlotKey := ComputePreciseSlotKey(coreSlot, oldStmtHash)

	// 存量库中只有 1 条该变量的缺陷 (独占单实例)
	activeDefects := []models.Defect{
		{
			ID:               401,
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     targetEntity,
			StmtAnchorHash:   oldStmtHash,
			CoreSlotKey:      coreSlot,
			SlotKey:          oldSlotKey,
			Status:           "ACTIVE",
		},
	}

	// 开发者进行了格式化换行，导致 StmtAnchorHash 发生变化
	newStmtHash := "new_hash_after_clang_format"
	newSlotKey := ComputePreciseSlotKey(coreSlot, newStmtHash)

	findings := []StabilizedFindingDTO{
		{
			FindingID:        "f_fmt",
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     targetEntity,
			StmtAnchorHash:   newStmtHash, // 锚点改变
			CoreSlotKey:      coreSlot,
			SlotKey:          newSlotKey,
			Confidence:       ConfidenceHigh,
		},
	}

	res := MatchBatchObservations(findings, activeDefects, nil, nil)
	if res.ExistCount != 1 || res.NewCount != 0 {
		t.Fatalf("format perturbation failed: exist=%d, new=%d", res.ExistCount, res.NewCount)
	}

	dec := res.Decisions[0]
	if dec.Tier != "CORE_SLOT_SINGLETON" || dec.DefectID != 401 {
		t.Fatalf("expected CORE_SLOT_SINGLETON claim on 401, got %+v", dec)
	}
	if !dec.NeedInPlaceAnchorHeal || dec.NewStmtAnchorHash != newStmtHash {
		t.Fatalf("in-place anchor self-healing not triggered properly: %+v", dec)
	}
}

// 5. 跨函数提取重构自愈验收（Tier 1 召回与别名自愈）
func Test_FunctionExtract_TwoStageRecall(t *testing.T) {
	normPath := "src/core/worker.cpp"
	classMajor := ClassMemLeak
	cleanToken := "char*buf=(char*)malloc(1024);memset(buf,0,1024);"

	// 历史存量在 Worker::Run#0 函数中
	activeDefects := []models.Defect{
		{
			ID:               501,
			NormPath:         normPath,
			ScopeSymbol:      "Worker::Run#0",
			DefectClassMajor: classMajor,
			TargetEntity:     "buf",
			CleanToken:       cleanToken,
			Status:           "ACTIVE",
		},
	}

	// 开发者重构将该逻辑提取到了新函数 Worker::InitBuffer#0 中
	newScope := "Worker::InitBuffer#0"
	findings := []StabilizedFindingDTO{
		{
			FindingID:        "f_extracted",
			NormPath:         normPath,
			ScopeSymbol:      newScope, // 函数更名
			DefectClassMajor: classMajor,
			TargetEntity:     "buf",
			CleanToken:       cleanToken, // Token 高度一致
			Confidence:       ConfidenceHigh,
		},
	}

	res := MatchBatchObservations(findings, activeDefects, nil, nil)
	if res.ExistCount != 1 || res.NewCount != 0 {
		t.Fatalf("function extract recall failed: exist=%d, new=%d", res.ExistCount, res.NewCount)
	}

	dec := res.Decisions[0]
	if dec.Tier != "ELASTIC_SELF_HEAL" || dec.DefectID != 501 {
		t.Fatalf("expected ELASTIC_SELF_HEAL on 501, got %+v", dec)
	}
}

// 6. 批次多 Finding 竞争单 Defect 互斥防踩踏验收
func Test_BatchMutualExclusion_NoCollision(t *testing.T) {
	normPath := "src/core/cache.cpp"
	scope := "Cache::Get#1"
	classMajor := ClassNPD

	coreSlot := ComputeCoreSlotKey(normPath, scope, classMajor, "entry")
	slotKey := ComputePreciseSlotKey(coreSlot, "hash_entry")

	activeDefects := []models.Defect{
		{
			ID:               601,
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     "entry",
			StmtAnchorHash:   "hash_entry",
			CoreSlotKey:      coreSlot,
			SlotKey:          slotKey,
			Status:           "ACTIVE",
		},
	}

	// 两个不同的 Finding 均尝试匹配该 Defect
	findings := []StabilizedFindingDTO{
		{
			FindingID:        "f1",
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     "entry",
			StmtAnchorHash:   "hash_entry",
			CoreSlotKey:      coreSlot,
			SlotKey:          slotKey,
			Confidence:       ConfidenceHigh,
		},
		{
			FindingID:        "f2",
			NormPath:         normPath,
			ScopeSymbol:      scope,
			DefectClassMajor: classMajor,
			TargetEntity:     "entry",
			StmtAnchorHash:   "hash_entry",
			CoreSlotKey:      coreSlot,
			SlotKey:          slotKey,
			Confidence:       ConfidenceHigh,
		},
	}

	res := MatchBatchObservations(findings, activeDefects, nil, nil)
	// 第 1 个 Finding 认领成功，第 2 个必须互斥锁死判定为 NEW，严禁踩踏
	if res.ExistCount != 1 || res.NewCount != 1 {
		t.Fatalf("mutual exclusion failed: exist=%d, new=%d", res.ExistCount, res.NewCount)
	}
	if res.Decisions[0].Verdict != "EXISTED" || res.Decisions[1].Verdict != "NEW" {
		t.Fatalf("decisions mismatch: d0=%+v, d1=%+v", res.Decisions[0], res.Decisions[1])
	}
}

// 7. 防吞噬安全护栏验收：CRITICAL 缺陷绝不保守合并
func Test_Critical_NeverSwallowed(t *testing.T) {
	obs := StabilizedFindingDTO{
		FindingID:        "f_crit",
		NormPath:         "src/auth/login.cpp",
		ScopeSymbol:      "Login#1",
		DefectClassMajor: ClassSecInject,
		Severity:         "CRITICAL", // 高危
		LineStart:        100,
		CleanToken:       "system(cmd);",
	}

	line50 := 50
	candidates := []models.Defect{
		{
			ID:               701,
			NormPath:         "src/auth/login.cpp",
			ScopeSymbol:      "Login#1",
			DefectClassMajor: ClassSecInject,
			CleanToken:       "system(anotherCmd);",
			LineStart:        &line50,
		},
	}

	best := selectStrictConservativeCandidate(obs, candidates)
	if best != nil {
		t.Fatalf("CRITICAL vulnerability was mistakenly swallowed! Candidate: %+v", best)
	}
}
