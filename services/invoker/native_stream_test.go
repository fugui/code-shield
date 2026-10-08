package invoker

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code-shield/models"
)

func TestReadNativeStreamingChatIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": connected\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(1200 * time.Millisecond)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"late\"}}]}\n\n"))
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	cfg := models.NativeLLMConfig{FirstByteTimeoutSeconds: 1, IdleTimeoutSeconds: 5}
	_, _, _, err = readNativeStreamingChat(resp, cfg, time.Now(), func(level, stream, format string, args ...any) {})
	if err == nil {
		t.Fatal("expected idle timeout error")
	}
}
