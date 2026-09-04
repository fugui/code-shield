package handlers

import (
	"fmt"
	"net/http"
	httpPprof "net/http/pprof"
	"runtime"
	"time"

	"code-shield/models"
	"code-shield/services"

	"github.com/gin-gonic/gin"
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
	totalActiveSlots := 0
	totalLimitSlots := 0
	totalRawSlots := 0

	if services.Dispatcher != nil {
		throttleInfo = services.Dispatcher.GetThrottleInfo()
		resourceList = services.Dispatcher.GetResourcesStatus()
		activeLeases = services.Dispatcher.GetActiveLeases()
		for _, r := range resourceList {
			totalActiveSlots += r.Active
			totalLimitSlots += r.Limit
			totalRawSlots += r.Concurrent
		}
	}
	if activeLeases == nil {
		activeLeases = []services.LLMSlotLease{}
	}

	// 2. 获取当前正在运行的在途任务快照
	runningTasks := services.GetRunningTasks()

	// 3. Worker 工作池与队列排队水位
	workerCount := models.AppConfig.Scanner.WorkerCount
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

	var todayTotal, todaySuccess, todayFailed int64
	models.DB.Model(&models.TaskReport{}).Where("created_at >= ?", todayStart).Count(&todayTotal)
	models.DB.Model(&models.TaskReport{}).Where("created_at >= ? AND status = ?", todayStart, models.StatusSuccess).Count(&todaySuccess)
	models.DB.Model(&models.TaskReport{}).Where("created_at >= ? AND status = ?", todayStart, models.StatusFailed).Count(&todayFailed)

	type TokenDefectSum struct {
		Tier1Tokens int64 `gorm:"column:tier1_tokens"`
		Tier2Tokens int64 `gorm:"column:tier2_tokens"`
		NewDefects  int64 `gorm:"column:new_defects"`
	}
	var sumResult TokenDefectSum
	models.DB.Model(&models.TaskReport{}).
		Select("COALESCE(SUM(tier1_tokens), 0) as tier1_tokens, COALESCE(SUM(tier2_tokens), 0) as tier2_tokens, COALESCE(SUM(new_defects_count), 0) as new_defects").
		Where("created_at >= ?", todayStart).
		Scan(&sumResult)

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
			"throttle_info":      throttleInfo,
			"resources":          resourceList,
			"total_active_slots": totalActiveSlots,
			"total_limit_slots":  totalLimitSlots,
			"total_raw_slots":    totalRawSlots,
			"active_leases":      activeLeases,
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
			"today_total":        todayTotal,
			"today_success":      todaySuccess,
			"today_failed":       todayFailed,
			"today_tier1_tokens": sumResult.Tier1Tokens,
			"today_tier2_tokens": sumResult.Tier2Tokens,
			"today_new_defects":  sumResult.NewDefects,
		},
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
