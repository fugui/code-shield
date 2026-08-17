package handlers

import (
	"code-shield/models"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetRepos(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "25"))
	deptID := c.Query("department_id")
	if deptID == "" {
		deptID = c.Query("team_id") // fallback
	}
	serviceGroup := c.Query("service_group")
	owner := c.Query("owner")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 15
	}
	if pageSize > 10000 {
		pageSize = 10000
	}

	query := models.DB.Model(&models.Repository{})

	if deptID != "" {
		query = query.Where("department_id = ?", deptID)
	}
	if serviceGroup != "" {
		query = query.Where("service_group LIKE ?", "%"+serviceGroup+"%")
	}
	if owner != "" {
		query = query.Joins("LEFT JOIN users ON repositories.owner_id = users.id").
			Where("users.name LIKE ? OR users.employee_id LIKE ? OR users.email LIKE ?", "%"+owner+"%", "%"+owner+"%", "%"+owner+"%")
	}

	var total int64
	query.Session(&gorm.Session{}).Count(&total)

	var repos []models.Repository
	offset := (page - 1) * pageSize
	query.Preload("Department").Preload("Owner").Offset(offset).Limit(pageSize).Find(&repos)

	if len(repos) > 0 {
		var repoIDs []uint
		for _, r := range repos {
			repoIDs = append(repoIDs, r.ID)
		}
		type ReportCount struct {
			RepoID uint
			Count  int64
		}
		var counts []ReportCount
		models.DB.Model(&models.TaskReport{}).
			Select("repo_id, COUNT(*) as count").
			Where("repo_id IN ?", repoIDs).
			Group("repo_id").
			Scan(&counts)

		countMap := make(map[uint]int64)
		for _, c := range counts {
			countMap[c.RepoID] = c.Count
		}

		for i := range repos {
			repos[i].ReportCount = countMap[repos[i].ID]
		}
	}

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	c.JSON(http.StatusOK, gin.H{
		"items":      repos,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

// ExportRepos exports all repositories as CSV
func ExportRepos(c *gin.Context) {
	var repos []models.Repository
	models.DB.Preload("Department").Preload("Owner").Find(&repos)

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=repositories.csv")

	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	writer.Write([]string{"ID", "代码仓名称", "Repo URL", "归属部门", "负责人ID", "负责人姓名", "分支", "服务组", "创建时间"})
	for _, r := range repos {
		deptName := ""
		if r.Department.Name != "" {
			deptName = r.Department.Name
		}
		ownerIDStr := ""
		ownerName := ""
		if r.Owner.ID != 0 {
			ownerName = r.Owner.Name
			ownerIDStr = r.Owner.EmployeeID
			if ownerIDStr == "" {
				ownerIDStr = r.Owner.Email
			}
		}
		writer.Write([]string{
			fmt.Sprintf("%d", r.ID),
			r.Name,
			r.URL,
			deptName,
			ownerIDStr,
			ownerName,
			r.Branch,
			r.ServiceGroup,
			r.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
}
