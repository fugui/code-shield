package handlers

import (
	"code-shield/models"
	"net/http"

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
	c.JSON(http.StatusOK, config)
}

func UpdateConfig(c *gin.Context) {
	ensureConfigExists()
	var req struct {
		AutoNotify *bool `json:"auto_notify" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var config models.SystemConfig
	models.DB.First(&config, 1)

	config.AutoNotify = *req.AutoNotify
	models.DB.Save(&config)

	c.JSON(http.StatusOK, config)
}
