package defectlifecycle

import (
	"errors"
	"testing"

	"code-shield/models"
)

func defect(id uint, status string) models.Defect {
	start := 10
	end := 12
	return models.Defect{
		ID: id, Status: status, NormPath: "src/a.cpp", ScopeKey: "scope-a",
		StmtShape: "shape-a", CleanToken: "delete ptr", DefectClassMajor: "memory",
		Severity: "严重", LineStart: &start, LineEnd: &end, PrevShape: "prev-a", NextShape: "next-a",
		ScopeBodyHash: "scope-body-a",
	}
}

func observation(uid, path, shape, clean string) ObservationGroup {
	return ObservationGroup{
		UID: uid,
		Identity: Identity{
			RepoID: 1, TaskTypeID: 2, NormPath: path, ScopeKey: "scope-a",
			StmtShape: shape, CleanToken: clean, DefectClassMajor: "memory",
			LineStart: 10, LineEnd: 12, Severity: "严重", PrevShape: "prev-a", NextShape: "next-a", Confidence: AnchorHigh,
			K1: "k1-" + uid, K2: "k2-" + uid, F1: "f1-" + uid,
		},
	}
}

func TestRunMatchingClaimsStrongAlias(t *testing.T) {
	existing := defect(7, "ACTIVE")
	group := observation("uid-1", "src/a.cpp", "shape-a", "delete ptr")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasF1, AliasValue: group.Identity.F1, AliasClass: AliasStrong}

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
	second.CleanToken = "delete other"
	firstGroup := observation("uid-1", "src/a.cpp", "shape-a", "delete ptr")
	secondGroup := observation("uid-2", "src/a.cpp", "shape-a", "delete ptr")
	aliases := []models.DefectAlias{
		{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasF1, AliasValue: firstGroup.Identity.F1, AliasClass: AliasStrong},
		{DefectID: 8, RepoID: 1, TaskTypeID: 2, AliasType: AliasF1, AliasValue: secondGroup.Identity.F1, AliasClass: AliasStrong},
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
	existing.StmtShape = "shape-new"
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
	existing.StmtShape = "shape-new"
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
	existing.StmtShape = "shape-new"
	existing.CleanToken = "release resource"
	group := observation("uid-1", "src/a.cpp", "shape-new", "release resource")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasPath, AliasValue: group.Identity.NormPath, AliasClass: AliasBucket}

	result := RunMatching(MatchingInput{
		Observations: []ObservationGroup{group},
		Defects:      []models.Defect{existing},
		Aliases:      []models.DefectAlias{alias},
		Arbitrator:   reviewingArbitrator{},
		Thresholds: MatchThresholds{
			StrongSame: 0.90, AssignBand: 0.75, RejectBelow: 0.45,
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
	existing.StmtShape = "shape-new"
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
	existing.StmtShape = "shape-new"
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
	existing.StmtShape = "shape-new"
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
	existing.StmtShape = "shape-other"
	existing.CleanToken = "unrelated statement"
	existing.DefectClassMajor = "race"
	existing.Severity = "建议"
	start, end := 180, 182
	existing.LineStart, existing.LineEnd = &start, &end
	existing.PrevShape, existing.NextShape = "prev-other", "next-other"

	group := observation("uid-1", "src/a.cpp", "shape-new", "close handle")
	alias := models.DefectAlias{DefectID: 7, RepoID: 1, TaskTypeID: 2, AliasType: AliasF1, AliasValue: group.Identity.F1, AliasClass: AliasStrong}
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
	existing.StmtShape = "shape-new"
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
	existing.StmtShape = "shape-new"
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
