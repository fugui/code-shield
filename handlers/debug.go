package handlers

import (
	"errors"
	"fmt"
	"net/http"
	httpPprof "net/http/pprof"
	"runtime"
	"strconv"
	"time"

	"code-shield/models"
	"code-shield/services"
	"code-shield/services/invoker"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var serverStartTime = time.Now()

// RegisterPProfRoutes 将标准 pprof 调试端点安全挂载在传入的 RouterGroup 下
func RegisterPProfRoutes(rg *gin.RouterGroup) {
	p := rg.Group("/pprof")
	{
		p.GET("/", gin.WrapF(httpPprof.Index))
		p.GET("/cmdline", gin.WrapF(httpPprof.Cmdline))
		p.GET("/profile", gin.WrapF(httpPprof.Profile))
		p.POST("/symbol", gin.WrapF(httpPprof.Symbol))
		p.GET("/symbol", gin.WrapF(httpPprof.Symbol))
		p.GET("/trace", gin.WrapF(httpPprof.Trace))
		p.GET("/goroutine", gin.WrapH(httpPprof.Handler("goroutine")))
		p.GET("/heap", gin.WrapH(httpPprof.Handler("heap")))
		p.GET("/mutex", gin.WrapH(httpPprof.Handler("mutex")))
		p.GET("/block", gin.WrapH(httpPprof.Handler("block")))
		p.GET("/threadcreate", gin.WrapH(httpPprof.Handler("threadcreate")))
		p.GET("/allocs", gin.WrapH(httpPprof.Handler("allocs")))
	}
}

// GetDebugOverview 提供用于前端系统诊断大盘的结构化运行态指标、任务看板与 AI 资源透视
func GetDebugOverview(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	numGoroutine := runtime.NumGoroutine()
	numCPU := runtime.NumCPU()
	uptimeSec := int64(time.Since(serverStartTime).Seconds())

	// 1. 获取模型调度器并发状态与总槽位统计
	var throttleInfo services.ThrottleInfo
	var resourceList []services.ModelResourceStatus
	var activeLeases []services.LLMSlotLease
	var recentLeases []services.LLMSlotLease
	var dispatcherMetrics services.DispatcherMetricsSnapshot
	var debugSnapshot services.DispatcherDebugSnapshot
	totalActiveSlots := 0
	totalLimitSlots := 0
	totalRawSlots := 0

	if services.Dispatcher != nil {
		throttleInfo = services.Dispatcher.GetThrottleInfo()
		debugSnapshot = services.Dispatcher.GetDebugSnapshot()
		resourceList = debugSnapshot.Resources
		activeLeases = debugSnapshot.ActiveLeases
		recentLeases = debugSnapshot.RecentLeases
		dispatcherMetrics = debugSnapshot.Metrics
		for _, r := range resourceList {
			totalActiveSlots += r.Active
			totalLimitSlots += r.Limit
			totalRawSlots += r.Concurrent
		}
	}
	if activeLeases == nil {
		activeLeases = []services.LLMSlotLease{}
	}
	if recentLeases == nil {
		recentLeases = []services.LLMSlotLease{}
	}
	if dispatcherMetrics.Pools == nil {
		dispatcherMetrics.Pools = []services.PoolMetricsSnapshot{}
	}
	if dispatcherMetrics.Tiers == nil {
		dispatcherMetrics.Tiers = []services.TierMetricsSnapshot{}
	}

	// 2. 获取当前正在运行的在途任务快照
	runningTasks := services.GetRunningTasks()

	// 3. Worker 工作池与队列排队水位
	workerCount := services.WorkerPoolSize()
	if workerCount <= 0 {
		workerCount = 5
	}
	maxQueueSize := models.AppConfig.Scanner.MaxQueueSize
	if maxQueueSize <= 0 {
		maxQueueSize = 2000
	}

	var pendingCount int64
	models.DB.Model(&models.TaskReport{}).Where("status = ?", models.StatusPending).Count(&pendingCount)

	// 4. 今日执行与算力消耗统计 (00:00 至今)
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	dailyStats := loadDailyStats(models.DB, todayStart)

	lastGCTimeStr := ""
	if m.LastGC > 0 {
		lastGCTimeStr = time.Unix(0, int64(m.LastGC)).Format("2006-01-02 15:04:05")
	}

	c.JSON(http.StatusOK, gin.H{
		"system": gin.H{
			"go_version":        runtime.Version(),
			"num_cpu":           numCPU,
			"num_goroutine":     numGoroutine,
			"server_start_time": serverStartTime.Format("2006-01-02 15:04:05"),
			"uptime_seconds":    uptimeSec,
			"uptime_formatted":  formatUptime(uptimeSec),
		},
		"memory": gin.H{
			"alloc_bytes":       m.Alloc,
			"alloc_formatted":   formatBytes(m.Alloc),
			"total_alloc_bytes": m.TotalAlloc,
			"total_alloc_fmt":   formatBytes(m.TotalAlloc),
			"sys_bytes":         m.Sys,
			"sys_formatted":     formatBytes(m.Sys),
			"heap_alloc_bytes":  m.HeapAlloc,
			"heap_alloc_fmt":    formatBytes(m.HeapAlloc),
			"heap_sys_bytes":    m.HeapSys,
			"heap_sys_fmt":      formatBytes(m.HeapSys),
			"heap_inuse_bytes":  m.HeapInuse,
			"heap_idle_bytes":   m.HeapIdle,
			"heap_released_fmt": formatBytes(m.HeapReleased),
			"heap_objects":      m.HeapObjects,
			"num_gc":            m.NumGC,
			"pause_total_ms":    float64(m.PauseTotalNs) / 1e6,
			"last_gc_time":      lastGCTimeStr,
		},
		"dispatcher": gin.H{
			"throttle_info":         throttleInfo,
			"resources":             resourceList,
			"total_active_slots":    totalActiveSlots,
			"total_limit_slots":     totalLimitSlots,
			"total_raw_slots":       totalRawSlots,
			"active_leases":         activeLeases,
			"recent_leases":         recentLeases,
			"metrics":               dispatcherMetrics,
			"opencode_continuation": invoker.GetOpenCodeContinuationMetrics(),
		},
		"active_leases": activeLeases,
		"workers": gin.H{
			"worker_count":   workerCount,
			"active_workers": len(runningTasks),
			"max_queue_size": maxQueueSize,
			"pending_tasks":  pendingCount,
			"is_paused":      services.IsQueuePaused(),
		},
		"active_tasks": runningTasks,
		"daily_stats": gin.H{
			"today_total":        dailyStats.Total,
			"today_success":      dailyStats.Success,
			"today_failed":       dailyStats.Failed,
			"today_tier1_tokens": dailyStats.Tier1Tokens,
			"today_tier2_tokens": dailyStats.Tier2Tokens,
		},
	})
}

type DailyStats struct {
	Total       int64
	Success     int64
	Failed      int64
	Tier1Tokens int64
	Tier2Tokens int64
}

type dailyStatusRow struct {
	Status      string `gorm:"column:status"`
	Count       int64  `gorm:"column:count"`
	Tier1Tokens int64  `gorm:"column:tier1_tokens"`
	Tier2Tokens int64  `gorm:"column:tier2_tokens"`
}

func loadDailyStats(db *gorm.DB, todayStart time.Time) DailyStats {
	var stats DailyStats
	if db == nil {
		return stats
	}
	var rows []dailyStatusRow
	db.Raw(`
		SELECT
			tr.status,
			COUNT(*) AS count,
			COALESCE(SUM(tr.tier1_tokens), 0) AS tier1_tokens,
			COALESCE(SUM(tr.tier2_tokens), 0) AS tier2_tokens
		FROM task_reports tr
		WHERE tr.created_at >= ?
		GROUP BY tr.status
	`, todayStart).Scan(&rows)

	for _, row := range rows {
		stats.Total += row.Count
		stats.Tier1Tokens += row.Tier1Tokens
		stats.Tier2Tokens += row.Tier2Tokens
		switch row.Status {
		case models.StatusSuccess, models.StatusDegraded:
			stats.Success += row.Count
		case models.StatusFailed:
			stats.Failed += row.Count
		}
	}
	return stats
}

// GetRecentLeases returns the read-only, in-memory history of completed LLM
// compute slot leases. No model request is executed by this endpoint.
func GetRecentLeases(c *gin.Context) {
	if services.Dispatcher == nil {
		c.JSON(http.StatusOK, gin.H{"leases": []services.LLMSlotLease{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"limit":                    50,
		"persistent":               false,
		"record_successful_leases": services.Dispatcher.RecordSuccessfulLeases(),
		"leases":                   services.Dispatcher.GetRecentLeases(),
	})
}

// UpdateRecentLeaseOptions updates the in-memory completion history policy.
// It only affects the current process and never changes running tasks.
func UpdateRecentLeaseOptions(c *gin.Context) {
	if services.Dispatcher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "dispatcher is not initialized"})
		return
	}
	var req struct {
		RecordSuccessfulLeases *bool `json:"record_successful_leases"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.RecordSuccessfulLeases == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "record_successful_leases is required"})
		return
	}
	services.Dispatcher.SetRecordSuccessfulLeases(*req.RecordSuccessfulLeases)
	c.JSON(http.StatusOK, gin.H{
		"record_successful_leases": *req.RecordSuccessfulLeases,
	})
}

// GetLeaseDetail returns the super-admin-only diagnostic metadata for one LLM
// slot lease. It performs no mutation and never executes a model request.
func GetLeaseDetail(c *gin.Context) {
	if services.Dispatcher == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dispatcher is not initialized"})
		return
	}
	lease, ok := services.Dispatcher.GetLeaseByID(c.Param("lease_id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "lease not found or expired"})
		return
	}

	response := gin.H{
		"lease_id":           lease.LeaseID,
		"server_id":          lease.ServerID,
		"driver":             lease.Driver,
		"model":              lease.Model,
		"report_id":          lease.ReportID,
		"repo_name":          lease.RepoName,
		"task_type":          lease.TaskType,
		"stage":              lease.Stage,
		"sub_task":           lease.SubTask,
		"tier_name":          lease.TierName,
		"queue_started_at":   lease.QueueStartedAt,
		"queue_wait_seconds": lease.QueueWaitSeconds,
		"start_time":         lease.StartTime,
		"duration_seconds":   lease.DurationSeconds,
		"status":             lease.Status,
		"error_class":        lease.ErrorClass,
		"error_message":      lease.ErrorMessage,
		"diagnostics":        lease.Diagnostics,
	}
	if lease.EndTime != nil {
		response["end_time"] = *lease.EndTime
	}
	if lease.Observability != nil {
		response["output"] = lease.Observability.OutputDetail(64 << 10)
		response["console"] = lease.Observability.Console.Status()
		response["prompt_available"] = lease.Observability.PromptAvailable()
		response["observability"] = gin.H{
			"degraded":        lease.Observability.Degraded,
			"degraded_reason": lease.Observability.DegradedReason,
		}
		if nativeMetrics := lease.Observability.NativeMetricsSnapshot(); nativeMetrics != nil {
			response["native_metrics"] = nativeMetrics
		}
	} else {
		response["prompt_available"] = false
	}

	c.JSON(http.StatusOK, response)
}

// GetLeasePrompt returns the bounded prompt detail for one active lease.
func GetLeasePrompt(c *gin.Context) {
	if services.Dispatcher == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dispatcher is not initialized"})
		return
	}
	lease, ok := services.Dispatcher.GetLeaseByID(c.Param("lease_id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "lease not found or expired"})
		return
	}
	if lease.Observability == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lease has no observability context"})
		return
	}

	detail, err := lease.Observability.PromptDetail()
	if errors.Is(err, invoker.ErrPromptProviderExpired) {
		c.JSON(http.StatusGone, gin.H{"error": "provider_expired"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// GetLeaseConsole incrementally returns bounded stdout/stderr/model events.
func GetLeaseConsole(c *gin.Context) {
	if services.Dispatcher == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dispatcher is not initialized"})
		return
	}
	lease, ok := services.Dispatcher.GetLeaseByID(c.Param("lease_id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "lease not found or expired"})
		return
	}
	if lease.Observability == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lease has no observability context"})
		return
	}

	afterSeq, err := strconv.ParseInt(c.DefaultQuery("after_seq", "0"), 10, 64)
	if err != nil || afterSeq < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid after_seq"})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if err != nil || limit < 1 || limit > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
		return
	}

	snapshot, status := lease.Observability.Console.Snapshot(afterSeq, limit)
	c.JSON(http.StatusOK, gin.H{
		"lease_id": lease.LeaseID,
		"status":   lease.Status,
		"console":  snapshot,
		"summary":  status,
	})
}

// TriggerGC 允许管理员手动触发一次 Full GC 并返回回收前后状态
func TriggerGC(c *gin.Context) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	start := time.Now()
	runtime.GC()
	durationMs := time.Since(start).Milliseconds()

	runtime.ReadMemStats(&after)

	freedBytes := int64(before.Alloc) - int64(after.Alloc)
	if freedBytes < 0 {
		freedBytes = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "GC completed successfully",
		"duration_ms":      durationMs,
		"freed_bytes":      freedBytes,
		"freed_formatted":  formatBytes(uint64(freedBytes)),
		"before_alloc_fmt": formatBytes(before.Alloc),
		"after_alloc_fmt":  formatBytes(after.Alloc),
		"num_gc":           after.NumGC,
	})
}

// ResetActiveSlots 允许管理员手动一键重置当前 AI 算力节点的活跃槽位（用于紧急解除因孤儿泄漏导致的死锁）
func ResetActiveSlots(c *gin.Context) {
	if services.Dispatcher == nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "Dispatcher not initialized",
			"cleared": 0,
		})
		return
	}

	cleared := services.Dispatcher.ResetActiveSlots()
	c.JSON(http.StatusOK, gin.H{
		"message": "Active slots successfully reset and waiting workers unblocked",
		"cleared": cleared,
	})
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatUptime(seconds int64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	mins := (seconds % 3600) / 60
	secs := seconds % 60

	if days > 0 {
		return fmt.Sprintf("%d天 %d小时 %d分 %d秒", days, hours, mins, secs)
	}
	if hours > 0 {
		return fmt.Sprintf("%d小时 %d分 %d秒", hours, mins, secs)
	}
	if mins > 0 {
		return fmt.Sprintf("%d分 %d秒", mins, secs)
	}
	return fmt.Sprintf("%d秒", secs)
}
