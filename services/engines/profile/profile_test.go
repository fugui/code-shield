package profile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseProfiles(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantName    string
		wantPrimary string
	}{
		{
			name:     "full review",
			raw:      `{"scan_profile":{"version":1,"name":"full_review","target_scope":"business","exclude_paths":["thirdparts"]}}`,
			wantName: NameFullReview,
		},
		{
			name:        "keyword occurrence review",
			raw:         `{"scan_profile":{"version":1,"name":"keyword_review","languages":["cpp"],"target_scope":"business","content_keywords":["pthread_create"],"primary_unit":"keyword_occurrence"}}`,
			wantName:    NameOccurrenceReview,
			wantPrimary: PrimaryUnitKeywordOccurrence,
		},
		{
			name:        "keyword file review defaults primary unit",
			raw:         `{"scan_profile":{"version":1,"name":"keyword_review","content_keywords":["cJSON_"]}}`,
			wantName:    NameOccurrenceReview,
			wantPrimary: PrimaryUnitFile,
		},
		{
			name:     "change review",
			raw:      `{"scan_profile":{"version":1,"name":"change_review","languages":["cpp","python","java"],"scope_policy":"changed_hunks","context_policy":"changed_files","base_policy":{"strategy":"since_days","since_days":7}}}`,
			wantName: NameChangeReview,
		},
		{
			name:     "legacy test entity review",
			raw:      `{"scan_profile":{"version":1,"name":"test_entity_review","target_scope":"test","languages":["cpp","python","java","go"]}}`,
			wantName: NameEntityReview,
		},
		{
			name:     "canonical entity review",
			raw:      `{"scan_profile":{"version":1,"name":"entity_review","target_scope":"test","languages":["cpp","python","java","go"],"entity_kind":"test_case"}}`,
			wantName: NameEntityReview,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, hash, err := Parse(json.RawMessage(test.raw))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if profile.Name != test.wantName {
				t.Fatalf("name = %q, want %q", profile.Name, test.wantName)
			}
			if test.wantPrimary != "" && profile.PrimaryUnit != test.wantPrimary {
				t.Fatalf("primary unit = %q, want %q", profile.PrimaryUnit, test.wantPrimary)
			}
			if !strings.HasPrefix(hash, "sha256:") {
				t.Fatalf("hash = %q, want sha256 prefix", hash)
			}
		})
	}
}

func TestParseRejectsInvalidProfiles(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		reason string
	}{
		{name: "missing scan profile", raw: `{"since_days":7}`, reason: "scan_profile is required"},
		{name: "unknown name", raw: `{"scan_profile":{"version":1,"name":"custom"}}`, reason: "unsupported scan_profile.name"},
		{name: "unknown version", raw: `{"scan_profile":{"version":2,"name":"full_review"}}`, reason: "unsupported scan_profile.version"},
		{name: "unknown field", raw: `{"scan_profile":{"version":1,"name":"full_review","new_field":1}}`, reason: "unknown scan_profile field"},
		{name: "forbidden keyword field", raw: `{"scan_profile":{"version":1,"name":"full_review","content_keywords":["x"]}}`, reason: "forbidden"},
		{name: "keyword missing", raw: `{"scan_profile":{"version":1,"name":"keyword_review"}}`, reason: "content_keywords is required"},
		{name: "change with go", raw: `{"scan_profile":{"version":1,"name":"change_review","languages":["cpp","python","go"],"scope_policy":"changed_hunks","context_policy":"changed_files","base_policy":{"strategy":"since_days","since_days":7}}}`, reason: "must be"},
		{name: "test entity business scope", raw: `{"scan_profile":{"version":1,"name":"test_entity_review","target_scope":"business"}}`, reason: "target_scope must be test"},
		{name: "legacy top level field", raw: `{"scan_profile":{"version":1,"name":"full_review"},"exclude_paths":[]}`, reason: "outside scan_profile"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := Parse(json.RawMessage(test.raw))
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), test.reason)
			}
		})
	}
}

func TestHashIsStable(t *testing.T) {
	first := ScanProfile{Version: 1, Name: NameOccurrenceReview, ContentKeywords: []string{"pthread_create"}}
	second := ScanProfile{Version: 1, Name: NameOccurrenceReview, ContentKeywords: []string{"pthread_create"}, PrimaryUnit: ""}
	third := ScanProfile{Version: 1, Name: NameOccurrenceReview, ContentKeywords: []string{"pthread_create"}, PrimaryUnit: PrimaryUnitFile}
	legacy := ScanProfile{Version: 1, Name: NameKeywordReview, ContentKeywords: []string{"pthread_create"}}
	firstHash := Hash(first)
	if firstHash != Hash(legacy) {
		t.Fatalf("legacy occurrence hash differs: %s %s", firstHash, Hash(legacy))
	}

	if firstHash != Hash(second) || firstHash != Hash(third) {
		t.Fatalf("canonical hashes differ: %s %s %s", firstHash, Hash(second), Hash(third))
	}

	canonicalEntity := ScanProfile{Version: 1, Name: NameEntityReview, TargetScope: "test", Languages: []string{LanguageGo}, EntityKind: "test_case"}
	legacyEntity := ScanProfile{Version: 1, Name: NameTestEntityReview, TargetScope: "test", Languages: []string{LanguageGo}}
	canonicalEntityHash := Hash(canonicalEntity)
	if canonicalEntityHash != Hash(legacyEntity) {
		t.Fatalf("legacy entity hash differs: %s %s", canonicalEntityHash, Hash(legacyEntity))
	}
	first.ContentKeywords[0] = "std::thread"
	if firstHash == Hash(first) {
		t.Fatal("changed profile should have a different hash")
	}
}
