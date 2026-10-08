package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/governance"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func governanceQueryInt(c *gin.Context, name string, defaultValue int) int {
	value, err := strconv.Atoi(c.DefaultQuery(name, strconv.Itoa(defaultValue)))
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func governanceListResponse(c *gin.Context, query *gorm.DB, items any) bool {
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return false
	}
	page := governanceQueryInt(c, "page", 1)
	pageSize := governanceQueryInt(c, "page_size", 50)
	if pageSize > 200 {
		pageSize = 200
	}
	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Find(items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return false
	}
	c.JSON(http.StatusOK, gin.H{
		"items":      items,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
		"total_page": (int(total) + pageSize - 1) / pageSize,
	})
	return true
}

func GetCategoryAliasUsageHandler(c *gin.Context) {
	query := models.DB.Model(&models.CategoryAliasUsage{})
	if value := c.Query("task_type_id"); value != "" {
		if id, err := strconv.ParseUint(value, 10, 64); err == nil {
			query = query.Where("task_type_id = ?", id)
		}
	}
	for name, column := range map[string]string{
		"taxonomy_hash": "taxonomy_hash",
		"alias_kind":    "alias_kind",
		"alias_label":   "alias_label",
	} {
		if value := c.Query(name); value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	query = query.Order("last_seen_at desc")
	var usages []models.CategoryAliasUsage
	governanceListResponse(c, query, &usages)
}

func GetCategoryRegressionSamplesHandler(c *gin.Context) {
	query := models.DB.Model(&models.CategoryRegressionSample{})
	for name, column := range map[string]string{
		"task_type":     "task_type",
		"pair_key":      "pair_key",
		"status":        "status",
		"outcome":       "outcome",
		"taxonomy_hash": "taxonomy_hash",
		"prompt_hash":   "prompt_hash",
		"model_backend": "model_backend",
	} {
		if value := c.Query(name); value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	query = query.Order("sample_key asc")
	var samples []models.CategoryRegressionSample
	governanceListResponse(c, query, &samples)
}

func SupersedeCategoryRegressionSampleHandler(c *gin.Context) {
	sampleID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || sampleID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid regression sample ID"})
		return
	}
	var request struct {
		SupersededBy     string `json:"superseded_by" binding:"required"`
		SupersededReason string `json:"superseded_reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var sample models.CategoryRegressionSample
	if err := models.DB.First(&sample, sampleID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Regression sample not found"})
		return
	}
	if err := models.DB.Model(&sample).Updates(map[string]any{
		"status":            governance.CategorySampleSuperseded,
		"superseded_by":     request.SupersededBy,
		"superseded_reason": request.SupersededReason,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := models.DB.First(&sample, sampleID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sample)
}

func ReviewCategoryRegressionSampleHandler(c *gin.Context) {
	sampleID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || sampleID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid regression sample ID"})
		return
	}
	var request models.CategoryRegressionReview
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.SampleID = uint(sampleID)
	if err := models.DB.Transaction(func(tx *gorm.DB) error {
		return governance.ReviewCategoryRegressionSample(tx, request)
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Regression sample not found"})
			return
		}
		if strings.Contains(err.Error(), "requires") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, request)
}

func GetCategoryConfusionMatricesHandler(c *gin.Context) {
	query := models.DB.Model(&models.CategoryConfusionMatrix{})
	for name, column := range map[string]string{
		"taxonomy_hash": "taxonomy_hash",
		"prompt_hash":   "prompt_hash",
		"model_backend": "model_backend",
	} {
		if value := c.Query(name); value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	if value := c.Query("period_start"); value != "" {
		if period, err := time.Parse(time.RFC3339, value); err == nil {
			query = query.Where("period_start >= ?", period)
		}
	}
	if value := c.Query("period_end"); value != "" {
		if period, err := time.Parse(time.RFC3339, value); err == nil {
			query = query.Where("period_end <= ?", period)
		}
	}
	query = query.Order("period_start desc")
	var matrices []models.CategoryConfusionMatrix
	governanceListResponse(c, query, &matrices)
}
