package dispatcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
)

func configureTierRecoveryTest(t *testing.T, resources []string, recovery models.TierRecoveryConfig) {
	t.Helper()
	previous := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource:              resources[0],
		Resources:             resources,
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		Recovery:              &recovery,
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = previous })
}

func newHealthTestDispatcher(t *testing.T) *ModelDispatcher {
	t.Helper()
	dispatcher := &ModelDispatcher{
		manualScale:   1,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	dispatcher.cond = sync.NewCond(&dispatcher.mu)
	dispatcher.resources = []*ModelResource{
		{ID: "recovery-primary", Driver: "recovery-primary", Concurrent: 1},
		{ID: "recovery-alternate", Driver: "recovery-alternate", Concurrent: 1},
	}
	GlobalDispatcher = dispatcher
	GlobalTierRouter = NewTierRouter(dispatcher)
	t.Cleanup(func() {
		close(dispatcher.stopHeartbeat)
		GlobalDispatcher = nil
		GlobalTierRouter = nil
	})
	return dispatcher
}

func TestTierRecoveryDefaultConfigNeverFailsOverResource(t *testing.T) {
	configureTierRecoveryTest(t, []string{"recovery-primary", "recovery-alternate"}, models.TierRecoveryConfig{})
	calls := 0
	stats := &TierRecoveryStats{}
	_, candidate, _, err := RunTierInvocationWithRecovery(
		context.Background(), "tier1_hunter", 10, stats,
		func(context.Context, TierCandidate, int, *invoker.InvocationMetrics) (string, int64, error) {
			calls++
			return "", 0, invoker.NewClassifiedError(invoker.ErrorClassResourceBusy, "resource busy")
		},
	)
	if err == nil {
		t.Fatal("expected resource busy failure after budget")
	}
	if calls != 2 || candidate.ResourceID != "recovery-primary" {
		t.Fatalf("calls=%d resource=%s, want two same-resource attempts", calls, candidate.ResourceID)
	}
	if stats.ResourceFailovers != 0 {
		t.Fatalf("resource failovers=%d, want 0", stats.ResourceFailovers)
	}
}

func TestNextTierCandidatePreservesForwardOrder(t *testing.T) {
	plan := TierPlan{
		Selected: TierCandidate{ResourceID: "second"},
		Candidates: []TierCandidate{
			{ResourceID: "first"},
			{ResourceID: "second"},
			{ResourceID: "third"},
		},
	}
	next, ok := NextTierCandidate(plan, map[string]struct{}{})
	if !ok || next.ResourceID != "third" {
		t.Fatalf("next=%+v ok=%v, want third after current", next, ok)
	}
	next, ok = NextTierCandidate(plan, map[string]struct{}{"third": {}})
	if ok {
		t.Fatalf("next=%+v, want no backward failover", next)
	}
}

func TestTierRecoveryAccumulatesTokensAcrossAttempts(t *testing.T) {
	configureTierRecoveryTest(t, []string{"recovery-primary"}, models.TierRecoveryConfig{
		MaxTotalAttempts:       2,
		MaxAttemptsPerResource: 2,
		RetryBackoffMs:         1,
		RetryOn:                []string{"idle_timeout"},
	})
	calls := 0
	stats := &TierRecoveryStats{}
	_, _, tokens, err := RunTierInvocationWithRecovery(
		context.Background(), "tier1_hunter", 10, stats,
		func(context.Context, TierCandidate, int, *invoker.InvocationMetrics) (string, int64, error) {
			calls++
			if calls == 1 {
				return "", 3, invoker.NewClassifiedError(invoker.ErrorClassIdleTimeout, "idle timeout")
			}
			return "ok", 5, nil
		},
	)
	if err != nil || calls != 2 || tokens != 8 {
		t.Fatalf("tokens=%d calls=%d err=%v, want accumulated 8 tokens", tokens, calls, err)
	}
}

func TestAcquireWithPreferencePinsResourceID(t *testing.T) {
	dispatcher := newHealthTestDispatcher(t)
	for _, resource := range dispatcher.resources {
		resource.Driver = "native"
		resource.Native = "shared-model"
	}
	dispatcher.resources[0].Active = 1

	resource, modelName, err := dispatcher.AcquireWithPreference(
		context.Background(), "native", "shared-model", "recovery-alternate",
	)
	if err != nil || resource == nil || resource.ID != "recovery-alternate" || modelName != "shared-model" {
		t.Fatalf("resource=%+v model=%q err=%v, want pinned alternate", resource, modelName, err)
	}
}

func TestTierRecoveryExplicitAvailabilityFailover(t *testing.T) {
	configureTierRecoveryTest(t, []string{"recovery-primary", "recovery-alternate"}, models.TierRecoveryConfig{
		MaxTotalAttempts:       2,
		MaxAttemptsPerResource: 1,
		MaxCandidateFailovers:  1,
		CandidateFailoverOn:    []string{"rate_limited"},
	})
	calls := map[string]int{}
	stats := &TierRecoveryStats{}
	raw, candidate, _, err := RunTierInvocationWithRecovery(
		context.Background(), "tier1_hunter", 10, stats,
		func(_ context.Context, selected TierCandidate, _ int, metrics *invoker.InvocationMetrics) (string, int64, error) {
			calls[selected.ResourceID]++
			metrics.QueueWaitMs = 12
			metrics.DurationMs = 340
			metrics.DriverFailovers = 1
			if selected.ResourceID == "recovery-primary" {
				return "", 0, invoker.NewClassifiedError(invoker.ErrorClassRateLimited, "rate limited")
			}
			return "ok", 7, nil
		},
	)
	if err != nil || raw != "ok" || candidate.ResourceID != "recovery-alternate" {
		t.Fatalf("failover result: raw=%q candidate=%+v err=%v", raw, candidate, err)
	}
	if calls["recovery-primary"] != 1 || calls["recovery-alternate"] != 1 {
		t.Fatalf("calls=%#v, want one attempt per resource", calls)
	}
	if stats.ResourceFailovers != 1 || stats.DriverFailovers != 2 || len(stats.ResourceChain) != 2 || len(stats.DurationSeconds) != 2 {
		t.Fatalf("stats=%+v, want explicit failover and attempt timing", stats)
	}
}

func TestTierRecoveryBreakerOpenMapsResourceUnavailable(t *testing.T) {
	dispatcher := newHealthTestDispatcher(t)
	primary := dispatcher.resources[0]
	for i := 0; i < resourceBreakerFailureThreshold; i++ {
		dispatcher.RecordResourceResult(primary, invoker.NewClassifiedError(invoker.ErrorClassNetworkTransient, "network failed"))
	}
	if available, open := dispatcher.ResourceHealth(primary.ID); available || !open {
		t.Fatalf("available=%v open=%v, want breaker open", available, open)
	}
	if available := dispatcher.IsResourceAvailable(primary.ID); available {
		t.Fatal("expected IsResourceAvailable to report breaker open")
	}
	configureTierRecoveryTest(t, []string{primary.ID}, models.TierRecoveryConfig{MaxTotalAttempts: 2})
	invoked := false
	stats := &TierRecoveryStats{}
	_, _, _, err := RunTierInvocationWithRecovery(
		context.Background(), "tier1_hunter", 10, stats,
		func(context.Context, TierCandidate, int, *invoker.InvocationMetrics) (string, int64, error) {
			invoked = true
			return "", 0, nil
		},
	)
	var classified *invoker.ClassifiedError
	if !errors.As(err, &classified) || classified.ErrorClass() != invoker.ErrorClassResourceUnavailable {
		t.Fatalf("error=%v, want resource_unavailable", err)
	}
	if invoked || stats.ResourceFailovers != 0 || len(stats.ErrorClasses) != 1 {
		t.Fatalf("invoked=%v stats=%+v, want unavailable attempt without invocation", invoked, stats)
	}
}

func TestResourceBreakerExpires(t *testing.T) {
	dispatcher := newHealthTestDispatcher(t)
	primary := dispatcher.resources[0]
	for i := 0; i < resourceBreakerFailureThreshold; i++ {
		dispatcher.RecordResourceResult(primary, errors.New("failed"))
	}
	if available, _ := dispatcher.ResourceHealth(primary.ID); available {
		t.Fatal("expected breaker open")
	}
	primary.Health.OpenUntil = time.Now().Add(-time.Second)
	if available, open := dispatcher.ResourceHealth(primary.ID); !available || open {
		t.Fatalf("available=%v open=%v, want breaker closed", available, open)
	}
}
