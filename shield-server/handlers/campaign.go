package handlers

import (
	"code-shield/models"
	"encoding/json"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

// CampaignRepoSummary defines the structure of repository campaign statistics
type CampaignRepoSummary struct {
	RepoID         uint      `json:"repo_id"`
	RepoName       string    `json:"repo_name"`
	RepoURL        string    `json:"repo_url"`
	Department     string    `json:"department"`
	OwnerName      string    `json:"owner_name"`
	TotalIssues    int       `json:"total_issues"`
	Blocking       int       `json:"blocking"`
	Critical       int       `json:"critical"`
	Major          int       `json:"major"`
	Hint           int       `json:"hint"`
	Suggestion     int       `json:"suggestion"`
	OpenIssues     int       `json:"open_issues"`
	ResolvedIssues int       `json:"resolved_issues"`
	FixRate        float64   `json:"fix_rate"`
	LastScanTime   time.Time `json:"last_scan_time"`
}

// Helper: Reflectively get field value
func getFieldValue(obj interface{}, fieldName string) interface{} {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if f.IsValid() {
		return f.Interface()
	}
	return nil
}

// Helper: Reflectively set field value
func setFieldValue(obj interface{}, fieldName string, val interface{}) {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if f.IsValid() && f.CanSet() {
		f.Set(reflect.ValueOf(val))
	}
}

// GetCampaignRepos lists all repositories with aggregated statistics for a generic campaign
func GetCampaignRepos[T any](taskTypeName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		sortBy := c.DefaultQuery("sort_by", "total_issues")
		sortOrder := c.DefaultQuery("sort_order", "desc")
		keyword := c.Query("keyword")
		department := c.Query("department")

		// 1. Fetch repositories
		var repos []models.Repository
		query := models.DB.Preload("Owner").Preload("Team")
		if err := query.Find(&repos).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch repositories"})
			return
		}

		// 2. Fetch campaign findings severity stats
		type DbSeverityStat struct {
			RepoID   uint   `gorm:"column:repo_id"`
			Severity string `gorm:"column:severity"`
			Count    int    `gorm:"column:count"`
		}
		var severityStats []DbSeverityStat
		models.DB.Model(new(T)).
			Select("repo_id, severity, count(*) as count").
			Group("repo_id, severity").
			Scan(&severityStats)

		severityMap := make(map[uint]map[string]int)
		for _, stat := range severityStats {
			if _, ok := severityMap[stat.RepoID]; !ok {
				severityMap[stat.RepoID] = make(map[string]int)
			}
			severityMap[stat.RepoID][stat.Severity] = stat.Count
		}

		// 3. Fetch active issues stats (status is 'open' or 'analyzing')
		type DbStatusStat struct {
			RepoID uint `gorm:"column:repo_id"`
			Count  int  `gorm:"column:count"`
		}
		var statusStats []DbStatusStat
		models.DB.Model(new(T)).
			Select("repo_id, count(*) as count").
			Where("status IN ?", []string{"open", "analyzing"}).
			Group("repo_id").
			Scan(&statusStats)

		statusMap := make(map[uint]int)
		for _, stat := range statusStats {
			statusMap[stat.RepoID] = stat.Count
		}

		// 4. Fetch resolved issues stats
		var resolvedStats []DbStatusStat
		models.DB.Model(new(T)).
			Select("repo_id, count(*) as count").
			Where("status IN ?", []string{"resolved", "closed", "invalid"}).
			Group("repo_id").
			Scan(&resolvedStats)

		resolvedMap := make(map[uint]int)
		for _, stat := range resolvedStats {
			resolvedMap[stat.RepoID] = stat.Count
		}

		// 5. Fetch last scan times from reports
		var taskType models.TaskType
		models.DB.Where("name = ?", taskTypeName).First(&taskType)

		type DbScanTime struct {
			RepoID    uint   `gorm:"column:repo_id"`
			CreatedAt string `gorm:"column:created_at"`
		}
		var scanTimes []DbScanTime
		if taskType.ID > 0 {
			models.DB.Model(&models.TaskReport{}).
				Select("repo_id, max(created_at) as created_at").
				Where("task_type_id = ? AND status = ?", taskType.ID, "success").
				Group("repo_id").
				Scan(&scanTimes)
		}

		scanTimeMap := make(map[uint]time.Time)
		for _, st := range scanTimes {
			if st.CreatedAt != "" {
				layouts := []string{
					"2006-01-02 15:04:05.999999999-07:00",
					"2006-01-02 15:04:05.999999999",
					"2006-01-02 15:04:05",
					time.RFC3339,
				}
				var parsedTime time.Time
				var err error
				for _, layout := range layouts {
					parsedTime, err = time.Parse(layout, st.CreatedAt)
					if err == nil {
						break
					}
				}
				if err == nil {
					scanTimeMap[st.RepoID] = parsedTime
				} else {
					log.Printf("Failed to parse campaign %s scan time %q: %v", taskTypeName, st.CreatedAt, err)
				}
			}
		}

		// 6. Aggregate metrics
		var summaries []CampaignRepoSummary
		for _, repo := range repos {
			// Filter by department
			repoDept := ""
			if repo.Owner.Department != "" {
				repoDept = repo.Owner.Department
			}
			if department != "" && repoDept != department {
				continue
			}

			// Filter by keyword
			if keyword != "" && !strings.Contains(strings.ToLower(repo.Name), strings.ToLower(keyword)) {
				continue
			}

			// Severity metrics
			repoSeverities := severityMap[repo.ID]
			blocking := repoSeverities["阻塞"]
			critical := repoSeverities["严重"]
			major := repoSeverities["主要"]
			hint := repoSeverities["提示"]
			suggestion := repoSeverities["建议"]

			openCount := statusMap[repo.ID]
			resolvedCount := resolvedMap[repo.ID]

			totalDefects := openCount + resolvedCount

			fixRate := 0.0
			if totalDefects > 0 {
				fixRate = (float64(resolvedCount) / float64(totalDefects)) * 100.0
			} else if !scanTimeMap[repo.ID].IsZero() {
				fixRate = 100.0
			}

			summaries = append(summaries, CampaignRepoSummary{
				RepoID:         repo.ID,
				RepoName:       repo.Name,
				RepoURL:        repo.URL,
				Department:     repoDept,
				OwnerName:      repo.Owner.Name,
				TotalIssues:    totalDefects,
				Blocking:       blocking,
				Critical:       critical,
				Major:          major,
				Hint:           hint,
				Suggestion:     suggestion,
				OpenIssues:     openCount,
				ResolvedIssues: resolvedCount,
				FixRate:        fixRate,
				LastScanTime:   scanTimeMap[repo.ID],
			})
		}

		// 7. Sort results
		sort.Slice(summaries, func(i, j int) bool {
			asc := sortOrder == "asc"
			var cmp bool

			switch sortBy {
			case "name":
				cmp = strings.ToLower(summaries[i].RepoName) < strings.ToLower(summaries[j].RepoName)
			case "total_issues":
				cmp = summaries[i].TotalIssues < summaries[j].TotalIssues
			case "blocking":
				cmp = summaries[i].Blocking < summaries[j].Blocking
			case "critical":
				cmp = summaries[i].Critical < summaries[j].Critical
			case "last_scan_time":
				cmp = summaries[i].LastScanTime.Before(summaries[j].LastScanTime)
			case "fix_rate":
				fallthrough
			default:
				cmp = summaries[i].FixRate < summaries[j].FixRate
			}

			if asc {
				return cmp
			}
			return !cmp
		})

		c.JSON(http.StatusOK, summaries)
	}
}

// GetCampaignFindings returns a paginated, filtered findings list
func GetCampaignFindings[T any]() gin.HandlerFunc {
	return func(c *gin.Context) {
		repoIDStr := c.Query("repo_id")
		severity := c.Query("severity")
		status := c.Query("status")
		category := c.Query("category")
		keyword := c.Query("keyword")

		pageStr := c.DefaultQuery("page", "1")
		pageSizeStr := c.DefaultQuery("pageSize", "10")

		page, _ := strconv.Atoi(pageStr)
		pageSize, _ := strconv.Atoi(pageSizeStr)
		if page < 1 {
			page = 1
		}
		if pageSize < 1 {
			pageSize = 10
		}
		if pageSize > 10000 {
			pageSize = 10000
		}

		query := models.DB.Model(new(T)).Preload("Assignee")

		if repoIDStr != "" {
			repoID, _ := strconv.Atoi(repoIDStr)
			query = query.Where("repo_id = ?", repoID)
		}
		if severity != "" {
			query = query.Where("severity = ?", severity)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if category != "" {
			query = query.Where("category = ?", category)
		}
		if keyword != "" {
			query = query.Where("file_path LIKE ? OR title LIKE ? OR detail LIKE ?", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
		}

		var total int64
		query.Count(&total)

		var findings []T
		offset := (page - 1) * pageSize
		err := query.Order("id desc").Offset(offset).Limit(pageSize).Find(&findings).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch findings"})
			return
		}

		totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
		if totalPages < 1 {
			totalPages = 1
		}

		severityStats := make(map[string]int)
		statusStats := make(map[string]int)

		if repoIDStr != "" {
			repoID, _ := strconv.Atoi(repoIDStr)

			var severityCounts []struct {
				Severity string
				Count    int
			}
			models.DB.Model(new(T)).
				Select("severity, count(*) as count").
				Where("repo_id = ?", repoID).
				Group("severity").
				Scan(&severityCounts)

			for _, sc := range severityCounts {
				severityStats[sc.Severity] = sc.Count
			}

			var statusCounts []struct {
				Status string
				Count  int
			}
			models.DB.Model(new(T)).
				Select("status, count(*) as count").
				Where("repo_id = ?", repoID).
				Group("status").
				Scan(&statusCounts)

			for _, sc := range statusCounts {
				statusStats[sc.Status] = sc.Count
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"findings":      findings,
			"total":         total,
			"page":          page,
			"pageSize":      pageSize,
			"totalPages":    totalPages,
			"severityStats": severityStats,
			"statusStats":   statusStats,
		})
	}
}

// UpdateCampaignFinding updates work flow audit log, assignee, and status of a generic CampaignFinding
func UpdateCampaignFinding[T any]() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, _ := strconv.Atoi(idStr)

		var input struct {
			Status     string  `json:"status"`
			AssigneeID *string `json:"assignee_id"`
			Feedback   string  `json:"feedback"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var finding T
		if err := models.DB.First(&finding, id).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
			return
		}

		// Read existing status log
		var statusLog []map[string]interface{}
		logBytesVal := getFieldValue(&finding, "StatusLog")
		if logBytesVal != nil {
			if bytes, ok := logBytesVal.([]byte); ok && len(bytes) > 0 {
				_ = json.Unmarshal(bytes, &statusLog)
			} else if datatypesJson, ok := logBytesVal.(datatypes.JSON); ok && len(datatypesJson) > 0 {
				_ = json.Unmarshal(datatypesJson, &statusLog)
			}
		}

		// Log audit change
		userVal, ok := c.Get("currentUser")
		operator := "system"
		if ok {
			if u, ok := userVal.(*models.User); ok {
				operator = u.Name
			}
		}

		statusLog = append(statusLog, map[string]interface{}{
			"status":  input.Status,
			"time":    time.Now().Format("2006-01-02 15:04:05"),
			"user":    operator,
			"comment": input.Feedback,
		})
		logBytes, _ := json.Marshal(statusLog)

		setFieldValue(&finding, "Status", input.Status)
		setFieldValue(&finding, "StatusLog", datatypes.JSON(logBytes))
		if input.AssigneeID != nil {
			setFieldValue(&finding, "AssigneeID", *input.AssigneeID)
		}

		if err := models.DB.Save(&finding).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update finding"})
			return
		}

		// Preload assignee for response
		models.DB.Preload("Assignee").First(&finding, id)
		c.JSON(http.StatusOK, finding)
	}
}

// CampaignDeptSummary defines department rankings details
type CampaignDeptSummary struct {
	Department     string  `json:"department"`
	TotalIssues    int     `json:"total_issues"`
	OpenIssues     int     `json:"open_issues"`
	ResolvedIssues int     `json:"resolved_issues"`
	FixRate        float64 `json:"fix_rate"`
}

// GetCampaignDepartments aggregates metrics grouped by department
func GetCampaignDepartments[T any]() gin.HandlerFunc {
	return func(c *gin.Context) {
		sortBy := c.DefaultQuery("sort_by", "fix_rate")
		sortOrder := c.DefaultQuery("sort_order", "desc")

		var departments []string
		models.DB.Model(&models.Member{}).Distinct().Pluck("department", &departments)

		var cleanDepts []string
		hasEmpty := false
		for _, dept := range departments {
			if dept != "" {
				cleanDepts = append(cleanDepts, dept)
			} else {
				hasEmpty = true
			}
		}
		if hasEmpty || len(cleanDepts) == 0 {
			cleanDepts = append(cleanDepts, "未知部门")
		}

		var summaries []CampaignDeptSummary
		for _, deptName := range cleanDepts {
			var repos []models.Repository
			var err error
			if deptName == "未知部门" {
				err = models.DB.Joins("Owner").Where("Owner.department = ? OR Owner.department IS NULL", "").Find(&repos).Error
			} else {
				err = models.DB.Joins("Owner").Where("Owner.department = ?", deptName).Find(&repos).Error
			}

			if err != nil || len(repos) == 0 {
				continue
			}

			var repoIDs []uint
			for _, r := range repos {
				repoIDs = append(repoIDs, r.ID)
			}

			var totalIssues, openIssues, resolvedIssues int64

			models.DB.Model(new(T)).
				Where("repo_id IN ?", repoIDs).
				Count(&totalIssues)

			models.DB.Model(new(T)).
				Where("repo_id IN ? AND status IN ?", repoIDs, []string{"open", "analyzing"}).
				Count(&openIssues)

			models.DB.Model(new(T)).
				Where("repo_id IN ? AND status IN ?", repoIDs, []string{"resolved", "closed", "invalid"}).
				Count(&resolvedIssues)

			fixRate := 0.0
			if totalIssues > 0 {
				fixRate = (float64(resolvedIssues) / float64(totalIssues)) * 100.0
			} else {
				fixRate = 100.0
			}

			summaries = append(summaries, CampaignDeptSummary{
				Department:     deptName,
				TotalIssues:    int(totalIssues),
				OpenIssues:     int(openIssues),
				ResolvedIssues: int(resolvedIssues),
				FixRate:        fixRate,
			})
		}

		sort.Slice(summaries, func(i, j int) bool {
			asc := sortOrder == "asc"
			var cmp bool
			switch sortBy {
			case "department":
				cmp = strings.ToLower(summaries[i].Department) < strings.ToLower(summaries[j].Department)
			case "total_issues":
				cmp = summaries[i].TotalIssues < summaries[j].TotalIssues
			case "fix_rate":
				fallthrough
			default:
				cmp = summaries[i].FixRate < summaries[j].FixRate
			}

			if asc {
				return cmp
			}
			return !cmp
		})

		c.JSON(http.StatusOK, summaries)
	}
}

// CampaignTrendPoint represents a single chronological stats unit
type CampaignTrendPoint struct {
	Date        string  `json:"date"`
	TotalIssues int     `json:"total_issues"`
	OpenIssues  int     `json:"open_issues"`
	FixRate     float64 `json:"fix_rate"`
}

// GetCampaignTrends returns historical defect count convergence trends over the past 30 days
func GetCampaignTrends[T any]() gin.HandlerFunc {
	return func(c *gin.Context) {
		daysStr := c.DefaultQuery("days", "30")
		days, _ := strconv.Atoi(daysStr)
		repoIDStr := c.Query("repo_id")
		department := c.Query("department")

		now := time.Now()
		var trendPoints []CampaignTrendPoint

		for i := days - 1; i >= 0; i-- {
			dateStr := now.AddDate(0, 0, -i).Format("2006-01-02")
			tEnd, _ := time.ParseInLocation("2006-01-02 15:04:05", dateStr+" 23:59:59", time.Local)

			queryTotal := models.DB.Model(new(T)).Where("created_at <= ?", tEnd)
			queryOpen := models.DB.Model(new(T)).Where("created_at <= ?", tEnd)

			if repoIDStr != "" {
				repoID, _ := strconv.Atoi(repoIDStr)
				queryTotal = queryTotal.Where("repo_id = ?", repoID)
				queryOpen = queryOpen.Where("repo_id = ?", repoID)
			} else if department != "" {
				var repos []models.Repository
				models.DB.Joins("Owner").Where("Owner.department = ?", department).Find(&repos)
				var repoIDs []uint
				for _, r := range repos {
					repoIDs = append(repoIDs, r.ID)
				}
				if len(repoIDs) > 0 {
					queryTotal = queryTotal.Where("repo_id IN ?", repoIDs)
					queryOpen = queryOpen.Where("repo_id IN ?", repoIDs)
				} else {
					queryTotal = queryTotal.Where("repo_id = 0")
					queryOpen = queryOpen.Where("repo_id = 0")
				}
			}

			var totalIssues, openIssues int64
			queryTotal.Count(&totalIssues)
			queryOpen.Where("status IN ?", []string{"open", "analyzing"}).Count(&openIssues)

			resolvedIssues := totalIssues - openIssues
			fixRate := 100.0
			if totalIssues > 0 {
				fixRate = (float64(resolvedIssues) / float64(totalIssues)) * 100.0
			}

			trendPoints = append(trendPoints, CampaignTrendPoint{
				Date:        dateStr,
				TotalIssues: int(totalIssues),
				OpenIssues:  int(openIssues),
				FixRate:     fixRate,
			})
		}

		c.JSON(http.StatusOK, trendPoints)
	}
}
