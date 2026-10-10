package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	NameFullReview       = "full_review"
	NameOccurrenceReview = "occurrence_review"
	NameKeywordReview    = "keyword_review"
	NameChangeReview     = "change_review"
	NameEntityReview     = "entity_review"
	NameTestEntityReview = "test_entity_review"

	PrimaryUnitFile              = "file"
	PrimaryUnitKeywordOccurrence = "keyword_occurrence"

	LanguageCPP    = "cpp"
	LanguagePython = "python"
	LanguageJava   = "java"
	LanguageGo     = "go"
)

type BasePolicy struct {
	Strategy  string `json:"strategy"`
	SinceDays int    `json:"since_days"`
}

type ScanProfile struct {
	Version           int         `json:"version"`
	Name              string      `json:"name"`
	Languages         []string    `json:"languages,omitempty"`
	TargetScope       string      `json:"target_scope,omitempty"`
	ScopePolicy       string      `json:"scope_policy,omitempty"`
	ContextPolicy     string      `json:"context_policy,omitempty"`
	BasePolicy        *BasePolicy `json:"base_policy,omitempty"`
	IncludeExtensions []string    `json:"include_extensions,omitempty"`
	ExcludePaths      []string    `json:"exclude_paths,omitempty"`
	ContentKeywords   []string    `json:"content_keywords,omitempty"`
	PrimaryUnit       string      `json:"primary_unit,omitempty"`
	EntityKind        string      `json:"entity_kind,omitempty"`
	MaxFiles          int         `json:"max_files,omitempty"`
	Depth             int         `json:"depth,omitempty"`
}

type EngineConfig struct {
	ScanProfile ScanProfile `json:"scan_profile"`
}

type fieldRule struct {
	optional   map[string]bool
	forbidden  map[string]bool
	validate   func(ScanProfile) error
	normalizer func(*ScanProfile)
}

var fieldNames = []string{
	"version", "name", "languages", "target_scope", "scope_policy", "context_policy",
	"base_policy", "include_extensions", "exclude_paths", "content_keywords",
	"primary_unit", "entity_kind", "max_files", "depth",
}

func rules() map[string]fieldRule {
	return map[string]fieldRule{
		NameFullReview: {
			optional: set("languages", "target_scope", "include_extensions", "exclude_paths", "max_files", "depth"),
			forbidden: set(
				"scope_policy", "context_policy", "base_policy", "content_keywords",
				"primary_unit", "entity_kind",
			),
		},
		NameOccurrenceReview: occurrenceReviewRule(),
		NameKeywordReview:    occurrenceReviewRule(),
		NameChangeReview: {
			optional: set("languages", "exclude_paths", "scope_policy", "context_policy", "base_policy"),
			forbidden: set(
				"target_scope", "include_extensions", "content_keywords",
				"primary_unit", "entity_kind", "max_files", "depth",
			),
			validate: func(p ScanProfile) error {
				if !slices.Equal(p.Languages, []string{LanguageCPP, LanguagePython, LanguageJava}) {
					return fmt.Errorf("scan_profile.languages must be %q for %s", []string{LanguageCPP, LanguagePython, LanguageJava}, NameChangeReview)
				}
				if p.ScopePolicy != "changed_hunks" || p.ContextPolicy != "changed_files" {
					return fmt.Errorf("scan_profile.scope_policy/context_policy are required for %s", NameChangeReview)
				}
				if p.BasePolicy == nil || p.BasePolicy.Strategy != "since_days" || p.BasePolicy.SinceDays <= 0 {
					return fmt.Errorf("scan_profile.base_policy.strategy must be since_days with a positive since_days")
				}
				return nil
			},
		},
		NameEntityReview: {
			optional:  set("languages", "exclude_paths", "target_scope", "entity_kind"),
			forbidden: set("scope_policy", "context_policy", "base_policy", "include_extensions", "content_keywords", "primary_unit", "max_files", "depth"),
			validate: func(p ScanProfile) error {
				if p.TargetScope != "" && p.TargetScope != "test" && p.TargetScope != "business" && p.TargetScope != "all" {
					return fmt.Errorf("scan_profile.target_scope must be test, business or all for %s", NameEntityReview)
				}
				for _, language := range p.Languages {
					if !slices.Contains([]string{LanguageCPP, LanguagePython, LanguageJava, LanguageGo}, language) {
						return fmt.Errorf("unsupported test entity language %q", language)
					}
				}
				return nil
			},
			normalizer: func(p *ScanProfile) {
				if p.TargetScope == "" {
					p.TargetScope = "test"
				}
				if p.EntityKind == "" {
					p.EntityKind = "test_case"
				}
			},
		},
	}
}

func occurrenceReviewRule() fieldRule {
	return fieldRule{
		optional: set(
			"languages", "target_scope", "include_extensions", "exclude_paths",
			"content_keywords", "primary_unit", "scope_policy", "context_policy", "base_policy",
		),
		validate: func(p ScanProfile) error {
			if len(p.ContentKeywords) == 0 {
				return fmt.Errorf("scan_profile.content_keywords is required for %s", NameOccurrenceReview)
			}
			if p.PrimaryUnit != PrimaryUnitFile && p.PrimaryUnit != PrimaryUnitKeywordOccurrence {
				return fmt.Errorf("scan_profile.primary_unit must be %q or %q", PrimaryUnitFile, PrimaryUnitKeywordOccurrence)
			}
			return nil
		},
		normalizer: func(p *ScanProfile) {
			if p.PrimaryUnit == "" {
				p.PrimaryUnit = PrimaryUnitFile
			}
		},
	}
}

func IsOccurrenceReview(name string) bool {
	return name == NameOccurrenceReview || name == NameKeywordReview
}

func IsEntityReview(name string) bool {
	return name == NameEntityReview || name == NameTestEntityReview
}

func set(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func Parse(raw json.RawMessage) (ScanProfile, string, error) {
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return ScanProfile{}, "", fmt.Errorf("invalid engine_config: %w", err)
	}
	profileRaw, ok := legacy["scan_profile"]
	if !ok {
		return ScanProfile{}, "", fmt.Errorf("engine_config.scan_profile is required")
	}
	delete(legacy, "scan_profile")
	if len(legacy) > 0 {
		keys := make([]string, 0, len(legacy))
		for key := range legacy {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		return ScanProfile{}, "", fmt.Errorf("engine_config contains fields outside scan_profile: %s", strings.Join(keys, ", "))
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(profileRaw, &fields); err != nil {
		return ScanProfile{}, "", fmt.Errorf("invalid scan_profile: %w", err)
	}
	var parsed ScanProfile
	if err := json.Unmarshal(profileRaw, &parsed); err != nil {
		return ScanProfile{}, "", fmt.Errorf("invalid scan_profile: %w", err)
	}
	if parsed.Version != 1 {
		return ScanProfile{}, "", fmt.Errorf("unsupported scan_profile.version: %d", parsed.Version)
	}
	if parsed.Name == NameKeywordReview {
		parsed.Name = NameOccurrenceReview
	}
	if parsed.Name == NameTestEntityReview {
		parsed.Name = NameEntityReview
	}
	rule, ok := rules()[parsed.Name]
	if !ok {
		return ScanProfile{}, "", fmt.Errorf("unsupported scan_profile.name: %q", parsed.Name)
	}
	for key := range fields {
		if key == "version" || key == "name" || rule.optional[key] {
			continue
		}
		if rule.forbidden[key] {
			return ScanProfile{}, "", fmt.Errorf("field scan_profile.%s is forbidden for %s", key, parsed.Name)
		}
		return ScanProfile{}, "", fmt.Errorf("unknown scan_profile field %q", key)
	}
	if parsed.Name == NameChangeReview && slices.Equal(parsed.Languages, nil) {
		return ScanProfile{}, "", fmt.Errorf("scan_profile.languages is required for %s", NameChangeReview)
	}
	if rule.normalizer != nil {
		rule.normalizer(&parsed)
	}
	if rule.validate != nil {
		if err := rule.validate(parsed); err != nil {
			return ScanProfile{}, "", err
		}
	}
	return parsed, Hash(parsed), nil
}

func Hash(profile ScanProfile) string {
	normalized := profile
	if normalized.Name == NameKeywordReview {
		normalized.Name = NameOccurrenceReview
	}
	if normalized.Name == NameTestEntityReview {
		normalized.Name = NameEntityReview
	}
	if IsOccurrenceReview(normalized.Name) && normalized.PrimaryUnit == "" {
		normalized.PrimaryUnit = PrimaryUnitFile
	}
	if normalized.Name == NameEntityReview && normalized.TargetScope == "" {
		normalized.TargetScope = "test"
	}
	if normalized.Name == NameEntityReview && normalized.EntityKind == "" {
		normalized.EntityKind = "test_case"
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
