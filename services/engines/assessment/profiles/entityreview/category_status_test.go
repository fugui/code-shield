package entityreview

import (
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines/assessment"
)

func TestBaseFindingHasExplicitCategoryStatus(t *testing.T) {
	finding := baseFinding(assessment.AssessmentContext{}, assessment.UnitAssessment{}, coverage.PlanUnit{ID: "unit-1", Path: "src/a.go"}, nil)
	if finding.CategoryStatus != "LEGACY" {
		t.Fatalf("category status = %q, want LEGACY", finding.CategoryStatus)
	}
}
