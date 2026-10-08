package invoker

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestClassifyErrorPrefersTypedClass(t *testing.T) {
	busy := NewClassifiedError(ErrorClassResourceBusy, "dispatcher pool full")
	wrapped := fmt.Errorf("hunter failed: %w", busy)
	if got := ClassifyError(wrapped); got != ErrorClassResourceBusy {
		t.Fatalf("ClassifyError() = %q, want %q", got, ErrorClassResourceBusy)
	}
	if !errors.Is(wrapped, busy) {
		t.Fatal("typed error chain was not preserved")
	}
}

func TestClassifyErrorUsesContextSentinels(t *testing.T) {
	if got := ClassifyError(context.Canceled); got != ErrorClassCanceled {
		t.Fatalf("ClassifyError(context.Canceled) = %q, want %q", got, ErrorClassCanceled)
	}
	if got := ClassifyError(context.DeadlineExceeded); got != ErrorClassTimeout {
		t.Fatalf("ClassifyError(context.DeadlineExceeded) = %q, want %q", got, ErrorClassTimeout)
	}
}

func TestClassifiedErrorPreservesCauseAndMessage(t *testing.T) {
	cause := errors.New("connection refused")
	err := WrapClassifiedError(ErrorClassNetworkTransient, cause, "endpoint %q failed", "primary")
	want := `endpoint "primary" failed: connection refused`
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause was not preserved")
	}
}
