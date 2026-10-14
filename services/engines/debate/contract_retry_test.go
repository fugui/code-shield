package debate

import (
	"errors"
	"strings"
	"testing"
)

func TestIsContractMismatch(t *testing.T) {
	if isContractMismatch(nil) {
		t.Fatalf("nil error is not a mismatch")
	}
	for _, message := range []string{
		"AI output schema mismatch: missing top-level key \"candidates\"",
		"invalid candidate H-001: category is empty",
		"terminal findings output is not valid JSON",
	} {
		if !isContractMismatch(errors.New(message)) {
			t.Fatalf("expected mismatch for %q", message)
		}
	}
	if isContractMismatch(errors.New("connection reset by peer")) {
		t.Fatalf("transport errors must not be treated as contract mismatch")
	}
}

func TestBuildContractRetryPromptMarkdownDetection(t *testing.T) {
	contract := OutputContract{
		SchemaID: "test-contract",
		TopLevel: "candidates",
	}
	prompt := "请分析代码并输出结果。"
	cause := errors.New("AI returned Markdown text instead of JSON")
	retryPrompt := buildContractRetryPrompt(prompt, contract, cause)
	if !strings.Contains(retryPrompt, "上一次输出是 Markdown 文本而非标准 JSON") {
		t.Fatalf("expected Markdown specific correction in retry prompt, got: %s", retryPrompt)
	}
	if !strings.Contains(retryPrompt, "必须输出符合契约的纯 UTF-8 JSON") {
		t.Fatalf("expected strict JSON reminder in retry prompt, got: %s", retryPrompt)
	}
}
