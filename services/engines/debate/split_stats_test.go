package debate

import (
	"context"
	"testing"

	"code-shield/models"
)

func TestSplitStatsAreTrackedAndCapped(t *testing.T) {
	ctx := beginSplitStats(context.Background())
	tierCfg := models.TierConfig{MaxSplitDepth: 2}

	if !canSplit(ctx, tierCfg) {
		t.Fatal("canSplit() = false at depth 0, want true")
	}
	beginSplit(ctx)
	if !canSplit(ctx, tierCfg) {
		t.Fatal("canSplit() = false at depth 1, want true")
	}
	beginSplit(ctx)
	if canSplit(ctx, tierCfg) {
		t.Fatal("canSplit() = true at depth 2, want false")
	}

	stats := splitStatsFromContext(ctx)
	if stats.Depth != 2 || stats.Count != 2 {
		t.Fatalf("stats = depth=%d count=%d, want depth=2 count=2", stats.Depth, stats.Count)
	}
}
