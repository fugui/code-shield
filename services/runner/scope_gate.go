package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/planner"
	"code-shield/services/engines/profile"
)

// RunScanProfileGate is the scheduler-safe Stage 2 gate. It turns the
// deterministic scope plan into one of the four runner lifecycle decisions.
func RunScanProfileGate(ctx *TaskContext) (*coverage.ScanPlan, planner.PrimaryPlan, engines.ParsedEngineProfile, error) {
	UpdateTaskStatus(ctx.Report.ID, models.StatusPreProcessing)

	if len(ctx.Report.ScanProfile) == 0 || ctx.Report.ScanProfileHash == "" {
		profileErr := fmt.Errorf("report %d has no scan profile execution snapshot", ctx.Report.ID)
		MarkFailed(ctx, profileErr.Error())
		return nil, nil, engines.ParsedEngineProfile{}, profileErr
	}
	parsedProfile, profileErr := engines.ParseProfileSnapshot(json.RawMessage(ctx.Report.ScanProfile))
	if profileErr != nil {
		MarkFailed(ctx, profileErr.Error())
		return nil, nil, engines.ParsedEngineProfile{}, profileErr
	}
	if parsedProfile.Hash != ctx.Report.ScanProfileHash {
		profileErr = fmt.Errorf(
			"report %d scan profile hash mismatch: snapshot=%s report=%s",
			ctx.Report.ID, parsedProfile.Hash, ctx.Report.ScanProfileHash,
		)
		MarkFailed(ctx, profileErr.Error())
		return nil, nil, engines.ParsedEngineProfile{}, profileErr
	}

	targetScope := parsedProfile.Profile.TargetScope
	if ctx.RunParams.TargetScope != nil {
		targetScope = *ctx.RunParams.TargetScope
	}

	var changeBaseline *planner.ChangeBaseline
	var changePlan *planner.ChangePlan
	if parsedProfile.Profile.Name == profile.NameChangeReview {
		plannerCtx := ctx.Ctx
		if plannerCtx == nil {
			plannerCtx = context.Background()
		}
		baseline, baselineErr := planner.ResolveChangeBaseline(
			plannerCtx, ctx.CodesPath, *parsedProfile.Profile.BasePolicy, ctx.Summary.StartTime,
		)
		if baselineErr != nil {
			return failChangeBaseline(ctx, parsedProfile.Profile.Name, baselineErr)
		}
		changePlan, baselineErr = planner.BuildChangePlan(
			plannerCtx, ctx.CodesPath, parsedProfile.Profile, parsedProfile.Hash, baseline,
		)
		if baselineErr != nil {
			return failChangeBaseline(ctx, parsedProfile.Profile.Name, baselineErr)
		}
		baseline.DiffManifestHash = changePlan.PrimaryManifestHash()
		changeBaseline = &baseline
		ctx.Report.ChangeCutoffAt = &baseline.CutoffAt
		if !baseline.BaseCommitTime.IsZero() {
			ctx.Report.BaseCommitTime = &baseline.BaseCommitTime
		}
		ctx.Report.BaseCommit = baseline.BaseCommit
		ctx.Report.DiffManifestHash = baseline.DiffManifestHash
		if models.DB != nil {
			updates := map[string]interface{}{
				"base_commit":        baseline.BaseCommit,
				"change_cutoff_at":   baseline.CutoffAt,
				"diff_manifest_hash": baseline.DiffManifestHash,
			}
			if !baseline.BaseCommitTime.IsZero() {
				updates["base_commit_time"] = baseline.BaseCommitTime
			}
			if _, err := models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, updates); err != nil {
				MarkFailed(ctx, err.Error())
				return nil, nil, engines.ParsedEngineProfile{}, err
			}
		}
	}

	scopeDecision, scanPlan, primaryPlan, planErr := planner.Build(planner.Input{
		CodesPath:     ctx.CodesPath,
		Profile:       parsedProfile.Profile,
		ProfileHash:   parsedProfile.Hash,
		Config:        parsedProfile.Config,
		ScanStartedAt: ctx.Summary.StartTime,
		TargetScope:   targetScope,
		ChangePlan:    changePlan,
	})
	if planErr != nil {
		MarkFailed(ctx, planErr.Error())
		return nil, nil, engines.ParsedEngineProfile{}, planErr
	}

	ctx.Summary.ScopeDecision = &scopeDecision
	if changeBaseline != nil {
		scopeDecision.ChangeBaseline = changeBaseline
		scopeDecision.ChangeHunks = changePlan.PrimaryCount()
		scopeDecision.PlanManifestHash = changePlan.PrimaryManifestHash()
		scopeDecision.ChangeOverview = changePlan.Overview()
	}
	switch scopeDecision.Decision {
	case planner.DecisionFailed:
		planFailure := fmt.Errorf("scope planner failed: %s: %s", scopeDecision.Reason, scopeDecision.Message)
		MarkFailed(ctx, planFailure.Error())
		return nil, nil, engines.ParsedEngineProfile{}, planFailure
	case planner.DecisionSkipped:
		ctx.Summary.Status = models.StatusSkipped
		WriteSummaryReport(ctx)
		return nil, nil, engines.ParsedEngineProfile{}, ErrSkipped
	default:
		return scanPlan, primaryPlan, parsedProfile, nil
	}
}

func failChangeBaseline(
	ctx *TaskContext,
	scopeProfile string,
	baselineErr error,
) (*coverage.ScanPlan, planner.PrimaryPlan, engines.ParsedEngineProfile, error) {
	reason := planner.ReasonInvalidBase
	if errors.Is(baselineErr, planner.ErrInsufficientHistory) {
		reason = planner.ReasonInsufficientHistory
	}
	scopeDecision := planner.ScopeDecision{
		Decision:     planner.DecisionFailed,
		Reason:       reason,
		ScopeProfile: scopeProfile,
		Message:      baselineErr.Error(),
	}
	ctx.Summary.ScopeDecision = &scopeDecision
	MarkFailed(ctx, baselineErr.Error())
	return nil, nil, engines.ParsedEngineProfile{}, baselineErr
}
