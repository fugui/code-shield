package handlers

import (
	"code-shield/models"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// FloatRepoSummary defines the structure of repository float comparison statistics
type FloatRepoSummary struct {
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

// GetFloatRepos lists all repositories with aggregated python float comparison statistics
func GetFloatRepos(c *gin.Context) {
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

	// 2. Fetch float findings severity stats
	type DbSeverityStat struct {
		RepoID   uint   `gorm:"column:repo_id"`
		Severity string `gorm:"column:severity"`
		Count    int    `gorm:"column:count"`
	}
	var severityStats []DbSeverityStat
	models.DB.Model(&models.FloatFinding{}).
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
	models.DB.Model(&models.FloatFinding{}).
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
	models.DB.Model(&models.FloatFinding{}).
		Select("repo_id, count(*) as count").
		Where("status IN ?", []string{"resolved", "closed", "invalid"}).
		Group("repo_id").
		Scan(&resolvedStats)

	resolvedMap := make(map[uint]int)
	for _, stat := range resolvedStats {
		resolvedMap[stat.RepoID] = stat.Count
	}

	// 5. Fetch last scan times from reports for 'float_comparison'
	var taskType models.TaskType
	models.DB.Where("name = ?", "float_comparison").First(&taskType)

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
				log.Printf("Failed to parse float scan time %q: %v", st.CreatedAt, err)
			}
		}
	}

	// 6. Aggregate metrics
	var summaries []FloatRepoSummary
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

		// Total defects = openCount + resolvedCount
		totalDefects := openCount + resolvedCount

		fixRate := 0.0
		if totalDefects > 0 {
			fixRate = (float64(resolvedCount) / float64(totalDefects)) * 100.0
		} else if !scanTimeMap[repo.ID].IsZero() {
			// Scanned but 0 defects found = 100% clean/fix rate
			fixRate = 100.0
		}

		summaries = append(summaries, FloatRepoSummary{
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

// GetFloatFindings returns a paginated, filtered findings list of FloatFinding
func GetFloatFindings(c *gin.Context) {
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
		pageSize = 10000 // Support full listing
	}

	query := models.DB.Model(&models.FloatFinding{}).Preload("Assignee")

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

	var findings []models.FloatFinding
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
		models.DB.Model(&models.FloatFinding{}).
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
		models.DB.Model(&models.FloatFinding{}).
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

// UpdateFloatFinding updates the workflow audit log, assignee, and status of a float comparison defect
func UpdateFloatFinding(c *gin.Context) {
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

	var finding models.FloatFinding
	if err := models.DB.First(&finding, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
		return
	}

	// Read existing status log
	var statusLog []map[string]interface{}
	if len(finding.StatusLog) > 0 {
		_ = json.Unmarshal(finding.StatusLog, &statusLog)
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

	finding.Status = input.Status
	finding.StatusLog = logBytes
	if input.AssigneeID != nil {
		finding.AssigneeID = *input.AssigneeID
	}

	if err := models.DB.Save(&finding).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update finding"})
		return
	}

	// Preload assignee for response
	models.DB.Preload("Assignee").First(&finding, finding.ID)
	c.JSON(http.StatusOK, finding)
}

// FloatDeptSummary defines department rankings details
type FloatDeptSummary struct {
	Department     string  `json:"department"`
	TotalIssues    int     `json:"total_issues"`
	OpenIssues     int     `json:"open_issues"`
	ResolvedIssues int     `json:"resolved_issues"`
	FixRate        float64 `json:"fix_rate"`
}

// GetFloatDepartments aggregates metrics grouped by organization department
func GetFloatDepartments(c *gin.Context) {
	sortBy := c.DefaultQuery("sort_by", "fix_rate")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// 1. Fetch all distinct departments from members table
	var departments []string
	models.DB.Model(&models.Member{}).Distinct().Pluck("department", &departments)

	// Filter out empty department name
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

	var summaries []FloatDeptSummary
	for _, deptName := range cleanDepts {
		// Filter repositories belonging to this department
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

		// Aggregate defect metrics
		var totalIssues, openIssues, resolvedIssues int64

		models.DB.Model(&models.FloatFinding{}).
			Where("repo_id IN ?", repoIDs).
			Count(&totalIssues)

		models.DB.Model(&models.FloatFinding{}).
			Where("repo_id IN ? AND status IN ?", repoIDs, []string{"open", "analyzing"}).
			Count(&openIssues)

		models.DB.Model(&models.FloatFinding{}).
			Where("repo_id IN ? AND status IN ?", repoIDs, []string{"resolved", "closed", "invalid"}).
			Count(&resolvedIssues)

		fixRate := 0.0
		if totalIssues > 0 {
			fixRate = (float64(resolvedIssues) / float64(totalIssues)) * 100.0
		} else {
			// Scanned but zero defects = 100% clean
			fixRate = 100.0
		}

		summaries = append(summaries, FloatDeptSummary{
			Department:     deptName,
			TotalIssues:    int(totalIssues),
			OpenIssues:     int(openIssues),
			ResolvedIssues: int(resolvedIssues),
			FixRate:        fixRate,
		})
	}

	// Sort results
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

// FloatTrendPoint represents a single chronological stats unit
type FloatTrendPoint struct {
	Date        string  `json:"date"`
	TotalIssues int     `json:"total_issues"`
	OpenIssues  int     `json:"open_issues"`
	FixRate     float64 `json:"fix_rate"`
}

// GetFloatTrends returns historical defect count convergence trends over the past 30 days
func GetFloatTrends(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "30")
	days, _ := strconv.Atoi(daysStr)
	repoIDStr := c.Query("repo_id")
	department := c.Query("department")

	now := time.Now()
	var trendPoints []FloatTrendPoint

	// Generate past 30 daily data points
	for i := days - 1; i >= 0; i-- {
		dateStr := now.AddDate(0, 0, -i).Format("2006-01-02")
		tEnd, _ := time.ParseInLocation("2006-01-02 15:04:05", dateStr+" 23:59:59", time.Local)

		queryTotal := models.DB.Model(&models.FloatFinding{}).Where("created_at <= ?", tEnd)
		queryOpen := models.DB.Model(&models.FloatFinding{}).Where("created_at <= ?", tEnd)

		// Filter by repo
		if repoIDStr != "" {
			repoID, _ := strconv.Atoi(repoIDStr)
			queryTotal = queryTotal.Where("repo_id = ?", repoID)
			queryOpen = queryOpen.Where("repo_id = ?", repoID)
		} else if department != "" {
			// Filter by department
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

		// Note: we fetch historical state snapshots defensively.
		// For simplicity, we aggregate active issues on that date using soft status log checks,
		// or standard current tracking snapshots filtered by creation date.
		queryTotal.Count(&totalIssues)
		queryOpen.Where("status IN ?", []string{"open", "analyzing"}).Count(&openIssues)

		resolvedIssues := totalIssues - openIssues
		fixRate := 100.0
		if totalIssues > 0 {
			fixRate = (float64(resolvedIssues) / float64(totalIssues)) * 100.0
		}

		trendPoints = append(trendPoints, FloatTrendPoint{
			Date:        dateStr,
			TotalIssues: int(totalIssues),
			OpenIssues:  int(openIssues),
			FixRate:     fixRate,
		})
	}

	c.JSON(http.StatusOK, trendPoints)
}
