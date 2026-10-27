package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-common/backend/testdb"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

func TestPureSchemaInitialization(t *testing.T) {
	// 验证全新数据库下的纯净化模型迁移：严格生成 27 张活跃业务表，绝无 key_issues 或已废弃表
	allModels := []interface{}{
		&User{},
		&Department{},
		&Repository{},
		&TaskType{},
		&TaskTypeRevision{},
		&TaskReport{},
		&SystemConfig{},
		&SystemDynamicConfig{},
		&ScheduleConfig{},
		&TaskTriggerLog{},
		&TaskExecutionLog{},
		&AnalysisFinding{},
		&ArtifactRepairAudit{},
		&ScanScopeEntry{},
		&TaskChunkExecution{},
		&Defect{},
		&DefectAlias{},
		&DefectObservation{},
		&DefectEvent{},
		&CampaignFinding{},
		&SysAuditLog{},
		&TaskDebateLog{},
		&RepoFeedbackRule{},
		&CategoryAliasUsage{},
		&CategoryRegressionSample{},
		&CategoryRegressionReview{},
		&CategoryConfusionMatrix{},
	}

	if len(allModels) != 27 {
		t.Fatalf("expected exactly 27 active core models, got %d", len(allModels))
	}

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pure.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	if err := db.AutoMigrate(allModels...); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	if err := EnsureDefectSlotIndexes(db); err != nil {
		t.Fatalf("EnsureDefectSlotIndexes failed: %v", err)
	}

	// 验证不存在 key_issues
	if db.Migrator().HasTable("key_issues") {
		t.Fatalf("unexpected legacy table key_issues exists in migrated database")
	}

	// 验证旧表全部不存在
	legacyTables := []string{
		"defect_fingerprint_records",
		"scan_reconciliations",
		"reconciliation_links",
		"defect_entities",
		"defect_entity_aliases",
		"defect_entity_relations",
		"defect_occurrences",
		"report_file_coverages",
		"defect_anchor_keys",
		"defect_entity_state_versions",
		"defect_entity_events",
		"defect_assignments",
	}
	for _, lt := range legacyTables {
		if db.Migrator().HasTable(lt) {
			t.Fatalf("unexpected legacy reconciliation table %s exists", lt)
		}
	}
}

func TestSaveTaskTypeMetaFilePreservesExtensionsAndOmitsDerivedHash(t *testing.T) {
	tempDir := t.TempDir()
	metaPath := filepath.Join(tempDir, "meta.json")

	// 模拟原有 meta.json 中包含 plugins 与 synthesis_config 等非 TaskType 字段
	originalContent := `{
  "name": "custom_task",
  "display_name": "Custom Task",
  "engine_mode": "debate_full",
  "plugins": {
    "preflight_gate": {"id": "test_radar"}
  },
  "synthesis_config": {
    "report_type": "static"
  }
}`
	if err := os.WriteFile(metaPath, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	taskType := TaskType{
		Name:            "custom_task",
		DisplayName:     "Updated Custom Task",
		EngineMode:      "debate_full",
		ScanProfileHash: "sha256:should-not-persist-to-disk",
		Timeout:         45,
	}

	if err := SaveTaskTypeMetaFile(metaPath, taskType); err != nil {
		t.Fatalf("SaveTaskTypeMetaFile failed: %v", err)
	}

	savedBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}

	var savedMap map[string]json.RawMessage
	if err := json.Unmarshal(savedBytes, &savedMap); err != nil {
		t.Fatal(err)
	}

	// 1. 验证原有扩展字段未被抹除
	if _, ok := savedMap["plugins"]; !ok {
		t.Fatal("expected plugins extension field to be preserved")
	}
	if _, ok := savedMap["synthesis_config"]; !ok {
		t.Fatal("expected synthesis_config extension field to be preserved")
	}

	// 2. 验证更新字段已生效
	var displayName string
	if err := json.Unmarshal(savedMap["display_name"], &displayName); err != nil || displayName != "Updated Custom Task" {
		t.Fatalf("display_name = %q, want 'Updated Custom Task'", displayName)
	}

	// 3. 验证派生 hash 字段没有污染磁盘
	if rawHash, ok := savedMap["scan_profile_hash"]; ok && string(rawHash) != `""` {
		t.Fatalf("unexpected scan_profile_hash on disk: %s", string(rawHash))
	}
}
