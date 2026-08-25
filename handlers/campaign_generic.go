package handlers

import (
	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-shield/models"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

var (
	campaignCache    sync.Map // key: campaignPath/name, value: *CachedTaskType
	campaignCacheTTL = 5 * time.Minute
)

// CachedTaskType 进程内 TaskType 缓存项
type CachedTaskType struct {
	TaskType *models.TaskType
	CachedAt time.Time
}

// InvalidateCampaignCache 供外部在更新/删除 TaskType 时主动调用，实现秒级即时失效
func InvalidateCampaignCache(campaignPath ...string) {
	if len(campaignPath) == 0 {
		campaignCache = sync.Map{}
		return
	}
	for _, p := range campaignPath {
		campaignCache.Delete(p)
	}
}

// ResolveCampaignMiddleware 根据路由中的 :campaign 参数解析 TaskType 并注入 gin.Context
func ResolveCampaignMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		campaign := c.Param("campaign")
		if campaign == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "campaign parameter is required"})
			return
		}

		// 1. 检查缓存
		if cached, ok := campaignCache.Load(campaign); ok {
			entry := cached.(*CachedTaskType)
			if time.Since(entry.CachedAt) < campaignCacheTTL {
				c.Set("taskType", entry.TaskType)
				c.Next()
				return
			}
		}

		// 2. 查库
		var tt models.TaskType
		if err := models.DB.Where("(campaign_path = ? OR name = ?) AND is_campaign = ?",
			campaign, campaign, true).First(&tt).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("专项分析任务不存在或未启用: %s", campaign)})
			return
		}

		campaignCache.Store(campaign, &CachedTaskType{TaskType: &tt, CachedAt: time.Now()})
		c.Set("taskType", &tt)
		c.Next()
	}
}

// DynamicCampaignRepoSummary 动态专项代码仓聚合指标结构（双模式自适应）
type DynamicCampaignRepoSummary struct {
	RepoID         uint      `json:"repo_id"`
	RepoName       string    `json:"repo_name"`
	RepoURL        string    `json:"repo_url"`
	Department     string    `json:"department"`
	OwnerName      string    `json:"owner_name"`
	TotalIssues    int       `json:"total_issues"`
	TotalEntities  int       `json:"total_entities"` // 实体总数 (UT等模式)
	PassCount      int       `json:"pass_count"`     // 合格数 (UT等模式)
	PassRate       float64   `json:"pass_rate"`      // 合格率 (UT等模式)
	Blocking       int       `json:"blocking"`
	Critical       int       `json:"critical"`
	Major          int       `json:"major"`
	Hint           int       `json:"hint"`
	Suggestion     int       `json:"suggestion"`
	OpenIssues     int       `json:"open_issues"`
	ResolvedIssues int       `json:"resolved_issues"`
	FixRate        float64   `json:"fix_rate"` // 修复率 (缺陷攻关模式)
	LastScanTime   time.Time `json:"last_scan_time"`
}

// GetDynamicCampaignRepos 动态专项代码仓列表及指标聚合
func GetDynamicCampaignRepos(c *gin.Context) {
	taskTypeVal, exists := c.Get("taskType")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TaskType context missing"})
		return
	}
	tt := taskTypeVal.(*models.TaskType)
	isEntityMode := tt.GovernanceMode == models.GovernanceModeEntityAssessment

	sortBy := c.DefaultQuery("sort_by", "total_issues")
	if isEntityMode && c.Query("sort_by") == "" {
		sortBy = "pass_rate"
	}
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

	// 2. Fetch campaign findings severity stats
	type DbSeverityStat struct {
		RepoID   uint   `gorm:"column:repo_id"`
		Severity string `gorm:"column:severity"`
		Count    int    `gorm:"column:count"`
	}
	var severityStats []DbSeverityStat
	models.DB.Model(&models.CampaignFinding{}).
		Select("repo_id, severity, count(*) as count").
		Where("task_type_id = ?", tt.ID).
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
	models.DB.Model(&models.CampaignFinding{}).
		Select("repo_id, count(*) as count").
		Where("task_type_id = ? AND status IN ?", tt.ID, []string{"open", "analyzing"}).
		Group("repo_id").
		Scan(&statusStats)

	statusMap := make(map[uint]int)
	for _, stat := range statusStats {
		statusMap[stat.RepoID] = stat.Count
	}

	// 4. Fetch resolved issues stats
	var resolvedStats []DbStatusStat
	models.DB.Model(&models.CampaignFinding{}).
		Select("repo_id, count(*) as count").
		Where("task_type_id = ? AND status IN ?", tt.ID, []string{"resolved", "closed", "invalid"}).
		Group("repo_id").
		Scan(&resolvedStats)

	resolvedMap := make(map[uint]int)
	for _, stat := range resolvedStats {
		resolvedMap[stat.RepoID] = stat.Count
	}

	// 5. Fetch last scan times from reports
	type DbScanTime struct {
		RepoID    uint   `gorm:"column:repo_id"`
		CreatedAt string `gorm:"column:created_at"`
	}
	var scanTimes []DbScanTime
	models.DB.Model(&models.TaskReport{}).
		Select("repo_id, max(created_at) as created_at").
		Where("task_type_id = ? AND status IN ?", tt.ID, []string{"success", "skipped"}).
		Group("repo_id").
		Scan(&scanTimes)

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
			}
		}
	}

	// 6. Aggregate metrics
	var summaries []DynamicCampaignRepoSummary
	for _, repo := range repos {
		repoDept := ""
		if repo.Department.Name != "" {
			repoDept = repo.Department.Name
		}
		if department != "" && repoDept != department {
			continue
		}

		if keyword != "" && !strings.Contains(strings.ToLower(repo.Name), strings.ToLower(keyword)) {
			continue
		}

		repoSeverities := severityMap[repo.ID]
		passCount := repoSeverities["合格"]
		blocking := repoSeverities["致命"] + repoSeverities["阻塞"]
		critical := repoSeverities["严重"]
		major := repoSeverities["一般"] + repoSeverities["主要"] + repoSeverities["提示"]
		hint := 0
		suggestion := repoSeverities["建议"]

		openCount := statusMap[repo.ID]
		resolvedCount := resolvedMap[repo.ID]

		// 实体模式下的总实体数与合格率
		totalEntities := passCount + blocking + critical + major + hint + suggestion
		passRate := 0.0
		if totalEntities > 0 {
			passRate = (float64(passCount) / float64(totalEntities)) * 100.0
		} else if !scanTimeMap[repo.ID].IsZero() {
			passRate = 100.0
		}

		// 缺陷攻关模式下的总缺陷数与修复率
		totalDefects := openCount + resolvedCount
		fixRate := 0.0
		if totalDefects > 0 {
			fixRate = (float64(resolvedCount) / float64(totalDefects)) * 100.0
		} else if !scanTimeMap[repo.ID].IsZero() {
			fixRate = 100.0
		}

		ownerName := ""
		if repo.Owner.Name != "" {
			ownerName = repo.Owner.Name
		} else {
			ownerName = "已离职/未知"
		}

		displayTotalIssues := totalDefects
		if isEntityMode {
			displayTotalIssues = totalEntities
		}

		summaries = append(summaries, DynamicCampaignRepoSummary{
			RepoID:         repo.ID,
			RepoName:       repo.Name,
			RepoURL:        repo.URL,
			Department:     repoDept,
			OwnerName:      ownerName,
			TotalIssues:    displayTotalIssues,
			TotalEntities:  totalEntities,
			PassCount:      passCount,
			PassRate:       passRate,
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
		case "pass_rate":
			cmp = summaries[i].PassRate < summaries[j].PassRate
		case "pass_count":
			cmp = summaries[i].PassCount < summaries[j].PassCount
		case "total_issues", "total_entities":
			cmp = summaries[i].TotalIssues < summaries[j].TotalIssues
		case "blocking":
			cmp = summaries[i].Blocking < summaries[j].Blocking
		case "critical":
			cmp = summaries[i].Critical < summaries[j].Critical
		case "open_issues":
			cmp = summaries[i].OpenIssues < summaries[j].OpenIssues
		case "last_scan_time":
			cmp = summaries[i].LastScanTime.Before(summaries[j].LastScanTime)
		case "fix_rate":
			fallthrough
		default:
			if isEntityMode {
				cmp = summaries[i].PassRate < summaries[j].PassRate
			} else {
				cmp = summaries[i].FixRate < summaries[j].FixRate
			}
		}

		if asc {
			return cmp
		}
		return !cmp
	})

	c.JSON(http.StatusOK, summaries)
}

// GetDynamicCampaignFindings 通用专项缺陷与实体列表查询（支持分页）
func GetDynamicCampaignFindings(c *gin.Context) {
	taskTypeVal, exists := c.Get("taskType")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TaskType context missing"})
		return
	}
	tt := taskTypeVal.(*models.TaskType)

	repoIDStr := c.Query("repo_id")
	severity := c.Query("severity")
	status := c.Query("status")
	category := c.Query("category")
	keyword := c.Query("keyword")

	pageStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "25")
	if c.Query("pageSize") != "" {
		pageSizeStr = c.Query("pageSize")
	}
	page, _ := strconv.Atoi(pageStr)
	pageSize, _ := strconv.Atoi(pageSizeStr)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 25
	}

	var parsedRepoID int
	if repoIDStr != "" {
		parsedRepoID, _ = strconv.Atoi(repoIDStr)
	}

	// 构建用于总数计数的独立 Query 句柄（避免 GORM Statement 污染）
	countQuery := models.DB.Model(&models.CampaignFinding{}).Where("task_type_id = ?", tt.ID)
	if parsedRepoID > 0 {
		countQuery = countQuery.Where("repo_id = ?", parsedRepoID)
	}
	if severity != "" {
		countQuery = countQuery.Where("severity = ?", severity)
	}
	if status != "" {
		countQuery = countQuery.Where("status = ?", status)
	}
	if category != "" {
		countQuery = countQuery.Where("category = ?", category)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		countQuery = countQuery.Where("file_path LIKE ? OR title LIKE ? OR detail LIKE ?", like, like, like)
	}

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count findings"})
		return
	}

	// 构建用于分页列表查询的独立 Query 句柄
	findQuery := models.DB.Model(&models.CampaignFinding{}).
		Preload("Assignee").Preload("Repo").
		Where("task_type_id = ?", tt.ID)
	if parsedRepoID > 0 {
		findQuery = findQuery.Where("repo_id = ?", parsedRepoID)
	}
	if severity != "" {
		findQuery = findQuery.Where("severity = ?", severity)
	}
	if status != "" {
		findQuery = findQuery.Where("status = ?", status)
	}
	if category != "" {
		findQuery = findQuery.Where("category = ?", category)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		findQuery = findQuery.Where("file_path LIKE ? OR title LIKE ? OR detail LIKE ?", like, like, like)
	}

	list := make([]models.CampaignFinding, 0)
	orderClause := "CASE severity WHEN '致命' THEN 1 WHEN '阻塞' THEN 1 WHEN '严重' THEN 2 WHEN '一般' THEN 3 WHEN '主要' THEN 3 WHEN '提示' THEN 3 WHEN '建议' THEN 4 WHEN '合格' THEN 5 ELSE 6 END, id DESC"
	if err := findQuery.Order(orderClause).Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch findings"})
		return
	}

	// 聚合当前专项（及当前仓库）维度的全量统计指标（等级、状态、分类）
	type DbAggStat struct {
		StatKey string `gorm:"column:stat_key"`
		Count   int    `gorm:"column:count"`
	}

	// 1. 影响等级统计
	var dbSevStats []DbAggStat
	sevQuery := models.DB.Model(&models.CampaignFinding{}).Where("task_type_id = ?", tt.ID)
	if parsedRepoID > 0 {
		sevQuery = sevQuery.Where("repo_id = ?", parsedRepoID)
	}
	sevQuery.Select("severity as stat_key, count(*) as count").
		Where("severity != '' AND severity IS NOT NULL").
		Group("severity").
		Scan(&dbSevStats)
	severityStats := make(map[string]int)
	for _, s := range dbSevStats {
		severityStats[s.StatKey] = s.Count
	}

	// 2. 治理审计状态统计
	var dbStatusStats []DbAggStat
	statusQuery := models.DB.Model(&models.CampaignFinding{}).Where("task_type_id = ?", tt.ID)
	if parsedRepoID > 0 {
		statusQuery = statusQuery.Where("repo_id = ?", parsedRepoID)
	}
	statusQuery.Select("status as stat_key, count(*) as count").
		Where("status != '' AND status IS NOT NULL").
		Group("status").
		Scan(&dbStatusStats)
	statusStats := make(map[string]int)
	for _, s := range dbStatusStats {
		statusStats[s.StatKey] = s.Count
	}

	// 3. 问题分类统计与全量分类列表
	var dbCatStats []DbAggStat
	catQuery := models.DB.Model(&models.CampaignFinding{}).Where("task_type_id = ?", tt.ID)
	if parsedRepoID > 0 {
		catQuery = catQuery.Where("repo_id = ?", parsedRepoID)
	}
	catQuery.Select("category as stat_key, count(*) as count").
		Where("category != '' AND category IS NOT NULL").
		Group("category").
		Order("count DESC").
		Scan(&dbCatStats)
	categoryStats := make(map[string]int)
	categories := make([]string, 0, len(dbCatStats))
	for _, c := range dbCatStats {
		categoryStats[c.StatKey] = c.Count
		categories = append(categories, c.StatKey)
	}

	c.JSON(http.StatusOK, gin.H{
		"total":          total,
		"page":           page,
		"page_size":      pageSize,
		"items":          list,
		"severity_stats": severityStats,
		"severityStats":  severityStats,
		"status_stats":   statusStats,
		"statusStats":    statusStats,
		"category_stats": categoryStats,
		"categoryStats":  categoryStats,
		"categories":     categories,
	})
}

// GetDynamicCampaignFinding 获取单个专项缺陷详情
func GetDynamicCampaignFinding(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid finding ID"})
		return
	}

	taskTypeVal, exists := c.Get("taskType")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TaskType context missing"})
		return
	}
	tt := taskTypeVal.(*models.TaskType)

	var finding models.CampaignFinding
	if err := models.DB.Preload("Assignee").Preload("Repo").Where("id = ? AND task_type_id = ?", id, tt.ID).First(&finding).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
		return
	}

	c.JSON(http.StatusOK, finding)
}

// UpdateDynamicCampaignFinding 通用更新专项缺陷状态与审计指派
func UpdateDynamicCampaignFinding(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid finding ID"})
		return
	}

	taskTypeVal, exists := c.Get("taskType")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TaskType context missing"})
		return
	}
	tt := taskTypeVal.(*models.TaskType)

	var finding models.CampaignFinding
	if err := models.DB.Where("id = ? AND task_type_id = ?", id, tt.ID).First(&finding).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
		return
	}
	oldFinding := finding

	var input struct {
		Status     string      `json:"status"`
		AssigneeID interface{} `json:"assignee_id"`
		Feedback   string      `json:"feedback"`
		Severity   string      `json:"severity"`
		Category   string      `json:"category"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	currentUserName := "系统用户"
	if uc := commonAuth.GetUserContext(c); uc != nil {
		if uc.Name != "" {
			currentUserName = uc.Name
		} else if uc.Username != "" {
			currentUserName = uc.Username
		} else if uc.Email != "" {
			currentUserName = uc.Email
		}
	}
	if currentUserName == "系统用户" {
		if name, exists := c.Get("name"); exists && fmt.Sprintf("%v", name) != "" {
			currentUserName = fmt.Sprintf("%v", name)
		} else if uname, exists := c.Get("username"); exists && fmt.Sprintf("%v", uname) != "" {
			currentUserName = fmt.Sprintf("%v", uname)
		} else if uidVal, exists := c.Get("userID"); exists {
			if uid, ok := uidVal.(uint); ok && uid > 0 {
				var u models.User
				if err := models.DB.First(&u, uid).Error; err == nil {
					if u.Name != "" {
						currentUserName = u.Name
					} else if u.Email != "" {
						currentUserName = u.Email
					}
				}
			}
		}
	}

	targetStatus := finding.Status
	if input.Status != "" {
		targetStatus = input.Status
	}

	statusChanged := input.Status != "" && input.Status != finding.Status
	hasFeedback := input.Feedback != ""

	if statusChanged || hasFeedback {
		var existingLog []map[string]interface{}
		if len(finding.StatusLog) > 0 {
			_ = json.Unmarshal(finding.StatusLog, &existingLog)
		}
		existingLog = append(existingLog, map[string]interface{}{
			"status":  targetStatus,
			"time":    time.Now().Format("2006-01-02 15:04:05"),
			"user":    currentUserName,
			"comment": input.Feedback,
		})
		logBytes, _ := json.Marshal(existingLog)
		finding.Status = targetStatus
		finding.StatusLog = datatypes.JSON(logBytes)
	}

	if input.Feedback != "" {
		finding.Feedback = input.Feedback
	}
	if input.Severity != "" {
		finding.Severity = input.Severity
	}
	if input.Category != "" {
		finding.Category = input.Category
	}

	if input.AssigneeID != nil {
		switch val := input.AssigneeID.(type) {
		case float64:
			if val <= 0 {
				finding.AssigneeID = nil
			} else {
				valUint := uint(val)
				finding.AssigneeID = &valUint
			}
		case int:
			if val <= 0 {
				finding.AssigneeID = nil
			} else {
				valUint := uint(val)
				finding.AssigneeID = &valUint
			}
		default:
			finding.AssigneeID = nil
		}
	}

	if err := models.DB.Save(&finding).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update finding"})
		return
	}

	// 注入全局操作审计打点
	summaryText := fmt.Sprintf("人工核销了专项缺陷 #%d (状态更新为: %s)", id, input.Status)
	if input.Feedback != "" {
		summaryText += fmt.Sprintf(", 处置意见: %s", input.Feedback)
	}
	commonAudit.SetAuditContext(c, "campaign", "audit_finding", commonModels.AuditLevelP1,
		summaryText,
		"campaign_finding", fmt.Sprintf("%d", id), fmt.Sprintf("缺陷-%d", id),
		oldFinding, finding)

	models.DB.Preload("Assignee").Preload("Repo").First(&finding, id)
	c.JSON(http.StatusOK, finding)
}

// ExportDynamicCampaignFindings 通用专项缺陷/用例导出至 Excel
func ExportDynamicCampaignFindings(c *gin.Context) {
	taskTypeVal, exists := c.Get("taskType")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TaskType context missing"})
		return
	}
	tt := taskTypeVal.(*models.TaskType)
	isEntityMode := tt.GovernanceMode == models.GovernanceModeEntityAssessment

	repoIDStr := c.Query("repo_id")
	if repoIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}
	repoID, _ := strconv.Atoi(repoIDStr)

	var repo models.Repository
	if err := models.DB.First(&repo, repoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Repository not found"})
		return
	}

	severity := c.Query("severity")
	status := c.Query("status")
	category := c.Query("category")
	keyword := c.Query("keyword")

	query := models.DB.Model(&models.CampaignFinding{}).
		Preload("Assignee").Preload("Repo").
		Where("task_type_id = ? AND repo_id = ?", tt.ID, repoID)

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
		query = query.Where("file_path LIKE ? OR title LIKE ? OR detail LIKE ?", like, like, like)
	}

	var dbFindings []models.CampaignFinding
	orderClause := "CASE severity WHEN '致命' THEN 1 WHEN '阻塞' THEN 1 WHEN '严重' THEN 2 WHEN '一般' THEN 3 WHEN '主要' THEN 3 WHEN '提示' THEN 3 WHEN '建议' THEN 4 WHEN '合格' THEN 5 ELSE 6 END, id DESC"
	if err := query.Order(orderClause).Find(&dbFindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch findings for export"})
		return
	}

	items := convertCampaignFindingsToExcelItems(dbFindings)

	generateCampaignExcel(c, repo.Name, tt.DisplayName, items, isEntityMode)
}

// GetDynamicCampaignDepartments 部门维度专项指标汇总
func GetDynamicCampaignDepartments(c *gin.Context) {
	taskTypeVal, exists := c.Get("taskType")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TaskType context missing"})
		return
	}
	tt := taskTypeVal.(*models.TaskType)
	isEntityMode := tt.GovernanceMode == models.GovernanceModeEntityAssessment

	sortBy := c.DefaultQuery("sort_by", "open_issues")
	if isEntityMode && c.Query("sort_by") == "" {
		sortBy = "pass_rate"
	}
	sortOrder := c.DefaultQuery("sort_order", "desc")

	var depts []models.Department
	if err := models.DB.Find(&depts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch departments"})
		return
	}

	type DeptSummary struct {
		Department     string  `json:"department"`
		ScannedRepos   int     `json:"scanned_repos"`
		TotalRepos     int     `json:"total_repos"`
		TotalIssues    int     `json:"total_issues"`
		OpenIssues     int     `json:"open_issues"`
		ResolvedIssues int     `json:"resolved_issues"`
		PassCount      int     `json:"pass_count"`
		PassRate       float64 `json:"pass_rate"`
		FixRate        float64 `json:"fix_rate"`
	}

	var summaries []DeptSummary
	for _, dept := range depts {
		var repos []models.Repository
		if err := models.DB.Where("department_id = ?", dept.ID).Find(&repos).Error; err != nil || len(repos) == 0 {
			continue
		}

		var repoIDs []uint
		for _, r := range repos {
			repoIDs = append(repoIDs, r.ID)
		}

		var totalIssues, openIssues, resolvedIssues, passCount int64

		models.DB.Model(&models.CampaignFinding{}).
			Where("task_type_id = ? AND repo_id IN ?", tt.ID, repoIDs).
			Count(&totalIssues)

		models.DB.Model(&models.CampaignFinding{}).
			Where("task_type_id = ? AND repo_id IN ? AND status IN ?", tt.ID, repoIDs, []string{"open", "analyzing"}).
			Count(&openIssues)

		models.DB.Model(&models.CampaignFinding{}).
			Where("task_type_id = ? AND repo_id IN ? AND status IN ?", tt.ID, repoIDs, []string{"resolved", "closed", "invalid"}).
			Count(&resolvedIssues)

		models.DB.Model(&models.CampaignFinding{}).
			Where("task_type_id = ? AND repo_id IN ? AND severity = ?", tt.ID, repoIDs, "合格").
			Count(&passCount)

		var scannedCount int64
		models.DB.Model(&models.CampaignFinding{}).
			Where("task_type_id = ? AND repo_id IN ?", tt.ID, repoIDs).
			Distinct("repo_id").
			Count(&scannedCount)

		fixRate := 0.0
		if totalIssues > 0 {
			fixRate = (float64(resolvedIssues) / float64(totalIssues)) * 100.0
		} else {
			fixRate = 100.0
		}

		passRate := 0.0
		if totalIssues > 0 {
			passRate = (float64(passCount) / float64(totalIssues)) * 100.0
		} else {
			passRate = 100.0
		}

		summaries = append(summaries, DeptSummary{
			Department:     dept.Name,
			ScannedRepos:   int(scannedCount),
			TotalRepos:     len(repos),
			TotalIssues:    int(totalIssues),
			OpenIssues:     int(openIssues),
			ResolvedIssues: int(resolvedIssues),
			PassCount:      int(passCount),
			PassRate:       passRate,
			FixRate:        fixRate,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		asc := sortOrder == "asc"
		var cmp bool
		switch sortBy {
		case "department":
			cmp = strings.ToLower(summaries[i].Department) < strings.ToLower(summaries[j].Department)
		case "scanned_repos":
			cmp = summaries[i].ScannedRepos < summaries[j].ScannedRepos
		case "total_issues":
			cmp = summaries[i].TotalIssues < summaries[j].TotalIssues
		case "open_issues":
			cmp = summaries[i].OpenIssues < summaries[j].OpenIssues
		case "pass_rate":
			cmp = summaries[i].PassRate < summaries[j].PassRate
		case "fix_rate":
			fallthrough
		default:
			if isEntityMode {
				cmp = summaries[i].PassRate < summaries[j].PassRate
			} else {
				cmp = summaries[i].FixRate < summaries[j].FixRate
			}
		}

		if asc {
			return cmp
		}
		return !cmp
	})

	c.JSON(http.StatusOK, summaries)
}

// GetDynamicCampaignTrends 通用专项 30 天收敛趋势统计
func GetDynamicCampaignTrends(c *gin.Context) {
	taskTypeVal, exists := c.Get("taskType")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TaskType context missing"})
		return
	}
	tt := taskTypeVal.(*models.TaskType)
	isEntityMode := tt.GovernanceMode == models.GovernanceModeEntityAssessment

	repoIDStr := c.Query("repo_id")
	deptName := c.Query("department")

	query := models.DB.Model(&models.CampaignFinding{}).Where("task_type_id = ?", tt.ID)

	if repoIDStr != "" {
		if repoID, err := strconv.Atoi(repoIDStr); err == nil && repoID > 0 {
			query = query.Where("repo_id = ?", repoID)
		}
	} else if deptName != "" {
		var dept models.Department
		if err := models.DB.Where("name = ?", deptName).First(&dept).Error; err == nil {
			var repos []models.Repository
			models.DB.Where("department_id = ?", dept.ID).Find(&repos)
			var repoIDs []uint
			for _, r := range repos {
				repoIDs = append(repoIDs, r.ID)
			}
			if len(repoIDs) > 0 {
				query = query.Where("repo_id IN ?", repoIDs)
			} else {
				query = query.Where("1 = 0")
			}
		}
	}

	var allFindings []models.CampaignFinding
	if err := query.Find(&allFindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch findings for trend"})
		return
	}

	type TrendPoint struct {
		Date        string  `json:"date"`
		TotalIssues int     `json:"total_issues"`
		OpenIssues  int     `json:"open_issues"`
		PassCount   int     `json:"pass_count,omitempty"`
		PassRate    float64 `json:"pass_rate,omitempty"`
		FixRate     float64 `json:"fix_rate"`
	}

	now := time.Now()
	trendMap := make([]TrendPoint, 30)

	for i := 29; i >= 0; i-- {
		targetDate := now.AddDate(0, 0, -i)
		dateStr := targetDate.Format("2006-01-02")
		endOfDay := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 23, 59, 59, 999999999, targetDate.Location())

		var totalIssues, openIssues, resolvedIssues, passCount int

		for _, f := range allFindings {
			if f.CreatedAt.After(endOfDay) {
				continue
			}

			totalIssues++

			// 1. 确定初始状态
			initialStatus := "open"
			if isEntityMode && f.Severity == "合格" {
				initialStatus = "closed"
			}

			statusOnDate := initialStatus

			if len(f.StatusLog) > 0 {
				var statusHistory []struct {
					Status string `json:"status"`
					Time   string `json:"time"`
				}
				if err := json.Unmarshal(f.StatusLog, &statusHistory); err == nil {
					var lastStatus string
					for _, logEntry := range statusHistory {
						t, err := time.Parse("2006-01-02 15:04:05", logEntry.Time)
						if err == nil && !t.After(endOfDay) {
							lastStatus = logEntry.Status
						}
					}
					if lastStatus != "" {
						statusOnDate = lastStatus
					}
				}
			}

			if statusOnDate == "open" || statusOnDate == "analyzing" {
				openIssues++
			} else if statusOnDate == "resolved" || statusOnDate == "closed" || statusOnDate == "invalid" {
				resolvedIssues++
			}

			if isEntityMode {
				if f.Severity == "合格" || statusOnDate == "closed" || statusOnDate == "resolved" {
					passCount++
				}
			}
		}

		fixRate := 100.0
		if totalIssues > 0 {
			fixRate = (float64(resolvedIssues) / float64(totalIssues)) * 100.0
		}

		passRate := 100.0
		if totalIssues > 0 {
			passRate = (float64(passCount) / float64(totalIssues)) * 100.0
		}

		idx := 29 - i
		trendMap[idx] = TrendPoint{
			Date:        dateStr,
			TotalIssues: totalIssues,
			OpenIssues:  openIssues,
			PassCount:   passCount,
			PassRate:    passRate,
			FixRate:     fixRate,
		}
	}

	c.JSON(http.StatusOK, trendMap)
}
