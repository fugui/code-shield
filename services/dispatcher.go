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
	Concurrent int
	Active     int // 当前正在运行的并发数
}

// ModelName 根据后端类型返回当前服务器映射的具体模型名
func (r *ModelResource) ModelName(backend string) string {
	if backend == "opencode" {
		return r.OpenCode
	}
	if backend == "claude" {
		return r.Claude
	}
	return ""
}

// ModelDispatcher 负责协调跨不同物理/逻辑 LLM 服务器的 AI 并发
type ModelDispatcher struct {
	mu               sync.Mutex
	cond             *sync.Cond
	resources        []*ModelResource
	enabled          bool
	concurrencyScale float64     // 内存中的并发折扣系数 [0.0, 3.0]
	scaleExpiresAt   time.Time   // 折扣失效时间
	scaleTimer       *time.Timer // 到期自动恢复定时器
	stopHeartbeat    chan struct{}
}

// Dispatcher 为多 LLM 并发分配器的全局单例
var Dispatcher *ModelDispatcher

// InitModelDispatcher 初始化全局并发调度器
func InitModelDispatcher() {
	d := &ModelDispatcher{
		concurrencyScale: 1.0,
		stopHeartbeat:    make(chan struct{}),
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
			Concurrent: concurrent,
		})
	}

	if len(d.resources) > 0 {
		d.enabled = true
		log.Printf("[Dispatcher] Initialized with %d custom LLM servers\n", len(d.resources))
		for _, r := range d.resources {
			log.Printf("  - Server #%d: opencode=%s, claude=%s, concurrent=%d\n", r.Index, r.OpenCode, r.Claude, r.Concurrent)
		}
	} else {
		d.enabled = false
		log.Println("[Dispatcher] No custom models configured, dispatcher is disabled (falling back to default backend settings)")
	}

	// 从数据库中恢复持久化的流控配置（如果数据库已就绪）
	if models.DB != nil {
		var cfg models.SystemConfig
		if err := models.DB.First(&cfg, 1).Error; err == nil {
			if cfg.ConcurrencyScale > 0 || cfg.ScaleExpiresAt != nil {
				if cfg.ScaleExpiresAt != nil && cfg.ScaleExpiresAt.After(time.Now()) {
					// 恢复未过期的限流状态
					dur := time.Until(*cfg.ScaleExpiresAt)
					d.concurrencyScale = cfg.ConcurrencyScale
					d.scaleExpiresAt = *cfg.ScaleExpiresAt
					d.scaleTimer = time.AfterFunc(dur, func() {
						d.RestoreScale()
					})
					log.Printf("[Dispatcher] Restored active throttle from DB: scale=%.2f, expires in %v (at %s)\n",
						d.concurrencyScale, dur.Round(time.Second), d.scaleExpiresAt.Format("15:04:05"))
				} else if cfg.ConcurrencyScale != 1.0 {
					// 已经过期，重置为 1.0 并更新 DB
					d.concurrencyScale = 1.0
					d.scaleExpiresAt = time.Time{}
					models.DB.Model(&models.SystemConfig{}).Where("id = 1").Updates(map[string]interface{}{
						"concurrency_scale": 1.0,
						"scale_expires_at":  nil,
					})
					log.Println("[Dispatcher] Stale throttle config from DB was reset to 1.0 (100%).")
				}
			}
		}
	}

	// 启动后台自愈守护心跳（每 1 秒触发一次广播检查，杜绝漏唤醒死锁）
	go d.startHeartbeat()

	Dispatcher = d
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
			// 检查是否过期
			if !d.scaleExpiresAt.IsZero() && now.After(d.scaleExpiresAt) {
				log.Printf("[Dispatcher] Heartbeat detected expired throttle (expired at %s). Restoring scale to 1.0.\n",
					d.scaleExpiresAt.Format("15:04:05"))
				d.concurrencyScale = 1.0
				d.scaleExpiresAt = time.Time{}
				if d.scaleTimer != nil {
					d.scaleTimer.Stop()
					d.scaleTimer = nil
				}
				d.syncToDBLocked()
			}
			// 定期唤醒等待者，自愈检查槽位与 Context 取消
			d.cond.Broadcast()
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
		"concurrency_scale": d.concurrencyScale,
		"scale_expires_at":  expiresPtr,
	})
}

// RestoreScale 恢复并发折扣系数为 100% (1.0)
func (d *ModelDispatcher) RestoreScale() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.scaleTimer != nil {
		d.scaleTimer.Stop()
		d.scaleTimer = nil
	}

	log.Printf("[Dispatcher] Concurrency throttle auto-restored to 100%% (Scale: 1.0, was: %.2f)\n", d.concurrencyScale)
	d.concurrencyScale = 1.0
	d.scaleExpiresAt = time.Time{}
	d.syncToDBLocked()

	// 核心：唤醒所有等待中的 Worker 协程
	d.cond.Broadcast()
}

// GetScaleAndExpiration 获取当前的并发折扣比率与过期时间
func (d *ModelDispatcher) GetScaleAndExpiration() (float64, time.Time) {
	if d == nil {
		return 1.0, time.Time{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.scaleExpiresAt.IsZero() && time.Now().After(d.scaleExpiresAt) {
		log.Printf("[Dispatcher] [GetScale] Throttle expired at %s, restoring scale to 1.0\n", d.scaleExpiresAt.Format("15:04:05"))
		d.concurrencyScale = 1.0
		d.scaleExpiresAt = time.Time{}
		if d.scaleTimer != nil {
			d.scaleTimer.Stop()
			d.scaleTimer = nil
		}
		d.syncToDBLocked()
		d.cond.Broadcast()
	}
	return d.concurrencyScale, d.scaleExpiresAt
}

// SetScale 设置并发折扣比率与持续时间
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

	// 停止之前的定时器
	if d.scaleTimer != nil {
		d.scaleTimer.Stop()
		d.scaleTimer = nil
	}

	d.concurrencyScale = scale
	if duration > 0 && scale != 1.0 {
		d.scaleExpiresAt = time.Now().Add(duration)
		log.Printf("[Dispatcher] Concurrency scale set to %.2f (duration: %v, will restore at %s)\n",
			scale, duration, d.scaleExpiresAt.Format("15:04:05"))
		// 注册到期定时主动唤醒
		d.scaleTimer = time.AfterFunc(duration, func() {
			d.RestoreScale()
		})
	} else {
		d.scaleExpiresAt = time.Time{}
		log.Printf("[Dispatcher] Concurrency scale set to %.2f (permanent / no expiration)\n", scale)
	}

	d.syncToDBLocked()

	// 唤醒所有正在等待槽位的 Goroutine
	d.cond.Broadcast()
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
// 返回 nil, "", nil 表示调度器未启用（降级回默认全局行为）。
func (d *ModelDispatcher) Acquire(ctx context.Context, backend string) (*ModelResource, string, error) {
	if d == nil || !d.enabled {
		return nil, "", nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for {
		// 1. 检查 Context 是否已提前取消
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}

		// 检查过期时间并获取当前内存系数
		if !d.scaleExpiresAt.IsZero() && time.Now().After(d.scaleExpiresAt) {
			log.Printf("[Dispatcher] [Acquire] Throttle expired at %s, auto-restoring scale to 1.0\n", d.scaleExpiresAt.Format("15:04:05"))
			d.concurrencyScale = 1.0
			d.scaleExpiresAt = time.Time{}
			if d.scaleTimer != nil {
				d.scaleTimer.Stop()
				d.scaleTimer = nil
			}
			d.syncToDBLocked()
			d.cond.Broadcast()
		}
		scale := d.concurrencyScale

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
			log.Printf("[Dispatcher] [Acquire] Server #%d allocated for backend %s (model: %s). Concurrency: %d/%d (Scale: %.2f, Raw: %d)\n",
				bestRes.Index, backend, bestRes.ModelName(backend), bestRes.Active, limit, scale, bestRes.Concurrent)
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
	limit := calculateLimit(res.Concurrent, d.concurrencyScale)
	log.Printf("[Dispatcher] [Release] Server #%d released for backend %s. Concurrency: %d/%d (Raw: %d)\n",
		res.Index, backend, res.Active, limit, res.Concurrent)
	d.cond.Broadcast()
}
