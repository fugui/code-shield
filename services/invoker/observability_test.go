package invoker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConsoleRingBoundedAndRedacted(t *testing.T) {
	ring := NewConsoleRing()
	ring.maxEvents = 4
	ring.maxLineBytes = 32

	ring.Append("system", "system", "authorization: super-secret-value")
	ring.Append("stdout", "stdout", "%s", strings.Repeat("x", 100))
	ring.Append("assistant", "assistant", "%s", "second")
	ring.Append("system", "system", "%s", "third")

	boundedSnapshot, _ := ring.Snapshot(0, 100)
	foundRedacted := false
	for _, event := range boundedSnapshot.Events {
		if strings.Contains(event.Message, "super-secret-value") {
			t.Fatalf("secret leaked into console event: %s", event.Message)
		}
		if strings.Contains(event.Message, "[REDACTED]") {
			foundRedacted = true
		}
		if len(event.Message) > 64 {
			t.Fatalf("event was not line-truncated: %s", event.Message)
		}
	}
	if !foundRedacted {
		t.Fatal("expected redacted credential marker")
	}

	ring.Append("system", "system", "%s", "fourth")
	status := ring.Status()
	if status.TotalEvents != 5 || status.DroppedEvents == 0 || len(ring.events) != 4 {
		t.Fatalf("unexpected bounded status: %+v retained=%d", status, len(ring.events))
	}

	snapshot, _ := ring.Snapshot(0, 100)
	if len(snapshot.Events) > 4 {
		t.Fatalf("expected at most 4 retained events, got %d", len(snapshot.Events))
	}
}

func TestConsoleWriterFlushesPartialLine(t *testing.T) {
	ring := NewConsoleRing()
	writer := ring.Writer("stdout", "stdout")
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if got := ring.Status().TotalEvents; got != 0 {
		t.Fatalf("expected partial line to remain buffered, got %d events", got)
	}
	if _, err := writer.Write([]byte(" line\n")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	writer.Flush()
	snapshot, _ := ring.Snapshot(0, 10)
	if len(snapshot.Events) != 1 || snapshot.Events[0].Message != "partial line" {
		t.Fatalf("unexpected events: %+v", snapshot.Events)
	}
}

func TestNewCallObservabilityPromptDetailWithoutInputContent(t *testing.T) {
	tempDir := t.TempDir()
	promptPath := filepath.Join(tempDir, "analysis_prompt.md")
	promptContent := "# readonly prompt"
	if err := os.WriteFile(promptPath, []byte(promptContent), 0600); err != nil {
		t.Fatalf("failed to write prompt: %v", err)
	}

	req := AIRequest{
		PromptFile: promptPath,
		PromptMsg:  "analyze",
		InputFiles: []string{"src/a.cpp", "src/b.cpp"},
		WorkDir:    tempDir,
	}
	obs := NewCallObservability(req, "native", "native")
	detail, err := obs.PromptDetail()
	if err != nil {
		t.Fatalf("PromptDetail failed: %v", err)
	}
	if detail.SystemPrompt != promptContent || detail.UserPrompt != "analyze" {
		t.Fatalf("unexpected prompt detail: %+v", detail)
	}
	if len(detail.InputFiles) != 2 {
		t.Fatalf("expected declared input paths, got %+v", detail.InputFiles)
	}
	for _, ref := range detail.InputFiles {
		if strings.Contains(ref.Path, promptContent) {
			t.Fatal("input path unexpectedly contains prompt content")
		}
	}

	obs.Finish(nil)
	if obs.PromptAvailable() {
		t.Fatal("provider should be unavailable after completion")
	}
	if _, err := obs.PromptDetail(); !errors.Is(err, ErrPromptProviderExpired) {
		t.Fatalf("expected provider expired, got %v", err)
	}
}

func TestOutputDetailPrefersFreshArtifactOverStaleOutput(t *testing.T) {
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "out.json")
	stdoutPath := outputPath + ".output.txt"

	staleTime := time.Now().Add(-time.Hour)
	if err := os.WriteFile(outputPath, []byte(`{"old":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(outputPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stdoutPath, []byte("fresh stdout"), 0600); err != nil {
		t.Fatal(err)
	}

	obs := NewCallObservability(AIRequest{OutputPath: outputPath}, "opencode", "opencode")
	obs.StartedAt = time.Now().Add(-time.Minute)
	detail := obs.OutputDetail(1024)

	if detail.PathRead != stdoutPath || detail.Stale {
		t.Fatalf("expected fresh stdout artifact, got path=%q stale=%t", detail.PathRead, detail.Stale)
	}
	if detail.Preview != "fresh stdout" {
		t.Fatalf("unexpected preview: %q", detail.Preview)
	}
}

func TestOutputDetailMarksStaleArtifact(t *testing.T) {
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "out.json")

	staleTime := time.Now().Add(-time.Hour)
	if err := os.WriteFile(outputPath, []byte(`{"old":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(outputPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	obs := NewCallObservability(AIRequest{OutputPath: outputPath}, "opencode", "opencode")
	obs.StartedAt = time.Now().Add(-time.Minute)
	detail := obs.OutputDetail(1024)

	if !detail.Exists || !detail.Stale || detail.PathRead != outputPath {
		t.Fatalf("expected stale output artifact, got exists=%t stale=%t path=%q", detail.Exists, detail.Stale, detail.PathRead)
	}
}

func TestRunCLIProcessCapturesConsoleWithoutChangingResult(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "out.json")
	req := AIRequest{
		ParentContext: context.Background(),
		WorkDir:       tempDir,
		PromptMsg:     "readonly test",
		OutputPath:    outPath,
		TimeoutMin:    1,
		Observability: NewCallObservability(AIRequest{OutputPath: outPath}, "sh", "sh"),
	}

	err := RunCLIProcess("sh", []string{"-c", "echo CLI_STDOUT; echo CLI_STDERR >&2; printf '{\\\"ok\\\":true}' > \"$1\"; echo PARTIAL_NO_NEWLINE", "sh", outPath}, req, "")
	if err != nil {
		t.Fatalf("unexpected invocation error: %v", err)
	}
	req.Observability.FlushConsole()
	req.Observability.Finish(nil)

	snapshot, _ := req.Observability.Console.Snapshot(0, 100)
	var stdout, stderr, partial bool
	for _, event := range snapshot.Events {
		if event.Stream == "stdout" && strings.Contains(event.Message, "CLI_STDOUT") {
			stdout = true
		}
		if event.Stream == "stderr" && strings.Contains(event.Message, "CLI_STDERR") {
			stderr = true
		}
		if event.Stream == "stdout" && strings.Contains(event.Message, "PARTIAL_NO_NEWLINE") {
			partial = true
		}
	}
	if !stdout || !stderr || !partial {
		t.Fatalf("expected stdout/stderr/partial capture, got %+v", snapshot.Events)
	}
	if _, statErr := os.Stat(outPath); statErr != nil {
		t.Fatalf("expected unchanged output file: %v", statErr)
	}
}
