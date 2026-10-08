package debate

import (
	"code-shield/models"
	"code-shield/services/dispatcher"
	"code-shield/services/invoker"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type CategoryRepairRequest struct {
	CandidateID      string `json:"candidate_id"`
	Title            string `json:"title,omitempty"`
	CodeSnippet      string `json:"code_snippet,omitempty"`
	TriggerLine      string `json:"trigger_line,omitempty"`
	TriggerCondition string `json:"trigger_condition,omitempty"`
	RawCode          string `json:"raw_category_code,omitempty"`
	RawLabel         string `json:"raw_category,omitempty"`
}

type CategoryRepairResponse struct {
	CandidateID             string `json:"candidate_id"`
	CategoryCode            string `json:"category_code"`
	ClassificationRationale string `json:"classification_rationale"`
	NeedsReview             bool   `json:"needs_review"`
}

type categoryRepairEnvelope struct {
	Repairs []json.RawMessage `json:"repairs"`
}

func parseCategoryRepairOutput(raw string, requests []CategoryRepairRequest, taxonomy models.CategoryTaxonomy) ([]CategoryRepairResponse, []ArtifactIssue, error) {
	var envelope categoryRepairEnvelope
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, nil, fmt.Errorf("decode category repair output: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, nil, fmt.Errorf("decode category repair output: unexpected trailing data")
	}
	if len(envelope.Repairs) != len(requests) {
		return nil, nil, fmt.Errorf("category repair output mismatch: expected %d repairs, got %d", len(requests), len(envelope.Repairs))
	}
	expected := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		expected[request.CandidateID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(envelope.Repairs))
	repairs := make([]CategoryRepairResponse, 0, len(envelope.Repairs))
	issues := make([]ArtifactIssue, 0)
	for index, rawRepair := range envelope.Repairs {
		var repair CategoryRepairResponse
		if err := json.Unmarshal(rawRepair, &repair); err != nil {
			return nil, nil, fmt.Errorf("decode category repair output: %w", err)
		}
		if _, exists := seen[repair.CandidateID]; exists {
			return nil, nil, fmt.Errorf("duplicate repaired candidate_id %q", repair.CandidateID)
		}
		seen[repair.CandidateID] = struct{}{}
		if _, exists := expected[repair.CandidateID]; !exists {
			return nil, nil, fmt.Errorf("unexpected repaired candidate_id %q", repair.CandidateID)
		}
		resolution := ResolveCategory(repair.CategoryCode, "", taxonomy)
		if resolution.Status != CategoryResolutionCodeAuthority {
			return nil, nil, fmt.Errorf("invalid repaired category_code %q", repair.CategoryCode)
		}
		if repair.ClassificationRationale == "" {
			return nil, nil, fmt.Errorf("empty classification_rationale for candidate %q", repair.CandidateID)
		}
		if repair.NeedsReview && repair.CategoryCode == "" {
			return nil, nil, fmt.Errorf("review repair requires category_code")
		}
		repairs = append(repairs, repair)

		var rawFields map[string]json.RawMessage
		if err := json.Unmarshal(rawRepair, &rawFields); err != nil {
			return nil, nil, fmt.Errorf("decode category repair output fields: %w", err)
		}
		extraFields := make([]string, 0, len(rawFields))
		for field := range rawFields {
			switch field {
			case "candidate_id", "category_code", "classification_rationale", "needs_review":
				continue
			default:
				extraFields = append(extraFields, field)
			}
		}
		if len(extraFields) == 0 {
			continue
		}
		sort.Strings(extraFields)
		issues = append(issues, ArtifactIssue{
			Stage:       "category_repair",
			JSONPath:    fmt.Sprintf("$.repairs[%d]", index),
			Code:        IssueCategoryExtraFieldIgnored,
			CandidateID: repair.CandidateID,
			Field:       strings.Join(extraFields, ","),
			Message: fmt.Sprintf(
				"category repair response ignored non-category fields %s for candidate %q",
				strings.Join(extraFields, ", "), repair.CandidateID,
			),
			Recoverable: true,
			RawArtifact: raw,
		})
	}
	return repairs, issues, nil
}

func executeCategoryRepairs(
	ctx context.Context,
	repoRoot string,
	requests []CategoryRepairRequest,
	taxonomy models.CategoryTaxonomy,
	contract OutputContract,
	stage string,
) ([]CategoryRepairResponse, int64, bool, error) {
	if !models.AppConfig.CategoryRepairEnabled() {
		return nil, 0, false, fmt.Errorf("category repair is disabled")
	}
	maxCandidates := models.AppConfig.CategoryRepairMaxCandidates()
	if len(requests) > maxCandidates {
		return nil, 0, false, fmt.Errorf("category repair candidate count %d exceeds limit %d", len(requests), maxCandidates)
	}
	backend := models.AppConfig.CategoryRepairResource()
	rawInv, ok := invoker.GetRawInvoker(backend)
	if !ok || rawInv == nil {
		return nil, 0, false, fmt.Errorf("category repair backend %q is unavailable", backend)
	}
	outputPath, cleanup, err := tempArtifactPath("category-repair", ".json")
	if err != nil {
		return nil, 0, false, err
	}
	defer cleanup()

	prompt, promptErr := buildCategoryRepairPrompt(requests, contract)
	if promptErr != nil {
		return nil, 0, false, promptErr
	}
	temperature := 0.0
	request := invoker.AIRequest{
		ParentContext:  ctx,
		WorkDir:        repoRoot,
		PromptMsg:      prompt,
		OutputPath:     outputPath,
		TimeoutSeconds: models.AppConfig.CategoryRepairTimeoutSeconds(),
		Temperature:    &temperature,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
			Stage:    stage,
			SubTask:  "定向修复受控分类短码",
			TierName: "system_tool",
		},
	}
	if invokeErr := dispatcher.WrapInvoker(rawInv).Invoke(request); invokeErr != nil {
		return nil, 0, true, invokeErr
	}
	repaired, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		return nil, 0, true, fmt.Errorf("read category repair output: %w", readErr)
	}
	repairs, _, parseErr := parseCategoryRepairOutput(string(repaired), requests, taxonomy)
	if parseErr != nil {
		return nil, int64(len(prompt) + len(repaired)), true, parseErr
	}
	return repairs, int64((len(prompt) + len(repaired)) / 4), true, nil
}

func executeCategoryRepairsWithAudit(
	ctx context.Context,
	repoRoot string,
	requests []CategoryRepairRequest,
	taxonomy models.CategoryTaxonomy,
	contract OutputContract,
	stage string,
) ([]CategoryRepairResponse, []ArtifactIssue, int64, string, bool, error) {
	if !models.AppConfig.CategoryRepairEnabled() {
		return nil, nil, 0, "", false, fmt.Errorf("category repair is disabled")
	}
	maxCandidates := models.AppConfig.CategoryRepairMaxCandidates()
	if len(requests) > maxCandidates {
		return nil, nil, 0, "", false, fmt.Errorf("category repair candidate count %d exceeds limit %d", len(requests), maxCandidates)
	}
	backend := models.AppConfig.CategoryRepairResource()
	rawInv, ok := invoker.GetRawInvoker(backend)
	if !ok || rawInv == nil {
		return nil, nil, 0, "", false, fmt.Errorf("category repair backend %q is unavailable", backend)
	}
	outputPath, cleanup, err := tempArtifactPath("category-repair", ".json")
	if err != nil {
		return nil, nil, 0, "", false, err
	}
	defer cleanup()

	prompt, promptErr := buildCategoryRepairPrompt(requests, contract)
	if promptErr != nil {
		return nil, nil, 0, "", false, promptErr
	}
	temperature := 0.0
	request := invoker.AIRequest{
		ParentContext:  ctx,
		WorkDir:        repoRoot,
		PromptMsg:      prompt,
		OutputPath:     outputPath,
		TimeoutSeconds: models.AppConfig.CategoryRepairTimeoutSeconds(),
		Temperature:    &temperature,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
			Stage:    stage,
			SubTask:  "定向修复受控分类短码",
			TierName: "system_tool",
		},
	}
	if invokeErr := dispatcher.WrapInvoker(rawInv).Invoke(request); invokeErr != nil {
		return nil, nil, 0, "", true, invokeErr
	}
	repaired, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		return nil, nil, 0, "", true, fmt.Errorf("read category repair output: %w", readErr)
	}
	rawArtifact := string(repaired)
	repairs, issues, parseErr := parseCategoryRepairOutput(string(repaired), requests, taxonomy)
	if parseErr != nil {
		for i := range issues {
			issues[i].RawArtifact = rawArtifact
		}
		return nil, issues, int64(len(prompt) + len(repaired)), rawArtifact, true, parseErr
	}
	return repairs, issues, int64((len(prompt) + len(repaired)) / 4), rawArtifact, true, nil
}

func applyCategoryRepairs(state *hunterArtifactState, repairs []CategoryRepairResponse, taxonomy models.CategoryTaxonomy) error {
	if state == nil || state.Output == nil {
		return fmt.Errorf("category repair state is unavailable")
	}
	byID := make(map[string]CategoryRepairResponse, len(repairs))
	for _, repair := range repairs {
		byID[repair.CandidateID] = repair
	}
	for i := range state.Output.Candidates {
		candidate := &state.Output.Candidates[i]
		if !candidate.ReviewRequired {
			continue
		}
		repair, ok := byID[candidate.CandidateID]
		if !ok {
			continue
		}
		resolution := ResolveCategory(repair.CategoryCode, "", taxonomy)
		if resolution.Status == CategoryResolutionNeedsRepair {
			return fmt.Errorf("category repair produced invalid code %q", repair.CategoryCode)
		}
		candidate.CategoryCode = resolution.Code
		candidate.Category = resolution.Label
		candidate.ReviewRequired = repair.NeedsReview
	}
	for _, candidate := range state.Output.Candidates {
		if candidate.ReviewRequired {
			state.ArtifactQualityDegraded = true
			break
		}
	}
	return nil
}

func categoryRepairRequests(state *hunterArtifactState) []CategoryRepairRequest {
	if state == nil || state.Output == nil {
		return nil
	}
	requests := make([]CategoryRepairRequest, 0)
	for _, candidate := range state.Output.Candidates {
		if !candidate.ReviewRequired {
			continue
		}
		requests = append(requests, CategoryRepairRequest{
			CandidateID:      candidate.CandidateID,
			Title:            candidate.Title,
			CodeSnippet:      candidate.CodeSnippet,
			TriggerLine:      candidate.TriggerLine,
			TriggerCondition: candidate.TriggerCondition,
			RawCode:          candidate.CategoryCode,
			RawLabel:         candidate.GetCategory(),
		})
	}
	return requests
}

func repairHunterCategories(ctx context.Context, repoRoot string, state *hunterArtifactState, contract OutputContract) (int, int, int64, error) {
	requests := categoryRepairRequests(state)
	if len(requests) == 0 {
		return 0, 0, 0, nil
	}
	repairs, auditIssues, repairTokens, rawArtifact, invoked, repairErr := executeCategoryRepairsWithAudit(
		ctx, repoRoot, requests, stateTaxonomy(state), contract, "系统工具: Category Repair",
	)
	if repairErr != nil {
		if invoked {
			state.RepairAudit.addLLMRepair(rawArtifact, repairTokens, false, false, false, repairErr)
			addHunterRepairMetrics(state.Output, 1, 0)
		}
		return 1, 0, repairTokens, repairErr
	}
	if applyErr := applyCategoryRepairs(state, repairs, stateTaxonomy(state)); applyErr != nil {
		state.RepairAudit.addLLMRepair(rawArtifact, repairTokens, false, false, false, applyErr)
		return 1, 0, repairTokens, applyErr
	}
	for i := range auditIssues {
		auditIssues[i].Schema = contract.SchemaID
	}
	state.Issues = append(state.Issues, auditIssues...)
	state.Output.CategoryMetrics.ExtraFieldsIgnored += len(auditIssues)
	state.RepairAudit.addLLMRepair(rawArtifact, repairTokens, true, false, false, nil)
	for _, issue := range auditIssues {
		state.RepairAudit.addIssue(issue)
	}
	addHunterRepairMetrics(state.Output, 1, 1)
	return 1, 1, repairTokens, nil
}

func repairJudgeCategories(
	ctx context.Context,
	repoRoot string,
	output *JudgeOutput,
	hunterCandidates []HunterCandidate,
	taxonomy models.CategoryTaxonomy,
	contract OutputContract,
) (int64, int, error) {
	if output == nil {
		return 0, 0, fmt.Errorf("judge output is nil")
	}
	hunterByID := make(map[string]HunterCandidate, len(hunterCandidates))
	for _, candidate := range hunterCandidates {
		hunterByID[candidate.CandidateID] = candidate
	}
	requests := make([]CategoryRepairRequest, 0)
	for i := range output.FinalVerdicts {
		verdict := &output.FinalVerdicts[i]
		if !verdict.ReviewRequired || verdict.Verdict == "REJECTED" {
			continue
		}
		hunter := hunterByID[verdict.CandidateID]
		requests = append(requests, CategoryRepairRequest{
			CandidateID:      verdict.CandidateID,
			Title:            verdict.Title,
			CodeSnippet:      verdict.CodeSnippet,
			TriggerLine:      verdict.TriggerLine,
			TriggerCondition: hunter.TriggerCondition,
			RawCode:          verdict.CategoryCode,
			RawLabel:         verdict.Category,
		})
	}
	if len(requests) == 0 {
		return 0, 0, nil
	}
	repairs, auditIssues, repairTokens, _, invoked, repairErr := executeCategoryRepairsWithAudit(
		ctx, repoRoot, requests, taxonomy, contract, "系统工具: Judge Category Repair",
	)
	if repairErr != nil {
		if invoked {
			addJudgeRepairMetrics(output, 1, 0)
		}
		return repairTokens, 1, repairErr
	}
	if applyErr := applyJudgeCategoryRepairs(output, repairs, taxonomy); applyErr != nil {
		return repairTokens, 1, applyErr
	}
	for i := range auditIssues {
		auditIssues[i].Schema = contract.SchemaID
	}
	if len(auditIssues) > 0 {
		output.CategoryMetrics.ExtraFieldsIgnored += len(auditIssues)
	}
	addJudgeRepairMetrics(output, 1, 1)
	return repairTokens, 1, nil
}

func applyJudgeCategoryRepairs(output *JudgeOutput, repairs []CategoryRepairResponse, taxonomy models.CategoryTaxonomy) error {
	if output == nil {
		return fmt.Errorf("judge output is nil")
	}
	byID := make(map[string]CategoryRepairResponse, len(repairs))
	for _, repair := range repairs {
		byID[repair.CandidateID] = repair
	}
	for i := range output.FinalVerdicts {
		verdict := &output.FinalVerdicts[i]
		repair, ok := byID[verdict.CandidateID]
		if !ok {
			continue
		}
		resolution := ResolveCategory(repair.CategoryCode, "", taxonomy)
		if resolution.Status == CategoryResolutionNeedsRepair {
			return fmt.Errorf("judge category repair produced invalid code %q", repair.CategoryCode)
		}
		verdict.CategoryCode = resolution.Code
		verdict.Category = resolution.Label
		verdict.ClassificationRationale = repair.ClassificationRationale
		verdict.ReviewRequired = repair.NeedsReview
	}
	return nil
}

func stateTaxonomy(state *hunterArtifactState) models.CategoryTaxonomy {
	if state == nil {
		return models.CategoryTaxonomy{}
	}
	return state.Taxonomy
}

func categoryRepairAuditMessages(state *hunterArtifactState) []string {
	if state == nil {
		return nil
	}
	messages := make([]string, 0)
	for _, issue := range state.Issues {
		if issue.Code == IssueCategoryExtraFieldIgnored {
			messages = append(messages, issue.Message)
		}
	}
	return messages
}

func buildCategoryRepairPrompt(requests []CategoryRepairRequest, contract OutputContract) (string, error) {
	if len(requests) == 0 {
		return "", fmt.Errorf("category repair requests are empty")
	}
	var sb strings.Builder
	sb.WriteString("你是受控分类修复器。只修复 category_code 和 classification_rationale，不得修改 title、file、line、snippet 或 trigger。\n\n")
	sb.WriteString("## Candidates\n```json\n")
	raw, err := json.MarshalIndent(requests, "", "  ")
	if err != nil {
		return "", err
	}
	sb.Write(raw)
	sb.WriteString("\n```\n\n")
	sb.WriteString("## Output Contract\n")
	sb.WriteString(`{"repairs":[{"candidate_id":"H-001","category_code":"CODE","classification_rationale":"证据与规则","needs_review":false}]}`)
	sb.WriteString("\n\nUse one exact category_code from the active taxonomy. Output JSON only.\n")
	sb.WriteString(RenderOutputContract(contract))
	return sb.String(), nil
}
