package invoker

import (
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const (
	stderrTailMaxBytes = 4 * 1024
	maxCompactErrorLen = 1024
)

type TokenUsage struct {
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens,omitempty"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens,omitempty"`
	TotalTokens           int64  `json:"total_tokens"`
	Source                string `json:"source"`
}

type CLIDiagnostics struct {
	LeaseID             string      `json:"lease_id,omitempty"`
	Driver              string      `json:"driver,omitempty"`
	CLI                 string      `json:"cli,omitempty"`
	SessionID           string      `json:"session_id,omitempty"`
	ExitCode            *int        `json:"exit_code,omitempty"`
	TerminationReason   string      `json:"termination_reason,omitempty"`
	ErrorClass          ErrorClass  `json:"error_class,omitempty"`
	StderrTail          string      `json:"stderr_tail,omitempty"`
	StderrTailTruncated bool        `json:"stderr_tail_truncated,omitempty"`
	StderrTailBytes     int64       `json:"stderr_tail_bytes,omitempty"`
	TokenUsage          *TokenUsage `json:"token_usage,omitempty"`
}

func truncateUTF8Tail(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	truncated := value[len(value)-maxBytes:]
	for len(truncated) > 0 && !isUTF8BoundaryStart(truncated[0]) {
		truncated = truncated[1:]
	}
	return truncated, true
}

func compactErrorMessage(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	return truncateString(message, maxCompactErrorLen)
}

var codexSessionIDPattern = regexp.MustCompile(`(?i)session id:\s*([0-9a-f][0-9a-f-]{35})`)
var codexTokenUsagePattern = regexp.MustCompile(`(?is)tokens used\s*\r?\n\s*([0-9]+)`)

func exitCodeFromWaitError(err error) *int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return &code
	}
	if err == nil {
		code := 0
		return &code
	}
	return nil
}

func extractCodexSessionID(stderr string) string {
	matches := codexSessionIDPattern.FindAllStringSubmatch(stderr, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

func extractCodexTokenUsage(stderr string) *TokenUsage {
	matches := codexTokenUsagePattern.FindAllStringSubmatch(stderr, -1)
	if len(matches) == 0 {
		return nil
	}
	total, err := strconv.ParseInt(matches[len(matches)-1][1], 10, 64)
	if err != nil || total <= 0 {
		return nil
	}
	return &TokenUsage{TotalTokens: total, Source: "stderr_summary"}
}

func applyCLIDiagnostics(req AIRequest, cliName string, result CLIProcessResult, stderr string, class ErrorClass, reason string, waitErr error) {
	if req.Observability == nil {
		return
	}
	diag := CLIDiagnostics{
		Driver:            req.Observability.Driver,
		CLI:               cliName,
		SessionID:         result.SessionID,
		ExitCode:          result.ExitCode,
		TerminationReason: reason,
		ErrorClass:        class,
		TokenUsage:        result.TokenUsage,
	}
	tail, truncated := truncateUTF8Tail(stderr, stderrTailMaxBytes)
	diag.StderrTail = redactSensitiveText(tail)
	diag.StderrTailTruncated = truncated
	diag.StderrTailBytes = int64(len(stderr))
	req.Observability.SetCLIDiagnostics(diag)
}
