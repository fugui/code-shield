package dispatcher

import (
	"errors"
	"sync"
	"testing"

	"code-shield/services/invoker"
)

func TestSlotLeaseObservabilityCompletionKeepsBoundedRecentCopy(t *testing.T) {
	d := &ModelDispatcher{
		cond:         sync.NewCond(&sync.Mutex{}),
		enabled:      true,
		activeLeases: make(map[string]*LLMSlotLease),
		resources: []*ModelResource{
			{Index: 0, ID: "native", Driver: "native", Native: "qwen-test", Concurrent: 1},
		},
	}

	req := invoker.AIRequest{
		PromptMsg:  "readonly prompt",
		OutputPath: "/tmp/does-not-exist-code-shield-test.json",
	}
	obs := invoker.NewCallObservability(req, "native", "native")
	obs.Console.Append("assistant", "assistant", "model response")

	leaseID := d.RegisterSlotLease(d.resources[0], "native", "qwen-test", nil, obs)
	obs.Finish(nil)
	d.CompleteSlotLease(leaseID, nil)

	lease, ok := d.GetLeaseByID(leaseID)
	if !ok {
		t.Fatal("expected recently completed lease")
	}
	if lease.Status != string(invoker.CallSucceeded) {
		t.Fatalf("expected succeeded lease, got %s", lease.Status)
	}
	if lease.Observability == nil || lease.Observability.PromptAvailable() {
		t.Fatal("completed lease must not retain prompt provider")
	}
	snapshot, status := lease.Observability.Console.Snapshot(0, 10)
	if len(snapshot.Events) == 0 || !status.Closed {
		t.Fatalf("expected retained closed console, got %+v", snapshot)
	}
	if _, active := d.activeLeases[leaseID]; active {
		t.Fatal("completed lease should not remain active")
	}
}

func TestCompleteSlotLeaseCopiesErrorClassAndDiagnostics(t *testing.T) {
	d := &ModelDispatcher{
		cond:         sync.NewCond(&sync.Mutex{}),
		enabled:      true,
		activeLeases: make(map[string]*LLMSlotLease),
		resources: []*ModelResource{
			{Index: 0, ID: "codex", Driver: "codex", Codex: "profile:deepseek", Concurrent: 1},
		},
	}
	req := invoker.AIRequest{PromptMsg: "test", OutputPath: "/tmp/does-not-exist.json"}
	obs := invoker.NewCallObservability(req, "codex", "codex")
	obs.SetCLIDiagnostics(invoker.CLIDiagnostics{
		Driver:              "codex",
		CLI:                 "codex",
		SessionID:           "session-id",
		ExitCode:            func() *int { code := 0; return &code }(),
		TerminationReason:   "output_missing",
		StderrTail:          "api_key=[REDACTED]",
		StderrTailTruncated: false,
		TokenUsage: &invoker.TokenUsage{
			TotalTokens: 618,
			Source:      "stderr_summary",
		},
	})

	leaseID := d.RegisterSlotLease(d.resources[0], "codex", "profile:deepseek", nil, obs)
	err := invoker.NewClassifiedError(invoker.ErrorClassOutputMissing, "codex produced empty final message")
	d.CompleteSlotLease(leaseID, err)

	lease, ok := d.GetLeaseByID(leaseID)
	if !ok {
		t.Fatal("expected failed lease")
	}
	if lease.ErrorClass != string(invoker.ErrorClassOutputMissing) {
		t.Fatalf("error class = %q", lease.ErrorClass)
	}
	if lease.ErrorMessage != "codex produced empty final message" {
		t.Fatalf("error message = %q", lease.ErrorMessage)
	}
	if lease.Diagnostics == nil || lease.Diagnostics.SessionID != "session-id" || lease.Diagnostics.ErrorClass != invoker.ErrorClassOutputMissing {
		t.Fatalf("diagnostics = %+v", lease.Diagnostics)
	}
	if lease.Diagnostics.TokenUsage == nil || lease.Diagnostics.TokenUsage.TotalTokens != 618 {
		t.Fatalf("diagnostic token usage = %+v", lease.Diagnostics.TokenUsage)
	}
}

func TestRecentLeasesSuccessfulRecordingOption(t *testing.T) {
	d := &ModelDispatcher{
		cond:                 sync.NewCond(&sync.Mutex{}),
		enabled:              true,
		activeLeases:         make(map[string]*LLMSlotLease),
		skipSuccessfulLeases: true,
		resources: []*ModelResource{
			{Index: 0, ID: "native", Driver: "native", Native: "qwen-test", Concurrent: 1},
		},
	}

	if d.RecordSuccessfulLeases() {
		t.Fatal("expected successful leases to be skipped by default")
	}
	successID := d.RegisterSlotLease(d.resources[0], "native", "qwen-test", nil)
	d.CompleteSlotLease(successID, nil)
	if _, ok := d.GetLeaseByID(successID); ok {
		t.Fatal("expected successful lease to be skipped by default")
	}

	failedID := d.RegisterSlotLease(d.resources[0], "native", "qwen-test", nil)
	d.CompleteSlotLease(failedID, errors.New("expected failure"))
	if lease, ok := d.GetLeaseByID(failedID); !ok || lease.Status != string(invoker.CallFailed) {
		t.Fatalf("expected failed lease to remain recorded, got %+v", lease)
	}

	d.SetRecordSuccessfulLeases(true)
	if !d.RecordSuccessfulLeases() {
		t.Fatal("expected successful lease recording to be enabled")
	}
	if len(d.GetRecentLeases()) != 1 {
		t.Fatalf("expected existing failed record to be unchanged, got %d records", len(d.GetRecentLeases()))
	}

	newSuccessID := d.RegisterSlotLease(d.resources[0], "native", "qwen-test", nil)
	d.CompleteSlotLease(newSuccessID, nil)
	if _, ok := d.GetLeaseByID(newSuccessID); !ok {
		t.Fatal("expected future successful lease to be recorded after enabling")
	}

	d.SetRecordSuccessfulLeases(false)
	before := d.GetRecentLeases()
	anotherSuccessID := d.RegisterSlotLease(d.resources[0], "native", "qwen-test", nil)
	d.CompleteSlotLease(anotherSuccessID, nil)
	after := d.GetRecentLeases()
	if len(before) != len(after) {
		t.Fatalf("expected disabling not to mutate current history, before=%d after=%d", len(before), len(after))
	}
	if _, ok := d.GetLeaseByID(anotherSuccessID); ok {
		t.Fatal("expected future successful lease to be skipped after disabling")
	}
}

func TestRecentLeasesRingKeepsNewestFifty(t *testing.T) {
	d := &ModelDispatcher{
		cond:         sync.NewCond(&sync.Mutex{}),
		enabled:      true,
		activeLeases: make(map[string]*LLMSlotLease),
		resources: []*ModelResource{
			{Index: 0, ID: "native", Driver: "native", Native: "qwen-test", Concurrent: 1},
		},
	}

	const total = maxRecentLeases + 1
	leaseIDs := make([]string, 0, total)
	for i := 0; i < total; i++ {
		leaseID := d.RegisterSlotLease(d.resources[0], "native", "qwen-test", nil)
		leaseIDs = append(leaseIDs, leaseID)
		d.CompleteSlotLease(leaseID, nil)
	}

	leases := d.GetRecentLeases()
	if len(leases) != maxRecentLeases {
		t.Fatalf("expected %d recent leases, got %d", maxRecentLeases, len(leases))
	}
	if leases[0].LeaseID != leaseIDs[total-1] {
		t.Fatalf("expected newest lease %s first, got %s", leaseIDs[total-1], leases[0].LeaseID)
	}
	if leases[maxRecentLeases-1].LeaseID != leaseIDs[1] {
		t.Fatalf("expected oldest retained lease %s last, got %s", leaseIDs[1], leases[maxRecentLeases-1].LeaseID)
	}
	if leases[0].Status != string(invoker.CallSucceeded) {
		t.Fatalf("expected newest succeeded lease, got %s", leases[0].Status)
	}
}
