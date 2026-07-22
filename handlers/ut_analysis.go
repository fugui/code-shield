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
	"gorm.io/datatypes"
)

// UTRepoSummary defines the structure of repository statistics
type UTRepoSummary struct {
	RepoID       uint      `json:"repo_id"`
	RepoName     string    `json:"repo_name"`
	RepoURL      string    `json:"repo_url"`
	Department   string    `json:"department"`
	OwnerName    string    `json:"owner_name"`
	TotalCases   int       `json:"total_cases"`
	PassCount    int       `json:"pass_count"`
	PassRate     float64   `json:"pass_rate"`
	Blocking     int       `json:"blocking"`
	Critical     int       `json:"critical"`
	Major        int       `json:"major"`
	Hint         int       `json:"hint"`
	Suggestion   int       `json:"suggestion"`
	OpenIssues   int       `json:"open_issues"`
	LastScanTime time.Time `json:"last_scan_time"`
}

// GetUTRepos lists all repositories with aggregated test case effectiveness statistics
func GetUTRepos(c *gin.Context) {
	sortBy := c.DefaultQuery("sort_by", "pass_rate")
	sortOrder := c.DefaultQuery("sort_order", "desc")
	keyword := c.Query("keyword")
	department := c.Query("department")

	// 1. Fetch repositories
	var repos []models.Repository
	query := models.DB.Preload("Owner").Preload("Department")
	if err := query.Find(&repos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch repositories"})
		return
	}

	// 2. Fetch test case findings severity stats
	type DbSeverityStat struct {
		RepoID   uint   `gorm:"column:repo_id"`
		Severity string `gorm:"column:severity"`
		Count    int    `gorm:"column:count"`
	}
	var severityStats []DbSeverityStat
	models.DB.Model(&models.TestCaseFinding{}).
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
	models.DB.Model(&models.TestCaseFinding{}).
		Select("repo_id, count(*) as count").
		Where("status IN ?", []string{"open", "analyzing"}).
		Group("repo_id").
		Scan(&statusStats)

	statusMap := make(map[uint]int)
	for _, stat := range statusStats {
		statusMap[stat.RepoID] = stat.Count
	}

	// 4. Fetch last scan times from reports for 'ut_effectiveness'
	var taskType models.TaskType
	models.DB.Where("name = ?", "ut_effectiveness").First(&taskType)

	type DbScanTime struct {
		RepoID    uint   `gorm:"column:repo_id"`
		CreatedAt string `gorm:"column:created_at"`
	}
	var scanTimes []DbScanTime
	if taskType.ID > 0 {
		models.DB.Model(&models.TaskReport{}).
			Select("repo_id, max(created_at) as created_at").
			Where("task_type_id = ? AND status IN ?", taskType.ID, []string{"success", "skipped"}).
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
				log.Printf("Failed to parse UT scan time %q: %v", st.CreatedAt, err)
			}
		}
	}

	// 5. Aggregate metrics
	var summaries []UTRepoSummary
	for _, repo := range repos {
		// Filter by department
		repoDept := ""
		if repo.Department.Name != "" {
			repoDept = repo.Department.Name
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
		passCount := repoSeverities["合格"]
		blocking := repoSeverities["致命"] + repoSeverities["阻塞"]
		critical := repoSeverities["严重"]
		major := repoSeverities["一般"] + repoSeverities["主要"] + repoSeverities["提示"]
		hint := 0
		suggestion := repoSeverities["建议"]

		total := passCount + blocking + critical + major + hint + suggestion

		passRate := 0.0
		if total > 0 {
			passRate = (float64(passCount) / float64(total)) * 100.0
		}

		ownerName := repo.Owner.Name
		if ownerName == "" {
			ownerName = "已离职/未知"
		}

		summaries = append(summaries, UTRepoSummary{
			RepoID:       repo.ID,
			RepoName:     repo.Name,
			RepoURL:      repo.URL,
			Department:   repoDept,
			OwnerName:    ownerName,
			TotalCases:   total,
			PassCount:    passCount,
			PassRate:     passRate,
			Blocking:     blocking,
			Critical:     critical,
			Major:        major,
			Hint:         hint,
			Suggestion:   suggestion,
			OpenIssues:   statusMap[repo.ID],
			LastScanTime: scanTimeMap[repo.ID],
		})
	}

	// 6. Sort results
	sort.Slice(summaries, func(i, j int) bool {
		asc := sortOrder == "asc"
		var cmp bool

		switch sortBy {
		case "name":
			cmp = strings.ToLower(summaries[i].RepoName) < strings.ToLower(summaries[j].RepoName)
		case "total_cases":
			cmp = summaries[i].TotalCases < summaries[j].TotalCases
		case "blocking":
			cmp = summaries[i].Blocking < summaries[j].Blocking
		case "critical":
			cmp = summaries[i].Critical < summaries[j].Critical
		case "last_scan_time":
			cmp = summaries[i].LastScanTime.Before(summaries[j].LastScanTime)
		case "pass_rate":
			fallthrough
		default:
			cmp = summaries[i].PassRate < summaries[j].PassRate
		}

		if asc {
			return cmp
		}
		return !cmp
	})

	c.JSON(http.StatusOK, summaries)
}

// GetUTFindings returns a filtered, paginated list of test case findings for a repository
func GetUTFindings(c *gin.Context) {
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

	query := models.DB.Model(&models.TestCaseFinding{}).Where("repo_id = ?", repoID).Preload("Assignee").Preload("Repo")

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
		query = query.Where("test_case_name LIKE ? OR detail LIKE ? OR file_path LIKE ?", like, like, like)
	}

	var total int64
	query.Count(&total)

	var findings []models.TestCaseFinding
	offset := (page - 1) * pageSize
	query.Order("CASE severity WHEN '致命' THEN 1 WHEN '阻塞' THEN 1 WHEN '严重' THEN 2 WHEN '一般' THEN 3 WHEN '主要' THEN 3 WHEN '提示' THEN 3 WHEN '建议' THEN 4 WHEN '合格' THEN 5 ELSE 6 END, file_path, test_case_name").
		Offset(offset).Limit(pageSize).Find(&findings)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	severityStats := make(map[string]int)
	statusStats := make(map[string]int)

	if repoIDStr != "" {
		var severityCounts []struct {
			Severity string
			Count    int
		}
		models.DB.Model(&models.TestCaseFinding{}).
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
		models.DB.Model(&models.TestCaseFinding{}).
			Select("status, count(*) as count").
			Where("repo_id = ?", repoID).
			Group("status").
			Scan(&statusCounts)

		for _, sc := range statusCounts {
			statusStats[sc.Status] = sc.Count
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":         findings,
		"total":         total,
		"page":          page,
		"pageSize":      pageSize,
		"totalPages":    totalPages,
		"severityStats": severityStats,
		"statusStats":   statusStats,
	})
}

// UpdateUTFinding updates a test case finding's status, assignee, and logs workflow history
func UpdateUTFinding(c *gin.Context) {
	id := c.Param("id")
	var finding models.TestCaseFinding
	if err := models.DB.First(&finding, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
		return
	}

	var req struct {
		Status     *string      `json:"status"`
		AssigneeID *interface{} `json:"assignee_id"`
		Feedback   *string      `json:"feedback"`
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
		if *req.AssigneeID == nil {
			updates["assignee_id"] = nil
		} else {
			switch val := (*req.AssigneeID).(type) {
			case string:
				if val == "" || val == "0" {
					updates["assignee_id"] = nil
				} else {
					idVal, err := strconv.Atoi(val)
					if err == nil {
						updates["assignee_id"] = idVal
					} else {
						updates["assignee_id"] = nil
					}
				}
			case float64:
				if val <= 0 {
					updates["assignee_id"] = nil
				} else {
					updates["assignee_id"] = int(val)
				}
			case int:
				if val <= 0 {
					updates["assignee_id"] = nil
				} else {
					updates["assignee_id"] = val
				}
			default:
				updates["assignee_id"] = nil
			}
		}
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
		updates["status_log"] = datatypes.JSON(logBytes)
	}

	if len(updates) > 0 {
		models.DB.Model(&finding).Updates(updates)
	}

	// Reload finding with fully loaded assignee
	models.DB.Preload("Assignee").Preload("Repo").First(&finding, id)
	c.JSON(http.StatusOK, finding)
}

// UTDeptSummary defines the structure of department statistics
type UTDeptSummary struct {
	Department   string  `json:"department"`
	ScannedRepos int     `json:"scanned_repos"`
	TotalRepos   int     `json:"total_repos"`
	TotalCases   int     `json:"total_cases"`
	PassCount    int     `json:"pass_count"`
	PassRate     float64 `json:"pass_rate"`
	Blocking     int     `json:"blocking"`
	Critical     int     `json:"critical"`
	Major        int     `json:"major"`
	Hint         int     `json:"hint"`
	Suggestion   int     `json:"suggestion"`
	IssuesCount  int     `json:"issues_count"`
	FixRate      float64 `json:"fix_rate"`
}

// GetUTDepartments aggregates test case statistics grouped by organization department
func GetUTDepartments(c *gin.Context) {
	sortBy := c.DefaultQuery("sort_by", "pass_rate")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// 1. Fetch all departments
	var depts []models.Department
	if err := models.DB.Find(&depts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch departments"})
		return
	}

	varSummaries := []UTDeptSummary{}

	for _, dept := range depts {
		// Find repos owned by this department
		var repoIDs []uint
		models.DB.Model(&models.Repository{}).Where("department_id = ?", dept.ID).Pluck("id", &repoIDs)
		if len(repoIDs) == 0 {
			continue
		}

		// Aggregate test case findings
		type DeptStats struct {
			Severity string
			Status   string
			Count    int
		}
		var stats []DeptStats
		models.DB.Model(&models.TestCaseFinding{}).
			Select("severity, status, count(*) as count").
			Where("repo_id IN ?", repoIDs).
			Group("severity, status").
			Scan(&stats)

		total := 0
		passCount := 0
		blocking := 0
		critical := 0
		major := 0
		hint := 0
		suggestion := 0
		resolvedCount := 0

		for _, s := range stats {
			total += s.Count
			if s.Severity == "合格" {
				passCount += s.Count
			} else {
				if s.Severity == "致命" || s.Severity == "阻塞" {
					blocking += s.Count
				} else if s.Severity == "严重" {
					critical += s.Count
				} else if s.Severity == "一般" || s.Severity == "主要" || s.Severity == "提示" {
					major += s.Count
				} else if s.Severity == "建议" {
					suggestion += s.Count
				}

				// Check if the issue is in a resolved/handled status
				if s.Status == "resolved" || s.Status == "closed" || s.Status == "invalid" {
					resolvedCount += s.Count
				}
			}
		}

		// Get scanned repositories count
		var scannedCount int64
		models.DB.Model(&models.TestCaseFinding{}).
			Where("repo_id IN ?", repoIDs).
			Distinct("repo_id").
			Count(&scannedCount)

		if total == 0 {
			continue
		}

		passRate := (float64(passCount) / float64(total)) * 100.0
		issuesCount := total - passCount
		fixRate := 0.0
		if issuesCount > 0 {
			fixRate = (float64(resolvedCount) / float64(issuesCount)) * 100.0
		}

		varSummaries = append(varSummaries, UTDeptSummary{
			Department:   dept.Name,
			ScannedRepos: int(scannedCount),
			TotalRepos:   len(repoIDs),
			TotalCases:   total,
			PassCount:    passCount,
			PassRate:     passRate,
			Blocking:     blocking,
			Critical:     critical,
			Major:        major,
			Hint:         hint,
			Suggestion:   suggestion,
			IssuesCount:  issuesCount,
			FixRate:      fixRate,
		})
	}

	// Sort summaries
	sort.Slice(varSummaries, func(i, j int) bool {
		asc := sortOrder == "asc"
		var cmp bool

		switch sortBy {
		case "department":
			cmp = varSummaries[i].Department < varSummaries[j].Department
		case "scanned_repos":
			cmp = varSummaries[i].ScannedRepos < varSummaries[j].ScannedRepos
		case "total_cases":
			cmp = varSummaries[i].TotalCases < varSummaries[j].TotalCases
		case "issues_count":
			cmp = varSummaries[i].IssuesCount < varSummaries[j].IssuesCount
		case "fix_rate":
			cmp = varSummaries[i].FixRate < varSummaries[j].FixRate
		case "pass_rate":
			fallthrough
		default:
			cmp = varSummaries[i].PassRate < varSummaries[j].PassRate
		}

		if asc {
			return cmp
		}
		return !cmp
	})

	c.JSON(http.StatusOK, varSummaries)
}

// UTTrendPoint defines a single historical trend point
type UTTrendPoint struct {
	Date       string  `json:"date"`
	TotalCases int     `json:"total_cases"`
	PassCount  int     `json:"pass_count"`
	PassRate   float64 `json:"pass_rate"`
	Blocking   int     `json:"blocking"`
	Critical   int     `json:"critical"`
	Issues     int     `json:"issues"`
}

// GetUTTrends retrieves historical trend data points over a 30-day window
func GetUTTrends(c *gin.Context) {
	repoIDStr := c.Query("repo_id")
	department := c.Query("department")
	daysStr := c.DefaultQuery("days", "30")
	days, _ := strconv.Atoi(daysStr)
	if days <= 0 || days > 90 {
		days = 30
	}

	// 1. Resolve ut_effectiveness task type
	var taskType models.TaskType
	models.DB.Where("name = ?", "ut_effectiveness").First(&taskType)
	if taskType.ID == 0 {
		c.JSON(http.StatusOK, []UTTrendPoint{})
		return
	}

	// 2. Determine target repository IDs
	var targetRepoIDs []uint
	if repoIDStr != "" {
		rid, _ := strconv.Atoi(repoIDStr)
		targetRepoIDs = append(targetRepoIDs, uint(rid))
	} else if department != "" {
		var dept models.Department
		if err := models.DB.Where("name = ?", department).First(&dept).Error; err == nil {
			models.DB.Model(&models.Repository{}).Where("department_id = ?", dept.ID).Pluck("id", &targetRepoIDs)
		}
		if len(targetRepoIDs) == 0 {
			c.JSON(http.StatusOK, []UTTrendPoint{})
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
		TotalCases int
		PassCount  int
		Blocking   int
		Critical   int
		Issues     int
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

		pass := metrics["pass"]
		if pass == 0 && metrics["合格"] > 0 {
			pass = metrics["合格"]
		}
		blocking := metrics["blocking"]
		critical := metrics["critical"]
		minor := metrics["minor"] + metrics["major"] + metrics["hint"]
		suggestion := metrics["suggestion"]

		total := pass + blocking + critical + minor + suggestion
		issues := blocking + critical + minor + suggestion

		if total == 0 {
			continue
		}

		if _, exists := dailyData[dateStr]; !exists {
			dailyData[dateStr] = &DailyAgg{}
			dateKeys = append(dateKeys, dateStr)
		}

		dailyData[dateStr].TotalCases += total
		dailyData[dateStr].PassCount += pass
		dailyData[dateStr].Blocking += blocking
		dailyData[dateStr].Critical += critical
		dailyData[dateStr].Issues += issues
	}

	sort.Strings(dateKeys)

	// 5. Build chronological trend points
	var trends []UTTrendPoint
	for _, dKey := range dateKeys {
		agg := dailyData[dKey]
		rate := 0.0
		if agg.TotalCases > 0 {
			rate = (float64(agg.PassCount) / float64(agg.TotalCases)) * 100.0
		}
		trends = append(trends, UTTrendPoint{
			Date:       dKey,
			TotalCases: agg.TotalCases,
			PassCount:  agg.PassCount,
			PassRate:   rate,
			Blocking:   agg.Blocking,
			Critical:   agg.Critical,
			Issues:     agg.Issues,
		})
	}

	c.JSON(http.StatusOK, trends)
}
