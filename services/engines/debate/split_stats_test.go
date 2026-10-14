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

func TestSplitStatsContractRepairs(t *testing.T) {
	ctx := beginSplitStats(context.Background())
	stats := splitStatsFromContext(ctx)
	if stats == nil {
		t.Fatal("stats is nil")
	}
	stats.ContractRepairs += 2
	stats.SchemaRepairAttempts += 2
	stats.SchemaRepairSuccesses += 1
	stats.JSONSyntaxRepairs += 1

	recovered := splitStatsFromContext(ctx)
	if recovered.ContractRepairs != 2 || recovered.SchemaRepairAttempts != 2 || recovered.SchemaRepairSuccesses != 1 || recovered.JSONSyntaxRepairs != 1 {
		t.Fatalf("unexpected stats: %+v", recovered)
	}
}
