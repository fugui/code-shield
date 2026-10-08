package debate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
)

type outputMissingInvoker struct {
	calls      int
	failures   int
	alwaysFail bool
}

func (i *outputMissingInvoker) Name() string { return "mock-output-missing" }

func (i *outputMissingInvoker) Invoke(req invoker.AIRequest) error {
	i.calls++
	if i.alwaysFail || i.calls <= i.failures {
		return invoker.NewClassifiedError(
			invoker.ErrorClassOutputMissing,
			"opencode completed with exit code 0 but target output was not generated",
		)
	}
	return os.WriteFile(req.OutputPath, []byte(fmt.Sprintf(
		`{"schema":%q,"candidates":[]}`,
		CandidatesArtifactSchemaV1,
	)), 0644)
}

func configureOutputMissingRetryTest(t *testing.T) {
	t.Helper()
	prevTier := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource:              "mock-output-missing",
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		MaxSplitDepth:         1,
	}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = prevTier
	})
}

func TestRunHunterStageRetriesOutputMissingInFreshSession(t *testing.T) {
	configureOutputMissingRetryTest(t)
	tempDir := t.TempDir()
	mock := &outputMissingInvoker{failures: 1}
	invoker.RegisterAIInvoker("mock-output-missing", mock)

	ctx := &engines.EngineContext{
		Ctx:        beginSplitStats(context.Background()),
		CodesPath:  tempDir,
		EngineMode: "debate_full",
	}
	bundle := chunker.SemanticBundle{
		Name:         "single",
		PrimaryFiles: []string{"src/a.cpp"},
		AllFiles:     []string{"src/a.cpp"},
	}

	out, _, err := (&DebateEngine{}).runHunterStageWithBudget(
		ctx, bundle, filepath.Join(tempDir, "hunter.json"), 10, 10,
	)
	if err != nil {
		t.Fatalf("runHunterStageWithBudget failed: %v", err)
	}
	if out == nil {
		t.Fatalf("unexpected hunter output: %+v", out)
	}
	if mock.calls != 2 {
		t.Fatalf("invocations = %d, want 2", mock.calls)
	}
	if stats := splitStatsFromContext(ctx.Ctx); stats == nil || stats.FreshRetries != 1 {
		t.Fatalf("fresh retry stats = %+v, want 1", stats)
	}
}

func TestRunHunterStageSplitsWhenFreshRetryStillMissesOutput(t *testing.T) {
	configureOutputMissingRetryTest(t)
	tempDir := t.TempDir()
	mock := &outputMissingInvoker{failures: 2}
	invoker.RegisterAIInvoker("mock-output-missing", mock)

	ctx := &engines.EngineContext{
		Ctx:        beginSplitStats(context.Background()),
		CodesPath:  tempDir,
		EngineMode: "debate_full",
	}
	bundle := chunker.SemanticBundle{
		Name:         "pair",
		PrimaryFiles: []string{"src/a.cpp", "src/b.cpp"},
		AllFiles:     []string{"src/a.cpp", "src/b.cpp"},
	}

	out, _, err := (&DebateEngine{}).runHunterStageWithBudget(
		ctx, bundle, filepath.Join(tempDir, "hunter.json"), 20, 10,
	)
	if err != nil {
		t.Fatalf("runHunterStageWithBudget failed: %v", err)
	}
	if out == nil || len(out.Candidates) != 0 {
		t.Fatalf("unexpected merged hunter output: %+v", out)
	}
	if mock.calls != 4 {
		t.Fatalf("invocations = %d, want 4 (original, fresh retry, two halves)", mock.calls)
	}
	stats := splitStatsFromContext(ctx.Ctx)
	if stats == nil || stats.FreshRetries != 1 || stats.Depth != 1 || stats.Count != 1 {
		t.Fatalf("stats = %+v, want one fresh retry and one split", stats)
	}
}

func TestRunHunterStageKeepsOutputMissingWhenRetryCannotSplit(t *testing.T) {
	configureOutputMissingRetryTest(t)
	tempDir := t.TempDir()
	mock := &outputMissingInvoker{alwaysFail: true}
	invoker.RegisterAIInvoker("mock-output-missing", mock)

	ctx := &engines.EngineContext{
		Ctx:        beginSplitStats(context.Background()),
		CodesPath:  tempDir,
		EngineMode: "debate_full",
	}
	bundle := chunker.SemanticBundle{
		Name:         "single",
		PrimaryFiles: []string{"src/a.cpp"},
		AllFiles:     []string{"src/a.cpp"},
	}

	_, _, err := (&DebateEngine{}).runHunterStageWithBudget(
		ctx, bundle, filepath.Join(tempDir, "hunter.json"), 10, 10,
	)
	if err == nil {
		t.Fatal("runHunterStageWithBudget failed: err = nil, want output_missing")
	}
	if got := invoker.ClassifyError(err); got != invoker.ErrorClassOutputMissing {
		t.Fatalf("ClassifyError = %q, want output_missing", got)
	}
	if mock.calls != 2 {
		t.Fatalf("invocations = %d, want 2", mock.calls)
	}
}
