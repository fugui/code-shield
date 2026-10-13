package debate

import (
	"log"
	"sync"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/plugins"
	_ "code-shield/services/engines/plugins/gates/testcase"
	_ "code-shield/services/engines/plugins/verifiers/native"
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

// applyPreflightGates 对 Bundle 内的每个 PrimaryUnit 执行前置门禁与受控并发复核分流
func (e *DebateEngine) applyPreflightGates(ctx *engines.EngineContext, bundle *chunker.SemanticBundle) (
	interceptFindings []models.AnalysisFinding,
	interceptRecords []coverage.AssessmentRecord,
	remainingUnits []coverage.PlanUnit,
	allDone bool,
) {
	if len(bundle.PrimaryUnits) == 0 || ctx.Plugins.PreflightGate == nil || ctx.Plugins.PreflightGate.ID == "" {
		return nil, nil, bundle.PrimaryUnits, false
	}

	gateCfg := ctx.Plugins.PreflightGate
	gate, err := plugins.GetPreflightGate(gateCfg.ID)
	if err != nil {
		log.Printf("[Preflight] Warning: gate plugin %q not found, fallback to main review: %v", gateCfg.ID, err)
		return nil, nil, bundle.PrimaryUnits, false
	}

	type pendingVerification struct {
		unit  coverage.PlanUnit
		hints []plugins.StructuralFactHint
	}

	var pending []pendingVerification

	// 阶段一：全量单元毫秒级静态内存筛查 (0 Token)
	for _, unit := range bundle.PrimaryUnits {
		res := gate.Inspect(ctx.CodesPath, unit, gateCfg.Options)
		if res.Decision == plugins.DecisionTier0Defect || res.Decision == plugins.DecisionFastPass {
			if res.Finding != nil {
				res.Finding.TaskReportID = ctx.ReportID
				res.Finding.TaskTypeID = ctx.TaskTypeID
				res.Finding.RepoID = ctx.RepoID
				interceptFindings = append(interceptFindings, *res.Finding)
			}
			status := "DEFECT"
			if res.Decision == plugins.DecisionFastPass {
				status = "PASS"
			}
			interceptRecords = append(interceptRecords, coverage.AssessmentRecord{
				PrimaryUnitID: unit.ID,
				Status:        status,
			})
		} else if ctx.Plugins.ThinVerifier != nil && ctx.Plugins.ThinVerifier.ID != "" {
			pending = append(pending, pendingVerification{unit: unit, hints: res.FactHints})
		} else {
			remainingUnits = append(remainingUnits, unit)
		}
	}

	// 阶段二：针对 NEED_VERIFY 争议单元执行受控并发轻量复核 (Worker Pool, Concurrency = 4)
	if len(pending) > 0 {
		verifierCfg := ctx.Plugins.ThinVerifier
		verifier, vErr := plugins.GetThinVerifier(verifierCfg.ID)
		if vErr != nil {
			log.Printf("[Preflight] Warning: thin verifier plugin %q not found: %v", verifierCfg.ID, vErr)
			for _, p := range pending {
				remainingUnits = append(remainingUnits, p.unit)
			}
		} else {
			const maxConcurrentVerifiers = 4
			sem := make(chan struct{}, maxConcurrentVerifiers)
			var mu sync.Mutex
			var wg sync.WaitGroup

			for _, p := range pending {
				wg.Add(1)
				sem <- struct{}{}
				go func(pv pendingVerification) {
					defer wg.Done()
					defer func() { <-sem }()

					vRes, err := verifier.Verify(ctx.CodesPath, pv.unit, pv.hints, verifierCfg.PromptFile, verifierCfg.TimeoutSeconds)
					mu.Lock()
					defer mu.Unlock()

					if err == nil && (vRes.Decision == plugins.DecisionTier0Defect || vRes.Decision == plugins.DecisionFastPass) {
						if vRes.Finding != nil {
							vRes.Finding.TaskReportID = ctx.ReportID
							vRes.Finding.TaskTypeID = ctx.TaskTypeID
							vRes.Finding.RepoID = ctx.RepoID
							interceptFindings = append(interceptFindings, *vRes.Finding)
						}
						status := "DEFECT"
						if vRes.Decision == plugins.DecisionFastPass {
							status = "PASS"
						}
						interceptRecords = append(interceptRecords, coverage.AssessmentRecord{
							PrimaryUnitID: pv.unit.ID,
							Status:        status,
						})
					} else {
						remainingUnits = append(remainingUnits, pv.unit)
					}
				}(p)
			}
			wg.Wait()
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
