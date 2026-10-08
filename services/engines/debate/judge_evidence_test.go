package debate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
)

func TestBuildJudgeEvidencePacksAndVerify(t *testing.T) {
	root := t.TempDir()
	if err := writeFile(filepath.Join(root, "src", "a.cpp"), "struct Service {\n\tstd::map<int, int> state;\n\tvoid update() {\n\t\tstate[1] = 2;\n\t}\n};\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(root, "src", "caller.cpp"), "Service::update();\n"); err != nil {
		t.Fatal(err)
	}

	ctx := &engines.EngineContext{
		Ctx:       t.Context(),
		CodesPath: root,
	}
	bundle := chunker.SemanticBundle{
		Name:     "bundle-1",
		AllFiles: []string{"src/a.cpp", "src/caller.cpp"},
	}
	candidates := []HunterCandidate{{
		CandidateID:      "H-001",
		FilePath:         "src/a.cpp",
		LineRange:        "2-4",
		TriggerLine:      "state[1] = 2;",
		ScopeSymbol:      "Service::update",
		Title:            "unsynchronized map access",
		AttackHypothesis: "concurrent access can corrupt the map",
		Category:         models.DebateVerdictConfirmed,
	}}
	defenses := map[string]ChallengerDefenseCase{
		"H-001": {CandidateID: "H-001", DefenseVerdict: "CHALLENGE_FAILED"},
	}

	packs, err := BuildJudgeEvidencePacks(ctx, bundle, candidates, defenses)
	if err != nil {
		t.Fatalf("BuildJudgeEvidencePacks failed: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("expected 1 evidence pack, got %d", len(packs))
	}
	if len(packs[0].PrimaryEvidence) == 0 {
		t.Fatalf("expected target source evidence")
	}
	if packs[0].Claim.Claim != candidates[0].AttackHypothesis {
		t.Fatalf("unexpected normalized claim: %q", packs[0].Claim.Claim)
	}
	if !allowedEvidencePath(packs[0], "src/caller.cpp") {
		t.Fatalf("expected caller file to be in evidence allowlist")
	}

	targetRef := packs[0].PrimaryEvidence[0]
	targetRef.Reason = "candidate context"
	callerRef := JudgeEvidenceRef{
		Kind:      JudgeEvidenceCaller,
		Path:      "src/caller.cpp",
		LineRange: "1",
		Snippet:   "Service::update();",
		Reason:    "reachable caller",
	}
	verdict := &JudgeFinalVerdict{
		CandidateID:         "H-001",
		Verdict:             models.DebateVerdictConfirmed,
		SeverityPreliminary: "严重",
		Category:            "测试分类",
		FilePath:            "src/a.cpp",
		LineRange:           "2-4",
		TriggerLine:         "state[1] = 2;",
		ScopeSymbol:         "Service::update",
		Title:               "unsynchronized map access",
		JudgementRationale:  "source and caller verified",
		CodeSnippet:         "state[1] = 2;",
		Suggestion:          "add a mutex",
		Evidence:            []JudgeEvidenceRef{targetRef, callerRef},
	}
	verification, err := VerifyJudgeEvidence(packs[0], verdict)
	if err != nil {
		t.Fatalf("VerifyJudgeEvidence failed: %v", err)
	}
	if !verification.SourceVerified || !verification.ReachabilityVerified {
		t.Fatalf("unexpected verification: %+v", verification)
	}
	if verdict.EvidenceDegraded {
		t.Fatalf("verified verdict must not be degraded")
	}

	targetOnly := *verdict
	targetOnly.Evidence = []JudgeEvidenceRef{targetRef}
	if _, err := VerifyJudgeEvidence(packs[0], &targetOnly); err == nil {
		t.Fatalf("expected CONFIRMED with target-only evidence to be rejected")
	}

	badSnippet := *verdict
	badSnippet.Evidence = []JudgeEvidenceRef{targetRef, callerRef}
	badSnippet.Evidence[1].Snippet = "This call does not exist."
	if _, err := VerifyJudgeEvidence(packs[0], &badSnippet); err == nil || !strings.Contains(err.Error(), "snippet does not match") {
		t.Fatalf("expected snippet mismatch, got %v", err)
	}
}

func TestVerifyChallengerEvidence(t *testing.T) {
	root := t.TempDir()
	if err := writeFile(filepath.Join(root, "src", "a.cpp"), "guarded call\n"); err != nil {
		t.Fatal(err)
	}
	pack := JudgeCaseEvidencePack{
		CandidateID:          "H-001",
		WorkspaceRoot:        root,
		AllowedEvidencePaths: []string{"src/a.cpp"},
	}
	out := &ChallengerOutput{
		DefenseCases: []ChallengerDefenseCase{
			{
				CandidateID:    "H-001",
				DefenseVerdict: "DEFENSE_SUCCESSFUL",
				DefenseArguments: []DefenseArgument{{
					Dimension: "Guards",
					Finding:   "caller is guarded",
					Evidence: []JudgeEvidenceRef{{
						Kind:      JudgeEvidenceCaller,
						Path:      "src/a.cpp",
						LineRange: "1",
						Snippet:   "guarded call",
						Reason:    "outer lock",
					}},
				}},
				MitigatingFactors: "guarded",
			},
		},
	}
	if _, err := VerifyChallengerEvidence([]JudgeCaseEvidencePack{pack}, out); err != nil {
		t.Fatalf("VerifyChallengerEvidence failed: %v", err)
	}

	out.DefenseCases[0].DefenseArguments[0].Evidence[0].Snippet = "not present"
	if _, err := VerifyChallengerEvidence([]JudgeCaseEvidencePack{pack}, out); err == nil || !strings.Contains(err.Error(), "snippet does not match") {
		t.Fatalf("expected snippet mismatch, got %v", err)
	}
}

func TestVerifyJudgeEvidenceMaterializesKnownEvidenceID(t *testing.T) {
	root := t.TempDir()
	if err := writeFile(filepath.Join(root, "src", "a.cpp"), "guarded call\n"); err != nil {
		t.Fatal(err)
	}
	pack := JudgeCaseEvidencePack{
		CandidateID:          "H-001",
		WorkspaceRoot:        root,
		AllowedEvidencePaths: []string{"src/a.cpp"},
		PrimaryEvidence: []JudgeEvidenceRef{{
			EvidenceID: "H-001-target",
			Kind:       JudgeEvidenceTarget,
			Path:       "src/a.cpp",
			LineRange:  "1",
			Snippet:    "guarded call",
			Reason:     "target source",
		}},
	}
	verdict := &JudgeFinalVerdict{
		CandidateID:         "H-001",
		Verdict:             models.DebateVerdictConditional,
		SeverityPreliminary: "严重",
		Category:            "测试分类",
		FilePath:            "src/a.cpp",
		LineRange:           "1",
		TriggerLine:         "guarded call",
		ScopeSymbol:         "Service::update",
		Title:               "test",
		JudgementRationale:  "source verified",
		CodeSnippet:         "guarded call",
		Suggestion:          "review",
		Evidence: []JudgeEvidenceRef{{
			EvidenceID: "H-001-target",
			Kind:       JudgeEvidenceTarget,
			Path:       "src/does-not-exist.cpp",
			LineRange:  "999",
			Snippet:    "model paraphrased snippet",
			Reason:     "drifted repeated evidence",
		}},
	}

	if _, err := VerifyJudgeEvidence(pack, verdict); err != nil {
		t.Fatalf("VerifyJudgeEvidence with known evidence_id failed: %v", err)
	}
	ref := verdict.Evidence[0]
	if ref.Path != "src/a.cpp" || ref.LineRange != "1" || ref.Snippet != "guarded call" {
		t.Fatalf("known evidence_id was not materialized from evidence pack: %+v", ref)
	}
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
