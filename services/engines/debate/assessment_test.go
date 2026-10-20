package debate

import (
	"testing"

	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
	"code-shield/services/engines/profile"
)

func TestResolveAssessmentProfileNames(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		scanName string
		wantName string
	}{
		{name: "config wins", config: `{"profile":"custom"}`, scanName: profile.NameKeywordReview, wantName: "custom"},
		{name: "canonical occurrence", scanName: profile.NameOccurrenceReview, wantName: "occurrencereview"},
		{name: "legacy occurrence", scanName: profile.NameKeywordReview, wantName: "occurrencereview"},
		{name: "legacy entity", scanName: profile.NameTestEntityReview, wantName: "entityreview"},
		{name: "legacy change", scanName: profile.NameChangeReview, wantName: "changereview"},
		{name: "full review", scanName: profile.NameFullReview, wantName: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := assessment.ResolveProfileName([]byte(test.config), test.scanName); got != test.wantName {
				t.Fatalf("ResolveProfileName() = %q, want %q", got, test.wantName)
			}
		})
	}
}

func TestGovernAssessmentFindings(t *testing.T) {
	taxonomy := models.CategoryTaxonomy{
		Hash: "sha256:task-taxonomy",
		Categories: []models.CategoryDefinition{
			{
				Code:            "HELF_MISSING_TERMINATE",
				Label:           "缺少终止",
				DefaultSeverity: "严重",
			},
			{
				Code:            "THREAD_MISSING_JOIN",
				Label:           "生命周期-join遗漏",
				DefaultSeverity: "一般",
			},
		},
	}

	ctx := &engines.EngineContext{
		Taxonomy:     taxonomy,
		TaxonomyHash: taxonomy.Hash,
	}

	findings := []models.AnalysisFinding{
		{
			AssessmentOutcome: "defect",
			CategoryCode:      "HELF_MISSING_TERMINATE",
			Category:          "缺少终止",
			CategoryStatus:    "LEGACY",
			TaxonomyGoverned:  false,
		},
		{
			AssessmentOutcome: "defect",
			CategoryCode:      "",
			Category:          "生命周期-join遗漏",
			CategoryStatus:    "LEGACY",
			TaxonomyGoverned:  false,
		},
		{
			AssessmentOutcome: "pass",
			CategoryCode:      "",
			Category:          "无问题",
			CategoryStatus:    "LEGACY",
			TaxonomyGoverned:  false,
		},
	}

	governed := governAssessmentFindings(ctx, findings)
	if len(governed) != 3 {
		t.Fatalf("governed len = %d, want 3", len(governed))
	}

	// 1. 精确匹配
	f1 := governed[0]
	if f1.CategoryCode != "HELF_MISSING_TERMINATE" || f1.Category != "缺少终止" {
		t.Fatalf("f1 category mismatch: code=%q label=%q", f1.CategoryCode, f1.Category)
	}
	if f1.CategoryStatus != CategoryStatusValid || !f1.TaxonomyGoverned {
		t.Fatalf("f1 status mismatch: status=%q governed=%v", f1.CategoryStatus, f1.TaxonomyGoverned)
	}
	if f1.TaxonomyHash != taxonomy.Hash || f1.CategorySource != CategorySourceDeterministic {
		t.Fatalf("f1 metadata mismatch: hash=%q source=%q", f1.TaxonomyHash, f1.CategorySource)
	}

	// 2. 标签自动推导补全 code
	f2 := governed[1]
	if f2.CategoryCode != "THREAD_MISSING_JOIN" || f2.Category != "生命周期-join遗漏" {
		t.Fatalf("f2 category mismatch: code=%q label=%q", f2.CategoryCode, f2.Category)
	}
	if f2.CategoryStatus != CategoryStatusValid || !f2.TaxonomyGoverned {
		t.Fatalf("f2 status mismatch: status=%q governed=%v", f2.CategoryStatus, f2.TaxonomyGoverned)
	}

	// 3. pass 不被修改
	f3 := governed[2]
	if f3.CategoryStatus != "LEGACY" || f3.TaxonomyGoverned {
		t.Fatalf("f3 should remain untouched: status=%q governed=%v", f3.CategoryStatus, f3.TaxonomyGoverned)
	}
}

func TestGovernAssessmentFindingsWithAllowedCategories(t *testing.T) {
	ctx := &engines.EngineContext{
		AllowedCategories: []string{"越界访问", "空指针解引用", "其它缺陷"},
	}

	findings := []models.AnalysisFinding{
		{
			AssessmentOutcome: "defect",
			Category:          "越界访问",
			CategoryStatus:    "LEGACY",
			TaxonomyGoverned:  false,
		},
	}

	governed := governAssessmentFindings(ctx, findings)
	if len(governed) != 1 {
		t.Fatalf("governed len = %d, want 1", len(governed))
	}
	if governed[0].Category != "越界访问" || !governed[0].TaxonomyGoverned || governed[0].CategoryStatus != CategoryStatusValid {
		t.Fatalf("governed finding mismatch: %+v", governed[0])
	}
}
