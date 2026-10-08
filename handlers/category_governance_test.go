package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/governance"

	"github.com/gin-gonic/gin"
)

func setupCategoryGovernanceDB(t *testing.T) {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, "shield_category_governance_handlers",
		&models.CategoryAliasUsage{},
		&models.CategoryRegressionSample{},
		&models.CategoryRegressionReview{},
		&models.CategoryConfusionMatrix{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}
	models.DB = db
	t.Cleanup(func() { models.DB = nil })
}

func TestCategoryGovernanceAPIs(t *testing.T) {
	setupCategoryGovernanceDB(t)
	now := time.Now()
	if err := governance.RecordCategoryAliasHit(models.DB, models.CategoryAliasUsage{
		TaskTypeID: 7, TaxonomyHash: "taxonomy-1", AliasKind: governance.CategoryAliasInline,
		AliasLabel: "inline alias", TargetCode: "CODE_A", LastReportID: 42, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	sample := models.CategoryRegressionSample{
		SampleKey: "regression/a-vs-b-01", TaskType: "coredump_risk", PairKey: "a-vs-b",
		CandidateFacts: []byte(`{"code":"free(x); x->read;"}`), ExpectedCode: "CODE_A",
		ActualCode: "CODE_B", Outcome: "FAIL", TaxonomyHash: "taxonomy-1",
		PromptHash: "prompt-1", ModelBackend: "test", LastRunAt: &now,
	}
	if err := models.DB.Create(&sample).Error; err != nil {
		t.Fatal(err)
	}
	matrix := models.CategoryConfusionMatrix{
		TaxonomyHash: "taxonomy-1", PromptHash: "prompt-1", ModelBackend: "test",
		PeriodStart: now, PeriodEnd: now.Add(time.Hour), Matrix: []byte(`[]`),
		SampleCount: 1, ErrorCount: 1, CreatedAt: now,
	}
	if err := models.DB.Create(&matrix).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/alias", GetCategoryAliasUsageHandler)
	router.GET("/samples", GetCategoryRegressionSamplesHandler)
	router.GET("/matrices", GetCategoryConfusionMatricesHandler)
	router.POST("/samples/:id/reviews", ReviewCategoryRegressionSampleHandler)
	router.PATCH("/samples/:id/supersede", SupersedeCategoryRegressionSampleHandler)

	request := httptest.NewRequest(http.MethodGet, "/alias?task_type_id=7&taxonomy_hash=taxonomy-1", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("alias usage status = %d, body=%s", response.Code, response.Body.String())
	}
	var aliasResult struct {
		Items []models.CategoryAliasUsage `json:"items"`
		Total int64                       `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &aliasResult); err != nil {
		t.Fatal(err)
	}
	if aliasResult.Total != 1 || len(aliasResult.Items) != 1 || aliasResult.Items[0].HitCount != 1 {
		t.Fatalf("unexpected alias result: %+v", aliasResult)
	}

	request = httptest.NewRequest(http.MethodGet, "/samples?status=ACTIVE", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("samples status = %d, body=%s", response.Code, response.Body.String())
	}
	var sampleResult struct {
		Items []models.CategoryRegressionSample `json:"items"`
		Total int64                             `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &sampleResult); err != nil {
		t.Fatal(err)
	}
	if sampleResult.Total != 1 || len(sampleResult.Items) != 1 || sampleResult.Items[0].ID != sample.ID {
		t.Fatalf("unexpected sample result: %+v", sampleResult)
	}

	initial, _ := json.Marshal(map[string]any{
		"stage": governance.CategoryReviewInitial, "reviewer": "alice",
		"outcome": governance.CategoryReviewDisputed, "dispute_reason": "boundary unclear",
	})
	request = httptest.NewRequest(http.MethodPost, "/samples/"+jsonUint(sample.ID)+"/reviews", bytes.NewReader(initial))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("initial review status = %d, body=%s", response.Code, response.Body.String())
	}
	second, _ := json.Marshal(map[string]any{
		"stage": governance.CategoryReviewSecond, "reviewer": "bob", "outcome": governance.CategoryReviewConfirmed,
	})
	request = httptest.NewRequest(http.MethodPost, "/samples/"+jsonUint(sample.ID)+"/reviews", bytes.NewReader(second))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("second review status = %d, body=%s", response.Code, response.Body.String())
	}
	var reviews []models.CategoryRegressionReview
	if err := models.DB.Order("id asc").Find(&reviews).Error; err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 || reviews[0].Stage != governance.CategoryReviewInitial || reviews[1].Stage != governance.CategoryReviewSecond {
		t.Fatalf("unexpected reviews: %+v", reviews)
	}

	supersede, _ := json.Marshal(map[string]any{"superseded_by": "replacement", "superseded_reason": "taxonomy evolved"})
	request = httptest.NewRequest(http.MethodPatch, "/samples/"+jsonUint(sample.ID)+"/supersede", bytes.NewReader(supersede))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("supersede status = %d, body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/samples?status=SUPERSEDED", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("superseded query status = %d", response.Code)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &sampleResult); err != nil {
		t.Fatal(err)
	}
	if sampleResult.Total != 1 || sampleResult.Items[0].SupersededBy != "replacement" {
		t.Fatalf("unexpected superseded result: %+v", sampleResult)
	}

	request = httptest.NewRequest(http.MethodGet, "/matrices?model_backend=test", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("matrix status = %d, body=%s", response.Code, response.Body.String())
	}
	var matrixResult struct {
		Items []models.CategoryConfusionMatrix `json:"items"`
		Total int64                            `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &matrixResult); err != nil {
		t.Fatal(err)
	}
	if matrixResult.Total != 1 || matrixResult.Items[0].SampleCount != 1 {
		t.Fatalf("unexpected matrix result: %+v", matrixResult)
	}
}

func jsonUint(value uint) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
