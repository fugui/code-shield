package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/defectlifecycle"

	"github.com/gin-gonic/gin"
)

func TestCompareReportsHandler_Contracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.SetupIsolatedDB(t, "rfc006_handler_compare",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.TaskChunkExecution{},
		&models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	origDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = origDB })

	repo1 := models.Repository{Name: "h-repo-1", URL: "https://example.com/1.git"}
	repo2 := models.Repository{Name: "h-repo-2", URL: "https://example.com/2.git"}
	tt := models.TaskType{Name: "coredump-risk", DisplayName: "Core Dump Risk", GovernanceMode: models.GovernanceModeFullLedger}
	for _, m := range []any{&repo1, &repo2, &tt} {
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
	}

	root := t.TempDir()
	r1 := models.TaskReport{RepoID: repo1.ID, TaskTypeID: tt.ID, Status: models.StatusAnalyzing}
	r2 := models.TaskReport{RepoID: repo1.ID, TaskTypeID: tt.ID, Status: models.StatusAnalyzing}
	rOther := models.TaskReport{RepoID: repo2.ID, TaskTypeID: tt.ID, Status: models.StatusAnalyzing}
	for _, r := range []*models.TaskReport{&r1, &r2, &rOther} {
		if err := db.Create(r).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := defectlifecycle.PersistScanFacts(defectlifecycle.ScanInput{
		DB: db, Report: r1, Repo: repo1, RepoRoot: root, TaskType: tt,
		Findings: []models.AnalysisFinding{{FilePath: "src/a.cpp", LineNumber: "2", Category: "空指针解引用", Severity: "严重", Title: "npd a", TriggerLine: "*p = 1;"}},
		Coverage: &coverage.Coverage{PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true, Files: []coverage.File{{Path: "src/a.cpp", Hash: "h1", Status: coverage.StatusSuccess}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := defectlifecycle.PersistScanFacts(defectlifecycle.ScanInput{
		DB: db, Report: r2, Repo: repo1, RepoRoot: root, TaskType: tt,
		Findings: []models.AnalysisFinding{{FilePath: "src/a.cpp", LineNumber: "2", Category: "空指针解引用", Severity: "严重", Title: "npd a", TriggerLine: "*p = 1;"}},
		Coverage: &coverage.Coverage{PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true, Files: []coverage.File{{Path: "src/a.cpp", Hash: "h1", Status: coverage.StatusSuccess}}},
	}); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/api/reports/compare", CompareReportsHandler)

	// 1. 缺少参数 -> 400
	wMissing := httptest.NewRecorder()
	reqMissing := httptest.NewRequest(http.MethodGet, "/api/reports/compare?base_report_id=1", nil)
	router.ServeHTTP(wMissing, reqMissing)
	if wMissing.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing target_report_id, got %d (%s)", wMissing.Code, wMissing.Body.String())
	}

	// 2. 不存在的报告 -> 404
	wNotFound := httptest.NewRecorder()
	reqNotFound := httptest.NewRequest(http.MethodGet, "/api/reports/compare?base_report_id=1&target_report_id=99999", nil)
	router.ServeHTTP(wNotFound, reqNotFound)
	if wNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown report, got %d (%s)", wNotFound.Code, wNotFound.Body.String())
	}

	// 3. 跨仓库对比 -> 400
	wCross := httptest.NewRecorder()
	reqCross := httptest.NewRequest(http.MethodGet, "/api/reports/compare?base_report_id="+strconv.Itoa(int(r1.ID))+"&target_report_id="+strconv.Itoa(int(rOther.ID)), nil)
	router.ServeHTTP(wCross, reqCross)
	if wCross.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-repo compare, got %d (%s)", wCross.Code, wCross.Body.String())
	}

	// 4. 合法同仓同任务类型对比 -> 200
	wOK := httptest.NewRecorder()
	reqOK := httptest.NewRequest(http.MethodGet, "/api/reports/compare?base_report_id="+strconv.Itoa(int(r1.ID))+"&target_report_id="+strconv.Itoa(int(r2.ID)), nil)
	router.ServeHTTP(wOK, reqOK)
	if wOK.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (%s)", wOK.Code, wOK.Body.String())
	}
	var parsed defectlifecycle.ReportCompareResult
	if err := json.Unmarshal(wOK.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal compare response: %v", err)
	}
	if parsed.Summary.CommonPersisted != 1 || parsed.DataSource != defectlifecycle.CompareDataSourceLedger {
		t.Fatalf("unexpected compare payload: %+v", parsed)
	}
}
