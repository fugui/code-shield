package dispatcher

import (
	"context"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
)

// TierRecoveryStats records the stage-owned recovery audit trail.
type TierRecoveryStats struct {
	FreshRetries       int
	ResourceFailovers  int
	DriverFailovers    int
	ResourceChain      []string
	StageChain         []string
	ErrorClasses       []string
	QueueWaitMS        []int64
	DurationSeconds    []float64
	LastResourceID     string
	AttemptsByResource map[string]int
}

type TierRecoverySplitFunc func(
	ctx context.Context,
	candidate TierCandidate,
	tokens int64,
	class invoker.ErrorClass,
	err error,
) (bool, string, int64, error)

type tierRecoveryOptions struct {
	split TierRecoverySplitFunc
}

type TierRecoveryOption func(*tierRecoveryOptions)

func WithTierSplitRecovery(split TierRecoverySplitFunc) TierRecoveryOption {
	return func(options *tierRecoveryOptions) {
		options.split = split
	}
}

func (s *TierRecoveryStats) trackAttempt(resourceID, tierName string) {
	if s == nil || resourceID == "" {
		return
	}
	s.ResourceChain = append(s.ResourceChain, resourceID)
	if tierName != "" {
		s.StageChain = append(s.StageChain, tierName)
	}
	if s.AttemptsByResource == nil {
		s.AttemptsByResource = map[string]int{}
	}
	s.AttemptsByResource[resourceID]++
	s.LastResourceID = resourceID
}

func (s *TierRecoveryStats) trackResource(resourceID string) {
	s.trackAttempt(resourceID, "")
}

func (s *TierRecoveryStats) attemptsOn(resourceID string) int {
	if s == nil {
		return 0
	}
	return s.AttemptsByResource[resourceID]
}

func (s *TierRecoveryStats) recordError(class invoker.ErrorClass) {
	if s == nil {
		return
	}
	s.ErrorClasses = append(s.ErrorClasses, string(class))
}

func (s *TierRecoveryStats) recordTiming(metrics *invoker.InvocationMetrics) {
	if s == nil || metrics == nil {
		return
	}
	s.QueueWaitMS = append(s.QueueWaitMS, metrics.QueueWaitMs)
	s.DurationSeconds = append(s.DurationSeconds, float64(metrics.DurationMs)/1000)
	s.DriverFailovers += metrics.DriverFailovers
}

func recoveryAllows(classes []string, class invoker.ErrorClass) bool {
	for _, item := range classes {
		if strings.EqualFold(strings.TrimSpace(item), string(class)) {
			return true
		}
	}
	return false
}

func shouldTierSplit(classes []string, class invoker.ErrorClass) bool {
	return recoveryAllows(classes, class)
}

func sameResourceRetryAllowed(recovery models.TierRecoveryConfig, class invoker.ErrorClass) bool {
	if recoveryAllows(recovery.RetryOn, class) {
		return true
	}
	if class == invoker.ErrorClassContractMismatch || class == invoker.ErrorClassJSONInvalid {
		return recoveryAllows(recovery.ContractRepairOn, class)
	}
	return false
}

func candidateFailoverAllowed(recovery models.TierRecoveryConfig, class invoker.ErrorClass) bool {
	if recovery.MaxCandidateFailovers <= 0 {
		return false
	}
	switch class {
	case invoker.ErrorClassResourceUnavailable, invoker.ErrorClassResourceBusy,
		invoker.ErrorClassRateLimited, invoker.ErrorClassNetworkTransient,
		invoker.ErrorClassOutputMissing:
		return recoveryAllows(recovery.CandidateFailoverOn, class)
	default:
		return false
	}
}

func waitTierRecoveryBackoff(ctx context.Context, recovery models.TierRecoveryConfig, attempt int) error {
	backoff := time.Duration(recovery.RetryBackoffMs) * time.Millisecond
	if backoff <= 0 {
		backoff = 3 * time.Second
	}
	maxBackoff := time.Duration(recovery.MaxBackoffSeconds) * time.Second
	if maxBackoff <= 0 {
		maxBackoff = 15 * time.Second
	}
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	if attempt > 1 {
		backoff *= time.Duration(attempt)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(backoff):
		return nil
	}
}

// RunTierInvocationWithRecovery executes one logical tier invocation under the
// stage recovery state machine. Ordinary retries preserve the selected
// resource with pause & backoff; failover requires both an availability class and an explicit budget.
func RunTierInvocationWithRecovery(
	ctx context.Context,
	tierName string,
	attemptSeconds int,
	stats *TierRecoveryStats,
	invoke func(ctx context.Context, candidate TierCandidate, timeoutSeconds int, metrics *invoker.InvocationMetrics) (string, int64, error),
	options ...TierRecoveryOption,
) (string, TierCandidate, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	plan, planErr := GetTierRouter().AcquireTierResourcePlan(ctx, tierName, nil)
	if planErr != nil {
		return "", TierCandidate{}, 0, planErr
	}

	recoveryOptions := &tierRecoveryOptions{}
	for _, option := range options {
		if option != nil {
			option(recoveryOptions)
		}
	}

	tierCfg := models.AppConfig.GetTierConfig(tierName)
	recovery := tierCfg.Recovery
	maxAttempts := recovery.MaxTotalAttempts
	if maxAttempts < 1 {
		maxAttempts = 2
	}
	if len(recovery.RetryOn) == 0 {
		recovery.RetryOn = []string{
			string(invoker.ErrorClassIdleTimeout),
			string(invoker.ErrorClassTimeout),
			string(invoker.ErrorClassOutputMissing),
			string(invoker.ErrorClassResourceBusy),
			string(invoker.ErrorClassRateLimited),
			string(invoker.ErrorClassNetworkTransient),
			string(invoker.ErrorClassResourceUnavailable),
		}
	}
	if len(recovery.SplitOn) == 0 {
		recovery.SplitOn = []string{
			string(invoker.ErrorClassIdleTimeout),
			string(invoker.ErrorClassTimeout),
			string(invoker.ErrorClassOutputMissing),
		}
	}
	if len(recovery.ContractRepairOn) == 0 {
		recovery.ContractRepairOn = []string{
			string(invoker.ErrorClassContractMismatch),
			string(invoker.ErrorClassJSONInvalid),
		}
	}
	if recovery.MaxAttemptsPerResource < 1 {
		recovery.MaxAttemptsPerResource = maxAttempts
	}

	excludedResources := map[string]struct{}{}
	var rawOutput string
	var tokens int64
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return rawOutput, plan.Selected, tokens, invoker.WrapClassifiedError(
				invoker.ErrorClassCanceled, ctx.Err(), "%s stage budget exhausted", tierName,
			)
		}
		candidate := plan.Selected
		stats.trackAttempt(candidate.ResourceID, tierName)

		attemptStart := time.Now()
		metrics := &invoker.InvocationMetrics{}
		attemptOutput, attemptTokens, invokeErr := invoke(ctx, candidate, attemptSeconds, metrics)
		rawOutput = attemptOutput
		tokens += attemptTokens
		lastErr = invokeErr
		if metrics.DurationMs <= 0 {
			metrics.DurationMs = time.Since(attemptStart).Milliseconds()
		}
		stats.recordTiming(metrics)
		class := invoker.ErrorClassNone
		if lastErr != nil {
			class = invoker.ClassifyError(lastErr)
		}
		stats.recordError(class)
		if lastErr == nil {
			return rawOutput, candidate, tokens, nil
		}

		if (class == invoker.ErrorClassContractMismatch || class == invoker.ErrorClassJSONInvalid) &&
			!sameResourceRetryAllowed(recovery, class) &&
			!shouldTierSplit(recovery.SplitOn, class) {
			return rawOutput, candidate, tokens, lastErr
		}

		if recoveryOptions.split != nil && class != invoker.ErrorClassOutputMissing && shouldTierSplit(recovery.SplitOn, class) {
			handled, splitOutput, splitTokens, splitErr := recoveryOptions.split(ctx, candidate, tokens, class, lastErr)
			if handled {
				if splitErr == nil {
					return splitOutput, candidate, splitTokens, nil
				}
				return rawOutput, candidate, splitTokens, splitErr
			}
		}

		// When candidate failover is explicitly enabled for resource_unavailable, switch candidate immediately
		if class == invoker.ErrorClassResourceUnavailable && candidateFailoverAllowed(recovery, class) && attempt < maxAttempts {
			excludedResources[candidate.ResourceID] = struct{}{}
			if next, ok := NextTierCandidate(plan, excludedResources); ok &&
				stats.ResourceFailovers < recovery.MaxCandidateFailovers {
				plan.Selected = next
				stats.ResourceFailovers++
				continue
			}
		}

		maxAttemptsForClass := recovery.MaxAttemptsPerResource
		if class == invoker.ErrorClassUnknown && maxAttemptsForClass > 2 {
			maxAttemptsForClass = 2
		}
		if sameResourceRetryAllowed(recovery, class) &&
			attempt < maxAttempts &&
			stats.attemptsOn(candidate.ResourceID) < maxAttemptsForClass {
			if waitErr := waitTierRecoveryBackoff(ctx, recovery, attempt); waitErr != nil {
				return rawOutput, candidate, tokens, invoker.WrapClassifiedError(
					invoker.ErrorClassCanceled, waitErr, "%s recovery canceled", tierName,
				)
			}
			stats.FreshRetries++
			continue
		}

		if candidateFailoverAllowed(recovery, class) && attempt < maxAttempts {
			excludedResources[candidate.ResourceID] = struct{}{}
			if next, ok := NextTierCandidate(plan, excludedResources); ok &&
				stats.ResourceFailovers < recovery.MaxCandidateFailovers {
				plan.Selected = next
				stats.ResourceFailovers++
				continue
			}
		}
		break
	}
	if recoveryOptions.split != nil && lastErr != nil {
		class := invoker.ClassifyError(lastErr)
		if shouldTierSplit(recovery.SplitOn, class) {
			handled, splitOutput, splitTokens, splitErr := recoveryOptions.split(ctx, plan.Selected, tokens, class, lastErr)
			if handled {
				if splitErr == nil {
					return splitOutput, plan.Selected, splitTokens, nil
				}
				return rawOutput, plan.Selected, splitTokens, splitErr
			}
			return rawOutput, plan.Selected, tokens, lastErr
		}
	}
	return rawOutput, plan.Selected, tokens, lastErr
}
