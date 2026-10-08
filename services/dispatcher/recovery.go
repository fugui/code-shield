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

func (s *TierRecoveryStats) trackResource(resourceID string) {
	if s == nil || resourceID == "" {
		return
	}
	s.ResourceChain = append(s.ResourceChain, resourceID)
	if s.AttemptsByResource == nil {
		s.AttemptsByResource = map[string]int{}
	}
	s.AttemptsByResource[resourceID]++
	s.LastResourceID = resourceID
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
		return nil
	}
	if recovery.MaxBackoffSeconds > 0 && backoff > time.Duration(recovery.MaxBackoffSeconds)*time.Second {
		backoff = time.Duration(recovery.MaxBackoffSeconds) * time.Second
	}
	if backoff > time.Second {
		backoff *= time.Duration(attempt)
		if recovery.MaxBackoffSeconds > 0 && backoff > time.Duration(recovery.MaxBackoffSeconds)*time.Second {
			backoff = time.Duration(recovery.MaxBackoffSeconds) * time.Second
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
// resource; failover requires both an availability class and an explicit budget.
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
		}
	}
	if len(recovery.SplitOn) == 0 {
		recovery.SplitOn = []string{
			string(invoker.ErrorClassIdleTimeout),
			string(invoker.ErrorClassTimeout),
			string(invoker.ErrorClassOutputMissing),
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
		stats.trackResource(candidate.ResourceID)
		if !candidate.Available {
			lastErr = invoker.NewClassifiedError(
				invoker.ErrorClassResourceUnavailable,
				"%s resource %s breaker is open", tierName, candidate.ResourceID,
			)
			stats.recordError(invoker.ErrorClassResourceUnavailable)
			if candidateFailoverAllowed(recovery, invoker.ErrorClassResourceUnavailable) && attempt < maxAttempts {
				excludedResources[candidate.ResourceID] = struct{}{}
				if next, ok := NextTierCandidate(plan, excludedResources); ok &&
					stats.ResourceFailovers < recovery.MaxCandidateFailovers {
					plan.Selected = next
					stats.ResourceFailovers++
					continue
				}
			}
			return rawOutput, candidate, tokens, lastErr
		}

		metrics := &invoker.InvocationMetrics{}
		attemptOutput, attemptTokens, invokeErr := invoke(ctx, candidate, attemptSeconds, metrics)
		rawOutput = attemptOutput
		tokens += attemptTokens
		lastErr = invokeErr
		stats.recordTiming(metrics)
		class := invoker.ErrorClassNone
		if lastErr != nil {
			class = invoker.ClassifyError(lastErr)
		}
		stats.recordError(class)
		if lastErr == nil {
			return rawOutput, candidate, tokens, nil
		}

		if class == invoker.ErrorClassContractMismatch || class == invoker.ErrorClassJSONInvalid {
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

		if recoveryAllows(recovery.RetryOn, class) &&
			attempt < maxAttempts &&
			stats.attemptsOn(candidate.ResourceID) < recovery.MaxAttemptsPerResource {
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
