package debate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRepairJSONTrailingCommas(t *testing.T) {
	raw := []byte(`{
  "schema": "code-shield.candidates.v2",
  "candidates": [
    {"candidate_id": "H-001", "text": "comma, brace } and bracket ]",},
  ],
}`)
	repaired, ok := repairJSONTrailingCommas(raw)
	if !ok {
		t.Fatalf("trailing comma recovery failed: %s", repaired)
	}
	if !json.Valid(repaired) {
		t.Fatalf("repaired output is invalid: %s", repaired)
	}
	if !jsonContains(repaired, `"comma, brace } and bracket ]"`) {
		t.Fatalf("string content changed: %s", repaired)
	}
}

func TestNormalizeHunterArtifactRepairsTrailingCommas(t *testing.T) {
	raw := []byte(`{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [
    {
      "candidate_id": "H-001",
      "file_path": "src/a.cpp",
      "line_range": "1-2",
      "trigger_line": "x",
      "scope_symbol": "A",
      "category": "测试分类",
      "title": "valid",
      "code_snippet": "x",
      "trigger_condition": "x",
    }
  ],
}`)
	normalized, issues := NormalizeHunterArtifactJSON(raw, "", nil)
	if !json.Valid(normalized) {
		t.Fatalf("normalized JSON is invalid: %s", normalized)
	}
	found := false
	for _, issue := range issues {
		if issue.Code == IssueJSONSyntaxNormalized && issue.Recoverable {
			found = true
		}
	}
	if !found {
		t.Fatalf("syntax normalization issue missing: %#v", issues)
	}
}

func jsonContains(raw []byte, needle string) bool {
	return strings.Contains(string(raw), needle)
}
