package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SyncUser 已废弃：微服务已直连 PostgreSQL 数据库共享数据
func SyncUser(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "deprecated: data is shared directly via database"})
}

// SyncDepartment 已废弃：微服务已直连 PostgreSQL 数据库共享数据
func SyncDepartment(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "deprecated: data is shared directly via database"})
}

// SyncRepo 已废弃：微服务已直连 PostgreSQL 数据库共享数据
func SyncRepo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "deprecated: data is shared directly via database"})
}
