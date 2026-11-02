package defectlifecycle

import "code-shield/models"

func RuntimeMatchBudget() MatchBudget {
	return MatchBudget{
		MaxCandidates:      models.AppConfig.Governance.Identity.MaxCandidatesPerObservation,
		MaxEdges:           models.AppConfig.Governance.Identity.MaxCandidateEdgesPerReport,
		MaxAICalls:         models.AppConfig.Governance.Arbitration.MaxCallsPerReport,
		MaxAssignmentWidth: models.AppConfig.Governance.Identity.MaxAIAssignmentWidth,
	}
}

func RuntimeMatchThresholds() MatchThresholds {
	return MatchThresholds{
		StrongSame:         models.AppConfig.Governance.Identity.StrongSameThreshold,
		AssignBand:         models.AppConfig.Governance.Identity.AssignBand,
		RejectBelow:        models.AppConfig.Governance.Identity.RejectBelow,
		AutoResolve:        models.AppConfig.GrayZoneAutoResolveEnabled(),
		AIConfidence:       models.AppConfig.Governance.Identity.AIArbitrationConfidence,
		FallbackMergeScore: models.AppConfig.Governance.Identity.GrayZoneFallbackMergeScore,
	}
}

func RuntimeLifecyclePolicy() LifecyclePolicy {
	return LifecyclePolicy{
		HighRiskSeverities:   append([]string(nil), models.AppConfig.Governance.Lifecycle.HighRiskSeverities...),
		ResolvedRounds:       models.AppConfig.Governance.Lifecycle.ResolvedRounds,
		DormantThreshold:     models.AppConfig.Governance.Lifecycle.DormantThreshold,
		ObsoleteAfterDormant: models.AppConfig.Governance.Lifecycle.ObsoleteAfterDormant,
		RequireCoverage:      models.AppConfig.Governance.Lifecycle.RequireCoverage,
		RequireChange:        models.AppConfig.Governance.Lifecycle.RequireChange,
		AutoResolveMissing:   models.AppConfig.Governance.Lifecycle.AutoResolveMissingEnabled(),
	}
}
