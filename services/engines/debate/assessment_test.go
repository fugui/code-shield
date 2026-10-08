package debate

import (
	"testing"

	"code-shield/services/engines/assessment"
	"code-shield/services/engines/profile"
)

func TestResolveAssessmentProfileNames(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		scanName string
		wantName string
	}{
		{name: "config wins", config: `{"profile":"custom"}`, scanName: profile.NameKeywordReview, wantName: "custom"},
		{name: "canonical occurrence", scanName: profile.NameOccurrenceReview, wantName: "occurrencereview"},
		{name: "legacy occurrence", scanName: profile.NameKeywordReview, wantName: "occurrencereview"},
		{name: "legacy entity", scanName: profile.NameTestEntityReview, wantName: "entityreview"},
		{name: "legacy change", scanName: profile.NameChangeReview, wantName: "changereview"},
		{name: "full review", scanName: profile.NameFullReview, wantName: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := assessment.ResolveProfileName([]byte(test.config), test.scanName); got != test.wantName {
				t.Fatalf("ResolveProfileName() = %q, want %q", got, test.wantName)
			}
		})
	}
}
