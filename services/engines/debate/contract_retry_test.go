package debate

import (
	"errors"
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
