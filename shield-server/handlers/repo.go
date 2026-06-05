package handlers

import (
	"bytes"
	"code-shield/models"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func GetRepos(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "15"))
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
	query.Count(&total)

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

func CreateRepo(c *gin.Context) {
	var repo models.Repository
	if err := c.ShouldBindJSON(&repo); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := models.DB.Create(&repo).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, repo)
}

func DeleteRepo(c *gin.Context) {
	id := c.Param("id")
	if err := models.DB.Delete(&models.Repository{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete repository"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Repository correctly deleted"})
}

func UpdateRepo(c *gin.Context) {
	id := c.Param("id")

	var repo models.Repository
	if err := models.DB.First(&repo, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Repository not found"})
		return
	}

	var input struct {
		Name           *string   `json:"name"`
		URL            *string   `json:"url"`
		OwnerID        *uint     `json:"owner_id"`
		Branch         *string   `json:"branch"`
		DepartmentID   *uint     `json:"department_id"`
		TeamID         *uint     `json:"team_id"` // fallback
		ServiceGroup   *string   `json:"service_group"`
		RelatedMembers *[]string `json:"related_members"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if input.Name != nil {
		updates["name"] = *input.Name
	}
	if input.URL != nil {
		updates["url"] = *input.URL
	}
	if input.OwnerID != nil {
		updates["owner_id"] = *input.OwnerID
	}
	if input.Branch != nil {
		updates["branch"] = *input.Branch
	}
	if input.DepartmentID != nil {
		updates["department_id"] = *input.DepartmentID
	} else if input.TeamID != nil {
		updates["department_id"] = *input.TeamID
	}
	if input.ServiceGroup != nil {
		updates["service_group"] = *input.ServiceGroup
	}
	if input.RelatedMembers != nil {
		if len(*input.RelatedMembers) == 0 {
			updates["related_members"] = nil // clear
		} else {
			b, _ := json.Marshal(*input.RelatedMembers)
			updates["related_members"] = b
		}
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
		return
	}

	if err := models.DB.Model(&repo).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update repository"})
		return
	}

	// Reload with associations
	models.DB.Preload("Department").Preload("Owner").First(&repo, id)
	c.JSON(http.StatusOK, repo)
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

func ImportRepos(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()

	b, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
		return
	}

	var reader *csv.Reader
	if utf8.Valid(b) {
		reader = csv.NewReader(bytes.NewReader(b))
	} else {
		decodedReader := transform.NewReader(bytes.NewReader(b), simplifiedchinese.GB18030.NewDecoder())
		reader = csv.NewReader(decodedReader)
	}
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read CSV header"})
		return
	}

	headerMap := make(map[string]int)
	for i, col := range header {
		cleanCol := strings.ReplaceAll(strings.TrimRight(strings.TrimLeft(col, "\xef\xbb\xbf\"' \t\r\n"), "\"' \t\r\n"), " ", "")
		cleanCol = strings.ReplaceAll(cleanCol, " ", "")
		headerMap[cleanCol] = i
	}

	requiredHeaders := []string{"服务组", "田主", "代码仓", "RepoURL", "分支", "部门名称"}
	for _, req := range requiredHeaders {
		if _, ok := headerMap[req]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Missing required column: %s", req)})
			return
		}
	}

	records, err := reader.ReadAll()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read CSV records"})
		return
	}

	placeholderPassword, _ := bcrypt.GenerateFromPassword([]byte("imported-account-no-local-password"), bcrypt.DefaultCost)

	successCount := 0
	for lineNum, record := range records {
		if len(record) == 0 {
			continue
		}

		getField := func(key string) string {
			idx, ok := headerMap[key]
			if ok && idx < len(record) {
				return strings.TrimSpace(record[idx])
			}
			return ""
		}

		serviceGroup := getField("服务组")
		ownerName := getField("田主")
		repoName := getField("代码仓")
		repoURL := getField("RepoURL")
		branch := getField("分支")
		departmentName := getField("部门名称")

		if repoName == "" || repoURL == "" || departmentName == "" {
			continue
		}
		if branch == "" {
			branch = "main"
		}

		// Resolve owner: try EmployeeID → Email → Name
		var user models.User
		ownerResolved := false
		if ownerName != "" {
			if err := models.DB.Where("employee_id = ? OR email = ? OR name = ?", ownerName, ownerName, ownerName).First(&user).Error; err == nil {
				ownerResolved = true
			}
		}

		if !ownerResolved && ownerName != "" {
			email := ownerName
			if !strings.Contains(email, "@") {
				email = ownerName + "@imported.code-shield"
			}
			user = models.User{
				EmployeeID: ownerName,
				Name:       ownerName,
				Email:      email,
				Password:   string(placeholderPassword),
				RegMethod:  "imported",
				IsActive:   false,
			}
			if err := models.DB.Create(&user).Error; err == nil {
				ownerResolved = true
			} else {
				log.Printf("Line %d: Failed to auto-create user %s: %v", lineNum+2, ownerName, err)
				continue
			}
		}

		// Find or create Department
		var dept models.Department
		if err := models.DB.Where("name = ?", departmentName).First(&dept).Error; err != nil {
			var leaderID *uint
			if ownerResolved {
				leaderID = &user.ID
			}
			dept = models.Department{
				Name:     departmentName,
				LeaderID: leaderID,
			}
			if err := models.DB.Create(&dept).Error; err != nil {
				log.Printf("Line %d: Failed to create department %s: %v", lineNum+2, departmentName, err)
				continue
			}
		}

		// Insert or update Repository
		var repo models.Repository
		if err := models.DB.Where("name = ?", repoName).First(&repo).Error; err != nil {
			repo = models.Repository{
				DepartmentID: dept.ID,
				Name:         repoName,
				URL:          repoURL,
				OwnerID:      user.ID,
				Branch:       branch,
				ServiceGroup: serviceGroup,
				IsActive:     true,
			}
			if err := models.DB.Create(&repo).Error; err != nil {
				log.Printf("Line %d: Failed to create repository %s: %v", lineNum+2, repoName, err)
				continue
			}
		} else {
			repo.DepartmentID = dept.ID
			repo.URL = repoURL
			repo.OwnerID = user.ID
			repo.Branch = branch
			repo.ServiceGroup = serviceGroup
			if err := models.DB.Save(&repo).Error; err != nil {
				log.Printf("Line %d: Failed to update repository %s: %v", lineNum+2, repoName, err)
				continue
			}
		}
		successCount++
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Successfully imported %d repositories", successCount)})
}
