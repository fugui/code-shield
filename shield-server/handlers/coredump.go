package handlers

import (
	"code-shield/models"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CoredumpRepoSummary defines the structure of repository statistics
type CoredumpRepoSummary struct {
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

// GetCoredumpRepos lists all repositories with aggregated coredump statistics
func GetCoredumpRepos(c *gin.Context) {
	sortBy := c.DefaultQuery("sort_by", "fix_rate")
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

	// 2. Fetch coredump findings severity stats
	type DbSeverityStat struct {
		RepoID   uint   `gorm:"column:repo_id"`
		Severity string `gorm:"column:severity"`
		Count    int    `gorm:"column:count"`
	}
	var severityStats []DbSeverityStat
	models.DB.Model(&models.CoredumpFinding{}).
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
	models.DB.Model(&models.CoredumpFinding{}).
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
	models.DB.Model(&models.CoredumpFinding{}).
		Select("repo_id, count(*) as count").
		Where("status IN ?", []string{"resolved", "closed", "invalid"}).
		Group("repo_id").
		Scan(&resolvedStats)

	resolvedMap := make(map[uint]int)
	for _, stat := range resolvedStats {
		resolvedMap[stat.RepoID] = stat.Count
	}

	// 5. Fetch last scan times from reports for 'coredump_risk'
	var taskType models.TaskType
	models.DB.Where("name = ?", "coredump_risk").First(&taskType)

	type DbScanTime struct {
		RepoID    uint      `gorm:"column:repo_id"`
		CreatedAt time.Time `gorm:"column:created_at"`
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
		scanTimeMap[st.RepoID] = st.CreatedAt
	}

	// 6. Aggregate metrics
	var summaries []CoredumpRepoSummary
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

		// Add up resolved ones if any not counted in active severities
		// To be safe, total defects tracked = openCount + resolvedCount
		totalDefects := openCount + resolvedCount

		fixRate := 0.0
		if totalDefects > 0 {
			fixRate = (float64(resolvedCount) / float64(totalDefects)) * 100.0
		} else if !scanTimeMap[repo.ID].IsZero() {
			// Scanned but 0 defects found = 100% clean/fix rate
			fixRate = 100.0
		}

		summaries = append(summaries, CoredumpRepoSummary{
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

// GetCoredumpFindings returns a filtered, paginated list of coredump findings for a repository
func GetCoredumpFindings(c *gin.Context) {
	repoIDStr := c.Query("repo_id")
	if repoIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}
	repoID, _ := strconv.Atoi(repoIDStr)

	severity := c.Query("severity")
	status := c.Query("status")
	category := c.Query("category")
	keyword := c.Query("keyword")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := models.DB.Model(&models.CoredumpFinding{}).Where("repo_id = ?", repoID).Preload("Assignee")

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
		like := "%" + keyword + "%"
		query = query.Where("title LIKE ? OR detail LIKE ? OR file_path LIKE ?", like, like, like)
	}

	var total int64
	query.Count(&total)

	var findings []models.CoredumpFinding
	offset := (page - 1) * pageSize
	query.Order("CASE severity WHEN '阻塞' THEN 1 WHEN '严重' THEN 2 WHEN '主要' THEN 3 WHEN '提示' THEN 4 WHEN '建议' THEN 5 ELSE 6 END, file_path").
		Offset(offset).Limit(pageSize).Find(&findings)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	c.JSON(http.StatusOK, gin.H{
		"items":      findings,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

// UpdateCoredumpFinding updates a coredump finding's status, assignee, and logs workflow history
func UpdateCoredumpFinding(c *gin.Context) {
	id := c.Param("id")
	var finding models.CoredumpFinding
	if err := models.DB.First(&finding, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
		return
	}

	var req struct {
		Status     *string `json:"status"`
		AssigneeID *string `json:"assignee_id"`
		Feedback   *string `json:"feedback"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userName := "system"
	if u, exists := c.Get("user"); exists {
		if usr, ok := u.(*models.User); ok {
			userName = usr.Name
		}
	}

	updates := map[string]interface{}{}
	if req.AssigneeID != nil {
		updates["assignee_id"] = *req.AssigneeID
	}

	if req.Status != nil && *req.Status != "" {
		updates["status"] = *req.Status

		// Append status timeline log
		var logEntries []map[string]interface{}
		if len(finding.StatusLog) > 0 {
			_ = json.Unmarshal(finding.StatusLog, &logEntries)
		}

		comment := ""
		if req.Feedback != nil {
			comment = *req.Feedback
		}

		logEntries = append(logEntries, map[string]interface{}{
			"status":  *req.Status,
			"time":    time.Now().Format("2006-01-02 15:04:05"),
			"user":    userName,
			"comment": comment,
		})
		logBytes, _ := json.Marshal(logEntries)
		updates["status_log"] = logBytes
	}

	if len(updates) > 0 {
		models.DB.Model(&finding).Updates(updates)
	}

	// Reload finding with fully loaded assignee
	models.DB.Preload("Assignee").First(&finding, id)
	c.JSON(http.StatusOK, finding)
}

// CoredumpDeptSummary defines the structure of department statistics
type CoredumpDeptSummary struct {
	Department   string  `json:"department"`
	ScannedRepos int     `json:"scanned_repos"`
	TotalIssues  int     `json:"total_issues"`
	Blocking     int     `json:"blocking"`
	Critical     int     `json:"critical"`
	Major        int     `json:"major"`
	Hint         int     `json:"hint"`
	Suggestion   int     `json:"suggestion"`
	OpenIssues   int     `json:"open_issues"`
	FixRate      float64 `json:"fix_rate"`
}

// GetCoredumpDepartments aggregates statistics grouped by organization department
func GetCoredumpDepartments(c *gin.Context) {
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

	var summaries []CoredumpDeptSummary

	for _, dept := range cleanDepts {
		var memberIDs []string
		if dept == "未知部门" {
			models.DB.Model(&models.Member{}).Where("department = '' OR department IS NULL").Pluck("id", &memberIDs)
		} else {
			models.DB.Model(&models.Member{}).Where("department = ?", dept).Pluck("id", &memberIDs)
		}

		if len(memberIDs) == 0 {
			continue
		}

		// Find repos owned by members of this department
		var repoIDs []uint
		models.DB.Model(&models.Repository{}).Where("owner_id IN ?", memberIDs).Pluck("id", &repoIDs)
		if len(repoIDs) == 0 {
			continue
		}

		// Aggregate coredump findings
		type DeptStats struct {
			Severity string
			Status   string
			Count    int
		}
		var stats []DeptStats
		models.DB.Model(&models.CoredumpFinding{}).
			Select("severity, status, count(*) as count").
			Where("repo_id IN ?", repoIDs).
			Group("severity, status").
			Scan(&stats)

		blocking := 0
		critical := 0
		major := 0
		hint := 0
		suggestion := 0
		openIssues := 0
		resolvedIssues := 0

		for _, s := range stats {
			if s.Severity == "阻塞" {
				blocking += s.Count
			} else if s.Severity == "严重" {
				critical += s.Count
			} else if s.Severity == "主要" {
				major += s.Count
			} else if s.Severity == "提示" {
				hint += s.Count
			} else if s.Severity == "建议" {
				suggestion += s.Count
			}

			if s.Status == "open" || s.Status == "analyzing" {
				openIssues += s.Count
			} else {
				resolvedIssues += s.Count
			}
		}

		// Get scanned repositories count
		var scannedCount int64
		models.DB.Model(&models.CoredumpFinding{}).
			Where("repo_id IN ?", repoIDs).
			Distinct("repo_id").
			Count(&scannedCount)

		totalIssues := openIssues + resolvedIssues
		if totalIssues == 0 && scannedCount == 0 {
			continue
		}

		fixRate := 0.0
		if totalIssues > 0 {
			fixRate = (float64(resolvedIssues) / float64(totalIssues)) * 100.0
		} else if scannedCount > 0 {
			fixRate = 100.0
		}

		summaries = append(summaries, CoredumpDeptSummary{
			Department:   dept,
			ScannedRepos: int(scannedCount),
			TotalIssues:  totalIssues,
			Blocking:     blocking,
			Critical:     critical,
			Major:        major,
			Hint:         hint,
			Suggestion:   suggestion,
			OpenIssues:   openIssues,
			FixRate:      fixRate,
		})
	}

	// Sort summaries
	sort.Slice(summaries, func(i, j int) bool {
		asc := sortOrder == "asc"
		var cmp bool

		switch sortBy {
		case "department":
			cmp = summaries[i].Department < summaries[j].Department
		case "scanned_repos":
			cmp = summaries[i].ScannedRepos < summaries[j].ScannedRepos
		case "total_issues":
			cmp = summaries[i].TotalIssues < summaries[j].TotalIssues
		case "open_issues":
			cmp = summaries[i].OpenIssues < summaries[j].OpenIssues
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

// CoredumpTrendPoint defines a single historical trend point
type CoredumpTrendPoint struct {
	Date        string `json:"date"`
	TotalIssues int    `json:"total_issues"`
	Blocking    int    `json:"blocking"`
	Critical    int    `json:"critical"`
	Major       int    `json:"major"`
	Hint        int    `json:"hint"`
	Suggestion  int    `json:"suggestion"`
}

// GetCoredumpTrends retrieves historical trend data points over a 30-day window
func GetCoredumpTrends(c *gin.Context) {
	repoIDStr := c.Query("repo_id")
	department := c.Query("department")
	daysStr := c.DefaultQuery("days", "30")
	days, _ := strconv.Atoi(daysStr)
	if days <= 0 || days > 90 {
		days = 30
	}

	// 1. Resolve coredump_risk task type
	var taskType models.TaskType
	models.DB.Where("name = ?", "coredump_risk").First(&taskType)
	if taskType.ID == 0 {
		c.JSON(http.StatusOK, []CoredumpTrendPoint{})
		return
	}

	// 2. Determine target repository IDs
	var targetRepoIDs []uint
	if repoIDStr != "" {
		rid, _ := strconv.Atoi(repoIDStr)
		targetRepoIDs = append(targetRepoIDs, uint(rid))
	} else if department != "" {
		// Filter owners in this department
		var memberIDs []string
		if department == "未知部门" {
			models.DB.Model(&models.Member{}).Where("department = '' OR department IS NULL").Pluck("id", &memberIDs)
		} else {
			models.DB.Model(&models.Member{}).Where("department = ?", department).Pluck("id", &memberIDs)
		}
		if len(memberIDs) > 0 {
			models.DB.Model(&models.Repository{}).Where("owner_id IN ?", memberIDs).Pluck("id", &targetRepoIDs)
		}
		if len(targetRepoIDs) == 0 {
			c.JSON(http.StatusOK, []CoredumpTrendPoint{})
			return
		}
	}

	// 3. Fetch successful reports in the last N days
	startTime := time.Now().AddDate(0, 0, -days)
	var reports []models.TaskReport
	dbQuery := models.DB.Where("task_type_id = ? AND status = ? AND created_at >= ?", taskType.ID, "success", startTime)
	if len(targetRepoIDs) > 0 {
		dbQuery = dbQuery.Where("repo_id IN ?", targetRepoIDs)
	}

	if err := dbQuery.Order("created_at asc").Find(&reports).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch task reports"})
		return
	}

	// 4. Group metrics by date (YYYY-MM-DD)
	type DailyAgg struct {
		Blocking   int
		Critical   int
		Major      int
		Hint       int
		Suggestion int
	}
	dailyData := make(map[string]*DailyAgg)
	var dateKeys []string

	for _, report := range reports {
		dateStr := report.CreatedAt.Format("2006-01-02")

		// Parse metrics
		var metrics map[string]int
		if len(report.Metrics) > 0 {
			_ = json.Unmarshal(report.Metrics, &metrics)
		}

		if metrics == nil {
			continue
		}

		blocking := metrics["blocking"]
		critical := metrics["critical"]
		major := metrics["major"]
		hint := metrics["hint"]
		suggestion := metrics["suggestion"]

		if _, exists := dailyData[dateStr]; !exists {
			dailyData[dateStr] = &DailyAgg{}
			dateKeys = append(dateKeys, dateStr)
		}

		dailyData[dateStr].Blocking += blocking
		dailyData[dateStr].Critical += critical
		dailyData[dateStr].Major += major
		dailyData[dateStr].Hint += hint
		dailyData[dateStr].Suggestion += suggestion
	}

	sort.Strings(dateKeys)

	// 5. Build chronological trend points
	var trends []CoredumpTrendPoint
	for _, dKey := range dateKeys {
		agg := dailyData[dKey]
		total := agg.Blocking + agg.Critical + agg.Major + agg.Hint + agg.Suggestion
		trends = append(trends, CoredumpTrendPoint{
			Date:       dKey,
			TotalIssues: total,
			Blocking:   agg.Blocking,
			Critical:   agg.Critical,
			Major:      agg.Major,
			Hint:       agg.Hint,
			Suggestion: agg.Suggestion,
		})
	}

	c.JSON(http.StatusOK, trends)
}
