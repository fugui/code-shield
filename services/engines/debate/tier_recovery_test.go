package debate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
)

type recoveryTestInvoker struct {
	name     string
	calls    int
	fail     func(call int) error
	sleep    time.Duration
	artifact string
	payload  func(call int) string
}

func (inv *recoveryTestInvoker) Name() string { return inv.name }

func (inv *recoveryTestInvoker) Invoke(req invoker.AIRequest) error {
	inv.calls++
	if inv.sleep > 0 {
		select {
		case <-req.ParentContext.Done():
			return invoker.WrapClassifiedError(invoker.ClassifyError(req.ParentContext.Err()), req.ParentContext.Err(), "invocation ended")
		case <-time.After(inv.sleep):
		}
	}
	payload := fmt.Sprintf(`{"schema":%q,"candidates":[]}`, CandidatesArtifactSchemaV1)
	if inv.payload != nil {
		payload = inv.payload(inv.calls)
	} else if inv.artifact != "" {
		payload = inv.artifact
	}
	if err := os.WriteFile(req.OutputPath, []byte(payload), 0644); err != nil {
		return err
	}
	if inv.fail != nil {
		if err := inv.fail(inv.calls); err != nil {
			return err
		}
	}
	return nil
}

func registerRecoveryInvoker(t *testing.T, name string, fail func(call int) error) *recoveryTestInvoker {
	t.Helper()
	inv := &recoveryTestInvoker{name: name, fail: fail}
	invoker.RegisterAIInvoker(name, inv)
	return inv
}

func configureRecoveryTier(t *testing.T, resources []string, recovery models.TierRecoveryConfig) {
	t.Helper()
	prevTier := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource:              resources[0],
		Resources:             resources,
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		MaxSplitDepth:         1,
		Recovery:              &recovery,
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = prevTier })
}

func runHunterRecovery(t *testing.T, files []string, budget time.Duration, attemptSeconds int) (*HunterOutput, *splitStats, error) {
	t.Helper()
	tempDir := t.TempDir()
	ctx := &engines.EngineContext{
		Ctx:        beginSplitStats(context.Background()),
		CodesPath:  tempDir,
		EngineMode: "debate_full",
	}
	stageCtx, cancel := context.WithTimeout(ctx.Ctx, budget)
	t.Cleanup(cancel)
	ctx.Ctx = stageCtx
	bundle := chunker.SemanticBundle{Name: files[0], PrimaryFiles: files, AllFiles: files}
	out, _, err := (&DebateEngine{}).runHunterStageWithBudget(
		ctx,
		bundle,
		filepath.Join(tempDir, "hunter.json"),
		max(1, int(budget.Seconds())),
		attemptSeconds,
	)
	return out, splitStatsFromContext(ctx.Ctx), err
}

func sameResourceChain(stats *splitStats) bool {
	return stats != nil && len(stats.ResourceChain) > 0 && len(uniqueStrings(stats.ResourceChain)) == 1
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	unique := make([]string, 0)
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func TestHunterRecoveryIdleTimeoutRetriesSameResource(t *testing.T) {
	primary := registerRecoveryInvoker(t, "recovery-idle-timeout", func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassIdleTimeout, "idle timeout")
		}
		return nil
	})
	configureRecoveryTier(t, []string{primary.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryOn:                []string{"idle_timeout"},
	})
	out, stats, err := runHunterRecovery(t, []string{"src/a.cpp"}, 10*time.Second, 10)
	if err != nil || out == nil {
		t.Fatalf("recovery failed: out=%+v err=%v", out, err)
	}
	if primary.calls != 2 {
		t.Fatalf("primary calls = %d, want 2", primary.calls)
	}
	if !sameResourceChain(stats) {
		t.Fatalf("resource chain = %#v, want one stable resource", stats.ResourceChain)
	}
	if stats.ResourceFailovers != 0 {
		t.Fatalf("resource failovers = %d, want 0", stats.ResourceFailovers)
	}
}

func TestHunterRecoveryOutputMissingRetriesAfterContinuation(t *testing.T) {
	primary := registerRecoveryInvoker(t, "recovery-output-missing", func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassOutputMissing, "continuation failed; output missing")
		}
		return nil
	})
	configureRecoveryTier(t, []string{primary.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryOn:                []string{"output_missing"},
	})
	out, stats, err := runHunterRecovery(t, []string{"src/a.cpp"}, 10*time.Second, 10)
	if err != nil || out == nil {
		t.Fatalf("recovery failed: out=%+v err=%v", out, err)
	}
	if primary.calls != 2 {
		t.Fatalf("primary calls = %d, want fresh retry after continuation", primary.calls)
	}
	if !sameResourceChain(stats) || stats.ResourceFailovers != 0 {
		t.Fatalf("chain=%#v failovers=%d, want stable resource and zero failovers", stats.ResourceChain, stats.ResourceFailovers)
	}
}

func TestHunterRecoveryIdleTimeoutSplitsMultiFileBundle(t *testing.T) {
	primary := registerRecoveryInvoker(t, "recovery-idle-split", func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassIdleTimeout, "idle timeout")
		}
		return nil
	})
	primary.payload = func(call int) string {
		if call == 2 {
			return fmt.Sprintf(`{"schema":%q,"candidates":[{"candidate_id":"H-001","title":"a","category_code":"内存安全","severity":"严重","file_path":"src/a.cpp","line_range":"1-2","trigger_line":1,"scope_symbol":"A","code_snippet":"x","trigger_condition":"x"}]}`, CandidatesArtifactSchemaV1)
		}
		if call == 3 {
			return fmt.Sprintf(`{"schema":%q,"candidates":[{"candidate_id":"H-001","title":"b","category_code":"内存安全","severity":"严重","file_path":"src/b.cpp","line_range":"1-2","trigger_line":1,"scope_symbol":"B","code_snippet":"x","trigger_condition":"x"}]}`, CandidatesArtifactSchemaV1)
		}
		return fmt.Sprintf(`{"schema":%q,"candidates":[]}`, CandidatesArtifactSchemaV1)
	}
	configureRecoveryTier(t, []string{primary.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		SplitOn:                []string{"idle_timeout"},
		RetryOn:                []string{"idle_timeout"},
	})
	out, stats, err := runHunterRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, 10*time.Second, 10)
	if err != nil || out == nil || len(out.Candidates) != 2 {
		t.Fatalf("idle split failed: out=%+v err=%v", out, err)
	}
	if primary.calls != 3 {
		t.Fatalf("calls=%d, want parent plus two split attempts", primary.calls)
	}
	if stats == nil || stats.Depth != 1 || stats.Count != 1 || stats.ResourceFailovers != 0 {
		t.Fatalf("stats=%+v, want one idle timeout split", stats)
	}
}

func TestHunterRecoveryTimeoutSplitsMultiFileBundle(t *testing.T) {
	primary := registerRecoveryInvoker(t, "recovery-split-timeout", func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassTimeout, "bundle timeout")
		}
		return nil
	})
	configureRecoveryTier(t, []string{primary.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		SplitOn:                []string{"timeout"},
		RetryOn:                []string{"timeout"},
	})
	out, stats, err := runHunterRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, 10*time.Second, 10)
	if err != nil || out == nil {
		t.Fatalf("split recovery failed: out=%+v err=%v", out, err)
	}
	if primary.calls != 3 {
		t.Fatalf("primary calls = %d, want one parent and two split children", primary.calls)
	}
	if stats == nil || stats.Depth != 1 || stats.Count != 1 {
		t.Fatalf("split stats = %+v, want one split", stats)
	}
	if stats.ResourceFailovers != 0 {
		t.Fatalf("resource failovers = %d, want 0", stats.ResourceFailovers)
	}
}

func TestHunterRecoveryTimeoutRetriesSingleFileBundle(t *testing.T) {
	primary := registerRecoveryInvoker(t, "recovery-single-timeout", func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassTimeout, "single-file timeout")
		}
		return nil
	})
	configureRecoveryTier(t, []string{primary.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryOn:                []string{"timeout"},
	})
	out, stats, err := runHunterRecovery(t, []string{"src/a.cpp"}, 10*time.Second, 10)
	if err != nil || out == nil {
		t.Fatalf("retry recovery failed: out=%+v err=%v", out, err)
	}
	if primary.calls != 2 {
		t.Fatalf("primary calls = %d, want one fresh retry", primary.calls)
	}
	if stats == nil || stats.Depth != 0 || stats.Count != 0 {
		t.Fatalf("split stats = %+v, want no split", stats)
	}
	if !sameResourceChain(stats) || stats.ResourceFailovers != 0 {
		t.Fatalf("chain=%#v failovers=%d, want stable resource", stats.ResourceChain, stats.ResourceFailovers)
	}
}

func TestHunterRecoveryContractMismatchRepairsWithoutNormalRetry(t *testing.T) {
	prevRepair := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact.SchemaRepairResource = "recovery-schema-repair"
	models.AppConfig.Scanner.Artifact.MaxSchemaRepairAttempts = 1
	models.AppConfig.Scanner.Artifact.SchemaRepairTimeoutSeconds = 10
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = prevRepair })

	primary := &recoveryTestInvoker{name: "recovery-contract", fail: func(call int) error {
		if call > 1 {
			t.Fatalf("ordinary retry attempted after contract mismatch (call %d)", call)
		}
		return nil
	}}
	primary.artifact = `{"schema":"wrong-schema"}`
	invoker.RegisterAIInvoker(primary.name, primary)
	repair := &recoveryTestInvoker{name: "recovery-schema-repair"}
	repair.artifact = `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[{"candidate_id":"H-001","file_path":"src/a.cpp","line_range":"1-2","trigger_line":"x","scope_symbol":"A","category":"测试分类","title":"valid","code_snippet":"x","trigger_condition":"x"}]}`
	invoker.RegisterAIInvoker(repair.name, repair)
	configureRecoveryTier(t, []string{primary.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryOn:                []string{"output_missing"},
	})

	out, stats, err := runHunterRecovery(t, []string{"src/a.cpp"}, 10*time.Second, 10)
	if err != nil || out == nil {
		t.Fatalf("contract repair failed: out=%+v err=%v", out, err)
	}
	if primary.calls != 1 || repair.calls != 1 {
		t.Fatalf("calls primary=%d repair=%d, want exactly one of each", primary.calls, repair.calls)
	}
	if stats == nil || stats.ContractRepairs != 1 {
		t.Fatalf("contract repairs = %+v, want 1", stats)
	}
}

func TestHunterRecoveryStopsImmediatelyWhenBudgetExhausted(t *testing.T) {
	primary := registerRecoveryInvoker(t, "recovery-budget", func(call int) error { return nil })
	primary.sleep = 1200 * time.Millisecond
	configureRecoveryTier(t, []string{primary.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryBackoffMs:         1,
		RetryOn:                []string{"timeout"},
	})
	_, stats, err := runHunterRecovery(t, []string{"src/a.cpp"}, 1*time.Second, 10)
	if err == nil {
		t.Fatal("budget exhaustion returned success")
	}
	if primary.calls != 1 {
		t.Fatalf("primary calls = %d, want no attempt after budget exhaustion", primary.calls)
	}
	if stats == nil || len(stats.ResourceChain) != 1 {
		t.Fatalf("resource chain = %#v, want one failed attempt", stats.ResourceChain)
	}
}

func TestHunterRecoveryDefaultConfigNeverFailsOverResource(t *testing.T) {
	primary := registerRecoveryInvoker(t, "recovery-default-primary", func(call int) error {
		return invoker.NewClassifiedError(invoker.ErrorClassResourceUnavailable, "resource unavailable")
	})
	alternate := registerRecoveryInvoker(t, "recovery-default-alternate", func(call int) error { return nil })
	configureRecoveryTier(t, []string{primary.name, alternate.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryOn:                []string{"resource_unavailable"},
		CandidateFailoverOn:    []string{"resource_unavailable"},
	})
	_, stats, err := runHunterRecovery(t, []string{"src/a.cpp"}, 10*time.Second, 10)
	if err == nil {
		t.Fatal("unavailable resource unexpectedly succeeded")
	}
	if alternate.calls != 0 {
		t.Fatalf("alternate calls = %d, want default no failover", alternate.calls)
	}
	if stats == nil || stats.ResourceFailovers != 0 || len(uniqueStrings(stats.ResourceChain)) != 1 {
		t.Fatalf("stats=%+v, want zero failovers and stable resource", stats)
	}
}

func TestHunterRecoveryExplicitFailoverOnlyForAvailabilityErrors(t *testing.T) {
	primary := registerRecoveryInvoker(t, "recovery-explicit-primary", func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassResourceUnavailable, "resource unavailable")
		}
		return nil
	})
	alternate := registerRecoveryInvoker(t, "recovery-explicit-alternate", func(call int) error { return nil })
	configureRecoveryTier(t, []string{primary.name, alternate.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 2,
		MaxCandidateFailovers:  1,
		CandidateFailoverOn:    []string{"resource_unavailable"},
	})
	out, stats, err := runHunterRecovery(t, []string{"src/a.cpp"}, 10*time.Second, 10)
	if err != nil || out == nil {
		t.Fatalf("explicit failover failed: out=%+v err=%v", out, err)
	}
	if primary.calls != 1 || alternate.calls != 1 {
		t.Fatalf("calls primary=%d alternate=%d, want one failover", primary.calls, alternate.calls)
	}
	if stats == nil || stats.ResourceFailovers != 1 || len(uniqueStrings(stats.ResourceChain)) != 2 {
		t.Fatalf("stats=%+v, want one observed resource failover", stats)
	}
}

func assessmentPayload() string {
	return `{"schema":"code-shield.unit-assessments.v2","assessments":[
		{"unit_ref":"u001","outcome":"pass","summary":"safe"},
		{"unit_ref":"u002","outcome":"pass","summary":"safe"}
	]}`
}

func TestSpecializedAssessmentIdleTimeoutRetriesSameResource(t *testing.T) {
	primary := registerRecoveryInvoker(t, "assessment-idle-timeout", func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassIdleTimeout, "idle timeout")
		}
		return nil
	})
	primary.payload = func(int) string { return assessmentPayload() }
	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		SplitOn:                []string{"timeout"},
		RetryOn:                []string{"idle_timeout"},
	})
	findings, records, stats, err := runSpecializedAssessmentRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, primary.name)
	if err != nil {
		t.Fatalf("assessment retry failed: %v", err)
	}
	if len(records) != 2 || len(findings) != 0 {
		t.Fatalf("assessment result records=%d findings=%d, want 2/0", len(records), len(findings))
	}
	if primary.calls != 2 {
		t.Fatalf("assessment calls = %d, want 2", primary.calls)
	}
	if !sameResourceChain(stats) || stats.ResourceFailovers != 0 {
		t.Fatalf("chain=%#v failovers=%d, want same resource", stats.ResourceChain, stats.ResourceFailovers)
	}
}

func TestSpecializedAssessmentEmptyArtifactRetriesSameResource(t *testing.T) {
	enabled := true
	previousEnabled := models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled
	models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = &enabled
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = previousEnabled })

	primary := registerRecoveryInvoker(t, "assessment-empty-output", func(call int) error { return nil })
	primary.payload = func(call int) string {
		if call == 1 {
			return "   "
		}
		return assessmentPayload()
	}
	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       2,
		MaxAttemptsPerResource: 2,
		RetryBackoffMs:         1,
		RetryOn:                []string{"output_missing"},
	})
	findings, records, stats, err := runSpecializedAssessmentRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, primary.name)
	if err != nil {
		t.Fatalf("empty artifact recovery failed: %v", err)
	}
	if len(records) != 2 || len(findings) != 0 {
		t.Fatalf("records=%d findings=%d, want two assessments and no findings", len(records), len(findings))
	}
	if primary.calls != 2 {
		t.Fatalf("primary calls = %d, want one fresh attempt", primary.calls)
	}
	if stats == nil || stats.FreshRetries != 1 || stats.ResourceFailovers != 0 || !sameResourceChain(stats) {
		t.Fatalf("stats=%+v, want one same-resource fresh attempt", stats)
	}
}

func TestSpecializedAssessmentEmptyArtifactFailsOverWhenExplicitlyConfigured(t *testing.T) {
	enabled := true
	previousEnabled := models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled
	models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = &enabled
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = previousEnabled })

	primary := registerRecoveryInvoker(t, "assessment-empty-primary", func(call int) error { return nil })
	primary.payload = func(int) string { return "   " }
	alternate := registerRecoveryInvoker(t, "assessment-empty-alternate", func(call int) error { return nil })
	alternate.payload = func(int) string { return assessmentPayload() }
	configureRecoveryTier(t, []string{primary.name, alternate.name}, models.TierRecoveryConfig{
		MaxTotalAttempts:       2,
		MaxAttemptsPerResource: 1,
		MaxCandidateFailovers:  1,
		RetryBackoffMs:         1,
		RetryOn:                []string{"output_missing"},
		CandidateFailoverOn:    []string{"output_missing"},
	})

	findings, records, stats, err := runSpecializedAssessmentRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, primary.name)
	if err != nil {
		t.Fatalf("empty artifact failover failed: %v", err)
	}
	if len(records) != 2 || len(findings) != 0 {
		t.Fatalf("records=%d findings=%d, want two assessments and no findings", len(records), len(findings))
	}
	if primary.calls != 1 || alternate.calls != 1 {
		t.Fatalf("calls primary=%d alternate=%d, want one resource failover", primary.calls, alternate.calls)
	}
	if stats == nil || stats.ResourceFailovers != 1 || stats.FreshRetries != 0 || len(uniqueStrings(stats.ResourceChain)) != 2 {
		t.Fatalf("stats=%+v, want one output-missing failover", stats)
	}
}

func TestSpecializedAssessmentContractMismatchRepairsWithoutNormalRetry(t *testing.T) {
	primary := registerRecoveryInvoker(t, "assessment-contract", func(call int) error { return nil })
	primary.payload = func(call int) string {
		if call == 1 {
			return `{"schema":"wrong-schema"}`
		}
		return assessmentPayload()
	}
	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryOn:                []string{"idle_timeout"},
	})
	_, records, stats, err := runSpecializedAssessmentRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, primary.name)
	if err != nil {
		t.Fatalf("assessment repair failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if primary.calls != 2 {
		t.Fatalf("calls = %d, want initial plus schema repair", primary.calls)
	}
	if stats == nil || stats.ContractRepairs != 1 {
		t.Fatalf("contract repairs = %+v, want 1", stats)
	}
}

func TestSpecializedAssessmentRepairsThenRetriesSameResource(t *testing.T) {
	primary := registerRecoveryInvoker(t, "assessment-repair-retry", func(call int) error { return nil })
	primary.payload = func(call int) string {
		if call == 2 {
			return `{"schema":"wrong-schema"}`
		}
		if call == 4 {
			return assessmentPayload()
		}
		return `{"schema":"wrong-schema"}`
	}
	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryBackoffMs:         1,
		RetryOn:                []string{"idle_timeout"},
	})
	findings, records, stats, err := runSpecializedAssessmentRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, primary.name)
	if err != nil || len(records) != 2 || len(findings) != 0 {
		t.Fatalf("repair retry failed: records=%d findings=%d err=%v", len(records), len(findings), err)
	}
	if primary.calls != 4 {
		t.Fatalf("calls=%d, want initial repair, failed repair, fresh attempt, successful repair", primary.calls)
	}
	if stats == nil || stats.FreshRetries != 1 || len(stats.ResourceChain) != 2 || !sameResourceChain(stats) {
		t.Fatalf("stats=%+v, want one same-resource fresh retry", stats)
	}
	if len(stats.QueueWaitMS) != 2 || len(stats.DurationSeconds) != 2 {
		t.Fatalf("timings=%#v/%#v, want one entry per logical attempt", stats.QueueWaitMS, stats.DurationSeconds)
	}
}

func TestSpecializedAssessmentRejectsDriftUncheckedRepair(t *testing.T) {
	enabled := true
	prevRepair := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = &enabled
	models.AppConfig.Scanner.Artifact.SchemaRepairResource = "assessment-drift-unchecked-repair"
	models.AppConfig.Scanner.Artifact.MaxSchemaRepairAttempts = 1
	models.AppConfig.Scanner.Artifact.SchemaRepairTimeoutSeconds = 10
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = prevRepair })

	primary := registerRecoveryInvoker(t, "assessment-drift-unchecked", func(call int) error { return nil })
	primary.payload = func(int) string { return "{broken" }
	repair := registerRecoveryInvoker(t, models.AppConfig.Scanner.Artifact.SchemaRepairResource, func(call int) error { return nil })
	repair.payload = func(int) string { return assessmentPayload() }
	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       2,
		MaxAttemptsPerResource: 2,
		RetryOn:                []string{"idle_timeout"},
	})

	findings, records, stats, err := runSpecializedAssessmentRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, primary.name)
	if err == nil || invoker.ClassifyError(err) != invoker.ErrorClassContractMismatch {
		t.Fatalf("error=%v class=%s, want contract mismatch", err, invoker.ClassifyError(err))
	}
	if len(findings) != 0 || len(records) != 0 {
		t.Fatalf("findings=%d records=%d, want unchecked repair to produce no assessments", len(findings), len(records))
	}
	if primary.calls != 1 || repair.calls != 1 {
		t.Fatalf("calls primary=%d repair=%d, want exactly one of each", primary.calls, repair.calls)
	}
	if stats == nil || stats.FreshRetries != 0 || stats.ResourceFailovers != 0 {
		t.Fatalf("stats=%+v, want no ordinary retry after drift-unchecked rejection", stats)
	}
}

func TestSpecializedAssessmentExplicitlyFailsAfterRepairAttemptsExhausted(t *testing.T) {
	enabled := true
	prevRepair := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = &enabled
	models.AppConfig.Scanner.Artifact.SchemaRepairResource = "assessment-exhausted-repair"
	models.AppConfig.Scanner.Artifact.MaxSchemaRepairAttempts = 2
	models.AppConfig.Scanner.Artifact.SchemaRepairTimeoutSeconds = 10
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = prevRepair })

	primary := registerRecoveryInvoker(t, "assessment-exhausted", func(call int) error { return nil })
	primary.payload = func(int) string { return `{"schema":"wrong-schema"}` }
	repair := registerRecoveryInvoker(t, models.AppConfig.Scanner.Artifact.SchemaRepairResource, func(call int) error { return nil })
	repair.payload = func(int) string { return "{broken" }
	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		RetryOn:                []string{"idle_timeout"},
	})

	findings, records, stats, err := runSpecializedAssessmentRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, primary.name)
	if err == nil || invoker.ClassifyError(err) != invoker.ErrorClassContractMismatch {
		t.Fatalf("error=%v class=%s, want explicit contract failure", err, invoker.ClassifyError(err))
	}
	if len(findings) != 0 || len(records) != 0 {
		t.Fatalf("findings=%d records=%d, want failed bundle to omit assessments", len(findings), len(records))
	}
	if primary.calls != 1 || repair.calls != 2 {
		t.Fatalf("calls primary=%d repair=%d, want one generation and two directed repairs", primary.calls, repair.calls)
	}
	if stats == nil || stats.FreshRetries != 0 || stats.ResourceFailovers != 0 {
		t.Fatalf("stats=%+v, want repair exhaustion without ordinary retry", stats)
	}
}

func TestSpecializedAssessmentTimeoutSplitsMultiFileBundle(t *testing.T) {
	primary := registerRecoveryInvoker(t, "assessment-split-timeout", func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassTimeout, "assessment timeout")
		}
		return nil
	})
	primary.payload = func(call int) string {
		if call == 2 {
			return `{"schema":"code-shield.unit-assessments.v2","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"safe"}]}`
		}
		if call == 3 {
			return `{"schema":"code-shield.unit-assessments.v2","assessments":[{"unit_ref":"u002","outcome":"pass","summary":"safe"}]}`
		}
		return assessmentPayload()
	}
	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       3,
		MaxAttemptsPerResource: 3,
		SplitOn:                []string{"timeout"},
		RetryOn:                []string{"timeout"},
	})
	_, records, stats, err := runSpecializedAssessmentRecovery(t, []string{"src/a.cpp", "src/b.cpp"}, primary.name)
	if err != nil {
		t.Fatalf("assessment split failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if primary.calls != 3 {
		t.Fatalf("calls = %d, want parent plus two children", primary.calls)
	}
	if stats == nil || stats.Depth != 1 || stats.Count != 1 || stats.ResourceFailovers != 0 {
		t.Fatalf("split stats=%+v, want one split and zero failovers", stats)
	}
}

func assessmentBundle(units ...string) chunker.SemanticBundle {
	planUnits := make([]coverage.PlanUnit, 0, len(units))
	refs := make([]string, 0, len(units))
	displayNames := make(map[string]string, len(units))
	for _, path := range units {
		unitID := fmt.Sprintf("%s#h1", path)
		planUnits = append(planUnits, coverage.PlanUnit{
			ID:          unitID,
			Kind:        coverage.PlanUnitChangeHunk,
			Path:        path,
			DisplayName: fmt.Sprintf("%s:1", path),
		})
		refs = append(refs, unitID)
		displayNames[unitID] = fmt.Sprintf("%s:1", path)
	}
	return chunker.SemanticBundle{
		Name:         "assessment-001",
		PrimaryFiles: units,
		AllFiles:     units,
		PrimaryUnits: planUnits,
	}
}

func configureAssessmentRecovery(t *testing.T, primary string, recovery models.TierRecoveryConfig) {
	t.Helper()
	configureRecoveryTier(t, []string{primary}, recovery)
}

func runSpecializedAssessmentRecovery(t *testing.T, files []string, invokerName string) ([]models.AnalysisFinding, []coverage.AssessmentRecord, *splitStats, error) {
	t.Helper()
	tempDir := t.TempDir()
	ctx := &engines.EngineContext{
		Ctx:               beginSplitStats(context.Background()),
		CodesPath:         tempDir,
		EngineMode:        "debate_full",
		AssessmentProfile: "changereview",
		AllowedCategories: []string{"无问题", "内存安全"},
	}
	bundle := assessmentBundle(files...)
	findings, records, _, err := (&DebateEngine{}).runSpecializedAssessment(ctx, bundle, filepath.Join(tempDir, "assessment.json"))
	return findings, records, splitStatsFromContext(ctx.Ctx), err
}
