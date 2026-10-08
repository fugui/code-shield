package governance

import (
	"encoding/json"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestCategoryAliasUsageIsAuditable(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_category_alias_usage", &models.CategoryAliasUsage{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	usage := models.CategoryAliasUsage{TaxonomyHash: "taxonomy", AliasKind: CategoryAliasInline, AliasLabel: "释放竞态", TargetCode: "RACE_RELEASE_ACCESS"}
	for range 3 {
		if err := RecordCategoryAliasHit(db, usage); err != nil {
			t.Fatal(err)
		}
	}
	var stored models.CategoryAliasUsage
	if err := db.Where("taxonomy_hash = ? AND alias_label = ?", "taxonomy", "释放竞态").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.HitCount != 3 {
		t.Fatalf("alias hit count = %d, want 3", stored.HitCount)
	}
	usage.AliasKind = "invalid"
	if err := RecordCategoryAliasHit(db, usage); err == nil {
		t.Fatal("invalid alias kind accepted")
	}
}

func TestCategoryRegressionReviewsAndConfusionMatrix(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_category_regression_governance",
		&models.CategoryRegressionSample{}, &models.CategoryRegressionReview{}, &models.CategoryConfusionMatrix{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	facts, _ := json.Marshal(map[string]string{"code": "free(ptr); ptr->read;"})
	sample := models.CategoryRegressionSample{
		SampleKey: "regression/uaf-vs-dangling-case-01", TaskType: "coredump_risk", PairKey: "uaf-vs-dangling",
		CandidateFacts: facts, ExpectedCode: "MEM_USE_AFTER_FREE", ActualCode: "LIFECYCLE_DANGLING_REF",
		TaxonomyHash: "taxonomy", PromptHash: "prompt", ModelBackend: "native",
	}
	if err := RecordCategoryRegressionRun(db, sample); err != nil {
		t.Fatal(err)
	}
	var stored models.CategoryRegressionSample
	if err := db.Where("sample_key = ?", sample.SampleKey).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Outcome != "FAIL" || stored.FailureCount != 1 {
		t.Fatalf("regression run invalid: %#v", stored)
	}

	if err := ReviewCategoryRegressionSample(db, models.CategoryRegressionReview{SampleID: stored.ID, Stage: CategoryReviewSecond, Reviewer: "reviewer-b", Outcome: CategoryReviewConfirmed}); err == nil {
		t.Fatal("second review accepted without initial review")
	}
	if err := ReviewCategoryRegressionSample(db, models.CategoryRegressionReview{SampleID: stored.ID, Stage: CategoryReviewInitial, Reviewer: "reviewer-a", Outcome: CategoryReviewDisputed}); err == nil {
		t.Fatal("disputed review accepted without reason")
	}

	initial := models.CategoryRegressionReview{SampleID: stored.ID, Stage: CategoryReviewInitial, Reviewer: "reviewer-a", Outcome: CategoryReviewDisputed, DisputeReason: "lifecycle boundary unclear"}
	if err := ReviewCategoryRegressionSample(db, initial); err != nil {
		t.Fatal(err)
	}
	second := models.CategoryRegressionReview{SampleID: stored.ID, Stage: CategoryReviewSecond, Reviewer: "reviewer-b", Outcome: CategoryReviewConfirmed}
	if err := ReviewCategoryRegressionSample(db, second); err != nil {
		t.Fatal(err)
	}
	if err := ReviewCategoryRegressionSample(db, models.CategoryRegressionReview{SampleID: 999, Stage: CategoryReviewInitial, Reviewer: "x", Outcome: CategoryReviewConfirmed}); err == nil {
		t.Fatal("review for missing sample accepted")
	}

	now := time.Now()
	matrix, err := BuildCategoryConfusionMatrix(db, "taxonomy", "prompt", "native", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if matrix.SampleCount != 1 || matrix.ErrorCount != 1 {
		t.Fatalf("confusion matrix counts invalid: %+v", matrix)
	}
	if err := SupersedeCategoryRegressionSample(db, sample.SampleKey, "replacement", "taxonomy evolved"); err != nil {
		t.Fatal(err)
	}
	if err := SupersedeCategoryRegressionSample(db, sample.SampleKey, "replacement", ""); err == nil {
		t.Fatal("superseded sample accepted without reason")
	}
	var superseded models.CategoryRegressionSample
	if err := db.Where("sample_key = ?", sample.SampleKey).First(&superseded).Error; err != nil {
		t.Fatal(err)
	}
	if superseded.Status != CategorySampleSuperseded || superseded.SupersededBy != "replacement" {
		t.Fatalf("superseded sample invalid: %#v", superseded)
	}
}
