package plugins

import (
	"testing"

	"code-shield/services/coverage"
)

type mockGate struct {
	id string
}

func (m *mockGate) ID() string { return m.id }
func (m *mockGate) Inspect(codesPath string, unit coverage.PlanUnit, options map[string]any) RadarResult {
	return RadarResult{Decision: DecisionFastPass}
}

type mockEnricher struct {
	id string
}

func (m *mockEnricher) ID() string { return m.id }
func (m *mockEnricher) Enrich(codesPath string, target any, options map[string]any) error {
	return nil
}

type mockVerifier struct {
	id string
}

func (m *mockVerifier) ID() string { return m.id }
func (m *mockVerifier) Verify(codesPath string, unit coverage.PlanUnit, hints []StructuralFactHint, promptTemplate string, timeoutSec float64) (RadarResult, error) {
	return RadarResult{Decision: DecisionPass}, nil
}

func TestPluginsRegistryLifecycle(t *testing.T) {
	ResetRegistriesForTest()
	defer ResetRegistriesForTest()

	// 1. PreflightGate
	gate := &mockGate{id: "test_gate"}
	if err := RegisterPreflightGate(gate); err != nil {
		t.Fatalf("RegisterPreflightGate failed: %v", err)
	}
	// duplicate registration must fail
	if err := RegisterPreflightGate(gate); err == nil {
		t.Fatal("expected duplicate registration error, got nil")
	}
	gotGate, err := GetPreflightGate("test_gate")
	if err != nil || gotGate.ID() != "test_gate" {
		t.Fatalf("GetPreflightGate mismatch: got %v, err %v", gotGate, err)
	}
	if _, err := GetPreflightGate("non_existent"); err == nil {
		t.Fatal("expected error for non_existent gate, got nil")
	}

	// 2. ContextEnricher
	enricher := &mockEnricher{id: "test_enricher"}
	if err := RegisterContextEnricher(enricher); err != nil {
		t.Fatalf("RegisterContextEnricher failed: %v", err)
	}
	if err := RegisterContextEnricher(enricher); err == nil {
		t.Fatal("expected duplicate registration error, got nil")
	}
	gotEnricher, err := GetContextEnricher("test_enricher")
	if err != nil || gotEnricher.ID() != "test_enricher" {
		t.Fatalf("GetContextEnricher mismatch: got %v, err %v", gotEnricher, err)
	}

	// 3. ThinVerifier
	verifier := &mockVerifier{id: "test_verifier"}
	if err := RegisterThinVerifier(verifier); err != nil {
		t.Fatalf("RegisterThinVerifier failed: %v", err)
	}
	if err := RegisterThinVerifier(verifier); err == nil {
		t.Fatal("expected duplicate registration error, got nil")
	}
	gotVerifier, err := GetThinVerifier("test_verifier")
	if err != nil || gotVerifier.ID() != "test_verifier" {
		t.Fatalf("GetThinVerifier mismatch: got %v, err %v", gotVerifier, err)
	}

	// 4. Reset
	ResetRegistriesForTest()
	if _, err := GetPreflightGate("test_gate"); err == nil {
		t.Fatal("expected error after reset, got nil")
	}
}

func TestEmptyIDRegistrationRejection(t *testing.T) {
	ResetRegistriesForTest()
	defer ResetRegistriesForTest()

	emptyGate := &mockGate{id: ""}
	if err := RegisterPreflightGate(emptyGate); err == nil {
		t.Fatal("expected error registering empty id gate, got nil")
	}
}
