package handlers

import (
	"net/http"
	"strconv"

	"code-shield/models"
	"code-shield/services/defectlifecycle"

	"github.com/gin-gonic/gin"
)

// GetMyFindings projects ledger defects and the latest scan finding payload.
func GetMyFindings(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	uid := userID.(uint)
	pageNumber, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "25"))
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 25
	}

	workbenchPage, err := defectlifecycle.ListMyDefects(models.DB, defectlifecycle.WorkbenchQuery{
		UserID: uid, Page: pageNumber, PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, workbenchPage)
}
