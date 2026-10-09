package defectlifecycle

import (
	"time"

	"code-shield/models"
	"code-shield/services/coverage"

	"gorm.io/gorm"
)

const (
	AlgorithmVersion = "v1"

	CoverageUnknown       = "UNKNOWN"
	CoverageComplete      = "COMPLETE"
	CoveragePartial       = "PARTIAL"
	CoverageChangeFocus   = "CHANGE_FOCUS"
	CoverageNotApplicable = "NOT_APPLICABLE"

	ScopePlanned          = "PLANNED"
	ScopeScanned          = "SCANNED"
	ScopeFailed           = "FAILED"
	ScopeExcluded         = "EXCLUDED"
	ScopeUnchangedSkipped = "UNCHANGED_SKIPPED"
	ScopeUnknown          = "UNKNOWN"

	AnchorHigh   = "HIGH"
	AnchorMedium = "MEDIUM"
	AnchorLow    = "LOW"
)

type ScanInput struct {
	DB                 *gorm.DB
	Report             models.TaskReport
	Repo               models.Repository
	RepoRoot           string
	TaskType           models.TaskType
	Findings           []models.AnalysisFinding
	Coverage           *coverage.Coverage
	CoverageState      string
	Exclusions         []string
	AlgorithmVersion   string
	Arbitrator         ArbitrationProvider
	RenameTargets      map[string]string
	StabilizedFindings []StabilizedFindingDTO
}

type ScanFactsResult struct {
	Findings      []models.AnalysisFinding
	Scope         []models.ScanScopeEntry
	ScopeHash     string
	CoverageState string
	WorktreeClean bool
	CommittedAt   time.Time
	Observations  []models.DefectObservation
	Events        []models.DefectEvent
	NewDefectIDs  []uint
	LedgerReady   bool
}

type Identity struct {
	RepoID           uint
	TaskTypeID       uint
	NormPath         string
	SymbolPath       string
	ScopeKind        string
	Arity            int
	ScopeKey         string
	StmtShape        string
	OccurrenceIndex  int
	DefectClassMajor string
	LineStart        int
	LineEnd          int
	CleanToken       string
	PrevShape        string
	NextShape        string
	ScopeBodyHash    string
	Severity         string
	Confidence       string
	RootFamily       string
	ResourceIdentity string
	ValidationChain  string
	K1               string
	K2               string
	F1               string
	F2               string
	F3               string
}

type ObservationGroup struct {
	UID            string
	Representative models.AnalysisFinding
	Findings       []models.AnalysisFinding
	Identity       Identity
}
