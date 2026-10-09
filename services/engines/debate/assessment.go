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
	"code-shield/services/invoker"
	"gorm.io/datatypes"
)

func (e *DebateEngine) runSpecializedAssessment(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	outPath string,
) ([]models.AnalysisFinding, []coverage.AssessmentRecord, int64, error) {
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
		return nil, nil, 0, err
	}
	execution, err := buildSpecializedAssessmentPlan(&stageCtx, bundle)
	if err != nil {
		return nil, nil, 0, err
	}
	return e.runSpecializedAssessmentWithBudget(&stageCtx, bundle, outPath, budgetSeconds, attemptSeconds, execution, plan)
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
	_, candidate, tokens, invocationErr := runTierInvocationWithRecovery(
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
				string(invoker.ErrorClassNone),
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
			if !shouldSplitBundleOnRecovery(ctx.Ctx, recovery, class, bundle, err) || len(bundle.PrimaryUnits) <= 1 {
				return false, "", tokens, err
			}
			findings, records, splitTokens, splitErr := e.splitSpecializedAssessmentBundle(
				ctx, bundle, outPath, budgetSeconds, attemptSeconds, execution, tierPlan,
				nil, nil, tokens, err, class,
			)
			if splitErr == nil {
				assessmentFindings = findings
				assessmentRecords = records
			}
			return true, "", splitTokens, splitErr
		}),
	)
	if shouldRunGenericAssessmentContractRetry(invocationErr) {
		remainingSeconds := hunterRemainingSeconds(ctx.Ctx, budgetSeconds)
		maxTotalAttempts := recovery.MaxTotalAttempts
		if maxTotalAttempts < 1 {
			maxTotalAttempts = 2
		}
		attemptCount := 0
		if stats := splitStatsFromContext(ctx.Ctx); stats != nil {
			attemptCount = len(stats.ResourceChain)
		}
		if remainingSeconds > 0 && attemptCount < maxTotalAttempts {
			retryTimeout := min(attemptSeconds, remainingSeconds)
			retryCtx, cancelRetry := context.WithTimeout(ctx.Ctx, time.Duration(retryTimeout)*time.Second)
			defer cancelRetry()
			retryEngineCtx := *ctx
			retryEngineCtx.Ctx = retryCtx

			retryMetrics := &invoker.InvocationMetrics{}
			retryFindings, retryRecords, retryTokens, retryErr := e.runSpecializedAssessmentOnce(
				&retryEngineCtx,
				bundle,
				outPath,
				retryTimeout,
				candidate,
				execution,
				string(invoker.ErrorClassNone),
				retryMetrics,
			)
			tokens += retryTokens
			recordFreshRetryMetrics(ctx.Ctx, candidate, retryMetrics, retryErr)
			if retryErr == nil {
				return retryFindings, retryRecords, tokens, nil
			}
			invocationErr = retryErr
		}
	}
	return assessmentFindings, assessmentRecords, tokens, invocationErr
}

func shouldRunGenericAssessmentContractRetry(err error) bool {
	return err != nil && isContractMismatch(err) &&
		contractRepairEnabled("tier1_hunter", err) &&
		!models.AppConfig.AssessmentContractRepairEnabled()
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
	if !canSplit(ctx.Ctx, models.AppConfig.GetTierConfig("tier1_hunter")) || len(bundle.AllFiles) <= 1 {
		log.Printf("[DebateEngine] Assessment bundle [%s]: cannot split further: class=%s files=%d (%v)",
			bundle.Name, class, len(bundle.AllFiles), err)
		return findings, records, tokens, err
	}
	subBundles := chunker.SplitSemanticBundleInHalf(bundle)
	if len(subBundles) == 0 {
		return findings, records, tokens, err
	}
	log.Printf("[DebateEngine] Assessment bundle [%s]: %s (%v); splitting %d files into %d smaller bundles",
		bundle.Name, class, err, len(bundle.AllFiles), len(subBundles))
	beginSplit(ctx.Ctx)

	mergedFindings := findings
	mergedRecords := records
	totalTokens := tokens
	for i, subBundle := range subBundles {
		remainingSeconds := hunterRemainingSeconds(ctx.Ctx, budgetSeconds)
		if remainingSeconds <= 0 {
			return nil, nil, totalTokens, fmt.Errorf("specialized assessment split budget exhausted before bundle %d/%d: %w", i+1, len(subBundles), ctx.Ctx.Err())
		}
		subFindings, subRecords, subTokens, subErr := e.runSpecializedAssessmentWithBudget(
			ctx, subBundle, assessmentSplitOutputPath(outPath, i+1), remainingSeconds, attemptSeconds, execution, tierPlan,
		)
		totalTokens += subTokens
		if subErr != nil {
			return nil, nil, totalTokens, fmt.Errorf("specialized assessment split bundle %d/%d failed: %w", i+1, len(subBundles), subErr)
		}
		mergedFindings = append(mergedFindings, subFindings...)
		mergedRecords = append(mergedRecords, subRecords...)
	}
	cleanupSplitArtifacts(outPath)
	return mergedFindings, mergedRecords, totalTokens, nil
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
	if strings.TrimSpace(promptSuffix) != "" {
		prompt += "\n\n" + promptSuffix
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
	queueWaitMs, durationMs := metrics.QueueWaitMs, metrics.DurationMs
	if err != nil {
		return nil, nil, tokens, err
	}
	pipelineObservation := assessmentPipelineObservation(candidate, responseFormatMode, metrics)

	findings, records, consumedTokens, consumeErr := e.consumePluginArtifact(
		ctx, bundle, rawOutput, tokens, execution.promptRefs, execution.registration, execution.artifact,
		pipelineObservation,
	)
	tokens += consumedTokens
	if consumeErr == nil {
		return findings, records, tokens, nil
	}
	if !isContractMismatch(consumeErr) || !contractRepairEnabled("tier1_hunter", consumeErr) {
		return nil, nil, tokens, consumeErr
	}
	if models.AppConfig.AssessmentContractRepairEnabled() && execution.artifact.SchemaID != "" {
		return nil, nil, tokens, consumeErr
	}

	metrics.ContractRepairs++
	if stats := splitStatsFromContext(ctx.Ctx); stats != nil {
		stats.ContractRepairs++
	}
	repairPrompt := buildAssessmentContractRepairPrompt(execution.prompt, execution.contract, consumeErr)
	metrics.QueueWaitMs = 0
	metrics.DurationMs = 0
	rawOutput, repairTokens, repairErr := callSpecializedAssessmentTier(
		ctx.Ctx,
		candidate.Driver,
		candidate.Model,
		repairPrompt,
		ctx.CodesPath,
		outPath,
		timeoutSeconds,
		tierTimeoutPolicy(models.AppConfig.GetTierConfig("tier1_hunter")),
		metrics,
		jsonSchema,
		workCtx,
	)
	metrics.QueueWaitMs += queueWaitMs
	metrics.DurationMs += durationMs
	tokens += repairTokens
	if repairErr != nil {
		return nil, nil, tokens, invoker.WrapClassifiedError(
			invoker.ErrorClassContractMismatch, repairErr, "assessment schema repair failed",
		)
	}
	findings, records, consumedTokens, consumeErr = e.consumePluginArtifact(
		ctx, bundle, rawOutput, tokens, execution.promptRefs, execution.registration, execution.artifact,
		pipelineObservation,
	)
	tokens += consumedTokens
	if consumeErr != nil {
		return nil, nil, tokens, invoker.WrapClassifiedError(
			invoker.ErrorClassContractMismatch, consumeErr, "assessment output contract mismatch",
		)
	}
	return findings, records, tokens, nil
}

func buildAssessmentContractRepairPrompt(
	prompt string,
	contract assessment.OutputContract,
	cause error,
) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(prompt, "\n"))
	sb.WriteString("\n\n## Output Contract Repair\n")
	sb.WriteString(fmt.Sprintf("上一次输出未通过服务端校验：%v。\n", cause))
	sb.WriteString("请修复 JSON 结构并重新输出完整结果；不得新增解释文字或 Markdown 围栏。\n")
	if contract.SchemaID != "" {
		sb.WriteString(fmt.Sprintf("Schema ID: %s\n", contract.SchemaID))
	}
	if contract.TopLevel != "" {
		sb.WriteString(fmt.Sprintf("Top-level key: %s\n", contract.TopLevel))
	}
	if len(contract.RequiredFields) > 0 {
		sb.WriteString(fmt.Sprintf("Required fields: %s\n", strings.Join(contract.RequiredFields, ", ")))
	}
	if len(contract.ForbiddenTopLevel) > 0 {
		sb.WriteString(fmt.Sprintf("Forbidden top-level keys: %s\n", strings.Join(contract.ForbiddenTopLevel, ", ")))
	}
	if len(contract.AllowedOutcomes) > 0 {
		sb.WriteString(fmt.Sprintf("Allowed outcomes: %s\n", strings.Join(contract.AllowedOutcomes, ", ")))
	}
	if contract.Example != "" {
		sb.WriteString("\nExample:\n```json\n" + contract.Example + "\n```\n")
	}
	return sb.String()
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
	if models.AppConfig.AssessmentContractRepairEnabled() && artifactContract.SchemaID != "" {
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
	if models.AppConfig.AssessmentContractRepairEnabled() && artifactContract.SchemaID != "" {
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
