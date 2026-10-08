package migration

import (
	"os"
	"path/filepath"
	"testing"

	"code-shield/services/engines/profile"
	"encoding/json"
)

func TestBuiltinTaskMetaProfilesAreValid(t *testing.T) {
	entries, err := os.ReadDir("../../tasks")
	if err != nil {
		t.Fatalf("read builtin tasks: %v", err)
	}
	found := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../../tasks", entry.Name(), "meta.json"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		configRaw, err := migrateTaskMeta(raw, entry.Name())
		if err != nil {
			t.Fatalf("migrate %s: %v", entry.Name(), err)
		}
		if _, _, err := profile.Parse(configRaw); err != nil {
			t.Fatalf("profile %s: %v", entry.Name(), err)
		}
		found++
	}
	if found == 0 {
		t.Fatal("no builtin task metas found")
	}
}

func TestMigrateTaskMetaCanonicalizesOccurrenceProfile(t *testing.T) {
	raw := []byte(`{
		"name": "legacy_occurrence",
		"engine_mode": "debate_full",
		"engine_config": {
			"scan_profile": {
				"version": 1,
				"name": "keyword_review",
				"content_keywords": ["pthread_create"],
				"primary_unit": "keyword_occurrence"
			}
		}
	}`)
	configRaw, err := migrateTaskMeta(raw, "legacy_occurrence")
	if err != nil {
		t.Fatalf("migrateTaskMeta() error = %v", err)
	}
	parsed, _, err := profile.Parse(configRaw)
	if err != nil {
		t.Fatalf("profile.Parse() error = %v", err)
	}
	if parsed.Name != profile.NameOccurrenceReview {
		t.Fatalf("profile name = %q, want %q", parsed.Name, profile.NameOccurrenceReview)
	}
}

func TestMigrateTaskMetaCanonicalizesLegacyEntityProfile(t *testing.T) {
	raw := []byte(`{
		"name": "legacy_test_entity",
		"engine_mode": "debate_full",
		"target_scope": "test",
		"engine_config": {
			"scan_profile": {
				"version": 1,
				"name": "test_entity_review",
				"target_scope": "test",
				"languages": ["cpp", "go"]
			}
		}
	}`)
	configRaw, err := migrateTaskMeta(raw, "legacy_test_entity")
	if err != nil {
		t.Fatalf("migrateTaskMeta() error = %v", err)
	}
	var config struct {
		ScanProfile profile.ScanProfile `json:"scan_profile"`
	}
	if err := json.Unmarshal(configRaw, &config); err != nil {
		t.Fatal(err)
	}
	if config.ScanProfile.Name != profile.NameEntityReview || config.ScanProfile.EntityKind != "test_case" {
		t.Fatalf("unexpected canonical entity profile: %+v", config.ScanProfile)
	}
	if _, _, err := profile.Parse(configRaw); err != nil {
		t.Fatalf("canonical profile is invalid: %v", err)
	}
}

func TestMigrateTaskMetaActivatesChangeReview(t *testing.T) {
	raw := []byte(`{
		"name": "change_review",
		"engine_mode": "debate_full",
		"engine_config": {
			"scan_profile": {
				"version": 1,
				"name": "full_review",
				"target_scope": "business",
				"exclude_paths": ["thirdparts"]
			}
		}
	}`)
	configRaw, err := migrateTaskMeta(raw, "change_review")
	if err != nil {
		t.Fatalf("migrateTaskMeta() error = %v", err)
	}
	parsed, _, err := profile.Parse(configRaw)
	if err != nil {
		t.Fatalf("profile.Parse() error = %v", err)
	}
	if parsed.Name != profile.NameChangeReview || parsed.ScopePolicy != "changed_hunks" ||
		parsed.ContextPolicy != "changed_files" || parsed.BasePolicy == nil || parsed.BasePolicy.SinceDays != 7 {
		t.Fatalf("unexpected change review profile: %+v", parsed)
	}
}

func TestBuiltinTaskMetasDefineSemanticMetadata(t *testing.T) {
	entries, err := os.ReadDir("../../tasks")
	if err != nil {
		t.Fatalf("read builtin tasks: %v", err)
	}
	found := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../../tasks", entry.Name(), "meta.json"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var meta struct {
			TargetSemantics  map[string]string `json:"target_semantics"`
			DisplaySemantics map[string]string `json:"display_semantics"`
		}
		var probe struct {
			DomainLabel string `json:"domain_label"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			t.Fatalf("unmarshal %s domain label: %v", entry.Name(), err)
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			t.Fatalf("unmarshal %s semantics: %v", entry.Name(), err)
		}
		if probe.DomainLabel == "" || meta.TargetSemantics["singular"] == "" || meta.TargetSemantics["plural"] == "" ||
			meta.DisplaySemantics["domain_label"] == "" || meta.DisplaySemantics["target_label"] == "" {
			t.Fatalf("task %s semantic metadata incomplete: domain_label=%q target=%+v display=%+v",
				entry.Name(), probe.DomainLabel, meta.TargetSemantics, meta.DisplaySemantics)
		}
		found++
	}
	if found == 0 {
		t.Fatal("no builtin task metas found")
	}
}
