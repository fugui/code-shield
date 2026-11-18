package defectlifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"code-shield/models"
)

func defect(id uint, status string) models.Defect {
	start := 10
	end := 12
	target := "ptr"
	clean := "delete ptr"
	stmtAnchorHash := ComputeStmtAnchorHash(clean)
	coreSlotKey := ComputeCoreSlotKey("src/a.cpp", "scope-a", "memory", target)
	slotKey := ComputePreciseSlotKey(coreSlotKey, stmtAnchorHash)
	return models.Defect{
		ID: id, Status: status, NormPath: "src/a.cpp", ScopeKey: "scope-a",
		CleanToken: clean, DefectClassMajor: "memory",
		Severity: "严重", LineStart: &start, LineEnd: &end,
		ScopeBodyHash: "scope-body-a", ScopeSymbol: "scope-a",
		TargetEntity: target, StmtAnchorHash: stmtAnchorHash,
		CoreSlotKey: coreSlotKey, SlotKey: slotKey,
		CanonicalFingerprint: slotKey, IdentityKind: "SLOT",
	}
}

func observation(uid, path, shape, clean string) ObservationGroup {
	target, _ := ExtractPhysicalTargetEntity(clean, "", "memory")
	if target == "" {
		target = "ptr"
	}
	stmtAnchorHash := ComputeStmtAnchorHash(clean)
	coreSlotKey := ComputeCoreSlotKey(path, "scope-a", "memory", target)
	slotKey := ComputePreciseSlotKey(coreSlotKey, stmtAnchorHash)
	return ObservationGroup{
		UID: uid,
		Identity: Identity{
			RepoID: 1, TaskTypeID: 2, NormPath: path, ScopeKey: "scope-a",
			StmtShape: shape, CleanToken: clean, DefectClassMajor: "memory",
			LineStart: 10, LineEnd: 12, Severity: "严重", PrevShape: "prev-a", NextShape: "next-a", Confidence: AnchorHigh,
			TargetEntity: target, StmtAnchorHash: stmtAnchorHash,
			CoreSlotKey: coreSlotKey, SlotKey: slotKey,
		},
	}
}

func TestRunMatchingClaimsStrongAlias(t *testing.T) {
	existing := defect(7, "ACTIVE")
	group := observation("uid-1", "src/a.cpp", "shape-a", "delete ptr")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasSlotHealed, AliasValue: group.Identity.SlotKey, AliasClass: AliasStrong}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
	})
	if len(result.Observations) != 1 {
		t.Fatalf("unexpected decisions: %+v", result.Observations)
	}
	decision := result.Observations[0]
	if decision.DefectID == nil || *decision.DefectID != 7 {
		t.Fatalf("expected defect 7, got %+v", decision)
	}
	if decision.Verdict != VerdictExisted || decision.MatchTier != MatchStrong {
		t.Fatalf("unexpected verdict/tier: %+v", decision)
	}
	if decision.Score < 0.90 || decision.ScoreDetail["clean_token_similarity"] != 1 {
		t.Fatalf("unexpected score: %+v", decision)
	}
}

func TestRunMatchingAssignsEachDefectOnce(t *testing.T) {
	first := defect(7, "ACTIVE")
	second := defect(8, "ACTIVE")
	secondClean := "delete other"
	secondTarget := "other"
	secondStmtHash := ComputeStmtAnchorHash(secondClean)
	secondCoreSlot := ComputeCoreSlotKey("src/a.cpp", "scope-a", "memory", secondTarget)
	secondSlot := ComputePreciseSlotKey(secondCoreSlot, secondStmtHash)
	second.CleanToken = secondClean
	second.TargetEntity = secondTarget
	second.StmtAnchorHash = secondStmtHash
	second.CoreSlotKey = secondCoreSlot
	second.SlotKey = secondSlot
	second.CanonicalFingerprint = secondSlot

	firstGroup := observation("uid-1", "src/a.cpp", "shape-a", "delete ptr")
	secondGroup := observation("uid-2", "src/a.cpp", "shape-a", secondClean)
	aliases := []models.DefectAlias{
		{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasSlotHealed, AliasValue: firstGroup.Identity.SlotKey, AliasClass: AliasStrong},
		{DefectID: 8, RepoID: 1, TaskTypeID: 2, AliasType: AliasSlotHealed, AliasValue: secondGroup.Identity.SlotKey, AliasClass: AliasStrong},
	}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{firstGroup, secondGroup},
		Defects:      []models.Defect{first, second},
		Aliases:      aliases,
	})
	claimed := map[uint]bool{}
	for _, decision := range result.Observations {
		if decision.DefectID == nil {
			t.Fatalf("expected assignment, got %+v", decision)
		}
		if claimed[*decision.DefectID] {
			t.Fatalf("defect %d assigned twice", *decision.DefectID)
		}
		claimed[*decision.DefectID] = true
	}
	if len(claimed) != 2 {
		t.Fatalf("expected two assignments, got %v", claimed)
	}
}

func TestRunMatchingNewWithoutCandidates(t *testing.T) {
	result := RunMatching(MatchingInput{Observations: []ObservationGroup{observation("uid-1", "src/a.cpp", "shape-a", "delete ptr")}})
	decision := result.Observations[0]
	if decision.Verdict != VerdictNew || decision.DefectID != nil || decision.Reason != reasonNoCandidate {
		t.Fatalf("unexpected NEW decision: %+v", decision)
	}
}

func TestRunMatchingGrayBandRequiresAIDifferent(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.CleanToken = "sharedstate release resource"
	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
		Arbitrator:   rejectingArbitrator{},
	})
	if result.AICalls != 1 {
		t.Fatalf("expected one AI call, got %d", result.AICalls)
	}
	if result.Observations[0].Verdict != VerdictNew {
		t.Fatalf("expected NEW after AI rejection, got %+v", result.Observations[0])
	}
}

func TestRunMatchingGrayBandRemainsProbableWithoutAI(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.CleanToken = "release resource"
	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
	})
	decision := result.Observations[0]
	if decision.Verdict != VerdictProbable || decision.DefectID != nil {
		t.Fatalf("expected PROBABLE, got %+v", decision)
	}
}

func TestRunMatchingAIReviewFallsBackToMergeAboveScore(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.CleanToken = "release resource"
	group := observation("uid-1", "src/a.cpp", "shape-new", "release resource")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
		Arbitrator:   reviewingArbitrator{},
		Thresholds: MatchThresholds{
			StrongSame: 0.90, AssignBand: 0.85, RejectBelow: 0.45,
			AutoResolve: true, AIConfidence: 0.70, FallbackMergeScore: 0.60,
		},
	})
	decision := result.Observations[0]
	if decision.DefectID == nil || *decision.DefectID != 7 || decision.Verdict != VerdictExisted {
		t.Fatalf("expected fallback merge, got %+v", decision)
	}
	if decision.MatchTier != MatchAIFallback || decision.Score < 0.60 {
		t.Fatalf("unexpected fallback decision: %+v", decision)
	}
	if decision.Reason != reasonAutoMerge+":"+reasonAIReview+":stable identity evidence is insufficient" {
		t.Fatalf("unexpected fallback reason: %+v", decision)
	}
}

func TestRunMatchingAIReviewFallsBackToNewBelowScore(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.CleanToken = "sharedstate release resource"
	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
		Arbitrator:   reviewingArbitrator{},
		Thresholds: MatchThresholds{
			StrongSame: 0.90, AssignBand: 0.65, RejectBelow: 0.45,
			AutoResolve: true, AIConfidence: 0.70, FallbackMergeScore: 0.60,
		},
	})
	decision := result.Observations[0]
	if decision.DefectID != nil || decision.Verdict != VerdictNew {
		t.Fatalf("expected fallback NEW, got %+v", decision)
	}
	if decision.MatchTier != MatchAIFallback || decision.Score >= 0.60 {
		t.Fatalf("unexpected fallback decision: %+v", decision)
	}
	if decision.Reason != reasonAutoNew+":"+reasonAIReview+":stable identity evidence is insufficient" {
		t.Fatalf("unexpected fallback reason: %+v", decision)
	}
}

func TestRunMatchingAIUnavailableKeepsProbable(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.CleanToken = "release resource"
	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
		Arbitrator:   failingArbitrator{},
	})
	decision := result.Observations[0]
	if decision.Verdict != VerdictProbable || decision.Reason != reasonAIUnavailable {
		t.Fatalf("expected unavailable AI to stay PROBABLE, got %+v", decision)
	}
}

type rejectingArbitrator struct{}

func (rejectingArbitrator) Arbitrate(request ArbitrationRequest) ([]ArbitrationDecision, error) {
	return []ArbitrationDecision{{DefectID: request.Candidates[0].Defect.ID, Decision: ArbitrationDifferent, Confidence: 1}}, nil
}

type reviewingArbitrator struct{}

func (reviewingArbitrator) Arbitrate(request ArbitrationRequest) ([]ArbitrationDecision, error) {
	return []ArbitrationDecision{{DefectID: request.Candidates[0].Defect.ID, Decision: ArbitrationReview, Confidence: 1, Reason: "stable identity evidence is insufficient"}}, nil
}

type failingArbitrator struct{}

func (failingArbitrator) Arbitrate(ArbitrationRequest) ([]ArbitrationDecision, error) {
	return nil, errors.New("unavailable")
}

func TestRunMatchingAIReviewKeepsReason(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.CleanToken = "release resource"
	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
		Arbitrator:   reviewingArbitrator{},
	})
	decision := result.Observations[0]
	expectedReason := reasonAIReview + ":stable identity evidence is insufficient"
	if decision.Verdict != VerdictProbable || decision.Reason != expectedReason {
		t.Fatalf("expected PROBABLE with AI review reason %q, got %+v", expectedReason, decision)
	}
}

func TestRunMatchingEvidenceVetoKeepsDifferentProblemNew(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.DefectClassMajor = "race"
	existing.Severity = "建议"
	group := observation("uid-1", "src/a.cpp", "shape-a", "delete ptr")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
	})
	decision := result.Observations[0]
	if decision.Verdict != VerdictNew || decision.DefectID != nil || result.AICalls != 0 {
		t.Fatalf("expected vetoed candidate to remain NEW, got %+v", decision)
	}
	if decision.ScoreDetail["evidence_veto"] != 1 {
		t.Fatalf("expected evidence veto detail, got %+v", decision.ScoreDetail)
	}
}

func TestRunMatchingStrongAliasRecallsDefectDespiteSnapshotDrift(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.NormPath = "src/a.cpp"
	existing.ScopeKey = "scope-other"
	existing.CleanToken = "unrelated statement"
	existing.DefectClassMajor = "race"
	existing.Severity = "建议"
	start, end := 180, 182
	existing.LineStart, existing.LineEnd = &start, &end

	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasSlotHealed, AliasValue: group.Identity.SlotKey, AliasClass: AliasStrong}
	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
	})
	decision := result.Observations[0]
	if decision.DefectID == nil || *decision.DefectID != 7 {
		t.Fatalf("expected strong alias to recall defect 7, got %+v", decision)
	}
	if decision.Verdict != VerdictExisted || decision.MatchTier != MatchStrong || decision.Score < 0.90 {
		t.Fatalf("unexpected strong alias decision: %+v", decision)
	}
	if decision.ScoreDetail["strong_alias_equal"] != 1 {
		t.Fatalf("expected strong alias evidence, got %+v", decision.ScoreDetail)
	}
}

func TestRunMatchingRootAliasEntersReviewWithoutMerge(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.CleanToken = "release resource"
	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	group.Identity.ResourceIdentity = "sharedstate"
	group.Identity.ValidationChain = "chain"
	group.Identity.RootFamily = rootFamilyHash(group.Identity.StmtShape, group.Identity.ResourceIdentity, group.Identity.ValidationChain)
	existing.CleanToken = "sharedstate release resource"
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
		Arbitrator:   reviewingArbitrator{},
	})
	decision := result.Observations[0]
	if result.AICalls != 1 || decision.Verdict != VerdictProbable || decision.DefectID != nil {
		t.Fatalf("expected lifted candidate to enter AI review, got AI=%d decision=%+v", result.AICalls, decision)
	}
	if decision.ScoreDetail["evidence_gate"] != 1 || decision.ScoreDetail["root_gate"] != 1 {
		t.Fatalf("expected root evidence gate, got %+v", decision.ScoreDetail)
	}
}

func TestRunMatchingResourceAliasCanBridgePathDriftForReview(t *testing.T) {
	existing := defect(7, "ACTIVE")
	existing.CleanToken = "release resource"
	existing.NormPath = "src/other.cpp"
	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	group.Identity.ResourceIdentity = "sharedstate"
	existing.CleanToken = "sharedstate release resource"
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasResource, AliasValue: "sharedstate", AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
		Arbitrator:   reviewingArbitrator{},
	})
	decision := result.Observations[0]
	if result.AICalls != 1 || decision.Verdict != VerdictProbable || decision.DefectID != nil {
		t.Fatalf("expected resource alias to enter AI review, got AI=%d decision=%+v", result.AICalls, decision)
	}
	if decision.ScoreDetail["evidence_gate"] != 1 || decision.ScoreDetail["resource_gate"] != 1 {
		t.Fatalf("expected resource evidence gate, got %+v", decision.ScoreDetail)
	}
}

func TestRunMatchingMatchesPreciseSlotKeyDirectly(t *testing.T) {
	normPath := "test/scan-test.cc"
	scopeSymbol := "TEST"
	targetEntity := "scan_test"
	defectClass := "断言有效性-空测试"
	cleanToken := "test(scan_test,invalid_format){"
	stmtAnchorHash := ComputeStmtAnchorHash(cleanToken)
	coreSlotKey := ComputeCoreSlotKey(normPath, scopeSymbol, defectClass, targetEntity)
	slotKey := ComputePreciseSlotKey(coreSlotKey, stmtAnchorHash)

	// 存量 defect 70 拥有相同的 slot_key，但指纹为 old-k2
	existing := defect(70, "ACTIVE")
	existing.NormPath = normPath
	existing.ScopeSymbol = scopeSymbol
	existing.TargetEntity = targetEntity
	existing.DefectClassMajor = defectClass
	existing.CleanToken = cleanToken
	existing.StmtAnchorHash = stmtAnchorHash
	existing.CoreSlotKey = coreSlotKey
	existing.SlotKey = slotKey
	existing.CanonicalFingerprint = "old-k2"

	// observation 携带新的指纹 new-k2，但槽位相同
	group := observation("uid-1", normPath, "shape", cleanToken)
	group.Identity.SymbolPath = scopeSymbol
	group.Identity.TargetEntity = targetEntity
	group.Identity.DefectClassMajor = defectClass
	group.Identity.StmtAnchorHash = stmtAnchorHash
	group.Identity.CoreSlotKey = coreSlotKey
	group.Identity.SlotKey = slotKey
	group.Identity.K2 = "new-k2"

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Thresholds:   RuntimeMatchThresholds(),
	})
	decision := result.Observations[0]
	if decision.Verdict != VerdictExisted {
		t.Fatalf("expected verdict EXISTED, got %s", decision.Verdict)
	}
	if decision.DefectID == nil || *decision.DefectID != 70 {
		t.Fatalf("expected matched defect ID 70, got %+v", decision.DefectID)
	}
	if decision.Reason != "SLOT_PRECISE" {
		t.Fatalf("expected reason SLOT_PRECISE, got %s", decision.Reason)
	}
}

func TestRunMatchingDoesNotProduceZeroDefectIDFromOrphanAlias(t *testing.T) {
	group := observation("uid-pass-1", "src/a.cpp", "shape-a", "delete ptr")
	group.Findings = []models.AnalysisFinding{{AssessmentStatus: "pass"}}
	orphanAlias := models.DefectAlias{DefectID: 999, RepoID: 1, TaskTypeID: 2, AliasType: AliasSlotHealed, AliasValue: group.Identity.SlotKey, AliasClass: AliasStrong}
	zeroAlias := models.DefectAlias{DefectID: 0, RepoID: 1, TaskTypeID: 2, AliasType: AliasSlotHealed, AliasValue: group.Identity.SlotKey, AliasClass: AliasStrong}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{defect(10, "ACTIVE")},
		Aliases:      []models.DefectAlias{orphanAlias, zeroAlias},
		Thresholds:   RuntimeMatchThresholds(),
	})
	if len(result.Observations) != 1 {
		t.Fatalf("expected 1 observation decision, got %d", len(result.Observations))
	}
	decision := result.Observations[0]
	if decision.DefectID != nil && *decision.DefectID == 0 {
		t.Fatalf("decision.DefectID must never be zero, got &0")
	}
	if decision.DefectID != nil && *decision.DefectID == 999 {
		t.Fatalf("orphan alias must not match defect 999 which is not in input.Defects")
	}
}

func TestCommittableDecisionsFiltersZeroDefectIDAndOrphanCleared(t *testing.T) {
	zeroID := uint(0)
	validID := uint(10)
	group := observation("uid-1", "src/a.cpp", "shape-a", "delete ptr")
	group.Findings = []models.AnalysisFinding{{AssessmentStatus: "pass"}}
	passOnlyGroup := observation("uid-2", "src/b.cpp", "shape-b", "test pass")
	passOnlyGroup.Findings = []models.AnalysisFinding{{AssessmentStatus: "pass"}}

	input := LedgerInput{
		Observations: []ObservationGroup{group, passOnlyGroup},
		Decisions: []ObservationDecision{
			{ObservationGroupUID: "uid-1", Verdict: VerdictCleared, DefectID: &zeroID},
			{ObservationGroupUID: "uid-2", Verdict: VerdictCleared, DefectID: nil},
			{ObservationGroupUID: "uid-1", Verdict: VerdictCleared, DefectID: &validID},
		},
		CoverageState: CoverageComplete,
		Scope: []models.ScanScopeEntry{
			{NormPath: "src/a.cpp", Outcome: ScopeScanned},
		},
	}
	committable := committableDecisions(input)
	if len(committable) != 1 {
		t.Fatalf("expected only 1 valid committable decision, got %d: %+v", len(committable), committable)
	}
	if committable[0].DefectID == nil || *committable[0].DefectID != validID {
		t.Fatalf("expected defect ID %d, got %+v", validID, committable[0].DefectID)
	}
}

func TestArbitrationDecisionJSONDeserialization(t *testing.T) {
	candidatePayload := `{"decisions":[{"candidate_id":42,"decision":"SAME","confidence":0.95,"reason":"same problem"}]}`
	var payload1 struct {
		Decisions []ArbitrationDecision `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(candidatePayload), &payload1); err != nil {
		t.Fatalf("unmarshal candidate_id: %v", err)
	}
	if len(payload1.Decisions) != 1 || payload1.Decisions[0].DefectID != 42 {
		t.Fatalf("expected DefectID=42 from candidate_id, got %+v", payload1.Decisions)
	}

	defectPayload := `{"decisions":[{"defect_id":88,"decision":"SAME","confidence":0.88,"reason":"same defect"}]}`
	var payload2 struct {
		Decisions []ArbitrationDecision `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(defectPayload), &payload2); err != nil {
		t.Fatalf("unmarshal defect_id: %v", err)
	}
	if len(payload2.Decisions) != 1 || payload2.Decisions[0].DefectID != 88 {
		t.Fatalf("expected DefectID=88 from defect_id, got %+v", payload2.Decisions)
	}
}

func TestApplyEvidenceGateLineProximityTolerance(t *testing.T) {
	ident := Identity{
		NormPath:         "src/a.cpp",
		ScopeKey:         "scope-1",
		DefectClassMajor: "logic",
	}
	cand := Candidate{
		Defect: models.Defect{
			NormPath:         "src/a.cpp",
			ScopeKey:         "scope-1",
			DefectClassMajor: "logic",
		},
	}
	detail := map[string]float64{
		"scope_key_equal":          1,
		"defect_class_major_equal": 1,
		"line_proximity":           0.9524, // 相距 1 行 (1 - 1/21)
		"clean_token_similarity":   0.20,
	}
	applyEvidenceGate(ident, cand, detail)
	if detail["anchor_gate"] != 1 {
		t.Fatalf("expected anchor_gate=1 for proximity 0.9524 within same scope, got %v", detail["anchor_gate"])
	}
	if detail["evidence_veto"] == 1 {
		t.Fatalf("expected no evidence veto for same scope and class, got veto=1")
	}
}

func TestRunMatchingStrongAliasPrioritizedOverPathBudget(t *testing.T) {
	obs := observation("uid-dense", "src/dense.cpp", "shape-target", "delete targetPtr")
	obs.Identity.SlotKey = "precise-slot-target"
	obs.Identity.DefectClassMajor = "memory_leak"

	defects := make([]models.Defect, 0, 7)
	aliases := make([]models.DefectAlias, 0, 8)
	// 先放入 5 条同文件历史 RESOLVED / OBSOLETE 与无关 ACTIVE 缺陷，故意设置极小预算 MaxCandidates=3
	for i := uint(1); i <= 4; i++ {
		status := DefectStatusResolved
		if i%2 == 0 {
			status = DefectStatusObsolete
		}
		d := defect(i, status)
		d.NormPath = "src/dense.cpp"
		d.SlotKey = fmt.Sprintf("old-slot-%d", i)
		defects = append(defects, d)
		aliases = append(aliases, models.DefectAlias{
			DefectID: i, RepoID: 1, TaskTypeID: 2,
			AliasType: AliasPath, AliasValue: "src/dense.cpp", AliasClass: AliasBucket,
		})
	}
	otherActive := defect(5, DefectStatusActive)
	otherActive.NormPath = "src/dense.cpp"
	otherActive.SlotKey = "other-active-slot"
	defects = append(defects, otherActive)
	aliases = append(aliases, models.DefectAlias{
		DefectID: 5, RepoID: 1, TaskTypeID: 2,
		AliasType: AliasPath, AliasValue: "src/dense.cpp", AliasClass: AliasBucket,
	})

	target := defect(42, DefectStatusActive)
	target.NormPath = "src/dense.cpp"
	target.SlotKey = "legacy-slot-42"
	defects = append(defects, target)
	aliases = append(aliases,
		models.DefectAlias{
			DefectID: 42, RepoID: 1, TaskTypeID: 2,
			AliasType: AliasPath, AliasValue: "src/dense.cpp", AliasClass: AliasBucket,
		},
		models.DefectAlias{
			DefectID: 42, RepoID: 1, TaskTypeID: 2,
			AliasType: AliasSlotHealed, AliasValue: "precise-slot-target", AliasClass: AliasStrong,
		},
	)

	res := RunMatching(MatchingInput{
		Observations: []ObservationGroup{obs},
		Defects:      defects,
		Aliases:      aliases,
		Budget:       MatchBudget{MaxCandidates: 3, MaxEdges: 100, MaxAssignmentWidth: 10, MaxAICalls: 5},
		Thresholds:   RuntimeMatchThresholds(),
	})
	dec := res.Observations[0]
	if dec.DefectID == nil || *dec.DefectID != 42 {
		t.Fatalf("expected strong healed alias to recall defect 42 under tight budget, got %+v", dec)
	}
	if dec.Reason != "SLOT_HEALED" || dec.Verdict != VerdictExisted {
		t.Fatalf("expected SLOT_HEALED & EXISTED, got reason=%s verdict=%s", dec.Reason, dec.Verdict)
	}
}

func TestRunMatchingAddAliasEnrichmentDoesNotExceedBudget(t *testing.T) {
	obs := observation("uid-enrich", "src/enrich.cpp", "shape-a", "delete ptr")
	obs.Identity.SlotKey = "slot-enrich"
	obs.Identity.RootFamily = "root-enrich"

	target := defect(7, DefectStatusActive)
	target.NormPath = "src/enrich.cpp"
	target.SlotKey = "slot-enrich"

	aliasIndex := map[string][]models.DefectAlias{
		"slot-enrich\x001\x002": {{
			DefectID: 7, RepoID: 1, TaskTypeID: 2,
			AliasType: AliasSlotHealed, AliasValue: "slot-enrich", AliasClass: AliasStrong,
		}},
		"root-enrich\x001\x002": {{
			DefectID: 7, RepoID: 1, TaskTypeID: 2,
			AliasType: AliasRoot, AliasValue: "root-enrich", AliasClass: AliasBucket,
		}},
	}
	cands, exceeded := retrieveCandidates(obs.Identity, aliasIndex, MatchingInput{
		Defects: []models.Defect{target},
	}, 1)
	if exceeded {
		t.Fatalf("expected enriching existing candidate under limit=1 not to flag budgetExceeded")
	}
	if len(cands) != 1 || !hasSource(cands[0], AliasSlotPrecise) || !hasSource(cands[0], AliasSlotHealed) || !hasSource(cands[0], AliasRoot) {
		t.Fatalf("expected candidate 7 with enriched sources, got %+v", cands)
	}
}

func TestRunMatchingSlotPreciseRecallsHumanClosedWithExisted(t *testing.T) {
	obs := observation("uid-exempt", "src/exempt.cpp", "shape-exempt", "delete ptr")
	obs.Identity.SlotKey = "slot-exempt-1"
	obs.Identity.DefectClassMajor = "memory_leak"

	closed := defect(88, DefectStatusHumanClosed)
	closed.NormPath = "src/exempt.cpp"
	closed.DefectClassMajor = "memory_leak"
	closed.SlotKey = "slot-exempt-1"

	res := RunMatching(MatchingInput{
		Observations: []ObservationGroup{obs},
		Defects:      []models.Defect{closed},
		Thresholds:   RuntimeMatchThresholds(),
	})
	dec := res.Observations[0]
	if dec.DefectID == nil || *dec.DefectID != 88 {
		t.Fatalf("expected HUMAN_CLOSED defect 88 recalled by SLOT_PRECISE, got %+v", dec)
	}
	if dec.Verdict != VerdictExisted || dec.Reason != "SLOT_PRECISE" {
		t.Fatalf("expected EXISTED / SLOT_PRECISE, got verdict=%s reason=%s", dec.Verdict, dec.Reason)
	}
}

func TestMatchedVerdictSemantics(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{DefectStatusActive, VerdictExisted},
		{DefectStatusCoverageGap, VerdictExisted},
		{DefectStatusVerifiedPending, VerdictExisted},
		{DefectStatusHumanClosed, VerdictExisted},
		{DefectStatusResolved, VerdictReopened},
		{DefectStatusDormant, VerdictReopened},
		{DefectStatusObsolete, VerdictReopened},
	}
	for _, tc := range cases {
		if got := matchedVerdict(tc.status); got != tc.want {
			t.Fatalf("matchedVerdict(%s) = %s, want %s", tc.status, got, tc.want)
		}
	}
}

func TestRunMatching_CrossScopeEntitySingletonAndResourceGate(t *testing.T) {
	obs := observation("uid-extracted", "src/refactor.cpp", "scope-new-helper", "memcpy(reqbuf,input,n)")
	obs.Identity.SymbolPath = "extractedCopy"
	obs.Identity.DefectClassMajor = ClassOOB
	obs.Identity.TargetEntity = "reqBuf"
	obs.Identity.ResourceIdentity = "reqbuf"
	obs.Identity.CoreSlotKey = ComputeCoreSlotKey("src/refactor.cpp", "extractedCopy", ClassOOB, "reqBuf")
	obs.Identity.SlotKey = ComputePreciseSlotKey(obs.Identity.CoreSlotKey, obs.Identity.StmtAnchorHash)
	obs.Identity.LineStart = 180
	obs.Identity.LineEnd = 180

	existing := defect(401, DefectStatusActive)
	existing.NormPath = "src/refactor.cpp"
	existing.ScopeKey = "scope-old-fn"
	existing.SymbolPath = "legacyCopy"
	existing.ScopeSymbol = "legacyCopy"
	existing.DefectClassMajor = ClassOOB
	existing.TargetEntity = "reqBuf"
	existing.CleanToken = "memcpy(reqbuf,src,n)"
	existing.CoreSlotKey = ComputeCoreSlotKey("src/refactor.cpp", "legacyCopy", ClassOOB, "reqBuf")
	existing.SlotKey = ComputePreciseSlotKey(existing.CoreSlotKey, "oldstmt")
	ls := 12
	existing.LineStart = &ls
	existing.LineEnd = &ls

	res := RunMatching(MatchingInput{
		Observations: []ObservationGroup{obs},
		Defects:      []models.Defect{existing},
		Thresholds:   RuntimeMatchThresholds(),
	})
	dec := res.Observations[0]
	if dec.DefectID == nil || *dec.DefectID != 401 {
		t.Fatalf("expected renamed/extracted singleton to match defect 401, got %+v", dec)
	}
	if dec.Verdict != VerdictExisted || dec.Reason != "CROSS_SCOPE_ENTITY_SINGLETON" {
		t.Fatalf("expected EXISTED / CROSS_SCOPE_ENTITY_SINGLETON, got verdict=%s reason=%s", dec.Verdict, dec.Reason)
	}

	// 验证 applyEvidenceGate 直接消费 Candidate.Defect.TargetEntity
	detail := map[string]float64{
		"scope_key_equal":          1,
		"clean_token_similarity":   0.9,
		"line_proximity":           1,
		"defect_class_major_equal": 1,
	}
	cand := Candidate{Defect: models.Defect{ID: 500, TargetEntity: "reqBuf", CleanToken: "if(size>0){work()}"}}
	applyEvidenceGate(obs.Identity, cand, detail)
	if detail["resource_gate"] != 1 {
		t.Fatalf("expected resource_gate=1 derived from TargetEntity, got detail=%+v", detail)
	}
}
