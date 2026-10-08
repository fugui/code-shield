package defectlifecycle

type LifecyclePolicy struct {
	HighRiskSeverities   []string
	ResolvedRounds       int
	DormantThreshold     int
	ObsoleteAfterDormant int
	RequireCoverage      bool
	RequireChange        bool
}
