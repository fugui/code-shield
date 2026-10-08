package debate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseJSONFromAIOutputSanitizesNestedNUL(t *testing.T) {
	raw := `{
		"candidates": [{
			"candidate_id": "H-001",
			"code_snippet": "const char *s = '\u0000';"
		}]
	}`

	var out HunterOutput
	if err := parseJSONFromAIOutput(raw, &out, t.TempDir()); err != nil {
		t.Fatalf("parseJSONFromAIOutput failed: %v", err)
	}

	snippet := out.Candidates[0].CodeSnippet
	if strings.ContainsRune(snippet, '\x00') {
		t.Fatalf("expected NUL to be sanitized, got %q", snippet)
	}
	if !strings.Contains(snippet, `\0`) {
		t.Fatalf("expected C-style NUL marker, got %q", snippet)
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if strings.Contains(string(encoded), `\u0000`) {
		t.Fatalf("encoded JSON still contains unsupported PostgreSQL escape: %s", encoded)
	}
}

func TestSanitizeNullStringsHandlesNestedEvidence(t *testing.T) {
	out := JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{{
		CandidateID: "H-001",
		CodeSnippet: "foo('\x00');",
		Evidence: []JudgeEvidenceRef{{
			Kind:    JudgeEvidenceTarget,
			Snippet: "bar == '\x00'",
		}},
	}}}

	sanitizeNullStrings(&out)

	if strings.ContainsRune(out.FinalVerdicts[0].CodeSnippet, '\x00') {
		t.Fatalf("top-level snippet still contains NUL: %q", out.FinalVerdicts[0].CodeSnippet)
	}
	if got := out.FinalVerdicts[0].Evidence[0].Snippet; got != `bar == '\0'` {
		t.Fatalf("nested evidence snippet = %q, want %q", got, `bar == '\0'`)
	}
}
