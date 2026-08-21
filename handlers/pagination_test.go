package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"code-shield/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = "host=127.0.0.1 port=5432 user=postgres password=CodeShield618! dbname=code_shield sslmode=disable"
	}
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Skipf("Skipping DB test: PostgreSQL not available (%v)", err)
		return nil
	}
	return db
}

func TestGetExecutionLogsPagination(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	models.DB = db

	for _, model := range []interface{}{
		&models.Department{},
		&models.User{},
		&models.Repository{},
		&models.TaskType{},
		&models.TaskReport{},
		&models.TaskExecutionLog{},
	} {
		_ = db.AutoMigrate(model)
	}

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

func jsonNum(n uint) string {
	b, _ := json.Marshal(n)
	return string(b)
}
