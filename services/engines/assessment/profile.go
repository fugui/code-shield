package assessment

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"sync"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/profile"
)

type Descriptor struct {
	Name               string
	DisplayName        string
	UnitKind           coverage.PlanUnitKind
	ArtifactSchema     string
	SupportedLanguages []string
}

type PlanContext struct {
	EngineContext *engines.EngineContext
	Config        engines.ChunkConfig
	Profile       profile.ScanProfile
}

type PrimaryPlan struct {
	BundleID     string
	UnitIDs      []string
	Units        []coverage.PlanUnit
	ManifestHash string
}

type Bundle struct {
	ID       string
	ParentID string
	Name     string
	Units    []coverage.PlanUnit
	AllFiles []string
	Depth    int
}

type AssessmentContext struct {
	EngineContext     *engines.EngineContext
	BundleID          string
	Attempt           int
	AllowedCategories []string
}

type PlanView struct {
	BundleID          string
	Units             []coverage.PlanUnit
	AllowedCategories []string
	PromptRefs        *PromptRefSession
}

type AssessmentResult struct {
	Artifact  AssessmentArtifact
	Valid     []UnitAssessment
	Invalid   []FailedUnit
	Missing   []string
	Unknown   []string
	Duplicate []string
}

type UnitReconciliation struct {
	coverage.PlanReconciliation
	DuplicateUnits []string
}

type Planner interface {
	BuildBundles(ctx PlanContext, units []coverage.PlanUnit) ([]chunker.SemanticBundle, error)
	BuildRepairBundles(ctx PlanContext, parent chunker.SemanticBundle, failedUnits []string, depth int) ([]chunker.SemanticBundle, error)
	PluginPlan(ctx PlanContext, bundle chunker.SemanticBundle) []coverage.PlannedFile
}

type PromptBuilder interface {
	BuildPrompt(ctx AssessmentContext, bundle Bundle) (string, *PromptRefSession, error)
}

type ContractProvider interface {
	Contract() OutputContract
}

type ArtifactNormalizer interface {
	Normalize(raw string, session *PromptRefSession) (AssessmentArtifact, error)
}

type ArtifactValidator interface {
	Validate(plan PlanView, artifact AssessmentArtifact) (AssessmentResult, error)
}

type UnitReconciler interface {
	Reconcile(plan PlanView, result AssessmentResult) UnitReconciliation
}

type FindingMapper interface {
	MapFindings(ctx AssessmentContext, bundle Bundle, result AssessmentResult) ([]models.AnalysisFinding, error)
}

type StatusMapper interface {
	OutcomeForStatus(status string) (AssessmentOutcome, bool)
	OutcomeForArtifact(artifact AssessmentArtifact) (AssessmentOutcome, bool)
}

type OutputContract struct {
	SchemaID          string
	TopLevel          string
	RequiredFields    []string
	ForbiddenTopLevel []string
	AllowedOutcomes   []string
	AllowedCategories []string
	Example           string
}

type ProfileRegistration struct {
	Descriptor  Descriptor
	Planner     Planner
	Contract    ContractProvider
	Prompt      PromptBuilder
	Normalize   ArtifactNormalizer
	Validate    ArtifactValidator
	Reconcile   UnitReconciler
	MapFindings FindingMapper
	Outcome     StatusMapper
}

type Registry struct {
	mu       sync.RWMutex
	profiles *FacetRegistry[ProfileRegistration]
	frozen   bool
}

func NewRegistry() *Registry {
	return &Registry{profiles: NewFacetRegistry[ProfileRegistration]()}
}

func (registry *Registry) Register(registration ProfileRegistration) error {
	if registration.Descriptor.Name == "" {
		return fmt.Errorf("%w: descriptor name is empty", ErrProfileRegistry)
	}
	if registration.Planner == nil || registration.Prompt == nil || registration.Normalize == nil || registration.Validate == nil ||
		registration.Reconcile == nil || registration.MapFindings == nil || registration.Outcome == nil {
		return fmt.Errorf("%w: profile %q is incomplete", ErrProfileRegistry, registration.Descriptor.Name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.frozen {
		return fmt.Errorf("%w: registry is frozen", ErrProfileRegistry)
	}
	return registry.profiles.Register(registration.Descriptor.Name, registration)
}

func (registry *Registry) Get(name string) (ProfileRegistration, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.profiles.Get(name)
}

func (registry *Registry) Names() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.profiles.Names()
}

func (registry *Registry) Freeze() error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.frozen {
		return fmt.Errorf("%w: registry is already frozen", ErrProfileRegistry)
	}
	registry.frozen = true
	return registry.profiles.Freeze()
}

func ResolveProfileName(assessmentConfig json.RawMessage, scanProfileName string) string {
	if len(assessmentConfig) != 0 {
		var envelope struct {
			Profile string `json:"profile"`
		}
		if err := json.Unmarshal(assessmentConfig, &envelope); err == nil && envelope.Profile != "" {
			return envelope.Profile
		}
	}
	switch scanProfileName {
	case profile.NameOccurrenceReview, profile.NameKeywordReview:
		return "occurrencereview"
	case profile.NameEntityReview, profile.NameTestEntityReview:
		return "entityreview"
	case profile.NameChangeReview:
		return "changereview"
	default:
		return ""
	}
}

type FacetRegistry[T any] struct {
	frozen bool
	facets map[string]T
}

func NewFacetRegistry[T any]() *FacetRegistry[T] {
	return &FacetRegistry[T]{facets: make(map[string]T)}
}

func (registry *FacetRegistry[T]) Register(name string, facet T) error {
	if name == "" {
		return fmt.Errorf("%w: facet name is empty", ErrProfileRegistry)
	}
	if registry.frozen {
		return fmt.Errorf("%w: facet %q registered after freeze", ErrProfileRegistry, name)
	}
	if _, exists := registry.facets[name]; exists {
		return fmt.Errorf("%w: facet %q is already registered", ErrProfileRegistry, name)
	}
	registry.facets[name] = facet
	return nil
}

func (registry *FacetRegistry[T]) Get(name string) (T, error) {
	facet, exists := registry.facets[name]
	if !exists {
		var zero T
		return zero, fmt.Errorf("%w: facet %q is not registered", ErrProfileRegistry, name)
	}
	return facet, nil
}

func (registry *FacetRegistry[T]) Names() []string {
	names := make([]string, 0, len(registry.facets))
	for name := range registry.facets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (registry *FacetRegistry[T]) Freeze() error {
	if registry.frozen {
		return fmt.Errorf("%w: registry is already frozen", ErrProfileRegistry)
	}
	registry.frozen = true
	return nil
}

func (registry *FacetRegistry[T]) Snapshot() map[string]T {
	return maps.Clone(registry.facets)
}

var ErrProfileRegistry = fmt.Errorf("assessment profile registry")
