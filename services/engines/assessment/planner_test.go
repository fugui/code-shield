package assessment

import (
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines/chunker"
)

func TestSelectedUnitsRebuildsRepairScope(t *testing.T) {
	parent := chunker.SemanticBundle{Name: "parent", PrimaryUnits: []coverage.PlanUnit{
		{ID: "u1", Path: "a.cpp"},
		{ID: "u2", Path: "a.cpp"},
	}}
	units, err := SelectedUnits(parent, []string{"u2"})
	if err != nil {
		t.Fatalf("SelectedUnits() error = %v", err)
	}
	if len(units) != 1 || units[0].ID != "u2" {
		t.Fatalf("unexpected repair units: %+v", units)
	}
}
