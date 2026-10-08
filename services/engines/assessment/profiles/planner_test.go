package profiles

import (
	"testing"

	"code-shield/services/engines/assessment"
)

func TestPlannerConfigUsesExecutionContext(t *testing.T) {
	ctx := assessment.PlanContext{EngineContext: nil}
	if _, maxUnits, negativeRules := assessment.PlannerConfig(ctx); maxUnits != 0 || negativeRules != nil {
		t.Fatalf("empty planner config = %d, %v", maxUnits, negativeRules)
	}
}
