package entityreview

import (
	"fmt"

	"code-shield/services/coverage"
	"code-shield/services/engines/assessment"
	"code-shield/services/engines/chunker"
)

type BundlePlanner struct{}

func (BundlePlanner) BuildBundles(ctx assessment.PlanContext, units []coverage.PlanUnit) ([]chunker.SemanticBundle, error) {
	codesPath, maxUnits, negativeRules := assessment.PlannerConfig(ctx)
	return chunker.BuildPrimaryUnitBundles(codesPath, units, maxUnits, negativeRules)
}

func (planner BundlePlanner) BuildRepairBundles(
	ctx assessment.PlanContext,
	parent chunker.SemanticBundle,
	failedUnits []string,
	depth int,
) ([]chunker.SemanticBundle, error) {
	units, err := assessment.SelectedUnits(parent, failedUnits)
	if err != nil {
		return nil, err
	}
	codesPath, maxUnits, negativeRules := assessment.PlannerConfig(ctx)
	bundles, err := chunker.BuildPrimaryUnitBundles(codesPath, units, maxUnits, negativeRules)
	if err != nil {
		return nil, err
	}
	for index := range bundles {
		bundles[index].Name = fmt.Sprintf("%s-r%d-%03d", parent.Name, depth, index+1)
	}
	return bundles, nil
}

func (BundlePlanner) PluginPlan(assessment.PlanContext, chunker.SemanticBundle) []coverage.PlannedFile {
	return nil
}
