package handlers

import (
	"code-shield/models"
	"code-shield/services"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ensureConfig exists ensures a row with ID=1 exists in SystemConfig
func ensureConfigExists() {
	var config models.SystemConfig
	res := models.DB.First(&config, 1)
	if res.Error != nil {
		config = models.SystemConfig{ID: 1, AutoNotify: false}
		models.DB.Create(&config)
	}
}

func GetConfig(c *gin.Context) {
	ensureConfigExists()
	var config models.SystemConfig
	models.DB.First(&config, 1)

	scale, expiresAt := services.Dispatcher.GetScaleAndExpiration()
	var expiresPtr *time.Time
	if !expiresAt.IsZero() {
		expiresPtr = &expiresAt
	}

	c.JSON(http.StatusOK, gin.H{
		"id":                config.ID,
		"auto_notify":       config.AutoNotify,
		"concurrency_scale": scale,
		"scale_expires_at":  expiresPtr,
	})
}

func UpdateConfig(c *gin.Context) {
	ensureConfigExists()
	var req struct {
		AutoNotify       *bool    `json:"auto_notify"`
		ConcurrencyScale *float64 `json:"concurrency_scale"`
		DurationHours    *float64 `json:"duration_hours"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var config models.SystemConfig
	models.DB.First(&config, 1)

	if req.AutoNotify != nil {
		config.AutoNotify = *req.AutoNotify
		models.DB.Save(&config)
	}

	if req.ConcurrencyScale != nil {
		var dur time.Duration
		if req.DurationHours != nil && *req.DurationHours > 0 {
			dur = time.Duration(*req.DurationHours * float64(time.Hour))
		}
		services.Dispatcher.SetScale(*req.ConcurrencyScale, dur)
	}

	scale, expiresAt := services.Dispatcher.GetScaleAndExpiration()
	var expiresPtr *time.Time
	if !expiresAt.IsZero() {
		expiresPtr = &expiresAt
	}

	c.JSON(http.StatusOK, gin.H{
		"id":                config.ID,
		"auto_notify":       config.AutoNotify,
		"concurrency_scale": scale,
		"scale_expires_at":  expiresPtr,
	})
}

