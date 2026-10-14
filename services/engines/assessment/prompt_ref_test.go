package assessment

import (
	"testing"

	"code-shield/services/coverage"
)

func TestPromptRefSessionMapsShortRefs(t *testing.T) {
	session, err := NewPromptRefSession("bundle-1", []string{"sha256:b", "sha256:a"}, map[string]string{
		"sha256:a": "pthread_create @ src/a.cpp:42",
	})
	if err != nil {
		t.Fatalf("NewPromptRefSession() error = %v", err)
	}
	if len(session.Refs) != 2 || session.Refs[0].Ref != "u001" || session.Refs[0].UnitID != "sha256:a" {
		t.Fatalf("unexpected refs: %+v", session.Refs)
	}
	unitID, err := session.UnitIDForRef("u002")
	if err != nil || unitID != "sha256:b" {
		t.Fatalf("UnitIDForRef() = %q, %v; want sha256:b, nil", unitID, err)
	}
	if _, ok := session.RefForUnitID("sha256:a"); !ok {
		t.Fatal("RefForUnitID() = false, want true")
	}
}

func TestPromptRefSessionRejectsDuplicateUnits(t *testing.T) {
	_, err := NewPromptRefSession("bundle-1", []string{"same", "same"}, nil)
	if err == nil {
		t.Fatal("NewPromptRefSession() error = nil, want duplicate error")
	}
}

func TestPromptRefSessionSubSession(t *testing.T) {
	session, err := NewPromptRefSession("parent-bundle", []string{"u-1", "u-2", "u-3", "u-4"}, map[string]string{
		"u-1": "test1",
		"u-2": "test2",
		"u-3": "test3",
		"u-4": "test4",
	})
	if err != nil {
		t.Fatalf("NewPromptRefSession() error = %v", err)
	}

	sub, err := session.SubSession("child-bundle", []coverage.PlanUnit{
		{ID: "u-3", DisplayName: "test3"},
		{ID: "u-4", DisplayName: "test4"},
	})
	if err != nil {
		t.Fatalf("SubSession() error = %v", err)
	}
	if len(sub.Refs) != 2 {
		t.Fatalf("sub.Refs = %d, want 2", len(sub.Refs))
	}
	if sub.Refs[0].Ref != "u003" || sub.Refs[0].UnitID != "u-3" {
		t.Fatalf("sub.Refs[0] = %+v, want u003 / u-3", sub.Refs[0])
	}
	if sub.Refs[1].Ref != "u004" || sub.Refs[1].UnitID != "u-4" {
		t.Fatalf("sub.Refs[1] = %+v, want u004 / u-4", sub.Refs[1])
	}
	unitID, err := sub.UnitIDForRef("u003")
	if err != nil || unitID != "u-3" {
		t.Fatalf("UnitIDForRef(u003) = %q, %v", unitID, err)
	}
	if _, err := sub.UnitIDForRef("u001"); err == nil {
		t.Fatal("expected error for u001 in sub session")
	}
}
