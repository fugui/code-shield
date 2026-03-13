package handlers

import (
	"bytes"
	"code-shield/models"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func GetRepos(c *gin.Context) {
	var repos []models.Repository
	models.DB.Preload("Team").Preload("Owner").Find(&repos)
	c.JSON(http.StatusOK, repos)
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

	// Read all bytes to check encoding
	b, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
		return
	}

	var reader *csv.Reader
	if utf8.Valid(b) {
		reader = csv.NewReader(bytes.NewReader(b))
	} else {
		// Assume GBK/GB18030 if not valid UTF-8
		decodedReader := transform.NewReader(bytes.NewReader(b), simplifiedchinese.GB18030.NewDecoder())
		reader = csv.NewReader(decodedReader)
	}

	// Some CSVs may not have the same number of fields per record, but we expect it.
	reader.FieldsPerRecord = -1

	// Read header
	header, err := reader.Read()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read CSV header"})
		return
	}

	// Map headers to indices
	headerMap := make(map[string]int)
	log.Printf("Original CSV headers: %v", header)
	for i, col := range header {
		// Clean BOM, spaces, quotes, and invisible characters
		cleanCol := strings.TrimRight(strings.TrimLeft(col, "\xef\xbb\xbf\"' \t\r\n"), "\"' \t\r\n")
		// Also remove any internal spaces for safety if they somehow matched exactly before but had extra space inserted by Excel
		cleanCol = strings.ReplaceAll(cleanCol, " ", "")
		headerMap[cleanCol] = i
	}
	log.Printf("Cleaned CSV header map: %v", headerMap)

	// Expecting columns: "服务组" (ServiceGroup), "田主" (OwnerName), "代码仓" (Name), "RepoURL", "分支" (Branch), "部门名称" (Department)
	// Note: We removed spaces above, so "Repo URL" becomes "RepoURL"
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

	successCount := 0
	for lineNum, record := range records {
		if len(record) == 0 {
			continue
		}
		
		// Safely get string from record based on header map, handle potentially truncated rows
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
		repoURL := getField("RepoURL") // Fixed: Removed space to match headerMap
		branch := getField("分支")
		departmentName := getField("部门名称")

		if repoName == "" || repoURL == "" || departmentName == "" {
			continue // Mandatory fields
		}
		if branch == "" {
			branch = "main" // Default branch
		}

		// Use a transaction for safer operations? GORM does not strictly require here unless we want to rollback per line.
		// For simplicity, handle directly.
		
		// Find or Create Member (Owner) based purely on name parsing
		var member models.Member
		if err := models.DB.Where("id = ?", ownerName).First(&member).Error; err != nil {
			member = models.Member{
				ID:   ownerName,
				Name: ownerName,
			}
			if err := models.DB.Create(&member).Error; err != nil {
				log.Printf("Line %d: Failed to auto-create member %s: %v", lineNum+2, ownerName, err)
			}
		}

		// Find or create Team (Department)
		var team models.Team
		if err := models.DB.Where("name = ?", departmentName).First(&team).Error; err != nil {
			team = models.Team{
				Name:     departmentName,
				LeaderID: ownerName,
			}
			if err := models.DB.Create(&team).Error; err != nil {
				log.Printf("Line %d: Failed to create team %s: %v", lineNum+2, departmentName, err)
				continue
			}
		}

		// Insert or update Repository
		var repo models.Repository
		if err := models.DB.Where("name = ?", repoName).First(&repo).Error; err != nil {
			repo = models.Repository{
				TeamID:       team.ID,
				Name:         repoName,
				URL:          repoURL,
				OwnerID:      ownerName, // Using the string ID
				Branch:       branch,
				ServiceGroup: serviceGroup,
				IsActive:     true,
			}
			if err := models.DB.Create(&repo).Error; err != nil {
				log.Printf("Line %d: Failed to create repository %s: %v", lineNum+2, repoName, err)
				continue
			}
		} else {
			// Update existing
			repo.TeamID = team.ID
			repo.URL = repoURL
			repo.OwnerID = ownerName
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
