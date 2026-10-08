package invoker

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"code-shield/models"
)

func TestNativeInvokerSetsUserAgent(t *testing.T) {
	userAgent := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent <- r.UserAgent()
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints:      []models.NativeEndpointConfig{{Name: "default", BaseURL: server.URL, Model: "glm-4-flash", Concurrent: 20}},
		MaxRetries:     0,
		RetryBackoffMs: 1,
	}

	invoker := NewNativeInvoker()
	err := invoker.Invoke(AIRequest{
		PromptMsg:     "test user agent",
		OutputPath:    filepath.Join(t.TempDir(), "output.txt"),
		Observability: NewCallObservability(AIRequest{OutputPath: filepath.Join(t.TempDir(), "output.txt")}, "native", "native"),
		TimeoutMin:    1,
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	select {
	case got := <-userAgent:
		if got != nativeUserAgent {
			t.Fatalf("User-Agent = %q, want %q", got, nativeUserAgent)
		}
	default:
		t.Fatalf("server did not receive request")
	}
}
