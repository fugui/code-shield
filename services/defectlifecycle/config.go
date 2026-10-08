package defectlifecycle

import "code-shield/models"

func RuntimeMatchBudget() MatchBudget {
	return MatchBudget{
		MaxCandidates:      models.AppConfig.Identity.MaxCandidatesPerObservation,
		MaxEdges:           models.AppConfig.Identity.MaxCandidateEdgesPerReport,
		MaxAICalls:         models.AppConfig.Arbitration.MaxCallsPerReport,
		MaxAssignmentWidth: models.AppConfig.Identity.MaxAIAssignmentWidth,
	}
}

func RuntimeMatchThresholds() MatchThresholds {
	return MatchThresholds{
		StrongSame:         models.AppConfig.Identity.StrongSameThreshold,
		AssignBand:         models.AppConfig.Identity.AssignBand,
		RejectBelow:        models.AppConfig.Identity.RejectBelow,
		AutoResolve:        models.AppConfig.GrayZoneAutoResolveEnabled(),
		AIConfidence:       models.AppConfig.Identity.AIArbitrationConfidence,
		FallbackMergeScore: models.AppConfig.Identity.GrayZoneFallbackMergeScore,
	}
}

func RuntimeLifecyclePolicy() LifecyclePolicy {
	return LifecyclePolicy{
		HighRiskSeverities:   append([]string(nil), models.AppConfig.Lifecycle.HighRiskSeverities...),
		ResolvedRounds:       models.AppConfig.Lifecycle.ResolvedRounds,
		DormantThreshold:     models.AppConfig.Lifecycle.DormantThreshold,
		ObsoleteAfterDormant: models.AppConfig.Lifecycle.ObsoleteAfterDormant,
		RequireCoverage:      models.AppConfig.Lifecycle.RequireCoverage,
		RequireChange:        models.AppConfig.Lifecycle.RequireChange,
	}
}
