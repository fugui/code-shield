package debate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/dispatcher"
	"code-shield/services/engines"
	"code-shield/services/invoker"
)

func configureTierStageRecovery(t *testing.T, tier string, invokerName string, recovery models.TierRecoveryConfig) {
	t.Helper()
	tiers := &models.AppConfig.Scanner.Debate.Tiers
	var previous models.TierBindingConfig
	switch tier {
	case "tier2_challenger":
		previous = tiers.Tier2Challenger
		tiers.Tier2Challenger = models.TierBindingConfig{
			Resource:              invokerName,
			Resources:             []string{invokerName},
			TimeoutSeconds:        10,
			AttemptTimeoutSeconds: 10,
			Recovery:              &recovery,
		}
	case "tier3_judge":
		previous = tiers.Tier3Judge
		tiers.Tier3Judge = models.TierBindingConfig{
			Resource:              invokerName,
			Resources:             []string{invokerName},
			TimeoutSeconds:        10,
			AttemptTimeoutSeconds: 10,
			Recovery:              &recovery,
		}
	default:
		t.Fatalf("unsupported tier %q", tier)
	}
	t.Cleanup(func() {
		if tier == "tier2_challenger" {
			tiers.Tier2Challenger = previous
		} else {
			tiers.Tier3Judge = previous
		}
	})
}

func TestTierStageRecoveryRetriesSameResource(t *testing.T) {
	for _, tier := range []string{"tier2_challenger", "tier3_judge"} {
		t.Run(tier, func(t *testing.T) {
			invokerName := tier + "-stable-resource"
			inv := &recoveryTestInvoker{name: invokerName}
			inv.fail = func(call int) error {
				if call == 1 {
					return invoker.NewClassifiedError(invoker.ErrorClassIdleTimeout, "idle timeout")
				}
				return nil
			}
			invoker.RegisterAIInvoker(invokerName, inv)
			configureTierStageRecovery(t, tier, invokerName, models.TierRecoveryConfig{
				MaxTotalAttempts:       2,
				MaxAttemptsPerResource: 2,
				RetryBackoffMs:         1,
				RetryOn:                []string{"idle_timeout"},
			})

			tempDir := t.TempDir()
			ctx := &engines.EngineContext{
				Ctx: beginSplitStats(context.Background()),
			}
			promptPath := filepath.Join(tempDir, "prompt.txt")
			if err := os.WriteFile(promptPath, []byte("prompt"), 0644); err != nil {
				t.Fatalf("write prompt: %v", err)
			}
			outputPath := filepath.Join(tempDir, "artifact.json")
			raw, _, tokens, err := runTierInvocationWithRecovery(
				ctx.Ctx,
				tier,
				10,
				&invoker.LLMWorkContext{TierName: tier},
				func(callCtx context.Context, candidate dispatcher.TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error) {
					return callAITierPromptFileWithMetrics(
						callCtx,
						candidate.Driver,
						candidate.Model,
						promptPath,
						tempDir,
						outputPath,
						timeoutSeconds,
						tierTimeoutPolicy(models.AppConfig.GetTierConfig(tier)),
						metrics,
					)
				},
			)
			if err != nil || raw == "" || tokens <= 0 {
				t.Fatalf("retry result: raw=%q tokens=%d err=%v", raw, tokens, err)
			}
			if inv.calls != 2 {
				t.Fatalf("calls=%d, want same-resource fresh retry", inv.calls)
			}
			stats := splitStatsFromContext(ctx.Ctx)
			if len(stats.ResourceChain) != 2 || stats.ResourceChain[0] != stats.ResourceChain[1] {
				t.Fatalf("resource chain=%#v, want repeated stable resource", stats.ResourceChain)
			}
			if stats.ResourceFailovers != 0 || stats.FreshRetries != 1 || len(stats.ErrorClasses) != 2 {
				t.Fatalf("stats=%+v, want stable recovery audit trail", stats)
			}
		})
	}
}

func TestTierStageRecoveryContractMismatchRepairsBeforeNormalRetry(t *testing.T) {
	invokerName := "tier2-contract-repair"
	inv := &recoveryTestInvoker{name: invokerName}
	inv.payload = func(call int) string {
		if call == 1 {
			return `{"not":"the contract"}`
		}
		return fmt.Sprintf(`{"schema":%q,"candidates":[]}`, CandidatesArtifactSchemaV1)
	}
	invoker.RegisterAIInvoker(invokerName, inv)
	configureTierStageRecovery(t, "tier2_challenger", invokerName, models.TierRecoveryConfig{
		MaxTotalAttempts:       2,
		MaxAttemptsPerResource: 2,
		RetryOn:                []string{"idle_timeout"},
	})

	tempDir := t.TempDir()
	ctx := &engines.EngineContext{Ctx: beginSplitStats(context.Background())}
	promptPath := filepath.Join(tempDir, "prompt.txt")
	outputPath := filepath.Join(tempDir, "artifact.json")
	raw, candidate, _, err := runTierInvocationWithRecovery(
		ctx.Ctx,
		"tier2_challenger",
		10,
		&invoker.LLMWorkContext{TierName: "tier2_challenger"},
		func(callCtx context.Context, selected dispatcher.TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error) {
			return callAITierPromptFileWithMetrics(
				callCtx, selected.Driver, selected.Model, promptPath, tempDir, outputPath,
				timeoutSeconds, tierTimeoutPolicy(models.AppConfig.GetTierConfig("tier2_challenger")), metrics,
			)
		},
	)
	if err != nil || raw == "" || candidate.ResourceID != invokerName {
		t.Logf("recovery debug: calls=%d raw=%q", inv.calls, raw)
		t.Fatalf("recovery result: candidate=%+v raw=%q err=%v", candidate, raw, err)
	}
	if inv.calls != 1 {
		t.Fatalf("invocation calls=%d, contract mismatch must not trigger ordinary retry", inv.calls)
	}
	stats := splitStatsFromContext(ctx.Ctx)
	if len(stats.ErrorClasses) != 1 || stats.ErrorClasses[0] != "none" {
		t.Fatalf("error classes=%#v, invocation-level class must remain none before contract validation", stats.ErrorClasses)
	}
}
