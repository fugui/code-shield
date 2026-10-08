package profiles

import (
	"testing"

	"code-shield/services/engines/assessment"
)

func TestOutcomeForStatusUsesProfileCompatibility(t *testing.T) {
	if outcome, ok := OutcomeForStatus("not_thread_creation"); !ok || outcome != assessment.OutcomeNotTarget {
		t.Fatalf("OutcomeForStatus(not_thread_creation) = %q, %v; want %q, true", outcome, ok, assessment.OutcomeNotTarget)
	}
}

func TestRegistryRegistersOccurrenceProfile(t *testing.T) {
	registry, err := Registry()
	if err != nil {
		t.Fatalf("Registry() error = %v", err)
	}
	names := registry.Names()
	if len(names) != 3 || names[0] != "changereview" || names[1] != "entityreview" || names[2] != "occurrencereview" {
		t.Fatalf("Registry() names = %v, want changereview, entityreview and occurrencereview", names)
	}
	registration, err := registry.Get("occurrencereview")
	if err != nil {
		t.Fatalf("registry.Get() error = %v", err)
	}
	if _, err = registry.Get("changereview"); err != nil {
		t.Fatalf("registry.Get(changereview) error = %v", err)
	}
	if _, err = registry.Get("entityreview"); err != nil {
		t.Fatalf("registry.Get(entityreview) error = %v", err)
	}
	if registration.Prompt == nil || registration.Normalize == nil || registration.Validate == nil ||
		registration.Reconcile == nil || registration.MapFindings == nil || registration.Outcome == nil {
		t.Fatalf("registration is incomplete: %+v", registration)
	}
	if err := registry.Register(registration); err == nil {
		t.Fatal("Register() after freeze = nil, want error")
	}
}
