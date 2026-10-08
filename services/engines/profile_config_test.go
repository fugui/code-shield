package engines

import (
	"encoding/json"
	"reflect"
	"testing"

	"code-shield/services/engines/profile"
)

func TestParseProfileConfigProjectsScanProfile(t *testing.T) {
	raw := json.RawMessage(`{
		"scan_profile": {
			"version": 1,
			"name": "keyword_review",
			"include_extensions": [".cpp"],
			"exclude_paths": ["thirdparts"],
			"content_keywords": ["pthread_create"]
		}
	}`)

	parsed, err := ParseProfileConfig(raw)
	if err != nil {
		t.Fatalf("ParseProfileConfig() error = %v", err)
	}
	if parsed.Profile.Name != profile.NameOccurrenceReview {
		t.Fatalf("unexpected profile: %+v", parsed.Profile)
	}
	want := ChunkConfig{
		MaxFiles:        DefaultChunkMaxFiles,
		Depth:           DefaultChunkDepth,
		FileExtensions:  []string{".cpp"},
		ContentKeywords: []string{"pthread_create"},
		ExcludePaths:    []string{"thirdparts"},
	}
	if !reflect.DeepEqual(parsed.Config, want) {
		t.Fatalf("ChunkConfig = %+v, want %+v", parsed.Config, want)
	}
	if parsed.Hash == "" {
		t.Fatal("profile hash is empty")
	}
}

func TestParseProfileConfigFullReviewDefaultsAndLimits(t *testing.T) {
	parsed, err := ParseProfileConfig(json.RawMessage(`{
		"scan_profile": {"version": 1, "name": "full_review", "max_files": 4, "depth": 2}
	}`))
	if err != nil {
		t.Fatalf("ParseProfileConfig() error = %v", err)
	}
	if parsed.Config.MaxFiles != 4 || parsed.Config.Depth != 2 {
		t.Fatalf("unexpected config: %+v", parsed.Config)
	}
}

func TestParseProfileConfigRejectsEmptyConfig(t *testing.T) {
	if _, err := ParseProfileConfig(nil); err == nil {
		t.Fatal("ParseProfileConfig(nil) = nil, want error")
	}
}
