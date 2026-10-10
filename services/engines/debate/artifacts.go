package debate

import (
	"bytes"
	"code-shield/models"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"code-shield/services/invoker"
)

var lineRangePattern = regexp.MustCompile(`^\d+(?:-\d+)?$`)

// decodeContractJSON validates the top-level envelope before unmarshalling the
// typed artifact. Missing schema is tolerated only for legacy debate artifacts.
func decodeContractJSON(rawOutput string, contract OutputContract, v interface{}) error {
	cleaned := cleanJSONOutput([]byte(rawOutput))
	if !json.Valid(cleaned) {
		return invoker.NewClassifiedError(invoker.ErrorClassContractMismatch, "AI output is not valid JSON")
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &envelope); err != nil {
		return invoker.WrapClassifiedError(invoker.ErrorClassContractMismatch, err, "AI output top-level is not a JSON object")
	}
	for _, forbidden := range contract.ForbiddenTopLevel {
		if _, exists := envelope[forbidden]; exists {
			return invoker.NewClassifiedError(invoker.ErrorClassContractMismatch, "AI output schema mismatch: forbidden top-level key %q", forbidden)
		}
	}
	for _, required := range contract.RequiredFields {
		if _, exists := envelope[required]; !exists {
			return invoker.NewClassifiedError(invoker.ErrorClassContractMismatch, "AI output schema mismatch: missing top-level key %q", required)
		}
	}
	if schemaRaw, exists := envelope["schema"]; exists {
		var schema string
		if err := json.Unmarshal(schemaRaw, &schema); err != nil {
			return invoker.WrapClassifiedError(invoker.ErrorClassContractMismatch, err, "AI output schema is not a string")
		}
		if schema != "" && schema != contract.SchemaID {
			return invoker.NewClassifiedError(invoker.ErrorClassContractMismatch, "AI output schema mismatch: expected %q, got %q", contract.SchemaID, schema)
		}
	}
	if err := json.Unmarshal(normalizeContractJSON(cleaned), v); err != nil {
		return invoker.WrapClassifiedError(invoker.ErrorClassContractMismatch, err, "failed to decode AI %s output", contract.Stage)
	}
	return nil
}

// normalizeContractJSON performs deterministic schema coercion for known model
// drift. Models sometimes emit trigger_line as a numeric line anchor; the
// pipeline treats it as a source statement, so accept the numeric form here and
// enrich it back to physical source text after contract decoding.
func normalizeContractJSON(data []byte) []byte {
	var root interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return data
	}

	out, err := json.Marshal(normalizeTriggerLineNumbers(root))
	if err != nil {
		return data
	}
	return out
}

func normalizeTriggerLineNumbers(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			if key == "trigger_line" {
				if number, ok := item.(json.Number); ok {
					typed[key] = number.String()
					continue
				}
			}
			typed[key] = normalizeTriggerLineNumbers(item)
		}
		return typed
	case []interface{}:
		for i, item := range typed {
			typed[i] = normalizeTriggerLineNumbers(item)
		}
		return typed
	default:
		return value
	}
}

func validateEnum(field string, value string, allowed []string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is empty", field)
	}
	for _, candidate := range allowed {
		if trimmed == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s %q is not allowed", field, trimmed)
}

func validateRelativeSourcePath(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("file_path is empty")
	}
	if filepath.IsAbs(trimmed) || strings.Contains(trimmed, "\\") {
		return fmt.Errorf("file_path must be a POSIX relative path, got %q", trimmed)
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("file_path escapes the repository root: %q", trimmed)
	}
	return nil
}

// validateHunterOutput enforces the minimum fields required to carry a candidate
// through Challenger and Judge without silently losing its anchor.
func validateHunterOutput(out *HunterOutput, _ []string) error {
	if out == nil {
		return fmt.Errorf("hunter output is nil")
	}
	seen := make(map[string]struct{}, len(out.Candidates))
	for i := range out.Candidates {
		if _, exists := seen[out.Candidates[i].CandidateID]; exists {
			return fmt.Errorf("invalid candidate[%d]: duplicate candidate_id %q", i, out.Candidates[i].CandidateID)
		}
		seen[out.Candidates[i].CandidateID] = struct{}{}
		if err := validateCandidateAnchors(&out.Candidates[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateCandidateAnchors(candidate *HunterCandidate) error {
	if candidate == nil {
		return fmt.Errorf("candidate is nil")
	}
	if strings.TrimSpace(candidate.CandidateID) == "" {
		return fmt.Errorf("invalid candidate: candidate_id is empty")
	}
	if err := validateRelativeSourcePath(candidate.FilePath); err != nil {
		return fmt.Errorf("invalid candidate %s: %w", candidate.CandidateID, err)
	}
	if !lineRangePattern.MatchString(strings.TrimSpace(candidate.LineRange)) {
		return fmt.Errorf("invalid candidate %s: line_range must be \"42\" or \"42-50\", got %q", candidate.CandidateID, candidate.LineRange)
	}
	for name, value := range map[string]string{
		"trigger_line":      candidate.TriggerLine,
		"scope_symbol":      candidate.ScopeSymbol,
		"title":             candidate.Title,
		"code_snippet":      candidate.CodeSnippet,
		"trigger_condition": candidate.TriggerCondition,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("invalid candidate %s: %s is empty", candidate.CandidateID, name)
		}
	}
	return nil
}

func resolveCandidateCategory(candidate *HunterCandidate, allowedCategories []string, taxonomy models.CategoryTaxonomy) error {
	if candidate == nil {
		return fmt.Errorf("candidate is nil")
	}
	if taxonomy.SchemaVersion > 1 && len(taxonomy.Categories) > 0 {
		resolution := ResolveCategory(candidate.CategoryCode, candidate.GetCategory(), taxonomy)
		if resolution.Status == CategoryResolutionNeedsRepair {
			return fmt.Errorf("category is not in taxonomy")
		}
		candidate.CategoryCode = resolution.Code
		candidate.Category = resolution.Label
		return nil
	}
	category := candidate.GetCategory()
	if len(allowedCategories) > 0 {
		if err := validateEnum("category", category, allowedCategories); err != nil {
			return err
		}
	} else if strings.TrimSpace(category) == "" {
		return fmt.Errorf("category is empty")
	}
	return nil
}

// validateChallengerOutput enforces field-level facts for one Challenger batch.
func validateChallengerOutput(out *ChallengerOutput, expectedIDs []string, allowedDimensions []string) error {
	if out == nil {
		return fmt.Errorf("challenger output is nil")
	}
	if len(expectedIDs) > 0 && len(out.DefenseCases) != len(expectedIDs) {
		return fmt.Errorf("challenger output schema mismatch: expected %d defense_cases, got %d", len(expectedIDs), len(out.DefenseCases))
	}

	seen := make(map[string]struct{}, len(out.DefenseCases))
	for i := range out.DefenseCases {
		item := &out.DefenseCases[i]
		if strings.TrimSpace(item.CandidateID) == "" {
			return fmt.Errorf("invalid defense_case[%d]: candidate_id is empty", i)
		}
		if _, exists := seen[item.CandidateID]; exists {
			return fmt.Errorf("invalid defense_case[%d]: duplicate candidate_id %q", i, item.CandidateID)
		}
		seen[item.CandidateID] = struct{}{}
		if err := validateEnum("defense_verdict", item.DefenseVerdict, []string{"DEFENSE_SUCCESSFUL", "DEFENSE_PARTIAL", "CHALLENGE_FAILED"}); err != nil {
			return fmt.Errorf("invalid defense_case %s: %w", item.CandidateID, err)
		}
		if item.DefenseVerdict != "CHALLENGE_FAILED" && len(item.DefenseArguments) == 0 {
			return fmt.Errorf("invalid defense_case %s: %s requires defense_arguments", item.CandidateID, item.DefenseVerdict)
		}
		for j := range item.DefenseArguments {
			argument := &item.DefenseArguments[j]
			if strings.TrimSpace(argument.Dimension) == "" {
				return fmt.Errorf("invalid defense_case %s: defense_arguments[%d].dimension is empty", item.CandidateID, j)
			}
			if len(allowedDimensions) > 0 {
				if err := validateEnum("dimension", argument.Dimension, allowedDimensions); err != nil {
					return fmt.Errorf("invalid defense_case %s: %w", item.CandidateID, err)
				}
			}
			if strings.TrimSpace(argument.Finding) == "" {
				return fmt.Errorf("invalid defense_case %s: defense_arguments[%d].finding is empty", item.CandidateID, j)
			}
			if item.DefenseVerdict != "CHALLENGE_FAILED" && len(argument.Evidence) == 0 {
				return fmt.Errorf("invalid defense_case %s: defense_arguments[%d].evidence is empty", item.CandidateID, j)
			}
			for k := range argument.Evidence {
				if err := validateJudgeEvidenceRef(&argument.Evidence[k]); err != nil {
					return fmt.Errorf("invalid defense_case %s defense_arguments[%d] evidence[%d]: %w", item.CandidateID, j, k, err)
				}
			}
		}
		if item.DefenseVerdict == "CHALLENGE_FAILED" && strings.TrimSpace(item.MitigatingFactors) == "" {
			item.MitigatingFactors = "无有效缓解因素；请 Judge 基于源码独立裁决。"
		} else if strings.TrimSpace(item.MitigatingFactors) == "" {
			return fmt.Errorf("invalid defense_case %s: mitigating_factors is empty", item.CandidateID)
		}
	}

	if len(expectedIDs) > 0 {
		for _, expected := range expectedIDs {
			if _, exists := seen[expected]; !exists {
				return fmt.Errorf("challenger output schema mismatch: missing candidate_id %q", expected)
			}
		}
	}
	return nil
}

// validateJudgeOutput enforces the terminal verdict contract for one batch.
func validateJudgeOutput(out *JudgeOutput, expectedIDs []string, allowedCategories []string, taxonomy models.CategoryTaxonomy) error {
	if out == nil {
		return fmt.Errorf("judge output is nil")
	}
	if len(expectedIDs) > 0 && len(out.FinalVerdicts) != len(expectedIDs) {
		return fmt.Errorf("judge output schema mismatch: expected %d final_verdicts, got %d", len(expectedIDs), len(out.FinalVerdicts))
	}

	seen := make(map[string]struct{}, len(out.FinalVerdicts))
	for i := range out.FinalVerdicts {
		item := &out.FinalVerdicts[i]
		if strings.TrimSpace(item.CandidateID) == "" {
			return fmt.Errorf("invalid final_verdict[%d]: candidate_id is empty", i)
		}
		if _, exists := seen[item.CandidateID]; exists {
			return fmt.Errorf("invalid final_verdict[%d]: duplicate candidate_id %q", i, item.CandidateID)
		}
		seen[item.CandidateID] = struct{}{}
		if err := validateEnum("verdict", item.Verdict, []string{"CONFIRMED", "REJECTED", "CONDITIONAL"}); err != nil {
			return fmt.Errorf("invalid final_verdict %s: %w", item.CandidateID, err)
		}
		if err := validateEnum("severity_preliminary", item.SeverityPreliminary, []string{"致命", "严重", "一般", "建议"}); err != nil {
			return fmt.Errorf("invalid final_verdict %s: %w", item.CandidateID, err)
		}
		if taxonomy.SchemaVersion > 1 && len(taxonomy.Categories) > 0 {
			resolution := ResolveCategory(item.CategoryCode, item.Category, taxonomy)
			if resolution.Status == CategoryResolutionNeedsRepair {
				item.CategoryCode = ""
				item.ReviewRequired = true
			} else {
				item.CategoryCode = resolution.Code
				item.Category = resolution.Label
				item.ReviewRequired = false
			}
		} else if len(allowedCategories) > 0 {
			if err := validateEnum("category", item.Category, allowedCategories); err != nil {
				item.CategoryCode = ""
				item.ReviewRequired = true
			}
		} else if strings.TrimSpace(item.Category) == "" {
			item.CategoryCode = ""
			item.ReviewRequired = true
		}
		if err := validateRelativeSourcePath(item.FilePath); err != nil {
			return fmt.Errorf("invalid final_verdict %s: %w", item.CandidateID, err)
		}
		modelLine := item.LineRange
		if strings.TrimSpace(modelLine) == "" {
			modelLine = item.LineNumber
		}
		if !lineRangePattern.MatchString(strings.TrimSpace(modelLine)) {
			normalized, ok := normalizeLineRange(modelLine)
			if !ok {
				return fmt.Errorf("invalid final_verdict %s: line_range must be \"42\" or \"42-50\", got %q", item.CandidateID, modelLine)
			}
			item.LineRange = normalized
			modelLine = normalized
		}
		for name, value := range map[string]string{
			"trigger_line":        item.TriggerLine,
			"scope_symbol":        item.ScopeSymbol,
			"title":               item.Title,
			"judgement_rationale": item.JudgementRationale,
			"code_snippet":        item.CodeSnippet,
			"suggestion":          item.Suggestion,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("invalid final_verdict %s: %s is empty", item.CandidateID, name)
			}
		}
		if len(item.Evidence) == 0 {
			return fmt.Errorf("invalid final_verdict %s: evidence is empty", item.CandidateID)
		}
		for j := range item.Evidence {
			if err := validateJudgeEvidenceRef(&item.Evidence[j]); err != nil {
				return fmt.Errorf("invalid final_verdict %s evidence[%d]: %w", item.CandidateID, j, err)
			}
		}
	}

	if len(expectedIDs) > 0 {
		for _, expected := range expectedIDs {
			if _, exists := seen[expected]; !exists {
				return fmt.Errorf("judge output schema mismatch: missing candidate_id %q", expected)
			}
		}
	}
	return nil
}

func validateJudgeEvidenceRef(ref *JudgeEvidenceRef) error {
	if ref == nil {
		return fmt.Errorf("evidence is nil")
	}
	switch ref.Kind {
	case JudgeEvidenceTarget, JudgeEvidenceRelated, JudgeEvidenceCaller, JudgeEvidenceDefinition, JudgeEvidenceMacro:
		if err := validateRelativeSourcePath(ref.Path); err != nil {
			return err
		}
		if !lineRangePattern.MatchString(strings.TrimSpace(ref.LineRange)) {
			normalized, ok := normalizeLineRange(ref.LineRange)
			if !ok {
				return fmt.Errorf("line_range must be \"42\" or \"42-50\", got %q", ref.LineRange)
			}
			ref.LineRange = normalized
		}
		if strings.TrimSpace(ref.Snippet) == "" {
			return fmt.Errorf("snippet is empty")
		}
	case JudgeEvidenceAbsence:
		if strings.TrimSpace(ref.ProbeID) == "" {
			return fmt.Errorf("probe_id is empty")
		}
	default:
		return fmt.Errorf("evidence kind %q is not allowed", ref.Kind)
	}
	if strings.TrimSpace(ref.Reason) == "" {
		return fmt.Errorf("reason is empty")
	}
	return nil
}

// normalizeLineRange accepts one interval from a model-provided list (for
// example "44-53, 1110-1130") and keeps only the primary interval. A single
// contiguous location is required for stable source anchors and fingerprints.
func normalizeLineRange(value string) (string, bool) {
	match := lineRangePattern.FindString(value)
	return match, match != ""
}
