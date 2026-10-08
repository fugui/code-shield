package runner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
)

type classifiedFailureInvoker struct {
	invocations int
	class       invoker.ErrorClass
}

func (m *classifiedFailureInvoker) Name() string { return "classified-failure" }

func (m *classifiedFailureInvoker) Invoke(req invoker.AIRequest) error {
	m.invocations++
	return invoker.NewClassifiedError(m.class, "classified failure")
}

func TestExecuteAnalysisDoesNotRetryNonRetryableClasses(t *testing.T) {
	tempDir := t.TempDir()
	backend := "classified-failure-backend"
	ai := &classifiedFailureInvoker{class: invoker.ErrorClassTimeout}
	invoker.RegisterAIInvoker(backend, ai)

	oldTier1 := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{Resource: backend}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = oldTier1
	})

	ctx := &TaskContext{
		Ctx:       context.Background(),
		Report:    models.TaskReport{ID: 77},
		TaskType:  models.TaskType{Timeout: 1},
		CodesPath: tempDir,
		JsonPath:  filepath.Join(tempDir, "analysis.json"),
	}

	_, err := ExecuteAnalysis(ctx, []string{"main.go"})
	if err == nil {
		t.Fatal("ExecuteAnalysis() error = nil, want failure")
	}
	if got := invoker.ClassifyError(err); got != invoker.ErrorClassTimeout {
		t.Fatalf("ClassifyError() = %q, want %q", got, invoker.ErrorClassTimeout)
	}
	if ai.invocations != 1 {
		t.Fatalf("invocations = %d, want 1 without retry", ai.invocations)
	}
}

func TestExecuteAnalysisUsesConfiguredRetryPolicy(t *testing.T) {
	tempDir := t.TempDir()
	backend := "rate-limited-failure-backend"
	ai := &classifiedFailureInvoker{class: invoker.ErrorClassRateLimited}
	invoker.RegisterAIInvoker(backend, ai)

	oldTier1 := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	oldRetry := models.AppConfig.Scanner.Analysis
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{Resource: backend}
	models.AppConfig.Scanner.Analysis = models.AnalysisRetryConfig{
		MaxRetries:        1,
		RetryBackoffMs:    1,
		MaxBackoffSeconds: 1,
		RetryableErrors:   []string{"rate_limited"},
	}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = oldTier1
		models.AppConfig.Scanner.Analysis = oldRetry
	})

	ctx := &TaskContext{
		Ctx:       context.Background(),
		Report:    models.TaskReport{ID: 79},
		TaskType:  models.TaskType{Timeout: 1},
		CodesPath: tempDir,
		JsonPath:  filepath.Join(tempDir, "analysis.json"),
	}

	_, err := ExecuteAnalysis(ctx, nil)
	if err == nil {
		t.Fatal("ExecuteAnalysis() error = nil, want failure")
	}
	if ai.invocations != 2 {
		t.Fatalf("invocations = %d, want configured initial attempt + one retry", ai.invocations)
	}
	if got := invoker.ClassifyError(err); got != invoker.ErrorClassRateLimited {
		t.Fatalf("ClassifyError() = %q, want %q", got, invoker.ErrorClassRateLimited)
	}
}

func TestAnalysisRetryBackoffIsExponentialAndCapped(t *testing.T) {
	base := 2 * time.Second
	maxBackoff := 8 * time.Second
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 2 * time.Second},
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 3, want: 8 * time.Second},
		{attempt: 6, want: maxBackoff},
	}
	for _, tc := range cases {
		if got := analysisRetryBackoff(base, tc.attempt, maxBackoff); got != tc.want {
			t.Fatalf("analysisRetryBackoff(attempt=%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestExecuteAnalysisStopsWhenContextCanceledDuringBackoff(t *testing.T) {
	tempDir := t.TempDir()
	backend := "unclassified-failure-backend"
	ai := &classifiedFailureInvoker{class: invoker.ErrorClassUnknown}
	invoker.RegisterAIInvoker(backend, ai)

	oldTier1 := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{Resource: backend}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = oldTier1
	})

	taskCtx, cancel := context.WithCancel(context.Background())
	ctx := &TaskContext{
		Ctx:       taskCtx,
		Report:    models.TaskReport{ID: 78},
		TaskType:  models.TaskType{Timeout: 1},
		CodesPath: tempDir,
		JsonPath:  filepath.Join(tempDir, "analysis.json"),
	}

	done := make(chan error, 1)
	go func() {
		_, err := ExecuteAnalysis(ctx, nil)
		done <- err
	}()

	cancel()
	err := <-done
	if err == nil {
		t.Fatal("ExecuteAnalysis() error = nil, want cancellation")
	}
	if got := invoker.ClassifyError(err); got != invoker.ErrorClassCanceled {
		t.Fatalf("ClassifyError() = %q, want %q", got, invoker.ErrorClassCanceled)
	}
	if ai.invocations > 2 {
		t.Fatalf("invocations = %d, want immediate stop after first failure", ai.invocations)
	}
}
