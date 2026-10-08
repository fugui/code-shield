package models

import (
	"encoding/json"
	"testing"

	"code-common/backend/testdb"
)

func TestCategoryTaxonomyV3Validation(t *testing.T) {
	valid := CategoryTaxonomy{
		SchemaVersion: 3,
		Categories: []CategoryDefinition{{
			Code: "CODE", Label: "标签", Definition: "定义",
			DecisionRules: []string{"规则"}, PositiveHints: []string{"正例"},
			NegativeHints: []string{"反例"}, Priority: 10, Status: "active",
		}},
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid taxonomy rejected: %v", err)
	}

	mutations := map[string]func(*CategoryTaxonomy){
		"definition":     func(value *CategoryTaxonomy) { value.Categories[0].Definition = "" },
		"decision rules": func(value *CategoryTaxonomy) { value.Categories[0].DecisionRules = nil },
		"empty rule":     func(value *CategoryTaxonomy) { value.Categories[0].DecisionRules = []string{" "} },
		"positive hints": func(value *CategoryTaxonomy) { value.Categories[0].PositiveHints = nil },
		"negative hints": func(value *CategoryTaxonomy) { value.Categories[0].NegativeHints = nil },
		"empty hint":     func(value *CategoryTaxonomy) { value.Categories[0].NegativeHints = []string{" "} },
		"priority":       func(value *CategoryTaxonomy) { value.Categories[0].Priority = 0 },
		"status":         func(value *CategoryTaxonomy) { value.Categories[0].Status = "" },
		"invalid default severity": func(value *CategoryTaxonomy) {
			value.Categories[0].DefaultSeverity = "高危"
		},
		"empty escalation reason": func(value *CategoryTaxonomy) {
			value.Categories[0].DefaultSeverity = "一般"
			value.Categories[0].EscalateWhen = []string{" "}
		},
		"empty downgrade reason": func(value *CategoryTaxonomy) {
			value.Categories[0].DefaultSeverity = "一般"
			value.Categories[0].DowngradeWhen = []string{" "}
		},
	}
	for name, mutate := range mutations {
		value := valid
		mutate(&value)
		if err := value.validate(); err == nil {
			t.Fatalf("%s: expected taxonomy validation error", name)
		}
	}

	legacy := valid
	legacy.SchemaVersion = 2
	legacy.Categories[0].Definition = ""
	legacy.Categories[0].DecisionRules = nil
	legacy.Categories[0].PositiveHints = nil
	legacy.Categories[0].NegativeHints = nil
	legacy.Categories[0].DefaultSeverity = ""
	legacy.Categories[0].EscalateWhen = nil
	legacy.Categories[0].DowngradeWhen = nil
	legacy.Categories[0].Priority = 0
	legacy.Categories[0].Status = ""
	if err := legacy.validate(); err != nil {
		t.Fatalf("v2 taxonomy rejected: %v", err)
	}
}

func TestTaskTypeTaxonomyMetadataSync(t *testing.T) {
	taxonomy := CategoryTaxonomy{
		SchemaVersion: 3,
		Hash:          "sha256:stale",
		Categories: []CategoryDefinition{{
			Code: "CODE", Label: "标签", Definition: "定义",
			DecisionRules: []string{"规则"}, PositiveHints: []string{"正例"},
			NegativeHints: []string{"反例"}, Status: "active", Priority: 10,
		}},
	}
	taxonomyRaw, err := json.Marshal(taxonomy)
	if err != nil {
		t.Fatal(err)
	}

	taskType := TaskType{Name: "taxonomy", DisplayName: "Taxonomy", Taxonomy: taxonomyRaw, TaxonomySchemaVersion: 2}
	if err := taskType.validate(); err == nil {
		t.Fatal("expected mismatched taxonomy schema version to be rejected")
	}

	taskType.TaxonomySchemaVersion = 0
	if err := taskType.validate(); err != nil {
		t.Fatal(err)
	}
	if taskType.TaxonomySchemaVersion != taxonomy.SchemaVersion {
		t.Fatalf("schema version = %d, want %d", taskType.TaxonomySchemaVersion, taxonomy.SchemaVersion)
	}
	if taskType.TaxonomyHash == "" || taskType.TaxonomyHash == "sha256:stale" {
		t.Fatalf("taxonomy hash was not recalculated: %q", taskType.TaxonomyHash)
	}
	if taskType.TaxonomyHash != HashCategoryTaxonomy(&taxonomy) {
		t.Fatalf("taxonomy hash = %q, want canonical %q", taskType.TaxonomyHash, HashCategoryTaxonomy(&taxonomy))
	}
}

func TestCategoryTaxonomyRejectsAmbiguousAliases(t *testing.T) {
	base := func() CategoryTaxonomy {
		return CategoryTaxonomy{
			SchemaVersion: 3,
			Categories: []CategoryDefinition{{
				Code: "CODE", Label: "标签", Definition: "定义",
				DecisionRules: []string{"规则"}, PositiveHints: []string{"正例"},
				NegativeHints: []string{"反例"}, Status: "active", Priority: 10,
			}},
		}
	}
	cases := map[string]func(*CategoryTaxonomy){
		"duplicate deprecated alias": func(taxonomy *CategoryTaxonomy) {
			taxonomy.DeprecatedAliases = []DeprecatedAlias{
				{Label: "旧名", TargetCode: "CODE"},
				{Label: "旧名", TargetCode: "CODE"},
			}
		},
		"duplicate inline alias": func(taxonomy *CategoryTaxonomy) {
			taxonomy.Categories[0].Aliases = []CategoryAlias{{Label: "别名"}, {Label: "别名"}}
		},
		"inline and deprecated conflict": func(taxonomy *CategoryTaxonomy) {
			taxonomy.DeprecatedAliases = []DeprecatedAlias{{Label: "别名", TargetCode: "CODE"}}
			taxonomy.Categories[0].Aliases = []CategoryAlias{{Label: "别名"}}
		},
		"inline conflicts with category label": func(taxonomy *CategoryTaxonomy) {
			taxonomy.Categories[0].Aliases = []CategoryAlias{{Label: "标签"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			taxonomy := base()
			mutate(&taxonomy)
			if err := taxonomy.validate(); err == nil {
				t.Fatalf("%s: expected validation error", name)
			}
		})
	}
}

func TestTaskTypeTaxonomyMetadataPersistence(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_task_type_taxonomy_metadata", &TaskType{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}
	taxonomy := CategoryTaxonomy{
		SchemaVersion: 3,
		Categories: []CategoryDefinition{{
			Code: "CODE", Label: "标签", Definition: "定义",
			DecisionRules: []string{"规则"}, PositiveHints: []string{"正例"},
			NegativeHints: []string{"反例"}, Status: "active", Priority: 10,
		}},
	}
	taxonomyRaw, err := json.Marshal(taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	taskType := TaskType{Name: "taxonomy", DisplayName: "Taxonomy", Taxonomy: taxonomyRaw}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	var stored TaskType
	if err := db.First(&stored, taskType.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TaxonomySchemaVersion != taxonomy.SchemaVersion || stored.TaxonomyHash != HashCategoryTaxonomy(&taxonomy) {
		t.Fatalf("stored metadata invalid: version=%d hash=%q", stored.TaxonomySchemaVersion, stored.TaxonomyHash)
	}
}
