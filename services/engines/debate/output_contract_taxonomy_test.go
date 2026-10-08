package debate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
)

func TestContractForStageWithTaxonomyUsesSameEnums(t *testing.T) {
	for _, stage := range []string{"hunter", "judge"} {
		contract, err := ContractForStageWithTaxonomy("debate_full", stage, nil, testTaxonomy())
		if err != nil {
			t.Fatal(err)
		}
		rendered := RenderOutputContract(contract)
		if len(contract.Enums["category_code"]) != len(testTaxonomy().Categories) || len(contract.Enums["category"]) != len(testTaxonomy().Categories) {
			t.Fatalf("%s enum source mismatch: %#v", stage, contract.Enums)
		}
		if !strings.Contains(rendered, "`category_code` 枚举") || !strings.Contains(rendered, "RACE_RELEASE_ACCESS") {
			t.Fatalf("%s rendered contract omitted category codes", stage)
		}
	}
	contract, _ := ContractForStageWithTaxonomy("debate_full", "hunter", nil, testTaxonomy())
	if contract.SchemaID != CandidatesArtifactSchemaV2 {
		t.Fatalf("hunter schema id = %q", contract.SchemaID)
	}
}

func TestContractForStageWithTaxonomyFiltersInactiveCategories(t *testing.T) {
	taxonomy := testTaxonomy()
	taxonomy.SchemaVersion = 3
	taxonomy.Categories = []models.CategoryDefinition{
		{Code: "ACTIVE_CODE", Label: "active label", Status: "active", Priority: 10, Definition: "定义", DecisionRules: []string{"规则"}, PositiveHints: []string{"正例"}, NegativeHints: []string{"反例"}},
		{Code: "DEPRECATED_CODE", Label: "deprecated label", Status: "deprecated", Priority: 20, Definition: "定义", DecisionRules: []string{"规则"}, PositiveHints: []string{"正例"}, NegativeHints: []string{"反例"}},
		{Code: "SUPERSEDED_CODE", Label: "superseded label", Status: "superseded", Priority: 30, Definition: "定义", DecisionRules: []string{"规则"}, PositiveHints: []string{"正例"}, NegativeHints: []string{"反例"}},
	}
	for _, stage := range []string{"hunter", "judge"} {
		contract, err := ContractForStageWithTaxonomy("debate_full", stage, nil, taxonomy)
		if err != nil {
			t.Fatal(err)
		}
		if len(contract.Enums["category_code"]) != 1 || contract.Enums["category_code"][0] != "ACTIVE_CODE" {
			t.Fatalf("%s server enum invalid: %#v", stage, contract.Enums["category_code"])
		}
		if len(contract.Enums["category"]) != 1 || contract.Enums["category"][0] != "active label" {
			t.Fatalf("%s prompt enum invalid: %#v", stage, contract.Enums["category"])
		}
		rendered := RenderOutputContract(contract)
		for _, forbidden := range []string{"DEPRECATED_CODE", "SUPERSEDED_CODE", "deprecated label", "superseded label"} {
			if strings.Contains(rendered, forbidden) {
				t.Fatalf("%s rendered enum contains inactive %q", stage, forbidden)
			}
		}
	}
}

func TestBuiltinTaskTypesUseV3Contracts(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join("..", "..", "..", "tasks", entry.Name(), "meta.json")
		metaRaw, err := os.ReadFile(metaPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		var taskType models.TaskType
		if err := json.Unmarshal(metaRaw, &taskType); err != nil {
			t.Fatalf("parse %s: %v", metaPath, err)
		}
		taxonomy := taskType.GetCategoryTaxonomy()
		if taxonomy == nil || taxonomy.SchemaVersion != 3 {
			t.Fatalf("%s does not expose v3 taxonomy", taskType.Name)
		}
		found++
		wantCode := taxonomy.Categories[0].Code
		for stage, wantSchema := range map[string]string{
			"hunter": CandidatesArtifactSchemaV2,
			"judge":  FinalVerdictsArtifactSchemaV3,
		} {
			contract, err := ContractForStageWithTaxonomy("debate_full", stage, taskType.GetAllowedCategories(), *taxonomy)
			if err != nil {
				t.Fatalf("%s %s contract: %v", taskType.Name, stage, err)
			}
			if contract.SchemaID != wantSchema {
				t.Fatalf("%s %s schema = %q, want %q", taskType.Name, stage, contract.SchemaID, wantSchema)
			}
			if len(contract.Enums["category_code"]) != len(taxonomy.Categories) {
				t.Fatalf("%s %s code enum mismatch", taskType.Name, stage)
			}
			wantExample := fmt.Sprintf(`"category_code": %q`, wantCode)
			if !strings.Contains(contract.Example, wantExample) {
				t.Fatalf("%s %s example lacks %s: %s", taskType.Name, stage, wantExample, contract.Example)
			}
		}
	}
	if found == 0 {
		t.Fatal("no builtin task metas found")
	}
}

func Test30428CandidateIsInContractRegression(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "category-regression.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var sample struct {
		CandidateID     string            `json:"candidate_id"`
		CandidateFacts  map[string]string `json:"candidate_facts"`
		ExpectedCode    string            `json:"expected_code"`
		ForbiddenLabels []string          `json:"forbidden_labels"`
	}
	firstLine := raw
	if index := strings.IndexByte(string(raw), '\n'); index >= 0 {
		firstLine = raw[:index]
	}
	if err := json.Unmarshal(firstLine, &sample); err != nil {
		t.Fatal(err)
	}
	if sample.CandidateID != "30428/H-010" || sample.ExpectedCode != "RACE_RELEASE_ACCESS" || len(sample.CandidateFacts) == 0 {
		t.Fatalf("30428 contract regression sample invalid: %#v", sample)
	}

	metaRaw, err := os.ReadFile(filepath.Join("..", "..", "..", "tasks", "coredump-risk", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var taskType models.TaskType
	if err := json.Unmarshal(metaRaw, &taskType); err != nil {
		t.Fatal(err)
	}
	taxonomy := taskType.GetCategoryTaxonomy()
	if taxonomy == nil || taxonomy.SchemaVersion != 3 {
		t.Fatal("coredump task does not expose v3 taxonomy")
	}
	contract, err := ContractForStageWithTaxonomy("debate_full", "judge", taskType.GetAllowedCategories(), *taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(contract.Enums["category_code"], sample.ExpectedCode) {
		t.Fatalf("expected code %q absent from contract enum", sample.ExpectedCode)
	}
	for _, forbidden := range sample.ForbiddenLabels {
		if !contains(contract.Enums["category_code"], forbidden) && !contains(contract.Enums["category"], forbidden) {
			t.Fatalf("forbidden label %q absent from contract enum", forbidden)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
