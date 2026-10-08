package debate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
)

type judgeRetryInvoker struct {
	calls int
}

func (i *judgeRetryInvoker) Name() string { return "mock-judge-retry" }

func (i *judgeRetryInvoker) Invoke(req invoker.AIRequest) error {
	i.calls++
	if i.calls == 1 {
		return invoker.NewClassifiedError(
			invoker.ErrorClassOutputMissing,
			"codex produced empty final message",
		)
	}
	raw := fmt.Sprintf(`{
  "schema": %q,
  "final_verdicts": [{
    "candidate_id": "H-001",
    "verdict": "CONDITIONAL",
    "severity_preliminary": "一般",
    "category": "并发安全",
    "file_path": "src/a.cpp",
    "line_range": "1-3",
    "trigger_line": "unsafe();",
    "scope_symbol": "run",
    "title": "unsafe call",
    "judgement_rationale": "source confirms unsafe call",
    "code_snippet": "unsafe();",
    "suggestion": "add guard",
    "evidence": [{
      "evidence_id": "H-001-target",
      "kind": "target",
      "path": "src/a.cpp",
      "line_range": "1-3",
      "snippet": "unsafe();"
    }]
  }]
}`, FinalVerdictsArtifactSchemaV2)
	return os.WriteFile(req.OutputPath, []byte(raw), 0644)
}

func TestRunJudgeStageRetriesOutputMissingInFreshSession(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "src", "a.cpp"), []byte("void run() {\n  unsafe();\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	backend := "mock-judge-retry"
	mock := &judgeRetryInvoker{}
	invoker.RegisterAIInvoker(backend, mock)
	prevTier := models.AppConfig.Scanner.Debate.Tiers.Tier3Judge
	models.AppConfig.Scanner.Debate.Tiers.Tier3Judge = models.TierBindingConfig{
		Resource:              backend,
		TimeoutSeconds:        1200,
		AttemptTimeoutSeconds: 1200,
		IdleTimeoutSeconds:    300,
	}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Debate.Tiers.Tier3Judge = prevTier
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	engineCtx := &engines.EngineContext{
		Ctx:               ctx,
		CodesPath:         tempDir,
		EngineMode:        "debate_full",
		AllowedCategories: []string{"并发安全"},
	}
	bundle := chunker.SemanticBundle{
		Name:         "single",
		PrimaryFiles: []string{"src/a.cpp"},
		AllFiles:     []string{"src/a.cpp"},
	}
	hunter := &HunterOutput{Candidates: []HunterCandidate{{
		CandidateID: "H-001",
		FilePath:    "src/a.cpp",
		LineRange:   "1-3",
		TriggerLine: "unsafe();",
		ScopeSymbol: "run",
		CodeSnippet: "unsafe();",
		Category:    "并发安全",
		Title:       "unsafe call",
	}}}
	challenger := &ChallengerOutput{DefenseCases: []ChallengerDefenseCase{{
		CandidateID:       "H-001",
		DefenseVerdict:    "CHALLENGE_FAILED",
		MitigatingFactors: "none",
	}}}

	out, _, err := (&DebateEngine{}).runJudgeStage(
		engineCtx, bundle, hunter, challenger, filepath.Join(tempDir, "judge.json"),
	)
	if err != nil {
		t.Fatalf("runJudgeStage failed: %v", err)
	}
	if mock.calls != 2 {
		t.Fatalf("invocations = %d, want 2", mock.calls)
	}
	if len(out.FinalVerdicts) != 1 || out.FinalVerdicts[0].Verdict != "CONDITIONAL" {
		t.Fatalf("unexpected judge output: %+v", out)
	}
	if workCtx := out.FinalVerdicts[0]; workCtx.EvidenceDegraded {
		t.Fatal("retry output should not be marked degraded")
	}
}
