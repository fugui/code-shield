package assessment

import "testing"

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
