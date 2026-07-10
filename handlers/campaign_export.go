package handlers

import (
	"net/http"
	"reflect"
	"strconv"

	"code-shield/models"
	"github.com/gin-gonic/gin"
)

// ExportCampaignFindings 泛型处理器：直接从数据库导出过滤后的专题缺陷至 Excel
func ExportCampaignFindings[T any]() gin.HandlerFunc {
	return func(c *gin.Context) {
		repoIDStr := c.Query("repo_id")
		if repoIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
			return
		}
		repoID, _ := strconv.Atoi(repoIDStr)

		var repo models.Repository
		if err := models.DB.First(&repo, repoID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Repository not found"})
			return
		}

		severity := c.Query("severity")
		status := c.Query("status")
		category := c.Query("category")
		keyword := c.Query("keyword")

		query := models.DB.Model(new(T)).Preload("Assignee").Preload("Repo").Where("repo_id = ?", repoID)

		if severity != "" {
			query = query.Where("severity = ?", severity)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if category != "" {
			query = query.Where("category = ?", category)
		}
		if keyword != "" {
			like := "%" + keyword + "%"
			query = query.Where("file_path LIKE ? OR title LIKE ? OR detail LIKE ?", like, like, like)
		}

		var dbFindings []T
		if err := query.Order("id desc").Find(&dbFindings).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch findings for export"})
			return
		}

		// 根据反射自动匹配专题标题
		typeName := reflect.TypeOf(new(T)).Elem().Name()
		campaignTitle := "专题分析"
		switch typeName {
		case "CoredumpFinding":
			campaignTitle = "Coredump 风险评估"
		case "FloatFinding":
			campaignTitle = "浮点数比较缺陷"
		case "ThreadFinding":
			campaignTitle = "新建线程分析"
		case "CjsonFinding":
			campaignTitle = "cJSON 内存泄漏"
		}

		// 转换并生成 Excel
		sliceVal := reflect.ValueOf(dbFindings)
		items := convertToExcelItems(sliceVal, false)

		generateCampaignExcel(c, repo.Name, campaignTitle, items, false)
	}
}

// ExportUTFindings 直接从数据库导出过滤后的 UT 测试用例评估缺陷至 Excel
func ExportUTFindings(c *gin.Context) {
	repoIDStr := c.Query("repo_id")
	if repoIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}
	repoID, _ := strconv.Atoi(repoIDStr)

	var repo models.Repository
	if err := models.DB.First(&repo, repoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Repository not found"})
		return
	}

	severity := c.Query("severity")
	status := c.Query("status")
	category := c.Query("category")
	keyword := c.Query("keyword")

	query := models.DB.Model(&models.TestCaseFinding{}).Preload("Assignee").Preload("Repo").Where("repo_id = ?", repoID)

	if severity != "" {
		query = query.Where("severity = ?", severity)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("test_case_name LIKE ? OR detail LIKE ? OR file_path LIKE ?", like, like, like)
	}

	var dbFindings []models.TestCaseFinding
	// 按照与页面列表展示相同的排序策略
	err := query.Order("CASE severity WHEN '致命' THEN 1 WHEN '阻塞' THEN 1 WHEN '严重' THEN 2 WHEN '一般' THEN 3 WHEN '主要' THEN 3 WHEN '提示' THEN 3 WHEN '建议' THEN 4 WHEN '合格' THEN 5 ELSE 6 END, file_path, test_case_name").
		Find(&dbFindings).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch UT findings for export"})
		return
	}

	sliceVal := reflect.ValueOf(dbFindings)
	items := convertToExcelItems(sliceVal, true)

	generateCampaignExcel(c, repo.Name, "测试用例有效性评估", items, true)
}
