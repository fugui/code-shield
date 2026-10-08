package debate

import (
	"context"
	"encoding/json"
	"testing"

	"code-shield/models"
	"code-shield/services/engines/chunker"
)

func TestParseAndRecoverHunterArtifactRecordsAliases(t *testing.T) {
	disableCategoryRepair(t)
	taxonomy := testTaxonomy()
	taxonomy.Hash = "taxonomy-hash"
	before, err := json.Marshal(taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	hits := make([]models.CategoryAliasUsage, 0, 1)
	contract, err := ContractForStage("debate_full", "hunter", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, recoverErr := parseAndRecoverHunterArtifact(
		context.Background(), rawHunterCandidate("", "释放后使用"), contract, "",
		chunker.SemanticBundle{}, nil, taxonomy, func(usage models.CategoryAliasUsage) {
			hits = append(hits, usage)
		},
	)
	if recoverErr != nil {
		t.Fatal(recoverErr)
	}
	if len(hits) != 1 {
		t.Fatalf("alias hits = %d, want 1", len(hits))
	}
	hit := hits[0]
	if hit.AliasKind != "inline" || hit.AliasLabel != "释放后使用" || hit.TargetCode != "RACE_RELEASE_ACCESS" {
		t.Fatalf("unexpected inline alias usage: %+v", hit)
	}
	if hit.TaxonomyHash != "taxonomy-hash" {
		t.Fatalf("taxonomy hash = %q, want frozen value", hit.TaxonomyHash)
	}
	after, err := json.Marshal(taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("alias recording mutated the taxonomy")
	}
}
