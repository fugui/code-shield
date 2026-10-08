package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"gorm.io/datatypes"
)

func categoryTestTaxonomy(definition string) models.CategoryTaxonomy {
	return models.CategoryTaxonomy{
		SchemaVersion: 3,
		Categories: []models.CategoryDefinition{{
			Code: "RACE_RELEASE_ACCESS", Label: "时序与初始化问题-释放与访问竞态",
			Definition: definition, DecisionRules: []string{"release and access race"},
			PositiveHints: []string{"unsynchronized release"}, NegativeHints: []string{"guarded access"},
			Priority: 10, Status: "active", Aliases: []models.CategoryAlias{{Label: "释放竞态"}},
		}},
		DeprecatedAliases: []models.DeprecatedAlias{{Label: "旧释放竞态", TargetCode: "RACE_RELEASE_ACCESS"}},
	}
}

func TestBuildTaskTypeRevisionFreezesTaxonomy(t *testing.T) {
	taxonomy := categoryTestTaxonomy("release/access race")
	raw, err := json.Marshal(taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	taskType := models.TaskType{Taxonomy: datatypes.JSON(raw)}
	revision, err := buildTaskTypeRevision(taskType)
	if err != nil {
		t.Fatal(err)
	}
	if revision.TaxonomySchemaVersion != 3 || revision.TaxonomyHash == "" {
		t.Fatalf("taxonomy revision invalid: version=%d hash=%q", revision.TaxonomySchemaVersion, revision.TaxonomyHash)
	}
	snapshot, err := buildExecutionSnapshot(revision, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Snapshot.Taxonomy.Categories) != 1 || snapshot.Snapshot.TaxonomyHash != revision.TaxonomyHash ||
		snapshot.Snapshot.Taxonomy.Hash != revision.TaxonomyHash {
		t.Fatalf("execution taxonomy snapshot mismatch: %#v", snapshot.Snapshot.Taxonomy)
	}

	changed := categoryTestTaxonomy("different semantics")
	rawChanged, _ := json.Marshal(changed)
	changedRevision, err := buildTaskTypeRevision(models.TaskType{Taxonomy: datatypes.JSON(rawChanged)})
	if err != nil {
		t.Fatal(err)
	}
	if changedRevision.TaxonomyHash == revision.TaxonomyHash || changedRevision.AggregateHash == revision.AggregateHash {
		t.Fatal("semantic definition change did not create a new taxonomy revision")
	}
}

func TestCategorySnapshotSemanticFieldChangesHash(t *testing.T) {
	base := categoryTestTaxonomy("release/access race")
	baseRaw, _ := json.Marshal(base)
	baseRevision, err := buildTaskTypeRevision(models.TaskType{Taxonomy: datatypes.JSON(baseRaw)})
	if err != nil {
		t.Fatal(err)
	}

	mutations := []func(*models.CategoryTaxonomy){
		func(taxonomy *models.CategoryTaxonomy) { taxonomy.Categories[0].Definition = "changed" },
		func(taxonomy *models.CategoryTaxonomy) {
			taxonomy.Categories[0].DecisionRules = []string{"changed rule"}
		},
		func(taxonomy *models.CategoryTaxonomy) {
			taxonomy.Categories[0].PositiveHints = []string{"changed hint"}
		},
		func(taxonomy *models.CategoryTaxonomy) {
			taxonomy.Categories[0].NegativeHints = []string{"changed hint"}
		},
		func(taxonomy *models.CategoryTaxonomy) { taxonomy.Categories[0].Priority = 20 },
	}
	for _, mutate := range mutations {
		changed := categoryTestTaxonomy("release/access race")
		mutate(&changed)
		changedRaw, _ := json.Marshal(changed)
		changedRevision, err := buildTaskTypeRevision(models.TaskType{Taxonomy: datatypes.JSON(changedRaw)})
		if err != nil {
			t.Fatal(err)
		}
		if changedRevision.TaxonomyHash == baseRevision.TaxonomyHash || changedRevision.AggregateHash == baseRevision.AggregateHash {
			t.Fatal("semantic taxonomy change did not create a new hash")
		}
	}
}

func TestCategorySnapshotHashIgnoresRuntimeHashAndAliasHits(t *testing.T) {
	taxonomy := categoryTestTaxonomy("release/access race")
	raw, _ := json.Marshal(taxonomy)
	revision, err := buildTaskTypeRevision(models.TaskType{Taxonomy: datatypes.JSON(raw)})
	if err != nil {
		t.Fatal(err)
	}

	runtimeHash := categoryTestTaxonomy("release/access race")
	runtimeHash.Hash = "runtime-hash-should-not-participate"
	runtimeRaw, _ := json.Marshal(runtimeHash)
	runtimeRevision, err := buildTaskTypeRevision(models.TaskType{Taxonomy: datatypes.JSON(runtimeRaw)})
	if err != nil {
		t.Fatal(err)
	}
	if runtimeRevision.TaxonomyHash != revision.TaxonomyHash || runtimeRevision.AggregateHash != revision.AggregateHash {
		t.Fatal("runtime taxonomy hash changed the snapshot hash")
	}

}

func TestExecutionSnapshotIsFrozenAfterTaskTypeChanges(t *testing.T) {
	taxonomy := categoryTestTaxonomy("release/access race")
	raw, _ := json.Marshal(taxonomy)
	revision, err := buildTaskTypeRevision(models.TaskType{Taxonomy: datatypes.JSON(raw)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := buildExecutionSnapshot(revision, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	changed := categoryTestTaxonomy("changed during run")
	changedRaw, _ := json.Marshal(changed)
	changedRevision, err := buildTaskTypeRevision(models.TaskType{Taxonomy: datatypes.JSON(changedRaw)})
	if err != nil {
		t.Fatal(err)
	}
	if changedRevision.TaxonomyHash == revision.TaxonomyHash {
		t.Fatal("running change did not produce a new revision hash")
	}
	if snapshot.Snapshot.TaxonomyHash != revision.TaxonomyHash ||
		len(snapshot.Snapshot.Taxonomy.Categories) != 1 ||
		snapshot.Snapshot.Taxonomy.Categories[0].Definition != "release/access race" {
		t.Fatalf("execution snapshot was not frozen: %#v", snapshot.Snapshot.Taxonomy)
	}
}

func TestBuiltinCoredumpTaskUsesCompleteV3Taxonomy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "tasks", "coredump-risk", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var taskType models.TaskType
	if err := json.Unmarshal(raw, &taskType); err != nil {
		t.Fatal(err)
	}
	revision, err := buildTaskTypeRevision(taskType)
	if err != nil {
		t.Fatal(err)
	}
	if revision.TaxonomySchemaVersion != 3 {
		t.Fatalf("coredump taxonomy schema version = %d", revision.TaxonomySchemaVersion)
	}
	if len(revision.Taxonomy) == 0 || revision.TaxonomyHash == "" {
		t.Fatalf("coredump taxonomy revision invalid: raw=%d hash=%q", len(revision.Taxonomy), revision.TaxonomyHash)
	}

	snapshot, err := buildExecutionSnapshot(revision, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Snapshot.TaxonomyHash != revision.TaxonomyHash || len(snapshot.Snapshot.Taxonomy.Categories) != 18 {
		t.Fatalf("coredump execution taxonomy invalid: hash=%q categories=%d",
			snapshot.Snapshot.TaxonomyHash, len(snapshot.Snapshot.Taxonomy.Categories))
	}
	for index, category := range snapshot.Snapshot.Taxonomy.Categories {
		if category.Code == "" || category.Label == "" || category.Definition == "" || category.Status != "active" || category.Priority == 0 ||
			len(category.DecisionRules) == 0 || len(category.PositiveHints) == 0 || len(category.NegativeHints) == 0 {
			t.Fatalf("incomplete coredump category[%d]: %#v", index, category)
		}
	}
}

func TestLegacyCategorySnapshotIsV1(t *testing.T) {
	taskType := models.TaskType{Categories: datatypes.JSON(`["legacy label"]`)}
	revision, err := buildTaskTypeRevision(taskType)
	if err != nil {
		t.Fatal(err)
	}
	if revision.TaxonomySchemaVersion != 1 || revision.TaxonomyHash == "" || len(revision.Taxonomy) != 0 {
		t.Fatalf("legacy snapshot invalid: version=%d hash=%q raw=%s", revision.TaxonomySchemaVersion, revision.TaxonomyHash, revision.Taxonomy)
	}
}
