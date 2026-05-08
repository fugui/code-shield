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

func GetTeams(c *gin.Context) {
	var teams []models.Team
	models.DB.Preload("Leader").Find(&teams)
	c.JSON(http.StatusOK, teams)
}

func CreateTeam(c *gin.Context) {
	var team models.Team
	if err := c.ShouldBindJSON(&team); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := models.DB.Create(&team).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, team)
}

func UpdateTeam(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name     string `json:"name"`
		LeaderID string `json:"leader_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var team models.Team
	if err := models.DB.First(&team, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Team not found"})
		return
	}

	if req.Name != "" {
		team.Name = req.Name
	}
	if req.LeaderID != "" {
		team.LeaderID = req.LeaderID
	}

	if err := models.DB.Save(&team).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update team"})
		return
	}
	c.JSON(http.StatusOK, team)
}

func DeleteTeam(c *gin.Context) {
	id := c.Param("id")

	// Look up the team first so we can use its name for the member check
	var team models.Team
	if err := models.DB.First(&team, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "部门不存在"})
		return
	}

	// Check for associated repositories
	var repoCount int64
	models.DB.Model(&models.Repository{}).Where("team_id = ?", team.ID).Count(&repoCount)
	if repoCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"error": fmt.Sprintf("该部门下仍有 %d 个代码仓，请先移除或转移后再删除部门。", repoCount),
		})
		return
	}

	// Check for associated members (Member.Department stores the team name as a string)
	var memberCount int64
	models.DB.Model(&models.Member{}).Where("department = ?", team.Name).Count(&memberCount)
	if memberCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"error": fmt.Sprintf("该部门下仍有 %d 名人员，请先移除或转移后再删除部门。", memberCount),
		})
		return
	}

	if err := models.DB.Delete(&team).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除部门失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "部门已删除"})
}

// ExportTeams exports all departments as CSV
func ExportTeams(c *gin.Context) {
	var teams []models.Team
	models.DB.Preload("Leader").Find(&teams)

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=departments.csv")

	// Write UTF-8 BOM for Excel compatibility
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	writer.Write([]string{"ID", "部门名称", "负责人ID", "负责人姓名", "创建时间"})
	for _, t := range teams {
		leaderName := ""
		if t.Leader.Name != "" {
			leaderName = t.Leader.Name
		}
		writer.Write([]string{
			fmt.Sprintf("%d", t.ID),
			t.Name,
			t.LeaderID,
			leaderName,
			t.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
}

// ImportTeams imports departments from CSV
func ImportTeams(c *gin.Context) {
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
		headerMap[cleanCol] = i
	}

	requiredHeaders := []string{"部门名称"}
	for _, req := range requiredHeaders {
		if _, ok := headerMap[req]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Missing column: %s. Expected columns: 部门名称, 负责人ID", req)})
			return
		}
	}

	records, err := reader.ReadAll()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read records"})
		return
	}

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

		name := getField("部门名称")
		leaderID := getField("负责人ID")
		if leaderID == "" {
			leaderID = getField("负责人工号")
		}

		if name == "" {
			continue
		}

		var team models.Team
		if err := models.DB.Where("name = ?", name).First(&team).Error; err != nil {
			team = models.Team{
				Name:     name,
				LeaderID: leaderID,
			}
			if err := models.DB.Create(&team).Error; err != nil {
				log.Printf("Line %d: Failed to create team: %v", lineNum+2, err)
			} else {
				successCount++
			}
		} else {
			if leaderID != "" {
				team.LeaderID = leaderID
			}

			if err := models.DB.Save(&team).Error; err == nil {
				successCount++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Successfully imported/updated %d departments", successCount)})
}
