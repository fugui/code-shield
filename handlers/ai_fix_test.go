package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"code-shield/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const aiFixTestToken = "ai-fix-test-token-with-enough-length"

func setupAIFixTest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/ai_fix.db"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Repository{},
		&models.TaskType{},
		&models.TaskReport{},
		&models.AnalysisFinding{},
		&models.Defect{},
		&models.DefectObservation{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	originalDB := models.DB
	originalAppConfig := models.AppConfig
	originalAIFixRequests := aiFixRequests
	t.Cleanup(func() {
		models.DB = originalDB
		models.AppConfig = originalAppConfig
		aiFixRequests = originalAIFixRequests
	})
	models.DB = db
	models.AppConfig.AIFix = models.AIFixConfig{
		FixURL:        "https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id={defect_id}",
		ContextToken:  aiFixTestToken,
		ReportBaseURL: "https://code.example.com",
	}
	aiFixRequests = &aiFixRateLimiter{entries: make(map[string][]time.Time)}

	repo := models.Repository{
		ID:       12,
		Name:     "example-repo",
		URL:      "https://user:secret-token@example.com/team/example-repo.git",
		HTTPURL:  "https://user:secret-token@example.com/team/example-repo.git?private_token=leaked",
		Branch:   "main",
		IsActive: true,
	}
	taskType := models.TaskType{
		ID:             3,
		Name:           "deep_scan",
		DisplayName:    "深度代码扫描",
		GovernanceMode: models.GovernanceModeFullLedger,
	}
	report := models.TaskReport{
		ID:             33061,
		RepoID:         repo.ID,
		TaskTypeID:     taskType.ID,
		Status:         "success",
		BaseCommit:     "5f2a8c8d1e0a9b7c6d5e4f3a2b1c0d9e8f7a6b5c",
		HeadCommit:     "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b",
		GovernanceMode: models.GovernanceModeFullLedger,
		CoverageState:  "COMPLETE",
	}
	finding := models.AnalysisFinding{
		ID:                  1001,
		TaskReportID:        report.ID,
		TaskTypeID:          taskType.ID,
		RepoID:              repo.ID,
		Title:               "Token refresh may leak",
		Severity:            "严重",
		Category:            "memory_leak",
		CategoryCode:        "MEMLEAK_RAII",
		FilePath:            "src/session/token.go",
		LineNumber:          "42-55",
		CodeSnippet:         "old := m.token\nm.token = next",
		Detail:              "The old token can leak.",
		Suggestion:          "Persist first.",
		ScopeSymbol:         "session.(*Manager).Refresh",
		ObservationGroupUID: "group-uid",
		AnchorConfidence:    "HIGH",
	}
	defect := models.Defect{
		ID:                   9182,
		RepoID:               repo.ID,
		TaskTypeID:           taskType.ID,
		Status:               "ACTIVE",
		CanonicalFingerprint: "fingerprint",
		IdentityKind:         "SYMBOL",
		NormPath:             "src/session/token.go",
		ScopeKey:             "pkg:session",
		SymbolPath:           "github.com/example/repo/session.(*Manager).Refresh",
		StmtShape:            "call:token_store.Save",
		DefectClassMajor:     "memory_crash",
		Severity:             "critical",
		FirstReportID:        report.ID,
		LastSeenReportID:     report.ID,
		LastMatchedReportID:  report.ID,
	}
	observation := models.DefectObservation{
		ID:                  1,
		ReportID:            report.ID,
		RepoID:              repo.ID,
		TaskTypeID:          taskType.ID,
		ObservationGroupUID: finding.ObservationGroupUID,
		DefectID:            &defect.ID,
		Verdict:             "EXISTED",
		MatchTier:           "STRONG",
	}
	for _, value := range []interface{}{&repo, &taskType, &report, &finding, &defect, &observation} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed test data: %v", err)
		}
	}

	router := gin.New()
	router.GET("/api/internal/ai-fix/v1/defects/:defect_id/context", GetAIFixDefectContext)
	return router
}

func serveAIFix(router *gin.Engine, defectID string, authorization string, traceID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/internal/ai-fix/v1/defects/"+defectID+"/context", nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if traceID != "" {
		request.Header.Set("X-Trace-ID", traceID)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeAIFixResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func TestGetAIFixDefectContext(t *testing.T) {
	router := setupAIFixTest(t)
	recorder := serveAIFix(router, "9182", "Bearer "+aiFixTestToken, "trace-9182")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store cache control, got %q", got)
	}

	body := decodeAIFixResponse(t, recorder)
	if body["schema_version"] != "1.0" {
		t.Fatalf("unexpected schema version: %v", body["schema_version"])
	}
	if generatedAt, ok := body["generated_at"].(string); !ok || !strings.HasSuffix(generatedAt, "Z") {
		t.Fatalf("expected RFC 3339 UTC generated_at, got %#v", body["generated_at"])
	}

	defect, ok := body["defect"].(map[string]interface{})
	if !ok || defect["id"].(float64) != 9182 || defect["status"] != "ACTIVE" {
		t.Fatalf("unexpected defect: %#v", body["defect"])
	}
	repository, ok := body["repository"].(map[string]interface{})
	if !ok || repository["clone_url"].(string) != "https://example.com/team/example-repo.git" {
		t.Fatalf("unexpected sanitized repository: %#v", body["repository"])
	}
	scan, ok := body["scan"].(map[string]interface{})
	if !ok || scan["head_commit"] == "" || scan["governance_mode"] != models.GovernanceModeFullLedger {
		t.Fatalf("unexpected scan: %#v", body["scan"])
	}
	primary, ok := body["primary_finding"].(map[string]interface{})
	if !ok || primary["id"].(float64) != 1001 {
		t.Fatalf("unexpected primary finding: %#v", body["primary_finding"])
	}
	sourceFindings, ok := body["source_findings"].([]interface{})
	if !ok || len(sourceFindings) != 1 {
		t.Fatalf("unexpected source findings: %#v", body["source_findings"])
	}
	links, ok := body["links"].(map[string]interface{})
	if !ok || links["finding_anchor"] != "finding-1001" {
		t.Fatalf("unexpected links: %#v", body["links"])
	}

	responseBytes := recorder.Body.Bytes()
	if bytes.Contains(responseBytes, []byte(aiFixTestToken)) || bytes.Contains(responseBytes, []byte("secret-token")) || bytes.Contains(responseBytes, []byte("leaked")) {
		t.Fatalf("response leaked credentials: %s", responseBytes)
	}
}

func TestGetAIFixDefectContextAuthorization(t *testing.T) {
	router := setupAIFixTest(t)
	tests := []struct {
		name          string
		authorization string
	}{
		{name: "missing token", authorization: ""},
		{name: "wrong token", authorization: "Bearer wrong-token"},
		{name: "wrong scheme", authorization: "Basic " + aiFixTestToken},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := serveAIFix(router, "9182", test.authorization, "")
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", recorder.Code, recorder.Body.String())
			}
			body := decodeAIFixResponse(t, recorder)
			if body["error"].(map[string]interface{})["code"] != "UNAUTHORIZED" {
				t.Fatalf("unexpected error body: %#v", body)
			}
		})
	}
}

func TestGetAIFixDefectContextErrors(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		defectID     string
		expectedCode int
		expected     string
	}{
		{name: "invalid id", defectID: "-1", expectedCode: http.StatusBadRequest, expected: "INVALID_DEFECT_ID"},
		{name: "missing defect", defectID: "999999", expectedCode: http.StatusNotFound, expected: "DEFECT_NOT_FOUND"},
		{name: "not fixable", status: "RESOLVED", defectID: "9182", expectedCode: http.StatusConflict, expected: "DEFECT_NOT_FIXABLE"},
		{name: "merged", status: "MERGED", defectID: "9182", expectedCode: http.StatusConflict, expected: "DEFECT_MERGED"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := setupAIFixTest(t)
			if test.status != "" {
				if err := models.DB.Model(&models.Defect{}).Where("id = ?", 9182).
					Update("status", test.status).Error; err != nil {
					t.Fatalf("update defect status: %v", err)
				}
			}
			recorder := serveAIFix(router, test.defectID, "Bearer "+aiFixTestToken, "")
			if recorder.Code != test.expectedCode {
				t.Fatalf("expected %d, got %d: %s", test.expectedCode, recorder.Code, recorder.Body.String())
			}
			body := decodeAIFixResponse(t, recorder)
			if body["error"].(map[string]interface{})["code"] != test.expected {
				t.Fatalf("expected %s, got %#v", test.expected, body)
			}
		})
	}
}

func TestGetAIFixDefectContextSelectsSmallestPrimaryFinding(t *testing.T) {
	router := setupAIFixTest(t)
	second := models.AnalysisFinding{
		ID:                  1002,
		TaskReportID:        33061,
		TaskTypeID:          3,
		RepoID:              12,
		Title:               "Duplicate observation",
		Severity:            "严重",
		FilePath:            "src/session/token.go",
		CodeSnippet:         "second snippet",
		ObservationGroupUID: "group-uid",
	}
	if err := models.DB.Create(&second).Error; err != nil {
		t.Fatalf("seed second finding: %v", err)
	}

	recorder := serveAIFix(router, "9182", "Bearer "+aiFixTestToken, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeAIFixResponse(t, recorder)
	primary := body["primary_finding"].(map[string]interface{})
	if primary["id"].(float64) != 1001 {
		t.Fatalf("expected primary finding 1001, got %#v", primary)
	}
	sourceFindings := body["source_findings"].([]interface{})
	if len(sourceFindings) != 2 {
		t.Fatalf("expected two source findings, got %#v", sourceFindings)
	}
	first := sourceFindings[0].(map[string]interface{})
	if first["id"].(float64) != 1001 {
		t.Fatalf("expected ordered source findings, got %#v", sourceFindings)
	}
}

func TestAIFixConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		fixURL  string
		wantErr bool
	}{
		{name: "valid", fixURL: "https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id={defect_id}"},
		{name: "missing", fixURL: "", wantErr: true},
		{name: "no placeholder", fixURL: "https://starmind-dev.sicarrier.com/shield-fix/fixdefect", wantErr: true},
		{name: "duplicate placeholder", fixURL: "https://starmind-dev.sicarrier.com/fix?id={defect_id}&legacy={defect_id}", wantErr: true},
		{name: "not https", fixURL: "http://starmind-dev.sicarrier.com/fix?id={defect_id}", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := models.AIFixConfig{FixURL: test.fixURL}
			err := config.Validate()
			if test.wantErr != (err != nil) {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}

func TestGetFrontendConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalAppConfig := models.AppConfig
	t.Cleanup(func() { models.AppConfig = originalAppConfig })
	models.AppConfig.AIFix = models.AIFixConfig{
		FixURL:        "https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id={defect_id}",
		ContextToken:  aiFixTestToken,
		ReportBaseURL: "https://code.example.com",
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
	})
	router.GET("/api/config/frontend", GetFrontendConfig)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/config/frontend", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), aiFixTestToken) || strings.Contains(recorder.Body.String(), "report_base_url") {
		t.Fatalf("frontend config leaked sensitive fields: %s", recorder.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode frontend config: %v", err)
	}
	aiFix, ok := body["ai_fix"].(map[string]interface{})
	if !ok || len(aiFix) != 1 || aiFix["fix_url"] != models.AppConfig.AIFix.FixURL {
		t.Fatalf("unexpected frontend config: %#v", body)
	}
}
