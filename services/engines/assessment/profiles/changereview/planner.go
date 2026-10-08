package changereview

import (
	"fmt"
	"os"
	"path/filepath"

	"code-shield/services/coverage"
	"code-shield/services/engines/assessment"
	"code-shield/services/engines/chunker"
)

type BundlePlanner struct{}

func (BundlePlanner) BuildBundles(ctx assessment.PlanContext, units []coverage.PlanUnit) ([]chunker.SemanticBundle, error) {
	codesPath, maxUnits, negativeRules := assessment.PlannerConfig(ctx)
	return chunker.BuildChangeUnitBundles(codesPath, units, maxUnits, negativeRules)
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
	bundles, err := chunker.BuildChangeUnitBundles(codesPath, units, maxUnits, negativeRules)
	if err != nil {
		return nil, err
	}
	for index := range bundles {
		bundles[index].Name = fmt.Sprintf("%s-r%d-%03d", parent.Name, depth, index+1)
	}
	return bundles, nil
}

func (BundlePlanner) PluginPlan(ctx assessment.PlanContext, bundle chunker.SemanticBundle) []coverage.PlannedFile {
	codesPath := ""
	if ctx.EngineContext != nil {
		codesPath = ctx.EngineContext.CodesPath
	}
	return changePlannedFiles(codesPath, bundle)
}

func changePlannedFiles(codesPath string, bundle chunker.SemanticBundle) []coverage.PlannedFile {
	plannedFiles := make([]coverage.PlannedFile, 0, len(bundle.PrimaryFiles))
	for _, path := range bundle.PrimaryFiles {
		_, statErr := os.Stat(filepath.Join(codesPath, filepath.FromSlash(path)))
		deleted := statErr != nil
		pathFiles := make([]coverage.PlannedFile, 0, len(bundle.PrimaryUnits))
		for _, unit := range bundle.PrimaryUnits {
			if unit.Path != path {
				continue
			}
			hunkRange := ""
			switch {
			case unit.EndLine > unit.StartLine:
				hunkRange = fmt.Sprintf("%d-%d", unit.StartLine, unit.EndLine)
			case unit.StartLine > 0:
				hunkRange = fmt.Sprintf("%d", unit.StartLine)
			}
			if hunkRange == "" {
				continue
			}
			pathFiles = append(pathFiles, coverage.PlannedFile{
				Path:        path,
				DiffTouched: true,
				Deleted:     deleted,
				HunkRanges:  []string{hunkRange},
			})
		}
		if len(pathFiles) == 0 {
			pathFiles = append(pathFiles, coverage.PlannedFile{Path: path, DiffTouched: true, Deleted: deleted})
		}
		plannedFiles = append(plannedFiles, pathFiles...)
	}
	return plannedFiles
}
