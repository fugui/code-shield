package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"code-shield/models"

	"github.com/gin-gonic/gin"
)

type SyncPayload struct {
	Action string          `json:"action"` // "upsert" or "delete"
	Data   json.RawMessage `json:"data,omitempty"`
	ID     uint            `json:"id,omitempty"`
}

func SyncUser(c *gin.Context) {
	var payload SyncPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Action == "delete" {
		if err := models.DB.Delete(&models.User{}, payload.ID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Deleted user ID %d", payload.ID)
		c.JSON(http.StatusOK, gin.H{"message": "User deleted"})
		return
	}

	var user models.User
	if err := json.Unmarshal(payload.Data, &user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user data"})
		return
	}

	// Double check user.ID is set to the payload ID
	user.ID = payload.ID

	var existing models.User
	if err := models.DB.First(&existing, user.ID).Error; err != nil {
		// Create new
		if err := models.DB.Create(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Created user ID %d (%s)", user.ID, user.Email)
	} else {
		// Update existing
		// Use Map or Updates to update all fields including empty ones
		if err := models.DB.Model(&existing).Updates(map[string]interface{}{
			"email":         user.Email,
			"name":          user.Name,
			"employee_id":   user.EmployeeID,
			"employee_type": user.EmployeeType,
			"unique_id":     user.UniqueID,
			"is_active":     user.IsActive,
			"is_admin":      user.IsAdmin,
			"reg_method":    user.RegMethod,
			"department_id": user.DepartmentID,
			"last_login":    user.LastLogin,
			"last_ip":       user.LastIP,
		}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Updated user ID %d (%s)", user.ID, user.Email)
	}

	c.JSON(http.StatusOK, gin.H{"message": "User synced successfully"})
}

func SyncDepartment(c *gin.Context) {
	var payload SyncPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Action == "delete" {
		if err := models.DB.Delete(&models.Department{}, payload.ID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Deleted department ID %d", payload.ID)
		c.JSON(http.StatusOK, gin.H{"message": "Department deleted"})
		return
	}

	var dept models.Department
	if err := json.Unmarshal(payload.Data, &dept); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid department data"})
		return
	}

	dept.ID = payload.ID

	var existing models.Department
	if err := models.DB.First(&existing, dept.ID).Error; err != nil {
		if err := models.DB.Create(&dept).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Created department ID %d (%s)", dept.ID, dept.Name)
	} else {
		if err := models.DB.Model(&existing).Updates(map[string]interface{}{
			"name":      dept.Name,
			"leader_id": dept.LeaderID,
		}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Updated department ID %d (%s)", dept.ID, dept.Name)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Department synced successfully"})
}

func SyncRepo(c *gin.Context) {
	var payload SyncPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Action == "delete" {
		if err := models.DB.Delete(&models.Repository{}, payload.ID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Deleted repository ID %d", payload.ID)
		c.JSON(http.StatusOK, gin.H{"message": "Repository deleted"})
		return
	}

	var repo models.Repository
	if err := json.Unmarshal(payload.Data, &repo); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid repository data"})
		return
	}

	repo.ID = payload.ID

	var existing models.Repository
	if err := models.DB.First(&existing, repo.ID).Error; err != nil {
		if err := models.DB.Create(&repo).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Created repository ID %d (%s)", repo.ID, repo.Name)
	} else {
		if err := models.DB.Model(&existing).Updates(map[string]interface{}{
			"name":            repo.Name,
			"url":             repo.URL,
			"branch":          repo.Branch,
			"owner_id":        repo.OwnerID,
			"department_id":   repo.DepartmentID,
			"service_group":   repo.ServiceGroup,
			"related_members": repo.RelatedMembers,
			"is_active":       repo.IsActive,
		}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Sync] Updated repository ID %d (%s)", repo.ID, repo.Name)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Repository synced successfully"})
}
