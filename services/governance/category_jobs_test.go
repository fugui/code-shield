package governance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/invoker"
)

func TestCategoryGovernanceJobsSeedRunAndBuildMonthlyMatrix(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_category_governance_jobs",
		&models.CategoryRegressionSample{}, &models.CategoryConfusionMatrix{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}
	corpus := filepath.Join(t.TempDir(), "category-regression.jsonl")
	samples := []map[string]any{
		{
			"task_type": "coredump_risk", "candidate_id": "regression/a-vs-b-case-01",
			"candidate_facts": map[string]string{"code": "free(x); x->read;"},
			"expected_code":   "CODE_A", "taxonomy_hash": "taxonomy-1", "prompt_hash": "prompt-1",
			"model_backend": "test",
		},
		{
			"task_type": "coredump_risk", "candidate_id": "regression/a-vs-b-case-02",
			"candidate_facts": map[string]string{"code": "read(x); free(x);"},
			"expected_code":   "CODE_A", "taxonomy_hash": "taxonomy-1", "prompt_hash": "prompt-1",
			"model_backend": "test",
		},
	}
	file, err := os.Create(corpus)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, sample := range samples {
		if err := encoder.Encode(sample); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	synced, err := SeedCategoryRegressionSamples(db, corpus)
	if err != nil || synced != len(samples) {
		t.Fatalf("seeded=%d err=%v, want %d", synced, err, len(samples))
	}
	secondSynced, err := SeedCategoryRegressionSamples(db, corpus)
	if err != nil || secondSynced != len(samples) {
		t.Fatalf("resynced=%d err=%v, want %d", secondSynced, err, len(samples))
	}
	var storedCount int64
	if err := db.Model(&models.CategoryRegressionSample{}).Count(&storedCount).Error; err != nil {
		t.Fatal(err)
	}
	if storedCount != int64(len(samples)) {
		t.Fatalf("stored samples=%d, want %d", storedCount, len(samples))
	}

	callCount := 0
	runCount, err := RunCategoryRegressionTrend(db, func(sample models.CategoryRegressionSample) (string, error) {
		callCount++
		if sample.SampleKey == "regression/a-vs-b-case-01" {
			return "CODE_A", nil
		}
		return "CODE_B", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if runCount != len(samples) || callCount != len(samples) {
		t.Fatalf("run=%d call=%d, want %d", runCount, callCount, len(samples))
	}
	var failed models.CategoryRegressionSample
	if err := db.Where("sample_key = ?", "regression/a-vs-b-case-02").First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.ActualCode != "CODE_B" || failed.Outcome != "FAIL" || failed.FailureCount != 1 {
		t.Fatalf("unexpected failed sample: %+v", failed)
	}

	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	periodEnd := periodStart.AddDate(0, 1, 0)
	matrices, err := BuildMonthlyCategoryConfusionMatrices(db, periodStart, periodEnd)
	if err != nil {
		t.Fatal(err)
	}
	if len(matrices) != 1 || matrices[0].SampleCount != 2 || matrices[0].ErrorCount != 1 {
		t.Fatalf("unexpected monthly matrices: %+v", matrices)
	}
}

func TestCategoryRegressionRunLimits(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_category_regression_limits",
		&models.CategoryRegressionSample{}, &models.CategoryConfusionMatrix{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}
	previous := models.AppConfig.Governance
	models.AppConfig.Governance.CategoryRegressionBatchSize = 2
	models.AppConfig.Governance.CategoryRegressionMaxFailures = 1
	t.Cleanup(func() { models.AppConfig.Governance = previous })

	for index := 1; index <= 3; index++ {
		sample := models.CategoryRegressionSample{
			SampleKey:      fmt.Sprintf("limit-case-%02d", index),
			TaskType:       "coredump_risk",
			PairKey:        "limit",
			CandidateFacts: []byte(`{"code":"x"}`),
			ExpectedCode:   "CODE_A",
			Status:         CategorySampleActive,
			TaxonomyHash:   "taxonomy-1",
			PromptHash:     "prompt-1",
			ModelBackend:   "test",
		}
		if err := db.Create(&sample).Error; err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	_, err := RunCategoryRegressionTrend(db, func(models.CategoryRegressionSample) (string, error) {
		calls++
		return "", fmt.Errorf("backend unavailable")
	})
	if err == nil || calls != 1 {
		t.Fatalf("calls=%d err=%v, want one failed call and stop", calls, err)
	}
	var failed models.CategoryRegressionSample
	if err := db.Where("sample_key = ?", "limit-case-01").First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Outcome != "ERROR" || failed.LastFailureReason == "" {
		t.Fatalf("failure not queryable: %+v", failed)
	}
}

func TestCategoryRegressionBackendExecutorValidation(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_category_regression_executor",
		&models.TaskType{}, &models.CategoryRegressionSample{}, &models.CategoryConfusionMatrix{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}
	taxonomy := models.CategoryTaxonomy{SchemaVersion: 3, Categories: []models.CategoryDefinition{{
		Code: "CODE_A", Label: "A", Definition: "A", DecisionRules: []string{"rule"},
		Status: "active", Priority: 1,
	}}}
	taxonomyRaw, err := json.Marshal(taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	taskType := models.TaskType{Name: "coredump_risk", DisplayName: "Core Dump", Taxonomy: taxonomyRaw}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	sample := models.CategoryRegressionSample{
		SampleKey: "executor-case-01", TaskType: "coredump_risk", PairKey: "executor",
		CandidateFacts: []byte(`{"code":"x"}`), ExpectedCode: "CODE_A",
		Status: CategorySampleActive, TaxonomyHash: taskType.TaxonomyHash, PromptHash: "prompt-1",
		ModelBackend: "test",
	}
	if err := db.Create(&sample).Error; err != nil {
		t.Fatal(err)
	}

	registerRegressionMock(t, `[1,2]`)
	if _, err := categoryRegressionBackendExecutor(sample); err == nil {
		t.Fatal("expected non-object output to be rejected")
	}
	registerRegressionMock(t, `{"category_code":"NOT_IN_TAXONOMY"}`)
	if _, err := categoryRegressionBackendExecutor(sample); err == nil {
		t.Fatal("expected taxonomy miss to be rejected")
	}
	registerRegressionMock(t, `{"category_code":"CODE_A"}`)
	if code, err := categoryRegressionBackendExecutor(sample); err != nil || code != "CODE_A" {
		t.Fatalf("code=%q err=%v", code, err)
	}
}

func registerRegressionMock(t *testing.T, response string) {
	t.Helper()
	invoker.RegisterAIInvoker("native", invokerFunc(func(request invoker.AIRequest) error {
		return os.WriteFile(request.OutputPath, []byte(response), 0644)
	}))
}

type invokerFunc func(invoker.AIRequest) error

func (fn invokerFunc) Invoke(request invoker.AIRequest) error { return fn(request) }
func (invokerFunc) Name() string                              { return "regression-test" }
