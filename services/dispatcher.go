package services

import (
	"code-shield/models"
	"context"
	"log"
	"math"
	"sync"
	"time"
)

// ModelResource 代表单台 LLM 服务器的模型资源以及并发追踪
type ModelResource struct {
	Index      int
	OpenCode   string
	Claude     string
	Codex      string
	Concurrent int
	Active     int // 当前正在运行的并发数
}

// ModelName 根据后端类型返回当前服务器映射的具体模型名
func (r *ModelResource) ModelName(backend string) string {
	switch backend {
	case "opencode":
		return r.OpenCode
	case "claude":
		return r.Claude
	case "codex":
		return r.Codex
	default:
		return ""
	}
}

// ThrottleInfo 封装当前流控全景状态信息
type ThrottleInfo struct {
	EffectiveScale  float64                        `json:"effective_scale"`   // 最终生效比例 (0.0 ~ 3.0)
	ThrottleMode    string                         `json:"throttle_mode"`     // "work_hours", "manual", "normal"
	ManualScale     float64                        `json:"manual_scale"`      // 手动设置的比例
	ScaleExpiresAt  *time.Time                     `json:"scale_expires_at"`  // 手动过期时间（有手动且有持续时间时）
	IsManual        bool                           `json:"is_manual"`         // 是否正处于手动覆盖中
	IsWorkHours     bool                           `json:"is_work_hours"`     // 当前是否命中工作时间
	WorkHoursConfig models.WorkHoursThrottleConfig `json:"work_hours_config"` // 工作时间自动限流配置
}

// ModelDispatcher 负责协调跨不同物理/逻辑 LLM 服务器的 AI 并发
type ModelDispatcher struct {
	mu                 sync.Mutex
	cond               *sync.Cond
	resources          []*ModelResource
	enabled            bool
	manualScale        float64     // 管理员手动设置的并发折扣系数 [0.0, 3.0]
	scaleExpiresAt     time.Time   // 手动设置失效时间
	scaleTimer         *time.Timer // 到期自动恢复定时器
	stopHeartbeat      chan struct{}
	unsupportedWarned  map[string]time.Time // 记录各 backend 最近一次直通降级告警时间（限频）
	lastEffectiveScale float64              // 记录上一时刻生效的 scale
	lastThrottleMode   string               // 记录上一时刻模式
}

// Dispatcher 为多 LLM 并发分配器的全局单例
var Dispatcher *ModelDispatcher

// isInsideWorkHours 判定指定时刻是否处于工作时间窗口内
func isInsideWorkHours(now time.Time, cfg models.WorkHoursThrottleConfig) bool {
	if !cfg.Enabled {
		return false
	}
	// 星期判定: Go time.Weekday: 0=Sunday, 1=Monday, ..., 6=Saturday
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	matchedDay := false
	for _, d := range cfg.Workdays {
		if d == wd || (d == 0 && wd == 7) {
			matchedDay = true
			break
		}
	}
	if !matchedDay {
		return false
	}

	// 时分判定: 如 "09:00" ~ "22:00"
	currentHM := now.Format("15:04")
	if cfg.StartTime <= cfg.EndTime {
		return currentHM >= cfg.StartTime && currentHM < cfg.EndTime
	}
	// 跨午夜情况（如 22:00 ~ 06:00）
	return currentHM >= cfg.StartTime || currentHM < cfg.EndTime
}

// InitModelDispatcher 初始化全局并发调度器
func InitModelDispatcher() {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
	}
	d.cond = sync.NewCond(&d.mu)

	for i, mc := range models.AppConfig.AI.Models {
		concurrent := mc.Concurrent
		if concurrent <= 0 {
			concurrent = 1
		}
		d.resources = append(d.resources, &ModelResource{
			Index:      i,
			OpenCode:   mc.OpenCode,
			Claude:     mc.Claude,
			Codex:      mc.Codex,
			Concurrent: concurrent,
		})
	}

	if len(d.resources) > 0 {
		d.enabled = true
		log.Printf("[Dispatcher] Initialized with %d custom LLM servers\n", len(d.resources))
		for _, r := range d.resources {
			log.Printf("  - Server #%d: opencode=%s, claude=%s, codex=%s, concurrent=%d\n", r.Index, r.OpenCode, r.Claude, r.Codex, r.Concurrent)
		}
	} else {
		d.enabled = false
		log.Println("[Dispatcher] No custom models configured, dispatcher is disabled (falling back to default backend settings)")
	}

	// 从数据库中恢复持久化的手动流控配置（如果数据库已就绪）
	if models.DB != nil {
		var cfg models.SystemConfig
		if err := models.DB.First(&cfg, 1).Error; err == nil {
			if cfg.ConcurrencyScale > 0 || cfg.ScaleExpiresAt != nil {
				if cfg.ScaleExpiresAt != nil && cfg.ScaleExpiresAt.After(time.Now()) {
					// 恢复未过期的手动限流状态
					dur := time.Until(*cfg.ScaleExpiresAt)
					d.manualScale = cfg.ConcurrencyScale
					d.scaleExpiresAt = *cfg.ScaleExpiresAt
					d.scaleTimer = time.AfterFunc(dur, func() {
						d.RestoreManualScale()
					})
					log.Printf("[Dispatcher] Restored active manual throttle from DB: scale=%.2f, expires in %v (at %s)\n",
						d.manualScale, dur.Round(time.Second), d.scaleExpiresAt.Format("15:04:05"))
				} else if cfg.ConcurrencyScale != 1.0 && cfg.ScaleExpiresAt != nil {
					// 已经过期，重置手动状态并更新 DB
					d.manualScale = 1.0
					d.scaleExpiresAt = time.Time{}
					models.DB.Model(&models.SystemConfig{}).Where("id = 1").Updates(map[string]interface{}{
						"concurrency_scale": 1.0,
						"scale_expires_at":  nil,
					})
					log.Println("[Dispatcher] Stale manual throttle config from DB was reset to default.")
				}
			}
		}
	}

	// 初始化计算生效状态
	initialInfo := d.getEffectiveScaleInfoLocked(time.Now())
	d.lastEffectiveScale = initialInfo.EffectiveScale
	d.lastThrottleMode = initialInfo.ThrottleMode
	log.Printf("[Dispatcher] Initial throttle state: mode=%s, effective_scale=%.2f\n",
		initialInfo.ThrottleMode, initialInfo.EffectiveScale)

	// 启动后台自愈守护心跳（每 1 秒触发一次广播检查，检测工作时间跨界或手动到期）
	go d.startHeartbeat()

	Dispatcher = d
}

// getEffectiveScaleInfoLocked 计算当前生效流控（需在持有 d.mu 时调用）
func (d *ModelDispatcher) getEffectiveScaleInfoLocked(now time.Time) ThrottleInfo {
	cfg := models.AppConfig.AI.WorkHoursThrottle
	isWorkHours := isInsideWorkHours(now, cfg)

	info := ThrottleInfo{
		WorkHoursConfig: cfg,
		IsWorkHours:     isWorkHours,
		ManualScale:     d.manualScale,
	}

	// 1. 如果设置了过期时间，但当前时刻已超过过期时间，则手动覆盖已失效，重置为默认
	if !d.scaleExpiresAt.IsZero() && !now.Before(d.scaleExpiresAt) {
		d.manualScale = 1.0
		d.scaleExpiresAt = time.Time{}
		if d.scaleTimer != nil {
			d.scaleTimer.Stop()
			d.scaleTimer = nil
		}
		d.syncToDBLocked()
	}

	// 如果当前存在有效的手动覆盖（永久手动 scale != 1.0，或未过期的时限手动）
	if d.manualScale != 1.0 || !d.scaleExpiresAt.IsZero() {
		info.EffectiveScale = d.manualScale
		info.ThrottleMode = "manual"
		info.IsManual = true
		if !d.scaleExpiresAt.IsZero() {
			exp := d.scaleExpiresAt
			info.ScaleExpiresAt = &exp
		}
		return info
	}

	// 2. 如果开启了工作时间自动限流且当前处于工作时间内
	if isWorkHours {
		info.EffectiveScale = cfg.Scale
		info.ThrottleMode = "work_hours"
		info.IsManual = false
		return info
	}

	// 3. 正常运行模式 (100%)
	info.EffectiveScale = 1.0
	info.ThrottleMode = "normal"
	info.IsManual = false
	return info
}

// startHeartbeat 启动后台自愈心跳
func (d *ModelDispatcher) startHeartbeat() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopHeartbeat:
			return
		case now := <-ticker.C:
			d.mu.Lock()
			info := d.getEffectiveScaleInfoLocked(now)

			// 检查流控模式或生效比例是否发生突变（例如到达 09:00 或 22:00，或者手动限流到期）
			if info.EffectiveScale != d.lastEffectiveScale || info.ThrottleMode != d.lastThrottleMode {
				log.Printf("[Dispatcher] Throttle status switched: mode=%s, scale=%.2f (was mode=%s, scale=%.2f)\n",
					info.ThrottleMode, info.EffectiveScale, d.lastThrottleMode, d.lastEffectiveScale)
				d.lastEffectiveScale = info.EffectiveScale
				d.lastThrottleMode = info.ThrottleMode
				d.syncToDBLocked()
				// 状态切换时主动广播唤醒等待中的 Worker
				d.cond.Broadcast()
			} else {
				// 每秒兜底心跳广播
				d.cond.Broadcast()
			}
			d.mu.Unlock()
		}
	}
}

// syncToDBLocked 将当前流控状态持久化到数据库（需在持有 d.mu 时调用）
func (d *ModelDispatcher) syncToDBLocked() {
	if models.DB == nil {
		return
	}
	var expiresPtr *time.Time
	if !d.scaleExpiresAt.IsZero() {
		expiresPtr = &d.scaleExpiresAt
	}
	models.DB.Model(&models.SystemConfig{}).Where("id = 1").Updates(map[string]interface{}{
		"concurrency_scale": d.manualScale,
		"scale_expires_at":  expiresPtr,
	})
}

// RestoreManualScale 恢复手动设置为默认（即解除手动覆盖，回归计划策略）
func (d *ModelDispatcher) RestoreManualScale() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.scaleTimer != nil {
		d.scaleTimer.Stop()
		d.scaleTimer = nil
	}

	d.manualScale = 1.0
	d.scaleExpiresAt = time.Time{}
	d.syncToDBLocked()

	info := d.getEffectiveScaleInfoLocked(time.Now())
	d.lastEffectiveScale = info.EffectiveScale
	d.lastThrottleMode = info.ThrottleMode

	log.Printf("[Dispatcher] Manual throttle released. Current effective mode=%s, scale=%.2f\n",
		info.ThrottleMode, info.EffectiveScale)

	// 核心：主动唤醒所有等待协程
	d.cond.Broadcast()
}

// SetScale 设置手动并发折扣比率与持续时间
func (d *ModelDispatcher) SetScale(scale float64, duration time.Duration) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	// 合法性 Clamp
	if scale < 0 {
		scale = 0
	}
	if scale > 3.0 {
		scale = 3.0
	}

	// 停止旧的定时器
	if d.scaleTimer != nil {
		d.scaleTimer.Stop()
		d.scaleTimer = nil
	}

	if scale == 1.0 && duration == 0 {
		// 用户重置为默认计划模式
		d.manualScale = 1.0
		d.scaleExpiresAt = time.Time{}
		log.Println("[Dispatcher] Reset throttle to scheduled baseline.")
	} else {
		d.manualScale = scale
		if duration > 0 {
			d.scaleExpiresAt = time.Now().Add(duration)
			log.Printf("[Dispatcher] Manual concurrency scale set to %.2f (duration: %v, will restore at %s)\n",
				scale, duration, d.scaleExpiresAt.Format("15:04:05"))
			d.scaleTimer = time.AfterFunc(duration, func() {
				d.RestoreManualScale()
			})
		} else {
			d.scaleExpiresAt = time.Time{}
			log.Printf("[Dispatcher] Manual concurrency scale set to %.2f (permanent override)\n", scale)
		}
	}

	d.syncToDBLocked()

	info := d.getEffectiveScaleInfoLocked(time.Now())
	d.lastEffectiveScale = info.EffectiveScale
	d.lastThrottleMode = info.ThrottleMode

	// 唤醒所有正在等待槽位的 Goroutine
	d.cond.Broadcast()
}

// GetScaleAndExpiration 获取当前的并发折扣比率与过期时间（兼容旧接口）
func (d *ModelDispatcher) GetScaleAndExpiration() (float64, time.Time) {
	if d == nil {
		return 1.0, time.Time{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	info := d.getEffectiveScaleInfoLocked(time.Now())
	var exp time.Time
	if info.ScaleExpiresAt != nil {
		exp = *info.ScaleExpiresAt
	}
	return info.EffectiveScale, exp
}

// GetThrottleInfo 获取全景流控信息供 Handler 使用
func (d *ModelDispatcher) GetThrottleInfo() ThrottleInfo {
	if d == nil {
		return ThrottleInfo{EffectiveScale: 1.0, ThrottleMode: "normal"}
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.getEffectiveScaleInfoLocked(time.Now())
}

// calculateLimit 语义计算：使用 math.Ceil 保证非 0 限速下至少分配 1 个并发
func calculateLimit(rawConcurrent int, scale float64) int {
	if scale <= 0 {
		return 0
	}
	limit := int(math.Ceil(float64(rawConcurrent) * scale))
	if limit < 1 {
		limit = 1
	}
	return limit
}

// Acquire 动态请求一个支持指定后端类型的空闲 LLM 模型资源槽位。
// 如果目前所有槽位已满或处于暂停状态（scale=0），则阻塞等待，直到有槽位空出、限速恢复或 Context 被取消。
// 返回 nil, "", nil 表示调度器未启用，或没有任何 server 资源支持该 backend（两种情况均降级回默认直通行为，不做并发调度）。
func (d *ModelDispatcher) Acquire(ctx context.Context, backend string) (*ModelResource, string, error) {
	if d == nil || !d.enabled {
		return nil, "", nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Context 已取消时优先返回错误（与下方循环内的检查语义一致）
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	// 检查是否有任何 resource 配置了当前请求的 backend。若没有任何 server 支持该 backend，降级回直通模式（不进行调度排队）
	hasSupportedServer := false
	for _, res := range d.resources {
		if res.ModelName(backend) != "" {
			hasSupportedServer = true
			break
		}
	}
	if !hasSupportedServer {
		// 限频告警：直通会绕过并发限流，需要让运维感知到 backend 配置缺失
		if last, ok := d.unsupportedWarned[backend]; !ok || time.Since(last) >= time.Minute {
			log.Printf("[Dispatcher] WARNING: no server resource supports backend %q; falling back to passthrough (concurrency throttling bypassed)", backend)
			if d.unsupportedWarned == nil {
				d.unsupportedWarned = make(map[string]time.Time)
			}
			d.unsupportedWarned[backend] = time.Now()
		}
		return nil, "", nil
	}

	for {
		// 1. 检查 Context 是否已提前取消
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}

		// 获取当前即时生效系数
		info := d.getEffectiveScaleInfoLocked(time.Now())
		scale := info.EffectiveScale

		// 2. 寻找有可用配额且支持当前后端的 LLM 配置
		var bestRes *ModelResource
		for _, res := range d.resources {
			modelName := res.ModelName(backend)
			if modelName != "" {
				limit := calculateLimit(res.Concurrent, scale)
				if res.Active < limit {
					bestRes = res
					break
				}
			}
		}

		if bestRes != nil {
			bestRes.Active++
			limit := calculateLimit(bestRes.Concurrent, scale)
			log.Printf("[Dispatcher] [Acquire] Server #%d allocated for backend %s (model: %s). Concurrency: %d/%d (Scale: %.2f [%s], Raw: %d)\n",
				bestRes.Index, backend, bestRes.ModelName(backend), bestRes.Active, limit, scale, info.ThrottleMode, bestRes.Concurrent)
			return bestRes, bestRes.ModelName(backend), nil
		}

		// 3. 阻塞等待空闲。安全启动监听协程响应 Context 取消以唤醒 Wait
		waitDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				d.mu.Lock()
				d.cond.Broadcast() // 唤醒 Wait 使其感知 ctx.Err()
				d.mu.Unlock()
			case <-waitDone:
			}
		}()

		d.cond.Wait()
		close(waitDone)
	}
}

// Release 释放指定的模型资源槽位，并通知其他等待中的任务
func (d *ModelDispatcher) Release(res *ModelResource, backend string) {
	if d == nil || !d.enabled || res == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if res.Active > 0 {
		res.Active--
	}
	info := d.getEffectiveScaleInfoLocked(time.Now())
	limit := calculateLimit(res.Concurrent, info.EffectiveScale)
	log.Printf("[Dispatcher] [Release] Server #%d released for backend %s. Concurrency: %d/%d (Scale: %.2f [%s], Raw: %d)\n",
		res.Index, backend, res.Active, limit, info.EffectiveScale, info.ThrottleMode, res.Concurrent)
	d.cond.Broadcast()
}
