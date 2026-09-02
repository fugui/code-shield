package handlers

import (
	"bytes"
	"code-shield/services"
	"fmt"
	"net/http"
	httpPprof "net/http/pprof"
	"regexp"
	"runtime"
	runtimePprof "runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

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

// GoroutineCluster 代表相同等待特征和调用栈顶部的协程聚类
type GoroutineCluster struct {
	State        string `json:"state"`         // 等待/执行状态，如 "sync.Cond.Wait", "chan receive", "running", "IO wait"
	KeyFunction  string `json:"key_function"`  // 识别出的关键业务/框架函数，如 "ModelDispatcher.Acquire", "DebateEngine.Run"
	Location     string `json:"location"`      // 源码行号位置
	Count        int    `json:"count"`         // 该特征聚类的协程总数
	SampleStack  string `json:"sample_stack"`  // 示例完整堆栈
	GoroutineIDs []int  `json:"goroutine_ids"` // 归属于该聚类的部分 Goroutine ID 列表
}

// GetDebugOverview 提供用于前端系统诊断大盘的结构化运行态指标与堆栈聚类
func GetDebugOverview(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	numGoroutine := runtime.NumGoroutine()
	numCPU := runtime.NumCPU()
	uptimeSec := int64(time.Since(serverStartTime).Seconds())

	// 1. 抓取并聚类 Goroutine 堆栈
	clusters := analyzeGoroutineStacks()

	// 2. 获取模型调度器并发状态
	var throttleInfo services.ThrottleInfo
	var resourceList []services.ModelResourceStatus
	if services.Dispatcher != nil {
		throttleInfo = services.Dispatcher.GetThrottleInfo()
		resourceList = services.Dispatcher.GetResourcesStatus()
	}

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
			"throttle_info": throttleInfo,
			"resources":     resourceList,
		},
		"goroutines": gin.H{
			"total":    numGoroutine,
			"clusters": clusters,
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

// analyzeGoroutineStacks 抓取 full stack 并智能按状态和函数聚类
func analyzeGoroutineStacks() []GoroutineCluster {
	var buf bytes.Buffer
	p := runtimePprof.Lookup("goroutine")
	if p == nil {
		return nil
	}
	_ = p.WriteTo(&buf, 2) // debug=2: full stack traces

	raw := buf.String()
	blocks := strings.Split(raw, "\n\n")

	type clusterKey struct {
		state   string
		keyFunc string
		loc     string
	}

	clusterMap := make(map[clusterKey]*GoroutineCluster)
	headerRegex := regexp.MustCompile(`^goroutine\s+(\d+)\s+\[([^\]]+)\]:`)

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		lines := strings.Split(block, "\n")
		if len(lines) == 0 {
			continue
		}

		match := headerRegex.FindStringSubmatch(lines[0])
		if len(match) < 3 {
			continue
		}

		gid, _ := strconv.Atoi(match[1])
		rawState := match[2]

		// 规范化状态（去除具体的等待分钟数，如 "sync.Cond.Wait, 15 minutes" -> "sync.Cond.Wait"）
		state := rawState
		if idx := strings.Index(state, ","); idx != -1 {
			state = strings.TrimSpace(state[:idx])
		}

		// 提取最有代表性的调用栈帧（优先寻找业务代码 code-shield，其次找有意义的第三方库或运行时顶层）
		keyFunc := "unknown"
		location := ""

		for i := 1; i < len(lines); i += 2 {
			fn := strings.TrimSpace(lines[i])
			loc := ""
			if i+1 < len(lines) {
				loc = strings.TrimSpace(lines[i+1])
			}

			// 跳过 runtime 内部基础分发帧
			if strings.HasPrefix(fn, "runtime.") || strings.HasPrefix(fn, "runtime/pprof.") {
				if keyFunc == "unknown" {
					keyFunc = fn
					location = loc
				}
				continue
			}

			keyFunc = fn
			location = loc

			// 如果命中了业务主逻辑（code-shield），作为高优先级聚类特征
			if strings.Contains(fn, "code-shield") {
				break
			}
		}

		// 简化函数名展示，提升可读性
		cleanKeyFunc := simplifyFunctionName(keyFunc)

		k := clusterKey{state: state, keyFunc: cleanKeyFunc, loc: location}
		if c, exists := clusterMap[k]; exists {
			c.Count++
			if len(c.GoroutineIDs) < 10 {
				c.GoroutineIDs = append(c.GoroutineIDs, gid)
			}
		} else {
			clusterMap[k] = &GoroutineCluster{
				State:        state,
				KeyFunction:  cleanKeyFunc,
				Location:     location,
				Count:        1,
				SampleStack:  block,
				GoroutineIDs: []int{gid},
			}
		}
	}

	var list []GoroutineCluster
	for _, c := range clusterMap {
		list = append(list, *c)
	}

	// 按数量从高到低排序
	sort.Slice(list, func(i, j int) bool {
		return list[i].Count > list[j].Count
	})

	return list
}

func simplifyFunctionName(fn string) string {
	// 去除多余路径前缀，保留包名与方法名
	if idx := strings.LastIndex(fn, "/"); idx != -1 {
		fn = fn[idx+1:]
	}
	return fn
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
