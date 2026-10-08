package services

import (
	"testing"

	"code-shield/services/engines"
)

func TestEngineRegistryOnlyRegistersDebateFull(t *testing.T) {
	if !engines.EngineExists("debate_full") {
		t.Fatal("debate_full must be registered")
	}
	for _, mode := range []string{"single", "chunked", "debate_selective", "chunked_fast"} {
		if engines.EngineExists(mode) {
			t.Fatalf("legacy engine mode %q is still registered", mode)
		}
	}
}
