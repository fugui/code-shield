package invoker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractCodexInvocationDiagnostics(t *testing.T) {
	stderr := "OpenAI Codex v0.147.0\nsession id: 01a0a8f2-b9f7-7110-989b-0601348ca8a5\ntokens used\n618\n"

	if got := extractCodexSessionID(stderr); got != "01a0a8f2-b9f7-7110-989b-0601348ca8a5" {
		t.Fatalf("session id = %q", got)
	}
	usage := extractCodexTokenUsage(stderr)
	if usage == nil || usage.TotalTokens != 618 || usage.Source != "stderr_summary" {
		t.Fatalf("token usage = %+v", usage)
	}
}

func TestRunCLIProcessCapturesCLIDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "report.json")
	req := AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "test",
		OutputPath: outputPath,
		TimeoutMin: 1,
	}
	req.Observability = NewCallObservability(req, "codex", "codex")

	result, err := RunCLIProcessWithResult("sh", []string{
		"-c", "echo 'session id: 01a0a8f2-b9f7-7110-989b-0601348ca8a5' >&2; echo 'tokens used' >&2; echo 618 >&2; echo 'api_key=secret' >&2; exit 1",
	}, req, "mock")
	if err == nil {
		t.Fatal("expected CLI failure")
	}
	if result.SessionID != "01a0a8f2-b9f7-7110-989b-0601348ca8a5" {
		t.Fatalf("session id = %q", result.SessionID)
	}
	if result.ExitCode == nil || *result.ExitCode != 1 {
		t.Fatalf("exit code = %+v, want 1", result.ExitCode)
	}
	if err.Error() != "AI execution failed: sh exit code 1" {
		t.Fatalf("compact error = %q", err.Error())
	}

	diag := req.Observability.CLIDiagnosticsSnapshot()
	if diag == nil {
		t.Fatal("expected CLI diagnostics")
	}
	if diag.TerminationReason != "cli_failure" || diag.ErrorClass != ErrorClassUnknown {
		t.Fatalf("diagnostics = %+v", diag)
	}
	if !strings.Contains(diag.StderrTail, "model") && !strings.Contains(diag.StderrTail, "api_key") {
		t.Fatalf("stderr tail missing failure context: %q", diag.StderrTail)
	}
	if strings.Contains(diag.StderrTail, "secret") {
		t.Fatalf("stderr tail contains secret: %q", diag.StderrTail)
	}
}

func TestFinalizeCodexOutputCapturesBlankMessageDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "report.json")
	if err := os.WriteFile(outputPath+".lastmsg", []byte("\n"), 0644); err != nil {
		t.Fatal(err)
	}
	req := AIRequest{OutputPath: outputPath}
	req.Observability = NewCallObservability(req, "codex", "codex")
	req.Observability.SetCLIDiagnostics(CLIDiagnostics{
		Driver:              "codex",
		CLI:                 "codex",
		TerminationReason:   "cli_exit",
		StderrTail:          "cli stderr tail",
		StderrTailTruncated: true,
		StderrTailBytes:     128,
	})
	processResult := CLIProcessResult{
		SessionID: "session",
		ExitCode:  func() *int { code := 0; return &code }(),
		TokenUsage: &TokenUsage{
			TotalTokens: 618,
			Source:      "stderr_summary",
		},
	}

	err := finalizeCodexOutput(req, processResult)
	if ClassifyError(err) != ErrorClassOutputMissing {
		t.Fatalf("error class = %q", ClassifyError(err))
	}
	if err.Error() != "codex produced empty final message" {
		t.Fatalf("error = %q", err.Error())
	}
	diag := req.Observability.CLIDiagnosticsSnapshot()
	if diag == nil || diag.TerminationReason != "output_missing" || diag.SessionID != "session" {
		t.Fatalf("diagnostics = %+v", diag)
	}
	if diag.TokenUsage == nil || diag.TokenUsage.TotalTokens != 618 {
		t.Fatalf("diagnostic token usage = %+v", diag.TokenUsage)
	}
	if diag.StderrTail != "cli stderr tail" || !diag.StderrTailTruncated || diag.StderrTailBytes != 128 {
		t.Fatalf("diagnostic stderr tail was not preserved: %+v", diag)
	}
}
