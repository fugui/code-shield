package engines

import "testing"

func TestAggregateArtifactCompleteTriState(t *testing.T) {
	tests := []struct {
		name    string
		details []ChunkDetails
		want    bool
		state   string
	}{
		{name: "all nil", details: []ChunkDetails{{}, {}}, want: true, state: "legacy_success"},
		{name: "nil plus true", details: []ChunkDetails{{}, {ArtifactComplete: boolPtr(true)}}, want: true, state: "observed_complete"},
		{name: "nil plus false", details: []ChunkDetails{{}, {ArtifactComplete: boolPtr(false)}}, want: false, state: "observed_incomplete"},
		{name: "true plus true", details: []ChunkDetails{{ArtifactComplete: boolPtr(true)}, {ArtifactComplete: boolPtr(true)}}, want: true, state: "observed_complete"},
		{name: "false plus true", details: []ChunkDetails{{ArtifactComplete: boolPtr(false)}, {ArtifactComplete: boolPtr(true)}}, want: false, state: "observed_incomplete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, state := AggregateArtifactComplete(tt.details)
			if got != tt.want || state != tt.state {
				t.Fatalf("AggregateArtifactComplete() = (%v, %q), want (%v, %q)", got, state, tt.want, tt.state)
			}
		})
	}
}

func TestGetEngineStrictRejectsUnknownMode(t *testing.T) {
	RegisterEngine("test_engine", stubEngine{})
	if !EngineExists("test_engine") {
		t.Fatal("test_engine must be registered")
	}
	if engine, err := GetEngineStrict("test_engine"); err != nil || engine == nil {
		t.Fatalf("GetEngineStrict(test_engine) = (%v, %v), want non-nil engine and nil error", engine, err)
	}

	engine, err := GetEngineStrict("not_a_real_engine")
	if err == nil || engine != nil {
		t.Fatalf("GetEngineStrict(unknown) = (%v, %v), want nil engine and error", engine, err)
	}
	if EngineExists("not_a_real_engine") {
		t.Fatal("unknown mode must not be registered")
	}
}

type stubEngine struct{}

func (stubEngine) Name() string { return "test_engine" }

func (stubEngine) Run(*EngineContext) (*EngineResult, error) { return nil, nil }

func boolPtr(value bool) *bool {
	return &value
}
