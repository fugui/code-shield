package services

import (
	"encoding/json"
	"testing"
)

func TestFixUnescapedQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool // 修复后是否为合法 JSON
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
			result := fixUnescapedQuotes(tt.input)
			if tt.valid && !json.Valid([]byte(result)) {
				t.Errorf("expected valid JSON after fix, got: %s", result)
			}
		})
	}
}
