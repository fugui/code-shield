package profiles

import (
	"sync"

	"code-shield/services/engines/assessment"
	"code-shield/services/engines/assessment/profiles/changereview"
	"code-shield/services/engines/assessment/profiles/entityreview"
	"code-shield/services/engines/assessment/profiles/occurrencereview"
)

var (
	registryOnce sync.Once
	registry     *assessment.Registry
	registryErr  error
)

func Registry() (*assessment.Registry, error) {
	registryOnce.Do(func() {
		registry = assessment.NewRegistry()
		registryErr = registry.Register(entityreview.Registration())
		if registryErr == nil {
			registryErr = registry.Register(changereview.Registration())
		}
		if registryErr == nil {
			registryErr = registry.Register(occurrencereview.Registration())
		}
		if freezeErr := registry.Freeze(); freezeErr != nil && registryErr == nil {
			registryErr = freezeErr
		}
	})
	if registryErr != nil {
		return nil, registryErr
	}
	return registry, nil
}

func OutcomeForStatus(status string) (assessment.AssessmentOutcome, bool) {
	profileRegistry, err := Registry()
	if err != nil {
		return "", false
	}
	for _, name := range profileRegistry.Names() {
		registration, err := profileRegistry.Get(name)
		if err != nil {
			continue
		}
		if outcome, ok := registration.Outcome.OutcomeForStatus(status); ok {
			return outcome, true
		}
	}
	return "", false
}
