package handlers

import (
	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-shield/models"
	"code-shield/services/defectlifecycle"
	"encoding/json"
	"errors"
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

	trendCacheLock sync.RWMutex
	trendCache     = make(map[string]campaignTrendCacheEntry)
)

type campaignTrendCacheEntry struct {
	data      interface{}
	expiresAt time.Time
}

func invalidateTrendCache(taskTypeID uint) {
	trendCacheLock.Lock()
	defer trendCacheLock.Unlock()
	prefix := fmt.Sprintf("%d_", taskTypeID)
	for k := range trendCache {
		if strings.HasPrefix(k, prefix) {
			delete(trendCache, k)
		}
	}
}

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
	TotalIssues    int       `json:"total_issues"`   // 实体模式下为用例总数，缺陷模式下为未关闭跟踪缺陷数 (open_issues)
	TotalDefects   int       `json:"total_defects"`  // 累计发现缺陷总数 (open + resolved)
	TotalEntities  int       `json:"total_entities"` // 实体总数 (UT等模式)
	PassCount      int       `json:"pass_count"`     // 合格数 (UT等模式)
	PassRate       float64   `json:"pass_rate"`      // 合格率 (UT等模式)
	Blocking       int       `json:"blocking"`       // 未关闭致命/阻塞数
	Critical       int       `json:"critical"`       // 未关闭严重数
	Major          int       `json:"major"`          // 未关闭一般/主要数
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

	summaries, err := FetchCampaignRepoSummaries(tt, keyword, department)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch repository summaries: " + err.Error()})
		return
	}

	// Sort results
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

// FetchCampaignRepoSummaries 获取指定专项下的所有代码仓指标聚合列表
func FetchCampaignRepoSummaries(tt *models.TaskType, keyword, department string) ([]DynamicCampaignRepoSummary, error) {
	isEntityMode := tt.GovernanceMode == models.GovernanceModeEntityAssessment

	if !isEntityMode {
		ledgerSummaries, err := defectlifecycle.ListLedgerRepoSummaries(models.DB, tt.ID)
		if err != nil {
			return nil, err
		}
		summaries := make([]DynamicCampaignRepoSummary, 0, len(ledgerSummaries))
		for _, item := range ledgerSummaries {
			if department != "" && item.Department != department {
				continue
			}
			if keyword != "" && !strings.Contains(strings.ToLower(item.RepoName), strings.ToLower(keyword)) {
				continue
			}
			summaries = append(summaries, DynamicCampaignRepoSummary{
				RepoID: item.RepoID, RepoName: item.RepoName, RepoURL: item.RepoURL,
				Department: item.Department, OwnerName: item.OwnerName,
				TotalIssues: item.TotalIssues, TotalDefects: item.TotalDefects,
				TotalEntities: item.TotalEntities, PassCount: item.PassCount,
				PassRate: item.PassRate, Blocking: item.Blocking,
				Critical: item.Critical, Major: item.Major, Hint: item.Hint,
				Suggestion: item.Suggestion, OpenIssues: item.OpenIssues,
				ResolvedIssues: item.ResolvedIssues, FixRate: item.FixRate,
				LastScanTime: item.LastScanTime,
			})
		}
		return summaries, nil
	}

	// 1. Fetch repositories
	var repos []models.Repository
	query := models.DB.Preload("Owner").Preload("Department")
	if err := query.Find(&repos).Error; err != nil {
		return nil, err
	}

	type DbFindingStat struct {
		RepoID        uint `gorm:"column:repo_id"`
		ActiveCount   int  `gorm:"column:active_count"`
		ResolvedCount int  `gorm:"column:resolved_count"`
		PassCount     int  `gorm:"column:pass_count"`
		BlockingCount int  `gorm:"column:blocking_count"`
		CriticalCount int  `gorm:"column:critical_count"`
		MajorCount    int  `gorm:"column:major_count"`
		SuggestCount  int  `gorm:"column:suggest_count"`
	}
	findingMap := make(map[uint]DbFindingStat, len(repos))
	if isEntityMode {
		// Entity assessment treats every record as a ledger entry, including closed cases.
		var entityStats []DbFindingStat
		if err := models.DB.Model(&models.CampaignFinding{}).
			Select(`
				repo_id,
				COUNT(*) FILTER (WHERE status IN ('open', 'analyzing')) AS active_count,
				COUNT(*) FILTER (WHERE status IN ('resolved', 'closed', 'invalid')) AS resolved_count,
				COUNT(*) FILTER (WHERE severity = '合格') AS pass_count,
				COUNT(*) FILTER (WHERE severity IN ('致命', '阻塞')) AS blocking_count,
				COUNT(*) FILTER (WHERE severity = '严重') AS critical_count,
				COUNT(*) FILTER (WHERE severity IN ('一般', '主要', '提示')) AS major_count,
				COUNT(*) FILTER (WHERE severity = '建议') AS suggest_count
			`).
			Where("task_type_id = ?", tt.ID).
			Group("repo_id").
			Scan(&entityStats).Error; err != nil {
			return nil, err
		}
		for _, stat := range entityStats {
			findingMap[stat.RepoID] = stat
		}
	} else {
		// Defect tracking only needs severity buckets for unclosed findings. Scanning
		// the two smaller lifecycle partitions avoids re-reading all resolved history.
		var activeStats []DbFindingStat
		if err := models.DB.Model(&models.CampaignFinding{}).
			Select(`
				repo_id,
				COUNT(*) AS active_count,
				COUNT(*) FILTER (WHERE severity = '合格') AS pass_count,
				COUNT(*) FILTER (WHERE severity IN ('致命', '阻塞')) AS blocking_count,
				COUNT(*) FILTER (WHERE severity = '严重') AS critical_count,
				COUNT(*) FILTER (WHERE severity IN ('一般', '主要', '提示')) AS major_count,
				COUNT(*) FILTER (WHERE severity = '建议') AS suggest_count
			`).
			Where("task_type_id = ? AND status IN ?", tt.ID, []string{"open", "analyzing"}).
			Group("repo_id").
			Scan(&activeStats).Error; err != nil {
			return nil, err
		}
		for _, stat := range activeStats {
			findingMap[stat.RepoID] = stat
		}

		type DbResolvedStat struct {
			RepoID        uint `gorm:"column:repo_id"`
			ResolvedCount int  `gorm:"column:resolved_count"`
		}
		var resolvedStats []DbResolvedStat
		if err := models.DB.Model(&models.CampaignFinding{}).
			Select("repo_id, COUNT(*) AS resolved_count").
			Where("task_type_id = ? AND status IN ?", tt.ID, []string{"resolved", "closed", "invalid"}).
			Group("repo_id").
			Scan(&resolvedStats).Error; err != nil {
			return nil, err
		}
		for _, stat := range resolvedStats {
			item := findingMap[stat.RepoID]
			item.RepoID = stat.RepoID
			item.ResolvedCount = stat.ResolvedCount
			findingMap[stat.RepoID] = item
		}
	}

	// 3. Fetch last scan times from reports
	type DbScanTime struct {
		RepoID    uint       `gorm:"column:repo_id"`
		CreatedAt *time.Time `gorm:"column:created_at"`
	}
	var scanTimes []DbScanTime
	models.DB.Model(&models.TaskReport{}).
		Select("repo_id, max(created_at) as created_at").
		Where("task_type_id = ? AND status IN ?", tt.ID, []string{"success", "skipped"}).
		Group("repo_id").
		Scan(&scanTimes)

	scanTimeMap := make(map[uint]time.Time)
	for _, st := range scanTimes {
		if st.CreatedAt != nil {
			scanTimeMap[st.RepoID] = *st.CreatedAt
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

		finding := findingMap[repo.ID]
		passCount := finding.PassCount
		blocking := finding.BlockingCount
		critical := finding.CriticalCount
		major := finding.MajorCount
		hint := 0
		suggestion := finding.SuggestCount

		openCount := finding.ActiveCount
		resolvedCount := finding.ResolvedCount

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

		displayTotalIssues := openCount
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
			TotalDefects:   totalDefects,
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
	return summaries, nil
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

	if models.ResolveGovernanceMode(tt.GovernanceMode) != models.GovernanceModeEntityAssessment {
		pageResult, err := defectlifecycle.ListCampaignDefects(models.DB, defectlifecycle.CampaignQuery{
			TaskTypeID: tt.ID, RepoID: uint(parsedRepoID),
			Severity: severity, Status: status, Category: category,
			Keyword: keyword, Page: page, PageSize: pageSize,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"total": pageResult.Total, "page": pageResult.Page, "page_size": pageResult.PageSize,
			"items": pageResult.Items, "severity_stats": pageResult.SeverityStats, "severityStats": pageResult.SeverityStats,
			"status_stats": pageResult.StatusStats, "statusStats": pageResult.StatusStats,
			"category_stats": pageResult.CategoryStats, "categoryStats": pageResult.CategoryStats,
			"categories": pageResult.Categories,
		})
		return
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

	if models.ResolveGovernanceMode(tt.GovernanceMode) != models.GovernanceModeEntityAssessment {
		finding, err := defectlifecycle.GetCampaignDefect(models.DB, tt.ID, uint(id))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, finding)
		return
	}

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

	if models.ResolveGovernanceMode(tt.GovernanceMode) != models.GovernanceModeEntityAssessment {
		userIDValue, exists := c.Get("userID")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		var input struct {
			Status     string      `json:"status"`
			AssigneeID interface{} `json:"assignee_id"`
			Feedback   string      `json:"feedback"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var assigneeID *uint
		switch value := input.AssigneeID.(type) {
		case float64:
			if value > 0 {
				converted := uint(value)
				assigneeID = &converted
			}
		case int:
			if value > 0 {
				converted := uint(value)
				assigneeID = &converted
			}
		}
		workflowInput := defectlifecycle.DefectWorkflowInput{
			Status: input.Status, Feedback: input.Feedback,
			AssigneeID: assigneeID, AssigneeSet: input.AssigneeID != nil,
			ActorID: userIDValue.(uint),
		}
		updated, err := defectlifecycle.UpdateDefectWorkflow(models.DB, uint(id), workflowInput)
		if err != nil {
			if errors.Is(err, defectlifecycle.ErrDefectChanged) {
				c.JSON(http.StatusConflict, gin.H{"error": "defect changed; refresh and retry"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		commonAudit.SetAuditContext(c, "campaign", "audit_finding", commonModels.AuditLevelP1,
			fmt.Sprintf("人工更新台账缺陷 #%d", id), "defect", fmt.Sprintf("%d", id),
			fmt.Sprintf("缺陷-%d", id), nil, updated)
		invalidateTrendCache(tt.ID)
		c.JSON(http.StatusOK, updated)
		return
	}

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

	invalidateTrendCache(tt.ID)

	// 注入全局操作审计打点
	summaryText := fmt.Sprintf("人工核销了专项缺陷 #%d (状态更新为: %s)", id, input.Status)
	if input.Feedback != "" {
		summaryText += ", 已填写处置意见"
	}
	commonAudit.SetAuditContext(c, "campaign", "audit_finding", commonModels.AuditLevelP1,
		summaryText,
		"campaign_finding", fmt.Sprintf("%d", id), fmt.Sprintf("缺陷-%d", id),
		oldFinding, finding)

	models.DB.Preload("Assignee").Preload("Repo").First(&finding, id)
	c.JSON(http.StatusOK, finding)
}

// ExportDynamicCampaignFindings 通用专项缺陷/用例导出至 Excel 或 JSON
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
	exportFormat := strings.ToLower(c.DefaultQuery("format", "excel"))
	if exportFormat != "excel" && exportFormat != "xlsx" && exportFormat != "json" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported export format: " + exportFormat})
		return
	}

	if models.ResolveGovernanceMode(tt.GovernanceMode) != models.GovernanceModeEntityAssessment {
		ledgerItems := make([]defectlifecycle.CampaignFinding, 0)
		page := 1
		for {
			pageResult, err := defectlifecycle.ListCampaignDefects(models.DB, defectlifecycle.CampaignQuery{
				TaskTypeID: tt.ID, RepoID: uint(repoID), Severity: severity,
				Status: status, Category: category, Keyword: keyword,
				Page: page, PageSize: 500,
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ledgerItems = append(ledgerItems, pageResult.Items...)
			if int64(page)*500 >= pageResult.Total || len(pageResult.Items) == 0 {
				break
			}
			page++
		}
		items := convertLedgerDefectsToExcelItems(ledgerItems)
		if exportFormat == "json" {
			c.JSON(http.StatusOK, items)
			return
		}
		generateCampaignExcel(c, repo.Name, tt.DisplayName, items, false)
		return
	}

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

	if exportFormat == "json" {
		c.JSON(http.StatusOK, items)
		return
	}
	generateCampaignExcel(c, repo.Name, tt.DisplayName, items, isEntityMode)
}

func convertLedgerDefectsToExcelItems(items []defectlifecycle.CampaignFinding) []ExcelFindingItem {
	excelItems := make([]ExcelFindingItem, 0, len(items))
	for _, item := range items {
		assignee := ""
		if item.Assignee != nil {
			assignee = item.Assignee.Name
		}
		comment := ""
		if len(item.StatusLog) > 0 {
			if value, ok := item.StatusLog[len(item.StatusLog)-1]["comment"].(string); ok {
				comment = value
			}
		}
		if comment == "" {
			comment = item.Feedback
		}
		excelItems = append(excelItems, ExcelFindingItem{
			ID: fmt.Sprintf("%d", item.ID), Severity: item.Severity, Category: item.Category,
			FilePath: item.FilePath, LineNumber: item.LineNumber, Title: item.Title,
			Detail: item.Detail, Suggestion: item.Suggestion, Status: item.Status,
			Assignee: assignee, Comment: comment,
		})
	}
	return excelItems
}

// DynamicCampaignDeptSummary 部门维度专项汇总结构
type DynamicCampaignDeptSummary struct {
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

	summaries, err := FetchCampaignDeptSummaries(tt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch department summaries: " + err.Error()})
		return
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

// FetchCampaignDeptSummaries 获取指定专项下的所有部门指标汇总列表
func FetchCampaignDeptSummaries(tt *models.TaskType) ([]DynamicCampaignDeptSummary, error) {
	repoSummaries, err := FetchCampaignRepoSummaries(tt, "", "")
	if err != nil {
		return nil, err
	}

	var depts []models.Department
	if err := models.DB.Find(&depts).Error; err != nil {
		return nil, err
	}

	metricsMap := make(map[string]*DynamicCampaignDeptSummary)
	for _, repo := range repoSummaries {
		department := repo.Department
		if department == "" {
			continue
		}
		metric := metricsMap[department]
		if metric == nil {
			metric = &DynamicCampaignDeptSummary{Department: department}
			metricsMap[department] = metric
		}
		metric.TotalRepos++
		if !repo.LastScanTime.IsZero() {
			metric.ScannedRepos++
		}
		metric.TotalIssues += repo.TotalIssues
		metric.OpenIssues += repo.OpenIssues
		metric.ResolvedIssues += repo.ResolvedIssues
		metric.PassCount += repo.PassCount
	}

	var summaries []DynamicCampaignDeptSummary
	for _, dept := range depts {
		metric, ok := metricsMap[dept.Name]
		if !ok || metric.TotalRepos == 0 {
			continue
		}

		fixRate := 100.0
		if metric.TotalIssues > 0 {
			fixRate = (float64(metric.ResolvedIssues) / float64(metric.TotalIssues)) * 100.0
		}

		passRate := 100.0
		if metric.TotalIssues > 0 {
			passRate = (float64(metric.PassCount) / float64(metric.TotalIssues)) * 100.0
		}

		summaries = append(summaries, DynamicCampaignDeptSummary{
			Department:     metric.Department,
			ScannedRepos:   metric.ScannedRepos,
			TotalRepos:     metric.TotalRepos,
			TotalIssues:    metric.TotalIssues,
			OpenIssues:     metric.OpenIssues,
			ResolvedIssues: metric.ResolvedIssues,
			PassCount:      metric.PassCount,
			PassRate:       passRate,
			FixRate:        fixRate,
		})
	}
	return summaries, nil
}

// GetDynamicCampaignTrends 通用专项 30 天收敛趋势统计 (高性能优化版)
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

	cacheKey := fmt.Sprintf("%d_%s_%s", tt.ID, repoIDStr, deptName)
	trendCacheLock.RLock()
	if entry, ok := trendCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		trendCacheLock.RUnlock()
		c.JSON(http.StatusOK, entry.data)
		return
	}
	trendCacheLock.RUnlock()

	if models.ResolveGovernanceMode(tt.GovernanceMode) != models.GovernanceModeEntityAssessment {
		repoIDs := make([]uint, 0)
		if repoIDStr != "" {
			if repoID, err := strconv.Atoi(repoIDStr); err == nil && repoID > 0 {
				repoIDs = append(repoIDs, uint(repoID))
			}
		} else if deptName != "" {
			var dept models.Department
			if err := models.DB.Where("name = ?", deptName).First(&dept).Error; err != nil {
				c.JSON(http.StatusOK, []defectlifecycle.CampaignTrendPoint{})
				return
			}
			var repos []models.Repository
			if err := models.DB.Where("department_id = ?", dept.ID).Find(&repos).Error; err == nil {
				for _, repo := range repos {
					repoIDs = append(repoIDs, repo.ID)
				}
			}
		}
		trend, err := defectlifecycle.LedgerTrend(models.DB, tt.ID, repoIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		trendCacheLock.Lock()
		trendCache[cacheKey] = campaignTrendCacheEntry{data: trend, expiresAt: time.Now().Add(60 * time.Second)}
		trendCacheLock.Unlock()
		c.JSON(http.StatusOK, trend)
		return
	}

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

	// 1. 轻量字段投影：仅查询 created_at, severity, status, status_log，杜绝全字段检索重型大文本
	type findingTrendProjection struct {
		CreatedAt time.Time      `gorm:"column:created_at"`
		Severity  string         `gorm:"column:severity"`
		Status    string         `gorm:"column:status"`
		StatusLog datatypes.JSON `gorm:"column:status_log"`
	}

	var allFindings []findingTrendProjection
	if err := query.Select("created_at, severity, status, status_log").Find(&allFindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch findings for trend"})
		return
	}

	// 2. 一次性 O(N) 预解析：将状态变更记录提前解析为 time.Time，彻底避免在 30 天循环内执行数百万次 json.Unmarshal / time.Parse
	type statusChangeEntry struct {
		t      time.Time
		status string
	}

	type preprocessedFinding struct {
		createdTime   time.Time
		initialStatus string
		isPass        bool
		history       []statusChangeEntry
	}

	parsedFindings := make([]preprocessedFinding, len(allFindings))
	for idx, f := range allFindings {
		isPass := isEntityMode && f.Severity == "合格"
		initialStatus := "open"
		if isPass {
			initialStatus = "closed"
		}

		pf := preprocessedFinding{
			createdTime:   f.CreatedAt,
			initialStatus: initialStatus,
			isPass:        isPass,
		}

		if len(f.StatusLog) > 4 {
			var statusHistory []struct {
				Status string `json:"status"`
				Time   string `json:"time"`
			}
			if err := json.Unmarshal(f.StatusLog, &statusHistory); err == nil && len(statusHistory) > 0 {
				pf.history = make([]statusChangeEntry, 0, len(statusHistory))
				for _, entry := range statusHistory {
					if t, err := time.Parse("2006-01-02 15:04:05", entry.Time); err == nil {
						pf.history = append(pf.history, statusChangeEntry{
							t:      t,
							status: entry.Status,
						})
					}
				}
			}
		}

		parsedFindings[idx] = pf
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

	// 3. 内存高效统计 30 天时序收敛曲线
	for i := 29; i >= 0; i-- {
		targetDate := now.AddDate(0, 0, -i)
		dateStr := targetDate.Format("2006-01-02")
		endOfDay := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 23, 59, 59, 999999999, targetDate.Location())

		var totalIssues, openIssues, resolvedIssues, passCount int

		for _, pf := range parsedFindings {
			if pf.createdTime.After(endOfDay) {
				continue
			}

			totalIssues++

			statusOnDate := pf.initialStatus
			if len(pf.history) > 0 {
				for _, h := range pf.history {
					if !h.t.After(endOfDay) {
						statusOnDate = h.status
					}
				}
			}

			if statusOnDate == "open" || statusOnDate == "analyzing" {
				openIssues++
			} else if statusOnDate == "resolved" || statusOnDate == "closed" || statusOnDate == "invalid" {
				resolvedIssues++
			}

			if isEntityMode {
				if pf.isPass || statusOnDate == "closed" || statusOnDate == "resolved" {
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

	// 4. 写入短 TTL 本地缓存
	trendCacheLock.Lock()
	trendCache[cacheKey] = campaignTrendCacheEntry{
		data:      trendMap,
		expiresAt: time.Now().Add(60 * time.Second),
	}
	trendCacheLock.Unlock()

	c.JSON(http.StatusOK, trendMap)
}
