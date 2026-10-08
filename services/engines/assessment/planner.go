package assessment

import (
	"fmt"

	"code-shield/services/coverage"
	"code-shield/services/engines/chunker"
)

func PlannerConfig(ctx PlanContext) (string, int, []string) {
	if ctx.EngineContext == nil {
		return "", ctx.Config.MaxFiles, nil
	}
	return ctx.EngineContext.CodesPath, ctx.Config.MaxFiles, ctx.EngineContext.NegativeRules
}

func SelectedUnits(parent chunker.SemanticBundle, failedUnits []string) ([]coverage.PlanUnit, error) {
	if len(failedUnits) == 0 {
		return nil, fmt.Errorf("repair planner has no failed units")
	}
	failed := make(map[string]bool, len(failedUnits))
	for _, unitID := range failedUnits {
		failed[unitID] = true
	}
	units := make([]coverage.PlanUnit, 0, len(failedUnits))
	for _, unit := range parent.PrimaryUnits {
		if failed[unit.ID] {
			units = append(units, unit)
		}
	}
	if len(units) == 0 {
		return nil, fmt.Errorf("repair planner found none of the failed units in bundle %q", parent.Name)
	}
	return units, nil
}
