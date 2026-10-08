package dispatcher

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"code-shield/services/invoker"
)

func TestDispatcherMetricsAggregatesPoolTierAndCrossDimensions(t *testing.T) {
	d := &ModelDispatcher{
		resources: []*ModelResource{
			{Index: 0, ID: "pool-a", Driver: "native", Native: "model-a", Concurrent: 2},
			{Index: 1, ID: "pool-b", Driver: "native", Native: "model-b", Concurrent: 1},
		},
		enabled:      true,
		activeLeases: make(map[string]*LLMSlotLease),
	}

	d.RecordDispatchStarted("tier1_hunter")
	d.RecordDispatchAssigned("tier1_hunter", d.resources[0], 2*time.Second)
	d.RecordDispatchCompleted("tier1_hunter", d.resources[0], 3*time.Second, nil)

	d.RecordDispatchStarted("tier1_hunter")
	d.RecordDispatchAssigned("tier1_hunter", d.resources[1], 1*time.Second)
	d.RecordDispatchCompleted("tier1_hunter", d.resources[1], 8*time.Second, context.DeadlineExceeded)

	d.RecordDispatchStarted("tier4_synthesis")
	d.RecordDispatchPassthrough("tier4_synthesis")
	d.RecordDispatchCompleted("tier4_synthesis", nil, time.Second, context.Canceled)

	d.RecordDispatchStarted("tier3_judge")
	d.RecordDispatchAcquireFailure("tier3_judge", context.Canceled)

	snapshot := d.GetMetricsSnapshot()
	if snapshot.Scope != "process_lifetime" {
		t.Fatalf("unexpected snapshot scope: %s", snapshot.Scope)
	}

	var poolA, poolB, passThrough *PoolMetricsSnapshot
	for i := range snapshot.Pools {
		switch snapshot.Pools[i].PoolKey {
		case "id:pool-a":
			poolA = &snapshot.Pools[i]
		case "id:pool-b":
			poolB = &snapshot.Pools[i]
		case passThroughPoolKey:
			passThrough = &snapshot.Pools[i]
		}
	}
	if poolA == nil || poolB == nil || passThrough == nil {
		t.Fatalf("expected pool-a, pool-b and passthrough snapshots, got %+v", snapshot.Pools)
	}
	if poolA.Metrics.Assigned != 1 || poolA.Metrics.Completed != 1 || poolA.Metrics.Succeeded != 1 {
		t.Fatalf("unexpected pool-a metrics: %+v", poolA.Metrics)
	}
	if poolB.Metrics.Assigned != 1 || poolB.Metrics.Failed != 1 || poolB.Metrics.Timeout != 1 {
		t.Fatalf("unexpected pool-b metrics: %+v", poolB.Metrics)
	}
	if passThrough.Metrics.Assigned != 1 || passThrough.Metrics.Canceled != 1 {
		t.Fatalf("unexpected passthrough metrics: %+v", passThrough.Metrics)
	}

	var hunter, synthesis, judge *TierMetricsSnapshot
	for i := range snapshot.Tiers {
		switch snapshot.Tiers[i].TierName {
		case "tier1_hunter":
			hunter = &snapshot.Tiers[i]
		case "tier4_synthesis":
			synthesis = &snapshot.Tiers[i]
		case "tier3_judge":
			judge = &snapshot.Tiers[i]
		}
	}
	if hunter == nil || synthesis == nil || judge == nil {
		t.Fatalf("expected normalized tier snapshots, got %+v", snapshot.Tiers)
	}
	if hunter.Level != 1 || hunter.Role != "Hunter" {
		t.Fatalf("unexpected tier1 display: level=%d role=%s", hunter.Level, hunter.Role)
	}
	if hunter.Dispatched != 2 || hunter.SlotAssigned != 2 || hunter.Metrics.Completed != 2 {
		t.Fatalf("unexpected tier1 metrics: %+v", hunter)
	}
	if hunter.AvgQueueSeconds < 1 || hunter.AvgQueueSeconds > 3 {
		t.Fatalf("unexpected tier1 average queue seconds: %f", hunter.AvgQueueSeconds)
	}
	if synthesis.Passthrough != 1 || synthesis.Metrics.Canceled != 1 {
		t.Fatalf("unexpected tier4 passthrough metrics: %+v", synthesis)
	}
	if judge.AcquireCanceled != 1 {
		t.Fatalf("unexpected tier3 acquire cancellation: %+v", judge)
	}

	foundCross := false
	for _, byPool := range hunter.ByPool {
		if byPool.PoolKey == "id:pool-b" && byPool.Metrics.Timeout == 1 {
			foundCross = true
		}
	}
	if !foundCross {
		t.Fatalf("expected tier1 x pool-b timeout metric, got %+v", hunter.ByPool)
	}
}

func TestDispatcherMetricsCancelAndSanitizeError(t *testing.T) {
	d := &ModelDispatcher{activeLeases: make(map[string]*LLMSlotLease)}
	longError := errors.New("boom\nwith  spaces")
	d.RecordDispatchStarted("unknown_tool")
	d.RecordDispatchPassthrough("unknown_tool")
	d.RecordDispatchCompleted("unknown_tool", nil, 1500*time.Millisecond, longError)

	snapshot := d.GetMetricsSnapshot()
	if len(snapshot.Tiers) != 1 || snapshot.Tiers[0].TierName != "unknown_tool" {
		t.Fatalf("unexpected unknown tier snapshot: %+v", snapshot.Tiers)
	}
	if snapshot.Tiers[0].Metrics.Failed != 1 {
		t.Fatalf("expected failed call, got %+v", snapshot.Tiers[0].Metrics)
	}
	if snapshot.Tiers[0].Metrics.LastError != "boom with spaces" {
		t.Fatalf("unexpected sanitized error: %q", snapshot.Tiers[0].Metrics.LastError)
	}
}

func TestDispatchingInvokerRecordsMetricsAndLeaseTier(t *testing.T) {
	d := &ModelDispatcher{
		cond:         sync.NewCond(&sync.Mutex{}),
		manualScale:  1.0,
		enabled:      true,
		activeLeases: make(map[string]*LLMSlotLease),
		resources: []*ModelResource{
			{Index: 0, ID: "native-pool", Driver: "native", Native: "model-a", Concurrent: 1},
		},
	}

	mock := &mockInvoker{NameStr: "native"}
	wrapped := NewDispatchingInvoker(mock, d)
	req := invoker.AIRequest{
		ParentContext: context.Background(),
		WorkContext: &invoker.LLMWorkContext{
			Stage:    "Tier 2: 辩护对抗 (Challenger)",
			SubTask:  "metrics smoke test",
			TierName: "tier2_challenger",
		},
	}
	if err := wrapped.Invoke(req); err != nil {
		t.Fatalf("wrapped invocation failed: %v", err)
	}

	snapshot := d.GetMetricsSnapshot()
	if len(snapshot.Tiers) != 1 || snapshot.Tiers[0].TierName != "tier2_challenger" {
		t.Fatalf("unexpected tier snapshot: %+v", snapshot.Tiers)
	}
	tier := snapshot.Tiers[0]
	if tier.Dispatched != 1 || tier.SlotAssigned != 1 || tier.Metrics.Completed != 1 || tier.Metrics.Succeeded != 1 {
		t.Fatalf("unexpected tier metrics: %+v", tier)
	}
	if tier.Running != 0 {
		t.Fatalf("expected completed lease to stop running, got running=%d", tier.Running)
	}
	if len(snapshot.Pools) != 1 || snapshot.Pools[0].PoolKey != "id:native-pool" {
		t.Fatalf("unexpected pool snapshot: %+v", snapshot.Pools)
	}
	if snapshot.Pools[0].Metrics.Assigned != 1 || snapshot.Pools[0].Metrics.Succeeded != 1 {
		t.Fatalf("unexpected pool metrics: %+v", snapshot.Pools[0].Metrics)
	}

	debug := d.GetDebugSnapshot()
	if len(debug.Resources) != 1 || debug.Resources[0].Active != 0 {
		t.Fatalf("unexpected debug resource snapshot: %+v", debug.Resources)
	}
	if len(debug.ActiveLeases) != 0 {
		t.Fatalf("expected no active leases after completion, got %+v", debug.ActiveLeases)
	}
	if len(debug.RecentLeases) != 1 || debug.RecentLeases[0].TierName != "tier2_challenger" {
		t.Fatalf("unexpected recent lease snapshot: %+v", debug.RecentLeases)
	}
}

func TestDispatchingInvokerUsesRequestTierWithoutWorkContext(t *testing.T) {
	d := &ModelDispatcher{
		cond:         sync.NewCond(&sync.Mutex{}),
		manualScale:  1.0,
		enabled:      true,
		activeLeases: make(map[string]*LLMSlotLease),
		resources: []*ModelResource{
			{Index: 0, ID: "native-pool", Driver: "native", Native: "model-a", Concurrent: 1},
		},
	}

	mock := &mockInvoker{NameStr: "native"}
	wrapped := NewDispatchingInvoker(mock, d)
	req := invoker.AIRequest{
		ParentContext: context.Background(),
		TierName:      "tier1_hunter",
	}
	if err := wrapped.Invoke(req); err != nil {
		t.Fatalf("wrapped invocation failed: %v", err)
	}

	recentLeases := d.GetRecentLeases()
	if len(recentLeases) != 1 {
		t.Fatalf("expected one completed lease, got %+v", recentLeases)
	}
	if recentLeases[0].TierName != "tier1_hunter" {
		t.Fatalf("expected request tier on lease, got %q", recentLeases[0].TierName)
	}
}

func TestSanitizeMetricErrorRedactsSecretsAndPreservesUTF8(t *testing.T) {
	input := errors.New(`request failed api_key=abc123 Authorization: Bearer xyz token=super-secret Bearer abcdef123456 状态=超时`)
	got := SanitizeMetricError(input)
	if strings.Contains(got, "abc123") || strings.Contains(got, "xyz") || strings.Contains(got, "super-secret") || strings.Contains(got, "abcdef123456") {
		t.Fatalf("secret leaked in metric error: %q", got)
	}
	if !strings.Contains(got, "状态=超时") {
		t.Fatalf("expected non-ASCII text to survive sanitization: %q", got)
	}
}
