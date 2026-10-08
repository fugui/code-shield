package invoker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
)

func TestNativeResponseFormatModes(t *testing.T) {
	schemaRequest := &JSONSchemaRequest{
		Name:   "unit_assessments_v2",
		Strict: true,
		Schema: map[string]any{"type": "object"},
	}
	format := nativeResponseFormat(AIRequest{JSONSchema: schemaRequest}, true)
	if format["type"] != "json_schema" {
		t.Fatalf("format type = %#v, want json_schema", format)
	}
	if nativeResponseFormat(AIRequest{JSONSchema: schemaRequest}, false) != nil {
		t.Fatal("text mode should not receive response_format")
	}
	format = nativeResponseFormat(AIRequest{}, true)
	if format["type"] != "json_object" {
		t.Fatalf("format type = %#v, want json_object", format)
	}
}

func TestJSONSchemaFallbackEligibility(t *testing.T) {
	request := AIRequest{JSONSchema: &JSONSchemaRequest{Schema: map[string]any{}}}
	badRequest := NewClassifiedError(ErrorClassUnknown, "endpoint returned HTTP 400: unsupported")
	if !jsonSchemaFallbackEligible(badRequest, request) {
		t.Fatal("HTTP 400 should be eligible for json_object fallback")
	}
	request.JSONSchema.Strict = true
	if jsonSchemaFallbackEligible(badRequest, request) {
		t.Fatal("strict json_schema should not fallback")
	}
	if jsonSchemaFallbackEligible(NewClassifiedError(ErrorClassUnknown, "HTTP 500"), request) {
		t.Fatal("non-400 errors should not trigger schema fallback")
	}
}

func TestNativeInvokerAutoSchemaFallbackToJSONObject(t *testing.T) {
	var formats []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ResponseFormat *struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		format := "none"
		if body.ResponseFormat != nil {
			format = body.ResponseFormat.Type
		}
		formats = append(formats, format)
		if len(formats) == 1 {
			http.Error(w, `{"error":{"message":"response_format json_schema is unsupported"}}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"content": `{"schema":"code-shield.unit-assessments.v2"}`},
			}},
			"usage": map[string]int{"total_tokens": 7},
		})
	}))
	defer server.Close()

	origConfig := models.AppConfig
	t.Cleanup(func() { models.AppConfig = origConfig })
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Model:      "test-model",
			Concurrent: 20,
		}},
		MaxRetries: 0,
	}

	outputPath := filepath.Join(t.TempDir(), "artifact.json")
	request := AIRequest{
		PromptMsg:      "produce assessment",
		OutputPath:     outputPath,
		TimeoutMin:     1,
		ResponseFormat: "json",
		JSONSchema: &JSONSchemaRequest{
			Name:   "unit_assessments_v2",
			Strict: false,
			Schema: map[string]any{"type": "object"},
		},
		Metrics: &InvocationMetrics{},
	}
	if err := NewNativeInvoker().Invoke(request); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(content) != `{"schema":"code-shield.unit-assessments.v2"}` {
		t.Fatalf("content = %q", string(content))
	}
	if len(formats) != 2 || formats[0] != "json_schema" || formats[1] != "json_object" {
		t.Fatalf("formats = %#v, want json_schema then json_object", formats)
	}
	if request.Metrics.ResponseFormatFallbacks != 1 {
		t.Fatalf("fallbacks = %d, want 1", request.Metrics.ResponseFormatFallbacks)
	}
}

func TestNativeInvokerStrictSchemaDoesNotFallback(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":{"message":"response_format json_schema is unsupported"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	origConfig := models.AppConfig
	t.Cleanup(func() { models.AppConfig = origConfig })
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Model:      "test-model",
			Concurrent: 20,
		}},
		MaxRetries: 0,
	}

	request := AIRequest{
		PromptMsg:  "produce assessment",
		OutputPath: filepath.Join(t.TempDir(), "artifact.json"),
		TimeoutMin: 1,
		JSONSchema: &JSONSchemaRequest{
			Name:   "unit_assessments_v2",
			Strict: true,
			Schema: map[string]any{"type": "object"},
		},
		Metrics: &InvocationMetrics{},
	}
	if err := NewNativeInvoker().Invoke(request); err == nil {
		t.Fatal("strict schema request should fail without fallback")
	}
	if requests != 1 || request.Metrics.ResponseFormatFallbacks != 0 {
		t.Fatalf("requests=%d fallbacks=%d, want one request and zero fallbacks", requests, request.Metrics.ResponseFormatFallbacks)
	}
}
