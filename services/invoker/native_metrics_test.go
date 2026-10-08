package invoker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
)

func TestNativeInvokerTextStreamMetricsAndMaxTokens(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"# 报告\"}}],\"usage\":{\"total_tokens\":12}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Concurrent: 20,
		}},
		MaxRetries: 0,
	}

	invoker := NewNativeInvoker()
	outputPath := filepath.Join(t.TempDir(), "report.md")
	req := AIRequest{
		PromptMsg:      "render report",
		OutputPath:     outputPath,
		TimeoutMin:     1,
		ResponseFormat: "text",
		Observability:  NewCallObservability(AIRequest{OutputPath: outputPath}, "native", "native"),
	}
	if err := invoker.Invoke(req); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if receivedBody["max_tokens"] != float64(32768) {
		t.Fatalf("expected default max_tokens 32768, got %#v", receivedBody["max_tokens"])
	}

	metrics := req.Observability.NativeMetricsSnapshot()
	if metrics == nil {
		t.Fatal("expected native stream metrics")
	}
	if metrics.Attempt != 1 || !metrics.StreamEnabled || metrics.FirstByteMS < 0 || metrics.OutputBytes == 0 {
		t.Fatalf("unexpected native metrics: %+v", metrics)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# 报告" {
		t.Fatalf("unexpected output: %q", content)
	}
}

func TestNativeInvokerStreamsDeltasToConsole(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"# Hello \"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Concurrent: 20,
		}},
		MaxRetries: 0,
	}

	outputPath := filepath.Join(t.TempDir(), "report.md")
	obs := NewCallObservability(AIRequest{OutputPath: outputPath}, "native", "native")
	req := AIRequest{
		PromptMsg:      "render report",
		OutputPath:     outputPath,
		TimeoutMin:     1,
		ResponseFormat: "text",
		Observability:  obs,
	}
	if err := NewNativeInvoker().Invoke(req); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	obs.Finish(nil)

	snapshot, status := obs.Console.Snapshot(0, 100)
	if status.TotalEvents == 0 {
		t.Fatal("expected console events")
	}

	var firstDelta, secondDelta, successEvent bool
	for _, event := range snapshot.Events {
		switch {
		case event.Stream == "assistant" && event.Message == "# Hello ":
			firstDelta = true
		case event.Stream == "assistant" && event.Message == "world":
			secondDelta = true
		case event.Stream == "system" && strings.Contains(event.Message, "native stream succeeded"):
			successEvent = true
		}
	}
	if !firstDelta || !secondDelta || !successEvent {
		t.Fatalf("expected streamed deltas and success event, got %+v", snapshot.Events)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# Hello world" {
		t.Fatalf("unexpected output: %q", string(content))
	}
}
