package debate

import (
	"log"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/plugins"
)

// executePreflightTriage 核心调度流水线中的纯通用前置分流与复核逻辑
func (e *DebateEngine) executePreflightTriage(ctx *engines.EngineContext, unit coverage.PlanUnit) (plugins.RadarResult, bool) {
	gateCfg := ctx.Plugins.PreflightGate
	if gateCfg == nil || gateCfg.ID == "" {
		return plugins.RadarResult{Decision: plugins.DecisionPass}, false
	}

	gate, err := plugins.GetPreflightGate(gateCfg.ID)
	if err != nil {
		log.Printf("[Preflight] Warning: gate plugin %q not found, fallback to main review: %v", gateCfg.ID, err)
		return plugins.RadarResult{Decision: plugins.DecisionPass}, false
	}

	// 1. 执行前置轻量门禁评估（黑盒）
	result := gate.Inspect(ctx.CodesPath, unit, gateCfg.Options)
	if result.Decision == plugins.DecisionTier0Defect || result.Decision == plugins.DecisionFastPass {
		return result, true
	}

	// 2. 若进入争议区 (NEED_VERIFY)，检查是否挂载了 thin_verifier 插件
	verifierCfg := ctx.Plugins.ThinVerifier
	if verifierCfg != nil && verifierCfg.ID != "" {
		verifier, err := plugins.GetThinVerifier(verifierCfg.ID)
		if err == nil {
			vResult, vErr := verifier.Verify(ctx.CodesPath, unit, result.FactHints, verifierCfg.PromptFile, verifierCfg.TimeoutSeconds)
			if vErr == nil && (vResult.Decision == plugins.DecisionTier0Defect || vResult.Decision == plugins.DecisionFastPass) {
				return vResult, true
			}
		} else {
			log.Printf("[Preflight] Warning: thin verifier plugin %q not found: %v", verifierCfg.ID, err)
		}
	}

	return result, false
}

// applyPreflightGates 对 Bundle 内的每个 PrimaryUnit 执行前置门禁与复核分流
func (e *DebateEngine) applyPreflightGates(ctx *engines.EngineContext, bundle *chunker.SemanticBundle) (
	interceptFindings []models.AnalysisFinding,
	interceptRecords []coverage.AssessmentRecord,
	remainingUnits []coverage.PlanUnit,
	allDone bool,
) {
	if len(bundle.PrimaryUnits) == 0 || ctx.Plugins.PreflightGate == nil || ctx.Plugins.PreflightGate.ID == "" {
		return nil, nil, bundle.PrimaryUnits, false
	}

	for _, unit := range bundle.PrimaryUnits {
		res, decided := e.executePreflightTriage(ctx, unit)
		if decided {
			if res.Decision == plugins.DecisionTier0Defect {
				if res.Finding != nil {
					res.Finding.TaskReportID = ctx.ReportID
					res.Finding.TaskTypeID = ctx.TaskTypeID
					res.Finding.RepoID = ctx.RepoID
					interceptFindings = append(interceptFindings, *res.Finding)
				}
				interceptRecords = append(interceptRecords, coverage.AssessmentRecord{
					PrimaryUnitID: unit.ID,
					Status:        "DEFECT",
				})
			} else if res.Decision == plugins.DecisionFastPass {
				interceptRecords = append(interceptRecords, coverage.AssessmentRecord{
					PrimaryUnitID: unit.ID,
					Status:        "PASS",
				})
			}
		} else {
			remainingUnits = append(remainingUnits, unit)
		}
	}

	if len(remainingUnits) == 0 {
		return interceptFindings, interceptRecords, nil, true
	}
	return interceptFindings, interceptRecords, remainingUnits, false
}

// applyContextEnrichers 检查并执行声明式挂载的上下文因果增强插件
func (e *DebateEngine) applyContextEnrichers(ctx *engines.EngineContext, bundle *chunker.SemanticBundle) {
	enricherCfg := ctx.Plugins.ContextEnricher
	if enricherCfg == nil || enricherCfg.ID == "" {
		return
	}
	enricher, err := plugins.GetContextEnricher(enricherCfg.ID)
	if err != nil {
		log.Printf("[ContextEnricher] Warning: enricher plugin %q not found: %v", enricherCfg.ID, err)
		return
	}
	if err := enricher.Enrich(ctx.CodesPath, bundle, enricherCfg.Options); err != nil {
		log.Printf("[ContextEnricher] Warning: enrich failed for bundle %s: %v", bundle.Name, err)
	}
}
