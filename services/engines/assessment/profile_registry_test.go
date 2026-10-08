package assessment

import "testing"

func TestFacetRegistryRegistersAndFreezes(t *testing.T) {
	registry := NewFacetRegistry[string]()
	if err := registry.Register("occurrence_review", "first"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Freeze(); err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}
	if err := registry.Register("entity_review", "second"); err == nil {
		t.Fatal("Register() after freeze = nil, want error")
	}
	value, err := registry.Get("occurrence_review")
	if err != nil || value != "first" {
		t.Fatalf("Get() = %q, %v; want first, nil", value, err)
	}
	if _, err = registry.Get("entity_review"); err == nil || !isError(err, ErrProfileRegistry) {
		t.Fatalf("Get() error = %v, want PROFILE_FACET_NOT_FOUND", err)
	}
}

func isError(target error, want error) bool {
	for current := target; current != nil; {
		if current == want {
			return true
		}
		if unwrapper, ok := current.(interface{ Unwrap() error }); ok {
			current = unwrapper.Unwrap()
			continue
		}
		return false
	}
	return false
}
