package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"

	"github.com/gin-gonic/gin"
)

func TestTriggerMissingTasksWithFailedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.SetupIsolatedDB(t, "shield_missing_tasks",
		&models.Department{},
		&models.User{},
		&models.Repository{},
		&models.TaskType{},
		&models.TaskReport{},
		&models.TaskExecutionLog{},
		&models.TaskTriggerLog{},
	)
	if db == nil {
		return
	}
	oldDB := models.DB
	models.DB = db
	defer func() { models.DB = oldDB }()

	// 1. 创建测试部门和用户
	dept := models.Department{Name: "test-missing-dept-" + time.Now().Format("150405.000000")}
	_ = db.Create(&dept).Error
	defer db.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-missing-user-" + time.Now().Format("150405.000000"), Email: "missing@test.com"}
	_ = db.Create(&user).Error
	defer db.Delete(&models.User{}, user.ID)

	// 2. 创建测试任务类型
	taskType := models.TaskType{
		Name:        "test-missing-type-" + time.Now().Format("150405.000000"),
		DisplayName: "测试漏扫补扫类型",
		EngineMode:  "single",
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("创建测试任务类型失败: %v", err)
	}
	defer db.Delete(&models.TaskType{}, taskType.ID)

	// 3. 创建 5 个测试代码仓
	// repo1: 过去 7 天内无任何报告 -> 应该被补扫
	// repo2: 过去 7 天内有报告，状态为 failed -> 应该被补扫（核心修复场景）
	// repo3: 过去 7 天内有报告，状态为 success -> 不应该被补扫
	// repo4: 过去 7 天内有报告，状态为 skipped -> 不应该被补扫
	// repo5: 过去 7 天内有两条报告：3天前 success，1天前 failed -> 最新为 failed，应该被补扫
	repos := make([]models.Repository, 5)
	for i := 0; i < 5; i++ {
		repos[i] = models.Repository{
			DepartmentID: dept.ID,
			OwnerID:      user.ID,
			Name:         fmt.Sprintf("repo-missing-test-%d-%d", i+1, time.Now().UnixNano()),
			URL:          fmt.Sprintf("http://example.com/repo-%d.git", i+1),
			IsActive:     true,
		}
		if err := db.Create(&repos[i]).Error; err != nil {
			t.Fatalf("创建仓库失败: %v", err)
		}
		defer db.Delete(&models.Repository{}, repos[i].ID)
	}

	now := time.Now()

	// repo2: failed 报告 (2 天前)
	r2Report := models.TaskReport{
		RepoID:     repos[1].ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusFailed,
		CreatedAt:  now.Add(-2 * 24 * time.Hour),
	}
	_ = db.Create(&r2Report).Error
	defer db.Delete(&models.TaskReport{}, r2Report.ID)

	// repo3: success 报告 (2 天前)
	r3Report := models.TaskReport{
		RepoID:     repos[2].ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusSuccess,
		CreatedAt:  now.Add(-2 * 24 * time.Hour),
	}
	_ = db.Create(&r3Report).Error
	defer db.Delete(&models.TaskReport{}, r3Report.ID)

	// repo4: skipped 报告 (2 天前)
	r4Report := models.TaskReport{
		RepoID:     repos[3].ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusSkipped,
		CreatedAt:  now.Add(-2 * 24 * time.Hour),
	}
	_ = db.Create(&r4Report).Error
	defer db.Delete(&models.TaskReport{}, r4Report.ID)

	// repo5: 3 天前 success，1 天前 failed
	r5Old := models.TaskReport{
		RepoID:     repos[4].ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusSuccess,
		CreatedAt:  now.Add(-3 * 24 * time.Hour),
	}
	_ = db.Create(&r5Old).Error
	defer db.Delete(&models.TaskReport{}, r5Old.ID)

	r5New := models.TaskReport{
		RepoID:     repos[4].ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusFailed,
		CreatedAt:  now.Add(-1 * 24 * time.Hour),
	}
	_ = db.Create(&r5New).Error
	defer db.Delete(&models.TaskReport{}, r5New.ID)

	// 4. 调用 TriggerMissingTasks
	router := gin.New()
	router.POST("/api/tasks/trigger-missing", TriggerMissingTasks)

	reqBody := map[string]interface{}{
		"task_type_id": taskType.ID,
		"days":         7,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/tasks/trigger-missing", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("预期状态码 200, 实际得到: %d, 响应: %s", w.Code, w.Body.String())
	}

	// 5. 校验 TaskTriggerLog 中的结果
	var triggerLog models.TaskTriggerLog
	if err := db.Where("task_type_id = ?", taskType.ID).Order("id desc").First(&triggerLog).Error; err != nil {
		t.Fatalf("查询 TriggerLog 失败: %v", err)
	}
	defer db.Delete(&models.TaskTriggerLog{}, triggerLog.ID)

	// 应该被纳入补扫的仓库共有 3 个 (repo1: 未扫描, repo2: 失败, repo5: 最新失败)
	// repo3(成功) 和 repo4(跳过) 应当被排除
	if triggerLog.TotalRepos != 3 {
		t.Errorf("预期纳入补扫仓库数为 3 (repo1, repo2, repo5)，实际得到: %d", triggerLog.TotalRepos)
	}
}
