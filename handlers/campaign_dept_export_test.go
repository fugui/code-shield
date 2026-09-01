package handlers

import (
	"bytes"
	"code-common/backend/testdb"
	"code-shield/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

func setupCampaignTestDB(t *testing.T) *gorm.DB {
	gin.SetMode(gin.TestMode)
	db := testdb.SetupIsolatedDB(t, "shield_campaign_export",
		&models.Department{},
		&models.User{},
		&models.Repository{},
		&models.TaskType{},
		&models.TaskReport{},
		&models.CampaignFinding{},
	)
	models.DB = db
	return db
}

func TestDynamicCampaignUnclosedDefectsStats(t *testing.T) {
	db := setupCampaignTestDB(t)
	if db == nil {
		return
	}

	// 1. 创建部门、用户与代码仓
	dept := models.Department{Name: "基础架构部-" + time.Now().Format("150405.000")}
	assert.NoError(t, db.Create(&dept).Error)

	user := models.User{Name: "张三", Email: "zhangsan@example.com"}
	assert.NoError(t, db.Create(&user).Error)

	repo := models.Repository{
		Name:         "test-repo-core",
		URL:          "git@github.com:example/test-repo-core.git",
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
	}
	assert.NoError(t, db.Create(&repo).Error)

	// 2. 创建专项任务类型 (缺陷攻关模式)
	taskType := models.TaskType{
		Name:           "coredump",
		DisplayName:    "Coredump 风险分析",
		CampaignPath:   "coredump",
		IsCampaign:     true,
		GovernanceMode: models.GovernanceModeDefectTracking,
	}
	assert.NoError(t, db.Create(&taskType).Error)

	// 3. 注入缺陷数据：
	//    - 1个 未关闭致命缺陷 (status: open)
	//    - 1个 未关闭严重缺陷 (status: analyzing)
	//    - 1个 已解决致命缺陷 (status: resolved)
	//    - 1个 已关闭严重缺陷 (status: closed)
	//    - 1个 已解决一般缺陷 (status: resolved)
	findings := []models.CampaignFinding{
		{
			TaskTypeID: taskType.ID,
			RepoID:     repo.ID,
			FilePath:   "src/core/ptr.c",
			LineNumber: "42",
			Title:      "空指针解引用高危",
			Severity:   "致命",
			Status:     "open",
			CreatedAt:  time.Now(),
		},
		{
			TaskTypeID: taskType.ID,
			RepoID:     repo.ID,
			FilePath:   "src/core/buf.c",
			LineNumber: "108",
			Title:      "缓冲区越界访问",
			Severity:   "严重",
			Status:     "analyzing",
			CreatedAt:  time.Now(),
		},
		{
			TaskTypeID: taskType.ID,
			RepoID:     repo.ID,
			FilePath:   "src/core/closed_ptr.c",
			LineNumber: "55",
			Title:      "已修复的历史空指针",
			Severity:   "致命",
			Status:     "resolved",
			CreatedAt:  time.Now(),
		},
		{
			TaskTypeID: taskType.ID,
			RepoID:     repo.ID,
			FilePath:   "src/core/closed_buf.c",
			LineNumber: "88",
			Title:      "已关闭的历史越界",
			Severity:   "严重",
			Status:     "closed",
			CreatedAt:  time.Now(),
		},
		{
			TaskTypeID: taskType.ID,
			RepoID:     repo.ID,
			FilePath:   "src/core/mem.c",
			LineNumber: "20",
			Title:      "已修复的一般缺陷",
			Severity:   "一般",
			Status:     "resolved",
			CreatedAt:  time.Now(),
		},
	}
	for i := range findings {
		assert.NoError(t, db.Create(&findings[i]).Error)
	}

	// 4. 调用 FetchCampaignRepoSummaries 验证统计指标
	repoSummaries, err := FetchCampaignRepoSummaries(&taskType, "", "")
	assert.NoError(t, err)
	assert.Len(t, repoSummaries, 1)

	r := repoSummaries[0]
	assert.Equal(t, "test-repo-core", r.RepoName)
	assert.Equal(t, dept.Name, r.Department)
	assert.Equal(t, "张三", r.OwnerName)
	// 验证跟踪缺陷数、致命、严重仅统计未关闭项
	assert.Equal(t, 2, r.TotalIssues, "跟踪缺陷数 (total_issues) 应该仅为未关闭的 2 个")
	assert.Equal(t, 2, r.OpenIssues, "未关闭缺陷数 (open_issues) 应该为 2")
	assert.Equal(t, 1, r.Blocking, "未关闭致命数 (blocking) 应该为 1 (排除已 resolved 的 1 个)")
	assert.Equal(t, 1, r.Critical, "未关闭严重数 (critical) 应该为 1 (排除已 closed 的 1 个)")
	assert.Equal(t, 5, r.TotalDefects, "累计缺陷总数 (total_defects) 应该为 5")
	assert.Equal(t, 3, r.ResolvedIssues, "已解决/关闭数 (resolved_issues) 应该为 3")
	assert.Equal(t, 60.0, r.FixRate, "修复率应为 (3/5)*100 = 60.0%")

	// 5. 验证 GetDynamicCampaignRepos HTTP 接口
	router := gin.New()
	campaignGroup := router.Group("/api/analysis/:campaign")
	campaignGroup.Use(func(c *gin.Context) {
		c.Set("taskType", &taskType)
		c.Next()
	})
	campaignGroup.GET("/repos", GetDynamicCampaignRepos)

	req, _ := http.NewRequest("GET", "/api/analysis/coredump/repos", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	var list []DynamicCampaignRepoSummary
	err = json.Unmarshal(resp.Body.Bytes(), &list)
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, 2, list[0].TotalIssues)
	assert.Equal(t, 1, list[0].Blocking)
	assert.Equal(t, 1, list[0].Critical)
}

func TestExportDynamicCampaignDepartments(t *testing.T) {
	db := setupCampaignTestDB(t)
	if db == nil {
		return
	}

	// 1. 创建测试数据
	dept := models.Department{Name: "云原生与服务端-" + time.Now().Format("150405.000")}
	assert.NoError(t, db.Create(&dept).Error)

	user := models.User{Name: "李四", Email: "lisi@example.com"}
	assert.NoError(t, db.Create(&user).Error)

	repo := models.Repository{
		Name:         "cloud-server",
		URL:          "git@github.com:example/cloud-server.git",
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
	}
	assert.NoError(t, db.Create(&repo).Error)

	taskType := models.TaskType{
		Name:           "coredump-export",
		DisplayName:    "Coredump 风险分析",
		CampaignPath:   "coredump-export",
		IsCampaign:     true,
		GovernanceMode: models.GovernanceModeDefectTracking,
	}
	assert.NoError(t, db.Create(&taskType).Error)

	finding := models.CampaignFinding{
		TaskTypeID: taskType.ID,
		RepoID:     repo.ID,
		FilePath:   "main.go",
		LineNumber: "10",
		Title:      "测试缺陷",
		Severity:   "致命",
		Status:     "open",
		CreatedAt:  time.Now(),
	}
	assert.NoError(t, db.Create(&finding).Error)

	// 2. 路由测试
	router := gin.New()
	campaignGroup := router.Group("/api/analysis/:campaign")
	campaignGroup.Use(func(c *gin.Context) {
		c.Set("taskType", &taskType)
		c.Next()
	})
	campaignGroup.GET("/departments/export", ExportDynamicCampaignDepartments)

	req, _ := http.NewRequest("GET", "/api/analysis/coredump-export/departments/export", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", resp.Header().Get("Content-Type"))
	assert.Contains(t, resp.Header().Get("Content-Disposition"), "attachment; filename=")

	// 3. 读取并解析导出的 Excel 内容
	excelFile, err := excelize.OpenReader(bytes.NewReader(resp.Body.Bytes()))
	assert.NoError(t, err)
	defer func() { _ = excelFile.Close() }()

	sheetList := excelFile.GetSheetList()
	assert.Contains(t, sheetList, "部门治理排行榜")
	assert.Len(t, sheetList, 1, "应该只生成单个二层可折叠的 Sheet")

	// 验证 Sheet 部门治理排行榜表头、部门行、子代码仓行
	rows, err := excelFile.GetRows("部门治理排行榜")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(rows), 3)

	// 表头
	assert.Equal(t, "序号/排名", rows[0][0])
	assert.Equal(t, "部门 / 代码仓", rows[0][1])
	assert.Equal(t, "负责人 / 覆盖仓", rows[0][2])
	assert.Equal(t, "跟踪缺陷数(未关闭)", rows[0][3])

	// 一级部门行
	assert.Equal(t, "1", rows[1][0])
	assert.Equal(t, dept.Name, rows[1][1])
	assert.Equal(t, "1/1 仓", rows[1][2])
	assert.Equal(t, "1", rows[1][3]) // 未整改缺陷
	assert.Equal(t, "1", rows[1][4]) // 部门未关闭致命
	assert.Equal(t, "0", rows[1][5]) // 部门未关闭严重

	// 二级子代码仓行 (大纲级别 1)
	assert.Equal(t, "1.1", rows[2][0])
	assert.Contains(t, rows[2][1], "cloud-server")
	assert.Equal(t, "李四", rows[2][2])
	assert.Equal(t, "1", rows[2][3]) // 跟踪缺陷数(未关闭)
	assert.Equal(t, "1", rows[2][4]) // 致命(未关闭)
	assert.Equal(t, "0", rows[2][5]) // 严重(未关闭)
}
