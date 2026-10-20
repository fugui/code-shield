package debate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/dispatcher"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
	assessmentprofiles "code-shield/services/engines/assessment/profiles"
	"code-shield/services/engines/chunker"
	"code-shield/services/governance"
	"code-shield/services/invoker"
	"gorm.io/datatypes"
)

func (e *DebateEngine) runSpecializedAssessment(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	outPath string,
) ([]models.AnalysisFinding, []coverage.AssessmentRecord, int64, error) {
	// 1. 执行上下文增强插件 (Context Enricher SPI)
	e.applyContextEnrichers(ctx, &bundle)

	// 2. 执行前置轻量门禁与单轮极速复核 (Preflight Gate & Thin Verifier SPI)
	interceptFindings, interceptRecords, remainingUnits, allDone := e.applyPreflightGates(ctx, &bundle)
	if allDone {
		log.Printf("[DebateEngine] Bundle [%s]: All %d units fully triaged by preflight SPI plugins (0 LLM Token).",
			bundle.Name, len(bundle.PrimaryUnits))
		return interceptFindings, interceptRecords, 0, nil
	}
	bundle.PrimaryUnits = remainingUnits

	tierCfg := models.AppConfig.GetTierConfig("tier1_hunter")
	budgetSeconds := tierCfg.TimeoutSeconds
	if budgetSeconds <= 0 {
		budgetSeconds = 600
	}
	attemptSeconds := tierCfg.AttemptTimeoutSeconds
	if attemptSeconds <= 0 {
		attemptSeconds = budgetSeconds
	}

	stageCtx := *ctx
	budgetCtx, cancel := context.WithTimeout(ctx.Ctx, time.Duration(budgetSeconds)*time.Second)
	defer cancel()
	stageCtx.Ctx = budgetCtx

	plan, err := dispatcher.GetTierRouter().AcquireTierResourcePlan(stageCtx.Ctx, "tier1_hunter", nil)
	if err != nil {
		return interceptFindings, interceptRecords, 0, err
	}
	execution, err := buildSpecializedAssessmentPlan(&stageCtx, bundle)
	if err != nil {
		return interceptFindings, interceptRecords, 0, err
	}
	llmFindings, llmRecords, tokens, err := e.runSpecializedAssessmentWithBudget(&stageCtx, bundle, outPath, budgetSeconds, attemptSeconds, execution, plan)
	finalFindings := append(interceptFindings, llmFindings...)
	finalRecords := append(interceptRecords, llmRecords...)
	if err != nil {
		return finalFindings, finalRecords, tokens, err
	}
	return finalFindings, finalRecords, tokens, nil
}

type specializedAssessmentPlan struct {
	plan         dispatcher.TierPlan
	registration assessment.ProfileRegistration
	contract     assessment.OutputContract
	artifact     assessment.ArtifactContract
	prompt       string
	promptRefs   *assessment.PromptRefSession
}

func buildSpecializedAssessmentPlan(ctx *engines.EngineContext, bundle chunker.SemanticBundle) (*specializedAssessmentPlan, error) {
	if ctx.AssessmentProfile == "" {
		return nil, fmt.Errorf("%w: assessment profile is not selected", assessment.ErrProfileRegistry)
	}
	profileName := ctx.AssessmentProfile
	registry, registryErr := assessmentprofiles.Registry()
	if registryErr != nil {
		return nil, registryErr
	}
	registration, registrationErr := registry.Get(profileName)
	if registrationErr != nil {
		return nil, registrationErr
	}

	assessmentCtx := assessment.AssessmentContext{
		EngineContext:     ctx,
		BundleID:          bundle.Name,
		AllowedCategories: ctx.AllowedCategories,
	}
	assessmentBundle := assessment.Bundle{
		ID:       bundle.Name,
		Name:     bundle.Name,
		Units:    bundle.PrimaryUnits,
		AllFiles: bundle.AllFiles,
	}
	prompt, promptRefs, promptErr := registration.Prompt.BuildPrompt(assessmentCtx, assessmentBundle)
	if promptErr != nil {
		return nil, promptErr
	}
	contract := registration.Contract.Contract()
	artifactContract, artifactContractErr := assessment.ArtifactContractForOutput(
		assessment.AssessmentStage, contract, "",
	)
	if artifactContractErr != nil {
		return nil, artifactContractErr
	}
	contractRegistry := assessment.NewArtifactContractRegistry()
	if registerErr := contractRegistry.Register(profileName, artifactContract); registerErr != nil {
		return nil, registerErr
	}
	artifactContract, artifactContractErr = contractRegistry.Get(profileName)
	if artifactContractErr != nil {
		return nil, artifactContractErr
	}
	return &specializedAssessmentPlan{
		registration: registration,
		contract:     contract,
		artifact:     artifactContract,
		prompt:       prompt,
		promptRefs:   promptRefs,
	}, nil
}

func (e *DebateEngine) runSpecializedAssessmentWithBudget(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	outPath string,
	budgetSeconds int,
	attemptSeconds int,
	execution *specializedAssessmentPlan,
	tierPlan dispatcher.TierPlan,
) ([]models.AnalysisFinding, []coverage.AssessmentRecord, int64, error) {
	recovery := models.AppConfig.GetTierConfig("tier1_hunter").Recovery
	if len(recovery.SplitOn) == 0 {
		recovery.SplitOn = []string{
			string(invoker.ErrorClassIdleTimeout),
			string(invoker.ErrorClassTimeout),
			string(invoker.ErrorClassOutputMissing),
		}
	}
	remainingSeconds := hunterRemainingSeconds(ctx.Ctx, budgetSeconds)
	if remainingSeconds <= 0 {
		return nil, nil, 0, fmt.Errorf("specialized assessment stage budget exhausted: %w", ctx.Ctx.Err())
	}
	attemptSeconds = min(attemptSeconds, remainingSeconds)

	var assessmentFindings []models.AnalysisFinding
	var assessmentRecords []coverage.AssessmentRecord
	_, _, tokens, invocationErr := runTierInvocationWithRecovery(
		ctx.Ctx,
		"tier1_hunter",
		attemptSeconds,
		nil,
		func(callCtx context.Context, candidate dispatcher.TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error) {
			callEngineCtx := *ctx
			callEngineCtx.Ctx = callCtx
			findings, records, tokens, err := e.runSpecializedAssessmentOnce(
				&callEngineCtx,
				bundle,
				outPath,
				timeoutSeconds,
				candidate,
				execution,
				"",
				metrics,
			)
			if err == nil {
				assessmentFindings = findings
				assessmentRecords = records
			}
			return "", tokens, err
		},
		dispatcher.WithTierSplitRecovery(func(
			_ context.Context,
			_ dispatcher.TierCandidate,
			tokens int64,
			class invoker.ErrorClass,
			err error,
		) (bool, string, int64, error) {
			if !shouldSplitBundleOnRecovery(ctx.Ctx, recovery, class, bundle, err) || (len(bundle.AllFiles) <= 1 && len(bundle.PrimaryUnits) <= 1) {
				return false, "", tokens, err
			}
			findings, records, splitTokens, splitErr := e.splitSpecializedAssessmentBundle(
				ctx, bundle, outPath, budgetSeconds, attemptSeconds, execution, tierPlan,
				nil, nil, tokens, err, class,
			)
			if len(findings) > 0 {
				assessmentFindings = findings
			}
			if len(records) > 0 {
				assessmentRecords = records
			}
			return true, "", splitTokens, splitErr
		}),
	)
	return assessmentFindings, assessmentRecords, tokens, invocationErr
}

func (e *DebateEngine) splitSpecializedAssessmentBundle(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	outPath string,
	budgetSeconds int,
	attemptSeconds int,
	execution *specializedAssessmentPlan,
	tierPlan dispatcher.TierPlan,
	findings []models.AnalysisFinding,
	records []coverage.AssessmentRecord,
	tokens int64,
	err error,
	class invoker.ErrorClass,
) ([]models.AnalysisFinding, []coverage.AssessmentRecord, int64, error) {
	if !canSplit(ctx.Ctx, models.AppConfig.GetTierConfig("tier1_hunter")) || (len(bundle.AllFiles) <= 1 && len(bundle.PrimaryUnits) <= 1) {
		log.Printf("[DebateEngine] Assessment bundle [%s]: cannot split further: class=%s files=%d units=%d (%v)",
			bundle.Name, class, len(bundle.AllFiles), len(bundle.PrimaryUnits), err)
		return findings, records, tokens, err
	}
	subBundles := chunker.SplitSemanticBundleInHalf(bundle)
	if len(subBundles) == 0 {
		return findings, records, tokens, err
	}
	log.Printf("[DebateEngine] Assessment bundle [%s]: %s (%v); splitting %d files, %d units into %d smaller bundles",
		bundle.Name, class, err, len(bundle.AllFiles), len(bundle.PrimaryUnits), len(subBundles))
	beginSplit(ctx.Ctx)

	mergedFindings := findings
	mergedRecords := records
	totalTokens := tokens
	for i, subBundle := range subBundles {
		remainingSeconds := hunterRemainingSeconds(ctx.Ctx, budgetSeconds)
		if remainingSeconds <= 0 {
			return mergedFindings, mergedRecords, totalTokens, fmt.Errorf("specialized assessment split budget exhausted before bundle %d/%d: %w", i+1, len(subBundles), ctx.Ctx.Err())
		}
		subExecution := execution
		if len(subBundle.PrimaryUnits) > 0 && execution != nil && execution.promptRefs != nil {
			if plan, err := buildSpecializedAssessmentSubPlan(ctx, subBundle, execution); err == nil {
				subExecution = plan
			} else {
				log.Printf("[DebateEngine] Warning: failed to build sub-plan for assessment bundle %s: %v", subBundle.Name, err)
			}
		}
		subFindings, subRecords, subTokens, subErr := e.runSpecializedAssessmentWithBudget(
			ctx, subBundle, assessmentSplitOutputPath(outPath, i+1), remainingSeconds, attemptSeconds, subExecution, tierPlan,
		)
		totalTokens += subTokens
		mergedFindings = append(mergedFindings, subFindings...)
		mergedRecords = append(mergedRecords, subRecords...)
		if subErr != nil {
			return mergedFindings, mergedRecords, totalTokens, fmt.Errorf("specialized assessment split bundle %d/%d failed: %w", i+1, len(subBundles), subErr)
		}
	}
	cleanupSplitArtifacts(outPath)
	return mergedFindings, mergedRecords, totalTokens, nil
}

func buildSpecializedAssessmentSubPlan(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	parent *specializedAssessmentPlan,
) (*specializedAssessmentPlan, error) {
	if parent == nil {
		return nil, errors.New("parent assessment plan is nil")
	}
	assessmentCtx := assessment.AssessmentContext{
		EngineContext:     ctx,
		BundleID:          bundle.Name,
		AllowedCategories: ctx.AllowedCategories,
		PromptRefs:        parent.promptRefs,
	}
	assessmentBundle := assessment.Bundle{
		ID:       bundle.Name,
		Name:     bundle.Name,
		Units:    bundle.PrimaryUnits,
		AllFiles: bundle.AllFiles,
	}
	prompt, promptRefs, promptErr := parent.registration.Prompt.BuildPrompt(assessmentCtx, assessmentBundle)
	if promptErr != nil {
		return nil, promptErr
	}
	return &specializedAssessmentPlan{
		registration: parent.registration,
		contract:     parent.contract,
		artifact:     parent.artifact,
		prompt:       prompt,
		promptRefs:   promptRefs,
	}, nil
}

func assessmentSplitOutputPath(outPath string, index int) string {
	if outPath == "" {
		return ""
	}
	ext := filepath.Ext(outPath)
	base := strings.TrimSuffix(outPath, ext)
	return fmt.Sprintf("%s.split-%d%s", base, index, ext)
}

func recordFreshRetryMetrics(
	ctx context.Context,
	candidate dispatcher.TierCandidate,
	metrics *invoker.InvocationMetrics,
	err error,
) {
	if stats := splitStatsFromContext(ctx); stats != nil {
		stats.FreshRetries++
		stats.ResourceChain = append(stats.ResourceChain, candidate.ResourceID)
		stats.StageChain = append(stats.StageChain, "specialized_assessment")
		class := invoker.ErrorClassNone
		if err != nil {
			class = invoker.ClassifyError(err)
		}
		stats.ErrorClasses = append(stats.ErrorClasses, string(class))
	}
	recordAttemptMetrics(ctx, metrics)
}

func recordAttemptMetrics(ctx context.Context, metrics *invoker.InvocationMetrics) {
	if metrics == nil {
		return
	}
	if stats := splitStatsFromContext(ctx); stats != nil {
		stats.QueueWaitMS = append(stats.QueueWaitMS, metrics.QueueWaitMs)
		stats.DurationSeconds = append(stats.DurationSeconds, float64(metrics.DurationMs)/1000)
		stats.DriverFailovers += metrics.DriverFailovers
	}
	metrics.QueueWaitMs = 0
	metrics.DurationMs = 0
	metrics.DriverFailovers = 0
}

func (e *DebateEngine) runSpecializedAssessmentOnce(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	outPath string,
	timeoutSeconds int,
	candidate dispatcher.TierCandidate,
	execution *specializedAssessmentPlan,
	promptSuffix string,
	metricsOpt ...*invoker.InvocationMetrics,
) ([]models.AnalysisFinding, []coverage.AssessmentRecord, int64, error) {
	workCtx := &invoker.LLMWorkContext{
		ReportID: ctx.ReportID,
		RepoName: ctx.RepoName,
		TaskType: ctx.TaskTypeName,
		Stage:    "Tier 1: 专项 Primary Unit 评估",
		SubTask:  fmt.Sprintf("分片束 %s (%d 个 primary units)", bundle.Name, len(bundle.PrimaryUnits)),
		TierName: "tier1_hunter",
	}
	workCtx.ResourceID = candidate.ResourceID
	prompt := execution.prompt
	trimmedSuffix := strings.TrimSpace(promptSuffix)
	if trimmedSuffix != "" && trimmedSuffix != string(invoker.ErrorClassNone) {
		prompt += "\n\n" + trimmedSuffix
	}
	metrics := &invoker.InvocationMetrics{}
	if len(metricsOpt) > 0 && metricsOpt[0] != nil {
		metrics = metricsOpt[0]
	}
	responseFormatMode := models.AppConfig.AssessmentJSONSchemaMode()
	jsonSchema := assessmentJSONSchemaRequest(execution.artifact, responseFormatMode)
	rawOutput, tokens, err := callSpecializedAssessmentTier(
		ctx.Ctx,
		candidate.Driver,
		candidate.Model,
		prompt,
		ctx.CodesPath,
		outPath,
		timeoutSeconds,
		tierTimeoutPolicy(models.AppConfig.GetTierConfig("tier1_hunter")),
		metrics,
		jsonSchema,
		workCtx,
	)
	if err != nil {
		return nil, nil, tokens, err
	}
	pipelineObservation := assessmentPipelineObservation(candidate, responseFormatMode, metrics)

	findings, records, consumedTokens, consumeErr := e.consumePluginArtifact(
		ctx, bundle, rawOutput, tokens, execution.promptRefs, execution.registration, execution.artifact,
		pipelineObservation,
	)
	tokens += consumedTokens
	if consumeErr != nil {
		return nil, nil, tokens, consumeErr
	}
	return findings, records, tokens, nil
}

func (e *DebateEngine) consumePluginArtifact(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	rawOutput string,
	tokens int64,
	promptRefs *assessment.PromptRefSession,
	registration assessment.ProfileRegistration,
	artifactContract assessment.ArtifactContract,
	observation assessment.ArtifactPipelineObservation,
) ([]models.AnalysisFinding, []coverage.AssessmentRecord, int64, error) {
	assessmentCtx := assessment.AssessmentContext{
		EngineContext:     ctx,
		BundleID:          bundle.Name,
		AllowedCategories: ctx.AllowedCategories,
	}
	assessmentBundle := assessment.Bundle{
		ID:       bundle.Name,
		Name:     bundle.Name,
		Units:    bundle.PrimaryUnits,
		AllFiles: bundle.AllFiles,
	}
	if promptRefs == nil {
		return nil, nil, tokens, fmt.Errorf("%w: prompt refs are required", assessment.ErrProfileRegistry)
	}
	planView := assessment.PlanView{
		BundleID:          bundle.Name,
		Units:             bundle.PrimaryUnits,
		AllowedCategories: ctx.AllowedCategories,
		PromptRefs:        promptRefs,
	}
	var result assessment.AssessmentResult
	var artifactRepairMetrics assessment.ArtifactRepairMetrics
	if artifactContract.SchemaID != "" {
		var validateErr error
		validation, artifactRepairMetrics, validateErr := assessment.ValidateAndRepairArtifact(
			ctx.Ctx,
			[]byte(rawOutput),
			artifactContract,
			planView,
			registration.Validate,
			assessment.ArtifactPipelineOptions{
				MaxLLMAttempts: models.AppConfig.MaxSchemaRepairAttempts(),
				Repair:         e.repairAssessmentArtifact,
				Observation:    observation,
			},
		)
		persistArtifactRepairAudit(ctx, bundle, artifactRepairMetrics)
		if stats := splitStatsFromContext(ctx.Ctx); stats != nil {
			stats.ArtifactSchemaID = artifactRepairMetrics.SchemaID
			stats.ArtifactSchemaHash = artifactRepairMetrics.SchemaHash
			stats.ResponseFormatMode = artifactRepairMetrics.ResponseFormatMode
			stats.ResponseFormatFallbacks = observation.ResponseFormatFallbacks
			stats.ContractRepairs += artifactRepairMetrics.LLMRepairAttempts
			stats.SchemaRepairAttempts += artifactRepairMetrics.LLMRepairAttempts
			stats.SchemaRepairSuccesses += artifactRepairMetrics.LLMRepairSuccesses
			stats.JSONSyntaxRepairs += artifactRepairMetrics.LocalRepairs
		}
		if validateErr != nil {
			if errors.Is(validateErr, assessment.ErrArtifactEmpty) {
				return nil, nil, tokens, invoker.WrapClassifiedError(
					invoker.ErrorClassOutputMissing, validateErr, "assessment artifact is empty",
				)
			}
			return nil, nil, tokens, invoker.WrapClassifiedError(
				invoker.ErrorClassContractMismatch, validateErr, "assessment artifact contract validation failed",
			)
		}
		result = assessment.AssessmentResult{Artifact: validation.Artifact, Valid: validation.Artifact.Assessments}
	} else {
		artifact, normalizeErr := registration.Normalize.Normalize(rawOutput, promptRefs)
		if normalizeErr != nil {
			return nil, nil, tokens, normalizeErr
		}
		validated, validateErr := registration.Validate.Validate(planView, artifact)
		if validateErr != nil {
			return nil, nil, tokens, validateErr
		}
		result = validated
	}
	if len(result.Valid) == 0 {
		return nil, nil, tokens, fmt.Errorf("%w: assessment produced no valid units", assessment.ErrArtifactInvalid)
	}
	reconciliation := registration.Reconcile.Reconcile(planView, result)
	records := make([]coverage.AssessmentRecord, 0, len(result.Valid))
	for _, item := range result.Valid {
		records = append(records, coverage.AssessmentRecord{
			PrimaryUnitID: item.PrimaryUnitID,
			Status:        string(item.Outcome),
		})
	}
	findings, err := registration.MapFindings.MapFindings(assessmentCtx, assessmentBundle, result)
	if err != nil {
		return nil, records, tokens, err
	}
	findings = governAssessmentFindings(ctx, findings)
	if artifactContract.SchemaID != "" {
		if encoded, encodeErr := json.Marshal(artifactRepairMetrics); encodeErr == nil {
			for index := range findings {
				findings[index].ArtifactSchemaID = artifactContract.SchemaID
				findings[index].ArtifactSchemaHash = artifactContract.SchemaHash
				findings[index].ArtifactRepairMetrics = datatypes.JSON(encoded)
			}
		}
	}
	if reconciliation.PlannedUnits != reconciliation.MatchedUnits {
		log.Printf("[DebateEngine] assessment partial salvage: profile=%s bundle=%s planned=%d matched=%d missing=%v invalid=%d unknown=%v",
			registration.Descriptor.Name, bundle.Name, reconciliation.PlannedUnits, reconciliation.MatchedUnits, reconciliation.MissingUnits,
			len(result.Invalid), result.Unknown)
	}
	return findings, records, tokens, nil
}

func governAssessmentFindings(ctx *engines.EngineContext, findings []models.AnalysisFinding) []models.AnalysisFinding {
	if ctx == nil {
		return findings
	}
	hasTaxonomy := len(ctx.Taxonomy.Categories) > 0
	hasAllowed := len(ctx.AllowedCategories) > 0
	if !hasTaxonomy && !hasAllowed {
		return findings
	}

	for i := range findings {
		if findings[i].AssessmentOutcome != string(assessment.OutcomeDefect) {
			continue
		}
		if hasTaxonomy {
			resolution := ResolveCategory(findings[i].CategoryCode, findings[i].Category, ctx.Taxonomy)
			switch resolution.Status {
			case CategoryResolutionValid, CategoryResolutionCodeAuthority, CategoryResolutionLabelAuthority, CategoryResolutionAliasAuthority:
				findings[i].CategoryCode = resolution.Code
				findings[i].Category = resolution.Label
				findings[i].CategoryStatus = CategoryStatusValid
				findings[i].TaxonomyGoverned = true
				findings[i].CategorySource = CategorySourceDeterministic
				findings[i].TaxonomyHash = ctx.TaxonomyHash
			default:
				if findings[i].CategoryCode != "" {
					findings[i].CategoryStatus = CategoryStatusReviewRequired
				}
			}
		} else if hasAllowed {
			normalized := governance.NormalizeCategory(findings[i].Category, ctx.AllowedCategories)
			findings[i].Category = normalized.Category
			findings[i].CategoryDetail = normalized.Detail
			findings[i].TaxonomyGoverned = normalized.Governed
			if normalized.Governed {
				findings[i].CategoryStatus = CategoryStatusValid
			}
		}
	}
	return findings
}

func assessmentRecordsFromFindings(findings ...models.AnalysisFinding) []coverage.AssessmentRecord {
	records := make([]coverage.AssessmentRecord, 0, len(findings))
	for _, finding := range findings {
		if finding.PrimaryUnitID == "" || finding.AssessmentStatus == "" {
			continue
		}
		records = append(records, coverage.AssessmentRecord{
			PrimaryUnitID: finding.PrimaryUnitID,
			Status:        finding.AssessmentStatus,
		})
	}
	return records
}

func assessmentPipelineObservation(
	candidate dispatcher.TierCandidate,
	responseFormatMode string,
	metrics *invoker.InvocationMetrics,
) assessment.ArtifactPipelineObservation {
	mode := responseFormatMode
	if mode == "auto" && metrics != nil && metrics.ResponseFormatFallbacks > 0 {
		mode = "json_object"
	}
	return assessment.ArtifactPipelineObservation{
		Driver:                  candidate.Driver,
		ResourceID:              candidate.ResourceID,
		ResponseFormatMode:      mode,
		ResponseFormatFallbacks: metrics.ResponseFormatFallbacks,
	}
}

func persistArtifactRepairAudit(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	metrics assessment.ArtifactRepairMetrics,
) {
	if models.DB == nil || ctx == nil || ctx.ReportID == 0 {
		return
	}
	schemaIssues, issuesErr := json.Marshal(metrics.SchemaRepairIssues)
	attempts, attemptsErr := json.Marshal(metrics.RepairAttempts)
	if issuesErr != nil || attemptsErr != nil {
		log.Printf("[AssessmentContract] failed to encode repair audit report=%d bundle=%s issues=%v attempts=%v",
			ctx.ReportID, bundle.Name, issuesErr, attemptsErr)
		return
	}
	audit := models.ArtifactRepairAudit{
		ReportID:             ctx.ReportID,
		RepoID:               ctx.RepoID,
		TaskTypeID:           ctx.TaskTypeID,
		BundleID:             bundle.Name,
		Driver:               metrics.Driver,
		ResourceID:           metrics.ResourceID,
		ResponseFormatMode:   metrics.ResponseFormatMode,
		SchemaID:             metrics.SchemaID,
		SchemaHash:           metrics.SchemaHash,
		OriginalArtifactHash: metrics.OriginalArtifactHash,
		FinalStatus:          metrics.FinalStatus,
		RepairTokens:         metrics.RepairTokens,
		LocalRepairs:         metrics.LocalRepairs,
		LLMRepairs:           metrics.LLMRepairs,
		LLMRepairAttempts:    metrics.LLMRepairAttempts,
		LLMRepairSuccesses:   metrics.LLMRepairSuccesses,
		RepairDrifted:        metrics.RepairDrifted,
		RepairDriftUnchecked: metrics.RepairDriftUnchecked,
		RepairDriftUnitRef:   metrics.RepairDriftUnitRef,
		SchemaRepairIssues:   datatypes.JSON(schemaIssues),
		RepairAttempts:       datatypes.JSON(attempts),
	}
	if err := models.CreateArtifactRepairAudit(models.DB, &audit); err != nil {
		log.Printf("[AssessmentContract] failed to persist repair audit report=%d bundle=%s err=%v",
			ctx.ReportID, bundle.Name, err)
	}
}

func assessmentLineNumber(unit coverage.PlanUnit) string {
	if unit.StartLine <= 0 {
		return ""
	}
	if unit.EndLine > unit.StartLine {
		return fmt.Sprintf("%d-%d", unit.StartLine, unit.EndLine)
	}
	return fmt.Sprintf("%d", unit.StartLine)
}
