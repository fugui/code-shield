package coverage

import (
	"reflect"
	"testing"
)

func TestReconcilePlanUnits(t *testing.T) {
	units := []PlanUnit{
		{ID: "unit-c", Kind: PlanUnitEntity},
		{ID: "unit-a", Kind: PlanUnitEntity},
		{ID: "unit-b", Kind: PlanUnitEntity},
	}
	assessments := []AssessmentRecord{
		{PrimaryUnitID: "unit-c", Status: "clean"},
		{PrimaryUnitID: "unit-a", Status: "finding"},
		{PrimaryUnitID: "orphan", Status: "finding"},
		{PrimaryUnitID: "", Status: "finding"},
		{PrimaryUnitID: "unit-a", Status: "finding"},
	}

	got := ReconcilePlanUnits(units, assessments)
	if got.PlannedUnits != 3 || got.MatchedUnits != 2 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if want := []string{"unit-b"}; !reflect.DeepEqual(got.MissingUnits, want) {
		t.Fatalf("MissingUnits = %#v, want %#v", got.MissingUnits, want)
	}
	if want := []string{"", "orphan", "unit-a"}; !reflect.DeepEqual(got.UnmatchedUnits, want) {
		t.Fatalf("UnmatchedUnits = %#v, want %#v", got.UnmatchedUnits, want)
	}
}

func TestReconcilePlanUnitsEmpty(t *testing.T) {
	got := ReconcilePlanUnits(nil, nil)
	if got.PlannedUnits != 0 || got.MatchedUnits != 0 || len(got.MissingUnits) != 0 || len(got.UnmatchedUnits) != 0 {
		t.Fatalf("unexpected empty result: %+v", got)
	}
}
