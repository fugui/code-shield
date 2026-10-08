package assessment

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func ExtractJSONObject(raw []byte) ([]byte, error) {
	content := strings.TrimSpace(strings.TrimPrefix(string(raw), "\xef\xbb\xbf"))
	if content == "" {
		return nil, fmt.Errorf("artifact is empty")
	}
	content = stripJSONFence(content)
	start := strings.Index(content, "{")
	if start < 0 {
		return nil, fmt.Errorf("artifact does not contain a JSON object")
	}
	object := content[start:]
	end, ok := balancedObjectEnd(object)
	if !ok {
		object = string(RepairJSONEscapes([]byte(object)))
		end, ok = balancedObjectEnd(object)
		if !ok {
			return nil, fmt.Errorf("artifact does not contain a balanced JSON object")
		}
	}
	content = object[:end+1]
	content = removeTrailingCommas(content)
	if !utf8.ValidString(content) {
		return nil, fmt.Errorf("artifact is not valid UTF-8")
	}
	if !json.Valid([]byte(content)) {
		escaped := RepairJSONEscapes([]byte(content))
		if json.Valid(escaped) {
			content = string(escaped)
		}
	}
	return []byte(content), nil
}

func stripJSONFence(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func balancedObjectEnd(content string) (int, bool) {
	inString, escaped, depth := false, false, 0
	for index, char := range content {
		if escaped {
			escaped = false
			continue
		}
		switch {
		case inString && char == '\\':
			escaped = true
		case char == '"':
			inString = !inString
		case inString:
		case char == '{':
			depth++
		case char == '}':
			depth--
			if depth == 0 {
				return index, true
			}
		}
	}
	return 0, false
}

func removeTrailingCommas(content string) string {
	var out strings.Builder
	out.Grow(len(content))
	inString, escaped := false, false
	for index, char := range content {
		if escaped {
			escaped = false
			out.WriteRune(char)
			continue
		}
		switch {
		case inString && char == '\\':
			escaped = true
			out.WriteRune(char)
		case char == '"':
			inString = !inString
			out.WriteRune(char)
		case char == ',' && !inString:
			next := strings.IndexFunc(content[index+1:], func(r rune) bool { return r != ' ' && r != '\t' && r != '\n' && r != '\r' })
			if next >= 0 && (content[index+1+next] == '}' || content[index+1+next] == ']') {
				continue
			}
			out.WriteRune(char)
		default:
			out.WriteRune(char)
		}
	}
	return out.String()
}

func NormalizeArtifactLocally(raw []byte, contract ArtifactContract, session *PromptRefSession) ([]byte, bool) {
	cleaned, err := ExtractJSONObject(raw)
	if err != nil {
		return raw, false
	}
	changed := false
	if !json.Valid(cleaned) {
		repaired := RepairJSONEscapes(cleaned)
		if !json.Valid(repaired) {
			return raw, false
		}
		cleaned = repaired
		changed = true
	}
	var generic map[string]any
	if err := json.Unmarshal(cleaned, &generic); err != nil {
		return raw, false
	}
	switch schemaValue, _ := generic["schema"].(string); schemaValue {
	case contract.SchemaID:
	case "code-shield.unit-assessments.v1":
		generic["schema"] = contract.SchemaID
		changed = true
	default:
		return raw, false
	}
	assessments, ok := generic["assessments"].([]any)
	if !ok {
		return raw, false
	}
	validRefs := make(map[string]bool, len(session.Refs))
	for _, ref := range session.Refs {
		validRefs[ref.Ref] = true
	}
	for index, rawItem := range assessments {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return raw, false
		}
		if rawEvidence, exists := item["evidence"]; exists {
			if evidence, isString := rawEvidence.(string); isString {
				item["evidence"] = map[string]any{"raw": evidence}
				changed = true
			}
		}
		if rawIssues, exists := item["issues"].([]any); exists {
			for issueIndex, rawIssue := range rawIssues {
				issue, isMap := rawIssue.(map[string]any)
				if !isMap {
					continue
				}
				if rawEvidence, exists := issue["evidence"]; exists {
					if evidence, isString := rawEvidence.(string); isString {
						issue["evidence"] = map[string]any{"raw": evidence}
						rawIssues[issueIndex] = issue
						changed = true
					}
				}
			}
			item["issues"] = rawIssues
		}
		unitRef, _ := item["unit_ref"].(string)
		if strings.TrimSpace(unitRef) == "" && len(session.Refs) == 1 && validRefs[session.Refs[0].Ref] {
			item["unit_ref"] = session.Refs[0].Ref
			changed = true
		}
		assessments[index] = item
	}
	if !changed {
		return cleaned, false
	}
	normalized, err := json.Marshal(generic)
	if err != nil {
		return raw, false
	}
	return normalized, true
}

func ArtifactUnitsUnchanged(before, after *AssessmentArtifact) (bool, string) {
	if before == nil {
		return true, ""
	}
	if len(before.Assessments) != len(after.Assessments) {
		return false, "*"
	}
	beforeFingerprints := ArtifactBusinessFingerprints(*before)
	afterFingerprints := ArtifactBusinessFingerprints(*after)
	for index := range beforeFingerprints {
		if beforeFingerprints[index] != afterFingerprints[index] {
			return false, before.Assessments[index].UnitRef
		}
	}
	return true, ""
}

func ArtifactBusinessFingerprints(artifact AssessmentArtifact) []string {
	fingerprints := make([]string, 0, len(artifact.Assessments))
	for _, item := range artifact.Assessments {
		item.UnitRef = ""
		item.PrimaryUnitID = ""
		if item.Evidence == nil {
			item.Evidence = map[string]any{}
		}
		if item.Domain == nil {
			item.Domain = map[string]json.RawMessage{}
		}
		if item.Issues == nil {
			item.Issues = []AssessmentIssue{}
		}
		encoded, _ := json.Marshal(item)
		fingerprints = append(fingerprints, string(encoded))
	}
	return fingerprints
}
