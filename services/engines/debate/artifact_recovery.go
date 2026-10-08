package debate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/dispatcher"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
)

type hunterArtifactState struct {
	Output                  *HunterOutput
	NormalizedRaw           []byte
	Issues                  []ArtifactIssue
	ValidCandidates         []HunterCandidate
	InvalidCandidates       []HunterCandidate
	Quarantined             int
	ArtifactComplete        bool
	ArtifactQualityDegraded bool
	UnresolvedIssueCount    int
	Taxonomy                models.CategoryTaxonomy
	RepairAudit             *HunterArtifactRepairMetrics
}

func (s *hunterArtifactState) unresolvedIssueCount() int {
	count := 0
	for _, issue := range s.Issues {
		if !issue.Recoverable {
			count++
		}
	}
	return count
}

func (s *hunterArtifactState) refreshDiagnostics() {
	if s == nil {
		return
	}
	s.UnresolvedIssueCount = s.unresolvedIssueCount()
}

func parseHunterArtifactState(
	raw string,
	contract OutputContract,
	repoRoot string,
	bundle chunker.SemanticBundle,
	allowedCategories []string,
	taxonomy models.CategoryTaxonomy,
	aliasRecorders ...func(models.CategoryAliasUsage),
) (*hunterArtifactState, error) {
	audit := newHunterRepairMetrics([]byte(raw), contract)
	var normalizedRaw []byte
	var normalizationIssues []ArtifactIssue
	if models.AppConfig.ArtifactGuardEnabled() {
		normalizedRaw, normalizationIssues = NormalizeHunterArtifactJSON(
			[]byte(raw), repoRoot, bundle.AllFiles,
		)
	} else {
		normalizedRaw = cleanJSONOutput([]byte(raw))
	}

	var out HunterOutput
	if decodeErr := decodeContractJSON(string(normalizedRaw), contract, &out); decodeErr != nil {
		state := &hunterArtifactState{
			NormalizedRaw:    normalizedRaw,
			Issues:           normalizationIssues,
			ArtifactComplete: false,
			RepairAudit:      audit,
		}
		audit.addIssue(ArtifactIssue{
			Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "artifact",
			Code: IssueUnresolved, Message: decodeErr.Error(), Recoverable: false,
		})
		if hasArtifactIssueCode(normalizationIssues, IssueJSONSyntaxNormalized) {
			audit.addSyntaxRepair(normalizedRaw, false, decodeErr, "trailing_comma")
		} else if !bytes.Equal(normalizedRaw, []byte(raw)) {
			audit.addLocalRepair(normalizedRaw, false, decodeErr)
		}
		return state, decodeErr
	}

	for i := range out.Candidates {
		out.Candidates[i].NormalizeAlignFields()
	}

	state := &hunterArtifactState{
		Output:           &out,
		NormalizedRaw:    normalizedRaw,
		Issues:           normalizationIssues,
		ArtifactComplete: true,
		Taxonomy:         taxonomy,
		RepairAudit:      audit,
	}
	if hasArtifactIssueCode(normalizationIssues, IssueJSONSyntaxNormalized) {
		audit.addSyntaxRepair(normalizedRaw, true, nil, "trailing_comma")
	} else if !bytes.Equal(normalizedRaw, []byte(raw)) {
		audit.addLocalRepair(normalizedRaw, true, nil)
	}
	addHunterCategoryMetrics(state.Output, taxonomy)
	state.refreshDiagnostics()

	for _, issue := range state.Issues {
		audit.addIssue(issue)
	}

	for i := range out.Candidates {
		candidate := out.Candidates[i]
		if err := validateCandidateAnchors(&candidate); err != nil {
			state.InvalidCandidates = append(state.InvalidCandidates, candidate)
			issue := ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID,
				JSONPath: fmt.Sprintf("$.candidates[%d]", i), CandidateID: candidate.CandidateID,
				Code: IssueUnresolved, Field: "candidate",
				Message: err.Error(), Recoverable: false,
			}
			state.Issues = append(state.Issues, issue)
			audit.addIssue(issue)
			continue
		}
		resolution := ResolveCategory(candidate.CategoryCode, candidate.GetCategory(), taxonomy)
		if resolution.Status == CategoryResolutionAliasAuthority {
			for _, recordAlias := range aliasRecorders {
				recordAlias(models.CategoryAliasUsage{
					TaxonomyHash: taxonomy.Hash,
					AliasKind:    resolution.AliasKind,
					AliasLabel:   resolution.AliasLabel,
					TargetCode:   resolution.Code,
				})
			}
		}
		if err := resolveCandidateCategory(&candidate, allowedCategories, taxonomy); err != nil {
			candidate.CategoryCode = ""
			candidate.Category = "未分类"
			candidate.ReviewRequired = true
			out.Candidates[i] = candidate
			state.Issues = append(state.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID,
				JSONPath: fmt.Sprintf("$.candidates[%d].category", i), CandidateID: candidate.CandidateID,
				Code: IssueCategoryReviewRequired, Field: "category",
				Message: err.Error(), Recoverable: true,
			})
			state.ValidCandidates = append(state.ValidCandidates, candidate)
			out.Candidates[i] = candidate
			continue
		}
		state.ValidCandidates = append(state.ValidCandidates, candidate)
	}
	if len(state.Output.Candidates) > 0 {
		for _, candidate := range state.Output.Candidates {
			if candidate.ReviewRequired {
				state.ArtifactQualityDegraded = true
				break
			}
		}
	}
	return state, nil
}

func hasArtifactIssueCode(issues []ArtifactIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func (s *hunterArtifactState) salvagedOutput() *HunterOutput {
	if s.Output == nil {
		return nil
	}
	out := *s.Output
	out.Candidates = append([]HunterCandidate(nil), s.ValidCandidates...)
	return &out
}

func repairHunterArtifact(
	ctx context.Context,
	repoRoot string,
	rawArtifact string,
	contract OutputContract,
	issues []ArtifactIssue,
	timeoutSeconds int,
) (string, int64, error) {
	backend := models.AppConfig.SchemaRepairResource()
	rawInv, ok := invoker.GetRawInvoker(backend)
	if !ok || rawInv == nil {
		return "", 0, fmt.Errorf("schema repair backend %q is unavailable", backend)
	}

	outputPath, cleanup, err := tempArtifactPath("hunter-artifact-repair", ".json")
	if err != nil {
		return "", 0, err
	}
	defer cleanup()

	prompt := buildArtifactRepairPrompt(rawArtifact, contract, issues)
	temperature := 0.0
	timeoutMin := 1
	if timeoutSeconds > 0 {
		timeoutMin = (timeoutSeconds + 59) / 60
	}
	req := invoker.AIRequest{
		ParentContext:  ctx,
		WorkDir:        repoRoot,
		PromptMsg:      prompt,
		OutputPath:     outputPath,
		TimeoutMin:     timeoutMin,
		Temperature:    &temperature,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
			Stage:    "系统工具: Hunter Artifact Schema Repair",
			SubTask:  "修复 AI 产物字段契约漂移",
			TierName: "system_tool",
		},
	}
	wrapped := dispatcher.WrapInvoker(rawInv)
	if invokeErr := wrapped.Invoke(req); invokeErr != nil {
		return "", 0, invokeErr
	}
	repaired, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		return "", 0, fmt.Errorf("read schema repair output: %w", readErr)
	}
	output := string(repaired)
	tokens := int64((len(prompt) + len(output)) / 4)
	return output, tokens, nil
}

func buildArtifactRepairPrompt(rawArtifact string, contract OutputContract, issues []ArtifactIssue) string {
	var sb strings.Builder
	sb.WriteString("你是 JSON Artifact 修复器。请只修复结构契约问题，不得改变业务结论、新增 candidate 或删除 candidate。\n\n")
	sb.WriteString("## Service-side Validation Issues\n```json\n")
	issueBytes, _ := json.MarshalIndent(issues, "", "  ")
	sb.Write(issueBytes)
	sb.WriteString("\n```\n\n")
	sb.WriteString("## Artifact To Repair\n```json\n")
	sb.WriteString(strings.TrimSpace(rawArtifact))
	sb.WriteString("\n```\n\n")
	sb.WriteString("## Required Output Contract\n")
	sb.WriteString(RenderOutputContract(contract))
	sb.WriteString("\n只输出修复后的完整 JSON；不要输出 Markdown 围栏、解释或日志。\n")
	return sb.String()
}

func tempArtifactPath(prefix, ext string) (string, func(), error) {
	file, err := os.CreateTemp("", prefix+"-*"+ext)
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return "", func() {}, closeErr
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func parseAndRecoverHunterArtifact(
	ctx context.Context,
	raw string,
	contract OutputContract,
	repoRoot string,
	bundle chunker.SemanticBundle,
	allowedCategories []string,
	taxonomy models.CategoryTaxonomy,
	aliasRecorders ...func(models.CategoryAliasUsage),
) (*hunterArtifactState, int, int, int64, error) {
	state, parseErr := parseHunterArtifactState(raw, contract, repoRoot, bundle, allowedCategories, taxonomy, aliasRecorders...)
	repairAttempts := 0
	repairSuccesses := 0
	var repairTokens int64
	if state == nil {
		state = &hunterArtifactState{ArtifactComplete: false, ArtifactQualityDegraded: true}
	}
	if state.RepairAudit == nil {
		state.RepairAudit = newHunterRepairMetrics([]byte(raw), contract)
	}
	state.refreshDiagnostics()
	if parseErr == nil && state.unresolvedIssueCount() == 0 {
		repairAttempts, repairSuccesses, categoryTokens, categoryErr := repairHunterCategories(ctx, repoRoot, state, contract)
		repairAttempts += repairAttempts
		repairSuccesses += repairSuccesses
		repairTokens += categoryTokens
		if categoryErr != nil {
			state.Issues = append(state.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID,
				Code: IssueCategoryReviewRequired, Field: "category",
				Message:     fmt.Sprintf("category repair failed: %v", categoryErr),
				Recoverable: true,
			})
		}
		state.refreshDiagnostics()
		state.RepairAudit.BaselineSignatureKnown = true
		if baseline, known := hunterBusinessSignature(state.NormalizedRaw); known {
			state.RepairAudit.BaselineSignatureHash = businessSignatureHash(baseline)
		}
		if state.RepairAudit.SyntaxRepairs > 0 {
			state.RepairAudit.RepairOutcome = "syntax_normalized"
		} else {
			state.RepairAudit.RepairOutcome = "verified_success"
		}
		status := "success"
		if state.ArtifactQualityDegraded || state.unresolvedIssueCount() > 0 {
			status = "degraded"
		}
		state.RepairAudit.setFinalStatus(status)
		return state, repairAttempts, repairSuccesses, repairTokens, nil
	}
	preRepairIssues := append([]ArtifactIssue(nil), state.Issues...)
	baselineSignature, baselineSignatureKnown := hunterBusinessSignature(state.NormalizedRaw)
	baselineKnown := baselineSignatureKnown && parseErr == nil && state.Output != nil
	state.RepairAudit.BaselineSignatureKnown = baselineKnown
	state.RepairAudit.BaselineSignatureHash = businessSignatureHash(baselineSignature)

	hunterBinding := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	contractRepairEnabled := true
	if hunterBinding.HasConfig() {
		contractRepairEnabled = recoveryAllows(
			models.AppConfig.GetTierConfig("tier1_hunter").Recovery.ContractRepairOn,
			invoker.ErrorClassContractMismatch,
		)
	}
	for attempt := 0; contractRepairEnabled && attempt < models.AppConfig.MaxSchemaRepairAttempts(); attempt++ {
		select {
		case <-ctx.Done():
			return state, repairAttempts, repairSuccesses, repairTokens, ctx.Err()
		default:
		}

		repairAttempts++
		timeoutSeconds := models.AppConfig.SchemaRepairTimeoutSeconds()
		repairedRaw, tokens, repairErr := repairHunterArtifact(
			ctx, repoRoot, string(state.NormalizedRaw), contract, state.Issues, timeoutSeconds,
		)
		repairTokens += tokens
		if repairErr != nil {
			state.RepairAudit.addLLMRepair("", tokens, false, false, false, repairErr)
			state.Issues = append(state.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID,
				Code: IssueUnresolved, Field: "artifact",
				Message:     fmt.Sprintf("schema repair attempt %d failed: %v", attempt+1, repairErr),
				Recoverable: false,
			})
			break
		}

		repairedState, repairedParseErr := parseHunterArtifactState(
			repairedRaw, contract, repoRoot, bundle, allowedCategories, taxonomy, aliasRecorders...,
		)
		for _, repairedAttempt := range repairedState.RepairAudit.RepairAttempts {
			repairedAttempt.Attempt = len(state.RepairAudit.RepairAttempts) + 1
			state.RepairAudit.RepairAttempts = append(state.RepairAudit.RepairAttempts, repairedAttempt)
		}
		for _, repairedIssue := range repairedState.Issues {
			state.RepairAudit.addIssue(repairedIssue)
		}
		repairedSignature, repairedKnown := hunterBusinessSignature(repairedState.NormalizedRaw)
		state.RepairAudit.RepairedSignatureKnown = repairedKnown
		state.RepairAudit.RepairedSignatureHash = businessSignatureHash(repairedSignature)
		unverifiedBaseline := !baselineKnown || !repairedKnown
		unverifiedAcceptable := unverifiedBaseline &&
			repairedParseErr == nil && repairedState != nil &&
			repairedState.unresolvedIssueCount() == 0 &&
			repairedState.Output != nil && len(repairedState.Output.Candidates) > 0
		businessAccepted := true
		if unverifiedBaseline && !unverifiedAcceptable {
			businessAccepted = false
		} else if !unverifiedBaseline && len(baselineSignature) != len(repairedSignature) {
			businessAccepted = false
			state.RepairAudit.RepairDrifted = true
			state.RepairAudit.RepairDriftUnitRef = firstBusinessSignatureDifference(baselineSignature, repairedSignature)
		} else {
			if !unverifiedBaseline {
				for candidateID, baseline := range baselineSignature {
					if repairedSignature[candidateID] != baseline {
						businessAccepted = false
						state.RepairAudit.RepairDrifted = true
						state.RepairAudit.RepairDriftUnitRef = candidateID
						break
					}
				}
			}
		}
		accepted := repairedParseErr == nil && repairedState != nil &&
			repairedState.unresolvedIssueCount() == 0 && businessAccepted
		state.RepairAudit.addLLMRepair(
			repairedRaw, tokens, accepted,
			!businessAccepted && !state.RepairAudit.RepairDriftUnchecked, false, nil,
		)
		if !businessAccepted {
			rejectionCode := "BUSINESS_DRIFT_REJECTED"
			if unverifiedBaseline {
				rejectionCode = "REPAIR_BASELINE_UNVERIFIED"
			}
			state.RepairAudit.RepairOutcome = "business_drift_rejected"
			if unverifiedBaseline {
				state.RepairAudit.RepairOutcome = "repair_failed"
			}
			state.Issues = append(state.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "repair",
				Code:        IssueUnresolved,
				Message:     fmt.Sprintf("%s on repair attempt %d", rejectionCode, attempt+1),
				Recoverable: false,
			})
			state.refreshDiagnostics()
			state.RepairAudit.setFinalStatus("failed")
			return state, repairAttempts, repairSuccesses, repairTokens, invoker.NewClassifiedError(
				invoker.ErrorClassContractMismatch, "hunter repair rejected: %s", strings.ToLower(rejectionCode),
			)
		}
		if unverifiedBaseline {
			unverifiedIssue := ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "repair",
				Code:        IssueRepairBaselineUnverified,
				Message:     "repair accepted without comparable business baseline",
				Recoverable: true,
			}
			state.NormalizedRaw = repairedState.NormalizedRaw
			state.Output = repairedState.Output
			state.ValidCandidates = repairedState.ValidCandidates
			state.InvalidCandidates = repairedState.InvalidCandidates
			state.Quarantined = repairedState.Quarantined
			state.ArtifactComplete = repairedState.ArtifactComplete
			state.Taxonomy = repairedState.Taxonomy
			state.ArtifactQualityDegraded = true
			state.Issues = make([]ArtifactIssue, 0, len(preRepairIssues)+len(repairedState.Issues)+1)
			for _, issue := range preRepairIssues {
				if issue.Recoverable {
					state.Issues = append(state.Issues, issue)
				}
			}
			state.Issues = append(state.Issues, repairedState.Issues...)
			state.Issues = append(state.Issues, unverifiedIssue)
			state.RepairAudit.addIssue(unverifiedIssue)
			state.RepairAudit.UnverifiedRepair = true
			state.RepairAudit.RepairOutcome = "unverified_quality_degraded"
			state.refreshDiagnostics()
			state.RepairAudit.setFinalStatus("degraded")
			repairSuccesses++
			return state, repairAttempts, repairSuccesses, repairTokens, nil
		}
		if repairedParseErr == nil && repairedState != nil && repairedState.unresolvedIssueCount() == 0 {
			// The repaired artifact replaces the prior normalized representation;
			// normalization observations from both attempts remain auditable.
			state.NormalizedRaw = repairedState.NormalizedRaw
			state.Output = repairedState.Output
			state.ValidCandidates = repairedState.ValidCandidates
			state.InvalidCandidates = repairedState.InvalidCandidates
			state.Quarantined = repairedState.Quarantined
			state.ArtifactComplete = repairedState.ArtifactComplete
			state.ArtifactQualityDegraded = repairedState.ArtifactQualityDegraded
			state.Taxonomy = repairedState.Taxonomy
			state.Issues = make([]ArtifactIssue, 0, len(preRepairIssues)+len(repairedState.Issues))
			for _, issue := range preRepairIssues {
				if issue.Recoverable {
					state.Issues = append(state.Issues, issue)
				}
			}
			state.Issues = append(state.Issues, repairedState.Issues...)
			repairSuccesses++
			state.refreshDiagnostics()
			state.RepairAudit.RepairOutcome = "verified_success"
			state.RepairAudit.setFinalStatus("success")
			return state, repairAttempts, repairSuccesses, repairTokens, nil
		}
	}

	state.refreshDiagnostics()

	// Completeness tracks lost business candidates only. Unresolved metadata or
	// enrichment issues must not turn into a false coverage/artifact degraded
	// signal when no candidate was dropped.
	if len(state.InvalidCandidates) > 0 {
		if state.Output == nil || len(state.ValidCandidates) == 0 ||
			!models.AppConfig.AllowCandidateSalvage() {
			return state, repairAttempts, repairSuccesses, repairTokens, invoker.NewClassifiedError(
				invoker.ErrorClassContractMismatch,
				"hunter output contract recovery failed: %s", strings.Join(issueMessages(state.Issues), "; "),
			)
		}

		total := len(state.ValidCandidates) + len(state.InvalidCandidates)
		quarantineRatio := float64(len(state.InvalidCandidates)) / float64(total)
		if quarantineRatio > models.AppConfig.MaxQuarantinedCandidateRatio() {
			return state, repairAttempts, repairSuccesses, repairTokens, invoker.NewClassifiedError(
				invoker.ErrorClassContractMismatch,
				"hunter candidate quarantine ratio %.2f exceeds %.2f", quarantineRatio,
				models.AppConfig.MaxQuarantinedCandidateRatio(),
			)
		}

		state.Quarantined = len(state.InvalidCandidates)
		state.ArtifactComplete = false
		state.Output = state.salvagedOutput()
		state.Issues = append(state.Issues, ArtifactIssue{
			Stage: contract.Stage, Schema: contract.SchemaID,
			Code: IssueCandidateQuarantined, Field: "candidate",
			Message:     fmt.Sprintf("%d invalid candidates were quarantined", state.Quarantined),
			Recoverable: true,
		})
		state.refreshDiagnostics()
		state.RepairAudit.setFinalStatus("failed")
		return state, repairAttempts, repairSuccesses, repairTokens, nil
	}

	if state.Output == nil || len(state.ValidCandidates) == 0 {
		return state, repairAttempts, repairSuccesses, repairTokens, invoker.NewClassifiedError(
			invoker.ErrorClassContractMismatch,
			"hunter output contract recovery failed: %s", strings.Join(issueMessages(state.Issues), "; "),
		)
	}

	// No candidate was dropped. Keep the result usable, but explicitly record
	// the residual quality warning for diagnostics.
	state.ArtifactComplete = true
	state.ArtifactQualityDegraded = state.unresolvedIssueCount() > 0
	state.Output = state.salvagedOutput()
	state.refreshDiagnostics()
	status := "success"
	if state.ArtifactQualityDegraded {
		status = "degraded"
	}
	if state.RepairAudit.RepairOutcome == "" {
		if state.RepairAudit.SyntaxRepairs > 0 {
			state.RepairAudit.RepairOutcome = "syntax_normalized"
		} else {
			state.RepairAudit.RepairOutcome = "verified_success"
		}
	}
	state.RepairAudit.setFinalStatus(status)
	return state, repairAttempts, repairSuccesses, repairTokens, nil
}

func issueMessages(issues []ArtifactIssue) []string {
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue.Code == IssueUnresolved {
			messages = append(messages, issue.Message)
		}
	}
	if len(messages) == 0 {
		return []string{"no structured issue"}
	}
	return messages
}

func artifactRepairTimeout() time.Duration {
	return time.Duration(models.AppConfig.SchemaRepairTimeoutSeconds()) * time.Second
}
