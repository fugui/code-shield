package defectlifecycle

import (
	"errors"
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
