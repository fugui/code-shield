package assessment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type ArtifactValidationResult struct {
	CleanedJSON   []byte
	LocalRepaired bool
	Artifact      AssessmentArtifact
	Issues        []ArtifactIssue
}

func ValidateArtifact(raw []byte, contract ArtifactContract, plan PlanView, validator ArtifactValidator) (ArtifactValidationResult, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return ArtifactValidationResult{}, ErrArtifactEmpty
	}
	cleaned, extractErr := ExtractJSONObject(raw)
	result := ArtifactValidationResult{CleanedJSON: cleaned}
	if extractErr != nil {
		result.CleanedJSON = raw
		result.Issues = append(result.Issues, ArtifactIssue{
			Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "artifact",
			Code: "JSON_PARSE_FAILED", Message: extractErr.Error(), Recoverable: true,
		})
		return result, ErrArtifactInvalid
	}
	result.LocalRepaired = !bytes.Equal(raw, cleaned)

	var generic any
	if err := json.Unmarshal(cleaned, &generic); err != nil {
		result.Issues = append(result.Issues, ArtifactIssue{
			Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "artifact",
			Code: "JSON_PARSE_FAILED", Message: err.Error(), Recoverable: true,
		})
		return result, ErrArtifactInvalid
	}
	result.Issues = append(result.Issues, validateJSONSchema(generic, "$", contract.JSONSchema, contract)...)
	if len(result.Issues) > 0 {
		return result, ErrArtifactInvalid
	}

	var artifact AssessmentArtifact
	if err := json.Unmarshal(cleaned, &artifact); err != nil {
		result.Issues = append(result.Issues, ArtifactIssue{
			Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "artifact",
			Code: "TYPED_DECODE_FAILED", Message: err.Error(), Recoverable: true,
		})
		return result, ErrArtifactInvalid
	}
	for index := range artifact.Assessments {
		unitID, err := plan.PromptRefs.UnitIDForRef(artifact.Assessments[index].UnitRef)
		if err == nil {
			artifact.Assessments[index].PrimaryUnitID = unitID
		}
	}
	result.Artifact = artifact
	result.Issues = append(result.Issues, validateArtifactSemantics(artifact, plan, contract)...)
	if len(result.Issues) == 0 {
		profileResult, profileErr := validator.Validate(plan, artifact)
		if profileErr != nil {
			result.Artifact = profileResult.Artifact
			result.Issues = append(result.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$.assessments", Field: "assessment",
				Code: "PROFILE_SEMANTIC_INVALID", Message: profileErr.Error(), Recoverable: true,
			})
		}
	}
	if len(result.Issues) == 0 {
		encoded, encodeErr := json.Marshal(result.Artifact)
		if encodeErr == nil {
			result.CleanedJSON = encoded
		}
	}
	if len(result.Issues) > 0 {
		return result, ErrArtifactInvalid
	}
	return result, nil
}

func validateArtifactSemantics(artifact AssessmentArtifact, plan PlanView, contract ArtifactContract) []ArtifactIssue {
	issues := make([]ArtifactIssue, 0)
	if artifact.Schema != contract.SchemaID {
		issues = append(issues, issue(contract, "$.schema", "schema", ErrorContractSchemaMismatch,
			fmt.Sprintf("expected schema %q, got %q", contract.SchemaID, artifact.Schema)))
	}
	seen := make(map[string]int, len(plan.Units))
	for index, item := range artifact.Assessments {
		path := fmt.Sprintf("$.assessments[%d]", index)
		if strings.TrimSpace(item.UnitRef) == "" {
			issues = append(issues, issue(contract, path+".unit_ref", "unit_ref", ErrorUnitRefMissing, "unit_ref is empty"))
			continue
		}
		unitID, err := plan.PromptRefs.UnitIDForRef(item.UnitRef)
		if err != nil {
			issues = append(issues, issue(contract, path+".unit_ref", "unit_ref", ErrorUnitRefUnknown,
				fmt.Sprintf("unit_ref %q is unknown", item.UnitRef)))
			continue
		}
		seen[unitID]++
		if seen[unitID] > 1 {
			issues = append(issues, issue(contract, path+".unit_ref", "unit_ref", ErrorUnitRefDuplicate,
				fmt.Sprintf("unit_ref %q maps to unit %q more than once", item.UnitRef, unitID)))
		}
		if err := item.Outcome.Validate(); err != nil {
			issues = append(issues, issue(contract, path+".outcome", "outcome", ErrorOutcomeInvalid, err.Error()))
		}
		for issueIndex := range item.Issues {
			assessmentIssue := item.Issues[issueIndex]
			issuePath := fmt.Sprintf("%s.issues[%d]", path, issueIndex)
			if strings.TrimSpace(assessmentIssue.Category) == "" {
				issues = append(issues, issue(contract, issuePath+".category", "category", ErrorIssueCategoryNotAllowed, "category is empty"))
			} else if !containsString(plan.AllowedCategories, assessmentIssue.Category) {
				issues = append(issues, issue(contract, issuePath+".category", "category", ErrorIssueCategoryNotAllowed,
					fmt.Sprintf("category %q is not allowed", assessmentIssue.Category)))
			}
			if strings.TrimSpace(assessmentIssue.Detail) == "" {
				issues = append(issues, issue(contract, issuePath+".detail", "detail", ErrorIssueDetailMissing, "detail is empty"))
			}
		}
	}
	for _, unit := range plan.Units {
		if seen[unit.ID] == 0 {
			issues = append(issues, issue(contract, "$.assessments", "assessments", "ASSESSMENT_UNIT_MISSING",
				fmt.Sprintf("unit %q is missing", unit.ID)))
		}
	}
	return issues
}

func issue(contract ArtifactContract, jsonPath, field, code, message string) ArtifactIssue {
	return ArtifactIssue{Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: jsonPath, Field: field, Code: code, Message: message, Recoverable: true}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateJSONSchema(value any, path string, schema map[string]any, contract ArtifactContract) []ArtifactIssue {
	var issues []ArtifactIssue
	if constValue, ok := schema["const"]; ok && value != constValue {
		issues = append(issues, issue(contract, path, pathField(path), "CONST_INVALID",
			fmt.Sprintf("expected %v, got %v", constValue, value)))
	}
	if _, ok := schema["not"]; ok && value != nil {
		issues = append(issues, issue(contract, path, pathField(path), "FORBIDDEN_FIELD", "field is not allowed"))
	}
	if enum, ok := schema["enum"].([]string); ok {
		if !valueMatchesEnum(value, enum) {
			issues = append(issues, issue(contract, path, pathField(path), "ENUM_INVALID", fmt.Sprintf("value %v is not allowed", value)))
		}
	}
	if expected, ok := schema["type"].(string); ok && !jsonTypeMatches(value, expected) {
		issues = append(issues, issue(contract, path, pathField(path), "TYPE_INVALID", fmt.Sprintf("expected %s", expected)))
		return issues
	}
	if valueMap, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		if properties != nil {
			for _, required := range stringSlice(schema["required"]) {
				if _, exists := valueMap[required]; !exists {
					issues = append(issues, issue(contract, path+"."+required, required, "REQUIRED_FIELD_MISSING", "required field is missing"))
				}
			}
			for key, child := range valueMap {
				childPath := path + "." + key
				childSchema, exists := properties[key].(map[string]any)
				if !exists {
					issues = append(issues, issue(contract, childPath, key, "UNKNOWN_FIELD", "field is not allowed"))
					continue
				}
				issues = append(issues, validateJSONSchema(child, childPath, childSchema, contract)...)
			}
		}
	}
	if valueSlice, ok := value.([]any); ok {
		items, _ := schema["items"].(map[string]any)
		for index, child := range valueSlice {
			issues = append(issues, validateJSONSchema(child, fmt.Sprintf("%s[%d]", path, index), items, contract)...)
		}
	}
	return issues
}

func valueMatchesEnum(value any, allowed []string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	return containsString(allowed, text)
}

func stringSlice(value any) []string {
	items, _ := value.([]string)
	return items
}

func jsonTypeMatches(value any, expected string) bool {
	switch expected {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	default:
		return false
	}
}

func pathField(path string) string {
	if index := strings.LastIndex(path, "."); index >= 0 {
		return path[index+1:]
	}
	return path
}
