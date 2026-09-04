package runner

import (
	"encoding/json"
	"testing"

	"code-shield/models"
)

func TestFixUnescapedQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{
			name:  "already valid JSON",
			input: `{"title": "hello world", "value": 42}`,
			valid: true,
		},
		{
			name:  "unescaped quotes in value",
			input: `{"title": "Use "proper" method", "score": 1}`,
			valid: true,
		},
		{
			name:  "multiple unescaped quotes",
			input: `{"detail": "Call "foo" then "bar" to fix", "severity": "high"}`,
			valid: true,
		},
		{
			name:  "already escaped quotes",
			input: `{"title": "Use \"proper\" method", "score": 1}`,
			valid: true,
		},
		{
			name:  "nested objects valid",
			input: `{"findings": [{"title": "test", "file": "a.go"}]}`,
			valid: true,
		},
		{
			name:  "unescaped in nested",
			input: `{"findings": [{"title": "Use "sync.Mutex" here", "file": "a.go"}]}`,
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FixUnescapedQuotes(tt.input)
			if tt.valid && !json.Valid([]byte(result)) {
				t.Errorf("expected valid JSON after fix, got: %s", result)
			}
		})
	}
}

func TestCleanJSONFromAI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "wrapped with code fence",
			input:    "```json\n{\"findings\": []}\n```",
			expected: `{"findings": []}`,
		},
		{
			name:     "surrounded with text",
			input:    "Here is your json output:\n{\"score\": 100}\nHope this helps!",
			expected: `{"score": 100}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(CleanJSONFromAI([]byte(tt.input)))
			if got != tt.expected {
				t.Errorf("CleanJSONFromAI(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeMarkdownReport(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "wrapped in markdown codeblock",
			input:    "```markdown\n# Report\nContent\n```",
			expected: "# Report\nContent",
		},
		{
			name:     "wrapped in json object",
			input:    `{"report": "# Hello Report"}`,
			expected: "# Hello Report",
		},
		{
			name:     "clean markdown",
			input:    "# Normal Report",
			expected: "# Normal Report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(SanitizeMarkdownReport([]byte(tt.input)))
			if got != tt.expected {
				t.Errorf("SanitizeMarkdownReport(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToLineStr(t *testing.T) {
	if ToLineStr(nil) != "" {
		t.Errorf("expected empty string for nil, got %q", ToLineStr(nil))
	}
	if ToLineStr(123.0) != "123" {
		t.Errorf("expected '123' for float64(123), got %q", ToLineStr(123.0))
	}
	if ToLineStr("45-60") != "45-60" {
		t.Errorf("expected '45-60' for string, got %q", ToLineStr("45-60"))
	}
}

func TestRunPostProcess(t *testing.T) {
	findings := []models.AnalysisFinding{
		{Severity: "fatal"},
		{Severity: "critical"},
		{Severity: "minor"},
		{Severity: "suggestion"},
	}

	res := RunPostProcess(findings, models.TaskType{})
	if res.Score != (5 + 4 + 2 + 0) {
		t.Errorf("unexpected score: got %d, expected 11", res.Score)
	}
	if res.Metrics["blocking"] != 1 || res.Metrics["critical"] != 1 || res.Metrics["minor"] != 1 || res.Metrics["suggestion"] != 1 {
		t.Errorf("unexpected metrics distribution: %v", res.Metrics)
	}
}
