package handlers

import (
	"bytes"
	commonAuth "code-common/backend/auth"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	return testdb.SetupIsolatedDB(t, "shield_pagination",
		&models.Department{},
		&models.User{},
		&models.Repository{},
		&models.TaskType{},
		&models.TaskReport{},
		&models.TaskExecutionLog{},
		&models.CampaignFinding{},
	)
}

func TestGetExecutionLogsPagination(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	models.DB = db

	// Create test department and owner to satisfy foreign key constraints
	dept := models.Department{Name: "test-pag-dept-" + time.Now().Format("150405.000000")}
	_ = db.Create(&dept).Error
	defer db.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-pag-user-" + time.Now().Format("150405.000000"), Email: "pag@test.com"}
	_ = db.Create(&user).Error
	defer db.Delete(&models.User{}, user.ID)

	// Create test repo and test task type
	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-pagination-repo-" + time.Now().Format("150405.000000"),
		URL:          "http://example.com/pagination-test.git",
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}
	defer db.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{Name: "test-pag-type-" + time.Now().Format("150405.000000"), DisplayName: "测试分页扫描"}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("Failed to create test task type: %v", err)
	}
	defer db.Delete(&models.TaskType{}, taskType.ID)

	// Clean up any old test logs for this repo
	defer db.Where("repo_id = ?", repo.ID).Delete(&models.TaskExecutionLog{})

	// Insert 60 execution logs for this specific repo (to have 3 pages of size 25: 25, 25, 10)
	for i := 1; i <= 60; i++ {
		execLog := models.TaskExecutionLog{
			RepoID:         repo.ID,
			TaskTypeID:     taskType.ID,
			TriggerType:    "manual",
			Status:         "success",
			StatusPriority: 3,
			StartTime:      time.Now(),
			CreatedAt:      time.Now(),
		}
		if err := db.Create(&execLog).Error; err != nil {
			t.Fatalf("Failed to create test log: %v", err)
		}
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/executions", GetExecutionLogs)

	// 1. Query Page 1 (pageSize = 25, repo_id filter)
	req1, _ := http.NewRequest("GET", "/api/executions?page=1&pageSize=25&repo_id="+jsonNum(repo.ID), nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w1.Code)
	}

	var resp1 struct {
		Items      []ExecutionLogResponse `json:"items"`
		Total      int64                  `json:"total"`
		Page       int                    `json:"page"`
		PageSize   int                    `json:"pageSize"`
		TotalPages int                    `json:"totalPages"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("Failed to parse json response: %v", err)
	}

	if resp1.Total != 60 {
		t.Errorf("Expected total 60, got %d", resp1.Total)
	}
	if resp1.TotalPages != 3 {
		t.Errorf("Expected totalPages 3, got %d", resp1.TotalPages)
	}
	if len(resp1.Items) != 25 {
		t.Errorf("Expected 25 items on page 1, got %d", len(resp1.Items))
	}

	// 2. Query Page 2 (pageSize = 25, repo_id filter)
	req2, _ := http.NewRequest("GET", "/api/executions?page=2&pageSize=25&repo_id="+jsonNum(repo.ID), nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w2.Code)
	}

	var resp2 struct {
		Items      []ExecutionLogResponse `json:"items"`
		Total      int64                  `json:"total"`
		Page       int                    `json:"page"`
		PageSize   int                    `json:"pageSize"`
		TotalPages int                    `json:"totalPages"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("Failed to parse json response: %v", err)
	}

	if len(resp2.Items) != 25 {
		t.Errorf("Expected 25 items on page 2, got %d (GORM statement pollution bug!)", len(resp2.Items))
	}

	// 3. Query Page 3 (pageSize = 25, repo_id filter, remaining 10 items)
	req3, _ := http.NewRequest("GET", "/api/executions?page=3&pageSize=25&repo_id="+jsonNum(repo.ID), nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)

	var resp3 struct {
		Items      []ExecutionLogResponse `json:"items"`
		Total      int64                  `json:"total"`
		Page       int                    `json:"page"`
		PageSize   int                    `json:"pageSize"`
		TotalPages int                    `json:"totalPages"`
	}
	if err := json.Unmarshal(w3.Body.Bytes(), &resp3); err != nil {
		t.Fatalf("Failed to parse json response: %v", err)
	}

	if len(resp3.Items) != 10 {
		t.Errorf("Expected 10 items on page 3, got %d", len(resp3.Items))
	}
}

func TestGetDynamicCampaignFindingsStats(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	models.DB = db

	dept := models.Department{Name: "test-finding-dept-" + time.Now().Format("150405.000000")}
	_ = db.Create(&dept).Error
	defer db.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-finding-user-" + time.Now().Format("150405.000000"), Email: "finding@test.com"}
	_ = db.Create(&user).Error
	defer db.Delete(&models.User{}, user.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-finding-repo-" + time.Now().Format("150405.000000"),
		URL:          "http://example.com/finding-test.git",
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}
	defer db.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{Name: "test-finding-type-" + time.Now().Format("150405.000000"), DisplayName: "测试专项分析", IsCampaign: true, CampaignPath: "test-finding-path"}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("Failed to create test task type: %v", err)
	}
	defer db.Delete(&models.TaskType{}, taskType.ID)

	// Clean up findings
	defer db.Where("task_type_id = ?", taskType.ID).Delete(&models.CampaignFinding{})

	// Insert test findings:
	// 2 Critical / 3 Major / 1 Suggestion
	// Status: 3 open / 2 resolved / 1 invalid
	// Category: 4 Concurrency / 2 MemoryLeak
	testFindings := []models.CampaignFinding{
		{RepoID: repo.ID, TaskTypeID: taskType.ID, Title: "Issue 1", FilePath: "a.cpp", Severity: "严重", Status: "open", Category: "并发安全"},
		{RepoID: repo.ID, TaskTypeID: taskType.ID, Title: "Issue 2", FilePath: "b.cpp", Severity: "严重", Status: "open", Category: "并发安全"},
		{RepoID: repo.ID, TaskTypeID: taskType.ID, Title: "Issue 3", FilePath: "c.cpp", Severity: "一般", Status: "open", Category: "并发安全"},
		{RepoID: repo.ID, TaskTypeID: taskType.ID, Title: "Issue 4", FilePath: "d.cpp", Severity: "一般", Status: "resolved", Category: "并发安全"},
		{RepoID: repo.ID, TaskTypeID: taskType.ID, Title: "Issue 5", FilePath: "e.cpp", Severity: "一般", Status: "resolved", Category: "内存泄漏"},
		{RepoID: repo.ID, TaskTypeID: taskType.ID, Title: "Issue 6", FilePath: "f.cpp", Severity: "建议", Status: "invalid", Category: "内存泄漏"},
	}
	for _, f := range testFindings {
		if err := db.Create(&f).Error; err != nil {
			t.Fatalf("Failed to create test finding: %v", err)
		}
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/analysis/:campaign/findings", func(c *gin.Context) {
		c.Set("taskType", &taskType)
		GetDynamicCampaignFindings(c)
	})

	req, _ := http.NewRequest("GET", "/api/analysis/test-finding-path/findings?repo_id="+jsonNum(repo.ID)+"&page=1&page_size=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var resp struct {
		Total         int64                    `json:"total"`
		Page          int                      `json:"page"`
		PageSize      int                      `json:"page_size"`
		Items         []models.CampaignFinding `json:"items"`
		SeverityStats map[string]int           `json:"severityStats"`
		StatusStats   map[string]int           `json:"statusStats"`
		CategoryStats map[string]int           `json:"categoryStats"`
		Categories    []string                 `json:"categories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse json response: %v", err)
	}

	if resp.Total != 6 {
		t.Errorf("Expected total 6, got %d", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Errorf("Expected page 1 items 2, got %d", len(resp.Items))
	}
	if resp.SeverityStats["严重"] != 2 || resp.SeverityStats["一般"] != 3 || resp.SeverityStats["建议"] != 1 {
		t.Errorf("Unexpected SeverityStats: %+v", resp.SeverityStats)
	}
	if resp.StatusStats["open"] != 3 || resp.StatusStats["resolved"] != 2 || resp.StatusStats["invalid"] != 1 {
		t.Errorf("Unexpected StatusStats: %+v", resp.StatusStats)
	}
	if resp.CategoryStats["并发安全"] != 4 || resp.CategoryStats["内存泄漏"] != 2 {
		t.Errorf("Unexpected CategoryStats: %+v", resp.CategoryStats)
	}
	if len(resp.Categories) != 2 || resp.Categories[0] != "并发安全" || resp.Categories[1] != "内存泄漏" {
		t.Errorf("Unexpected Categories ordering: %+v", resp.Categories)
	}
}

func jsonNum(n uint) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestUpdateDynamicCampaignFindingOperator(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	models.DB = db

	dept := models.Department{Name: "test-op-dept-" + time.Now().Format("150405.000000")}
	_ = db.Create(&dept).Error
	defer db.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-op-user-" + time.Now().Format("150405.000000"), Name: "张三测试员", Email: "zhangsan@test.com"}
	_ = db.Create(&user).Error
	defer db.Delete(&models.User{}, user.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-op-repo-" + time.Now().Format("150405.000000"),
		URL:          "http://example.com/op-test.git",
	}
	_ = db.Create(&repo).Error
	defer db.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{Name: "test-op-type-" + time.Now().Format("150405.000000"), DisplayName: "测试操作人扫描"}
	_ = db.Create(&taskType).Error
	defer db.Delete(&models.TaskType{}, taskType.ID)

	finding := models.CampaignFinding{
		TaskTypeID: taskType.ID,
		RepoID:     repo.ID,
		FilePath:   "src/main.cpp",
		LineNumber: "42",
		Title:      "测试缺陷1",
		Severity:   "严重",
		Status:     "open",
	}
	if err := db.Create(&finding).Error; err != nil {
		t.Fatalf("Failed to create finding: %v", err)
	}
	defer db.Delete(&models.CampaignFinding{}, finding.ID)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PATCH("/api/analysis/:campaign/findings/:id", func(c *gin.Context) {
		c.Set("taskType", &taskType)
		commonAuth.SetUserContext(c, &commonAuth.UserContext{
			UserID:   user.ID,
			Name:     user.Name,
			Username: user.Username,
			Email:    user.Email,
		})
		UpdateDynamicCampaignFinding(c)
	})

	updateBody := map[string]interface{}{
		"status":   "analyzing",
		"feedback": "已由张三介入核验",
	}
	bodyBytes, _ := json.Marshal(updateBody)

	req, _ := http.NewRequest("PATCH", fmt.Sprintf("/api/analysis/test-op-path/findings/%d", finding.ID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.CampaignFinding
	if err := db.First(&updated, finding.ID).Error; err != nil {
		t.Fatalf("Failed to query updated finding: %v", err)
	}

	if updated.Status != "analyzing" {
		t.Errorf("Expected status 'analyzing', got '%s'", updated.Status)
	}

	var logs []map[string]interface{}
	if err := json.Unmarshal(updated.StatusLog, &logs); err != nil {
		t.Fatalf("Failed to parse status_log: %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("Expected 1 log entry, got %d", len(logs))
	}

	if logs[0]["user"] != "张三测试员" {
		t.Errorf("Expected operator user to be '张三测试员', got '%v'", logs[0]["user"])
	}
	if logs[0]["comment"] != "已由张三介入核验" {
		t.Errorf("Expected comment '已由张三介入核验', got '%v'", logs[0]["comment"])
	}

	// Test GET single finding by ID
	router.GET("/api/analysis/:campaign/findings/:id", func(c *gin.Context) {
		c.Set("taskType", &taskType)
		GetDynamicCampaignFinding(c)
	})

	getReq, _ := http.NewRequest("GET", fmt.Sprintf("/api/analysis/test-op-path/findings/%d", finding.ID), nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("Expected status 200 for GET finding by ID, got %d: %s", getW.Code, getW.Body.String())
	}

	var fetchedFinding models.CampaignFinding
	if err := json.Unmarshal(getW.Body.Bytes(), &fetchedFinding); err != nil {
		t.Fatalf("Failed to parse fetched finding: %v", err)
	}
	if fetchedFinding.ID != finding.ID || fetchedFinding.Title != "测试缺陷1" {
		t.Errorf("Unexpected fetched finding content: %+v", fetchedFinding)
	}
}

func TestConvertCampaignFindingsToExcelItems(t *testing.T) {
	findings := []models.CampaignFinding{
		{
			ID:       1,
			Title:    "缺陷A",
			Feedback: "旧的Feedback备用",
			StatusLog: []byte(`[
				{"status":"open","time":"2026-08-25 10:00:00","user":"system","reason":"Initial scan discovery"},
				{"status":"analyzing","time":"2026-08-25 11:00:00","user":"开发A","comment":"分析中发现确实存在越界风险"}
			]`),
		},
		{
			ID:       2,
			Title:    "缺陷B",
			Feedback: "直接通过Feedback提供意见",
		},
		{
			ID:    3,
			Title: "缺陷C",
			StatusLog: []byte(`[
				{"status":"open","time":"2026-08-25 10:00:00","user":"system","reason":"Initial scan discovery"}
			]`),
		},
	}

	items := convertCampaignFindingsToExcelItems(findings)
	if len(items) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(items))
	}

	if items[0].Comment != "分析中发现确实存在越界风险" {
		t.Errorf("Expected items[0].Comment to be '分析中发现确实存在越界风险', got '%s'", items[0].Comment)
	}
	if items[1].Comment != "直接通过Feedback提供意见" {
		t.Errorf("Expected items[1].Comment to be '直接通过Feedback提供意见', got '%s'", items[1].Comment)
	}
	if items[2].Comment != "Initial scan discovery" {
		t.Errorf("Expected items[2].Comment to be 'Initial scan discovery', got '%s'", items[2].Comment)
	}
}
