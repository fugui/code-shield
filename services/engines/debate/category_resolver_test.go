package debate

import (
	"testing"

	"code-shield/models"
	"encoding/json"
)

func testTaxonomy() models.CategoryTaxonomy {
	return models.CategoryTaxonomy{
		SchemaVersion: 2,
		Categories: []models.CategoryDefinition{
			{Code: "RACE_RELEASE_ACCESS", Label: "时序与初始化问题-释放与访问竞态", Aliases: []models.CategoryAlias{{Label: "释放后使用"}}},
			{Code: "MEM_USE_AFTER_FREE", Label: "内存问题-释放后使用"},
		},
		DeprecatedAliases: []models.DeprecatedAlias{{Label: "旧释放竞态", TargetCode: "RACE_RELEASE_ACCESS"}},
	}
}

func TestResolveCategory(t *testing.T) {
	taxonomy := testTaxonomy()
	cases := []struct {
		name   string
		code   string
		label  string
		status int
		want   models.CategoryDefinition
	}{
		{name: "matching code and label", code: "RACE_RELEASE_ACCESS", label: "时序与初始化问题-释放与访问竞态", status: CategoryResolutionValid, want: taxonomy.Categories[0]},
		{name: "code authority", code: "RACE_RELEASE_ACCESS", label: "invalid", status: CategoryResolutionCodeAuthority, want: taxonomy.Categories[0]},
		{name: "label authority", label: "内存问题-释放后使用", status: CategoryResolutionLabelAuthority, want: taxonomy.Categories[1]},
		{name: "conflicting label uses code", code: "RACE_RELEASE_ACCESS", label: "内存问题-释放后使用", status: CategoryResolutionCodeAuthority, want: taxonomy.Categories[0]},
		{name: "inline alias", label: "释放后使用", status: CategoryResolutionAliasAuthority, want: taxonomy.Categories[0]},
		{name: "deprecated alias", label: "旧释放竞态", status: CategoryResolutionAliasAuthority, want: taxonomy.Categories[0]},
		{name: "both invalid", code: "RACE", label: "释放访问", status: CategoryResolutionNeedsRepair},
		{name: "unicode normalized", code: "ＲＡＣＥ_ＲＥＬＥＡＳＥ_ＡＣＣＥＳＳ", label: "时序与初始化问题-释放与访问竞态", status: CategoryResolutionValid, want: taxonomy.Categories[0]},
		{name: "case tolerant", code: "race_release_access", status: CategoryResolutionCodeAuthority, want: taxonomy.Categories[0]},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			got := ResolveCategory(item.code, item.label, taxonomy)
			if got.Status != item.status {
				t.Fatalf("status = %d, want %d", got.Status, item.status)
			}
			if item.status != CategoryResolutionNeedsRepair && (got.Code != item.want.Code || got.Label != item.want.Label) {
				t.Fatalf("resolution = (%q, %q), want (%q, %q)", got.Code, got.Label, item.want.Code, item.want.Label)
			}
			switch item.name {
			case "inline alias":
				if got.AliasKind != "inline" || got.AliasLabel != "释放后使用" {
					t.Fatalf("inline alias metadata = %+v", got)
				}
			case "deprecated alias":
				if got.AliasKind != "deprecated" || got.AliasLabel != "旧释放竞态" {
					t.Fatalf("deprecated alias metadata = %+v", got)
				}
			}
		})
	}
}

func TestResolveCategoryRejectsAmbiguousAliases(t *testing.T) {
	t.Run("duplicate deprecated alias", func(t *testing.T) {
		taxonomy := testTaxonomy()
		taxonomy.DeprecatedAliases = append(taxonomy.DeprecatedAliases, models.DeprecatedAlias{Label: "旧释放竞态", TargetCode: "MEM_USE_AFTER_FREE"})
		if got := ResolveCategory("", "旧释放竞态", taxonomy); got.Status != CategoryResolutionNeedsRepair {
			t.Fatalf("ambiguous deprecated alias status = %d, want NEEDS_REPAIR", got.Status)
		}
	})
	t.Run("duplicate inline alias", func(t *testing.T) {
		taxonomy := testTaxonomy()
		taxonomy.Categories[1].Aliases = []models.CategoryAlias{{Label: "释放后使用"}}
		if got := ResolveCategory("", "释放后使用", taxonomy); got.Status != CategoryResolutionNeedsRepair {
			t.Fatalf("ambiguous inline alias status = %d, want NEEDS_REPAIR", got.Status)
		}
	})
	t.Run("inline conflicts with deprecated", func(t *testing.T) {
		taxonomy := testTaxonomy()
		taxonomy.Categories[0].Aliases = append(taxonomy.Categories[0].Aliases, models.CategoryAlias{Label: "旧释放竞态"})
		if got := ResolveCategory("", "旧释放竞态", taxonomy); got.Status != CategoryResolutionNeedsRepair {
			t.Fatalf("conflicting alias status = %d, want NEEDS_REPAIR", got.Status)
		}
	})
}

func TestResolveCategoryRejectsFuzzyMatch(t *testing.T) {
	got := ResolveCategory("", "释放与访问竞态", testTaxonomy())
	if got.Status != CategoryResolutionNeedsRepair {
		t.Fatalf("fuzzy label resolved with status %d", got.Status)
	}
}

func TestResolveCategoryRejectsLabelFuzzyAdoption(t *testing.T) {
	taxonomy := testTaxonomy()
	resolution := ResolveCategory("", "释放竞态附近的语义", taxonomy)
	if resolution.Status != CategoryResolutionNeedsRepair {
		t.Fatalf("fuzzy label unexpectedly resolved: %+v", resolution)
	}
}

func TestResolveAliasDoesNotMutateTaxonomy(t *testing.T) {
	taxonomy := testTaxonomy()
	before, err := json.Marshal(taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	if resolution := ResolveCategory("", "释放后使用", taxonomy); resolution.Status != CategoryResolutionAliasAuthority {
		t.Fatalf("alias resolution status = %d", resolution.Status)
	}
	after, err := json.Marshal(taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("alias hit mutated the taxonomy snapshot")
	}
}
