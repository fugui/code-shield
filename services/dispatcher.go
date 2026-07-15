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
	concurrencyScale float64   // 内存中的并发折扣系数
	scaleExpiresAt   time.Time // 折扣失效时间
}

// Dispatcher 为多 LLM 并发分配器的全局单例
var Dispatcher *ModelDispatcher

// InitModelDispatcher 初始化全局并发调度器
func InitModelDispatcher() {
	d := &ModelDispatcher{}
	d.cond = sync.NewCond(&d.mu)
	d.concurrencyScale = 1.0 // 默认折算系数为 1.0

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

	Dispatcher = d
}

// GetScaleAndExpiration 获取当前的并发折扣比率与过期时间
func (d *ModelDispatcher) GetScaleAndExpiration() (float64, time.Time) {
	if d == nil {
		return 1.0, time.Time{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.scaleExpiresAt.IsZero() && time.Now().After(d.scaleExpiresAt) {
		// 已过期，重置为 1.0
		d.concurrencyScale = 1.0
		d.scaleExpiresAt = time.Time{}
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

	d.concurrencyScale = scale
	if duration > 0 {
		d.scaleExpiresAt = time.Now().Add(duration)
	} else {
		d.scaleExpiresAt = time.Time{}
	}

	// 唤醒所有正在等待槽位的 Goroutine，因为并发限额改变了
	d.cond.Broadcast()
}

// Acquire 动态请求一个支持指定后端类型的空闲 LLM 模型资源槽位。
// 如果目前所有槽位已满，则阻塞等待，直到有槽位空出或 Context 被取消。
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
			d.concurrencyScale = 1.0
			d.scaleExpiresAt = time.Time{}
		}
		scale := d.concurrencyScale

		// 2. 寻找有可用配额且支持当前后端的 LLM 配置
		var bestRes *ModelResource
		for _, res := range d.resources {
			modelName := res.ModelName(backend)
			if modelName != "" {
				// 动态计算实际并发限额
				limit := int(math.Round(float64(res.Concurrent) * scale))
				if scale > 0 && limit < 1 {
					limit = 1
				}
				if res.Active < limit {
					bestRes = res
					break
				}
			}
		}

		if bestRes != nil {
			bestRes.Active++
			// 计算实际限制用于日志输出
			limit := int(math.Round(float64(bestRes.Concurrent) * scale))
			if scale > 0 && limit < 1 {
				limit = 1
			}
			log.Printf("[Dispatcher] [Acquire] Server #%d allocated for backend %s (model: %s). Concurrency: %d/%d (Scale: %.2f, Raw limit: %d)\n",
				bestRes.Index, backend, bestRes.ModelName(backend), bestRes.Active, limit, scale, bestRes.Concurrent)
			return bestRes, bestRes.ModelName(backend), nil
		}

		// 3. 阻塞等待空闲。启动守护协程响应 Context 取消以提前唤醒 Wait
		waitDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				d.mu.Lock()
				d.cond.Broadcast() // 唤醒本 Wait，使其能够在唤醒后感知到 ctx.Err() 并退出
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

	res.Active--
	log.Printf("[Dispatcher] [Release] Server #%d released for backend %s. Concurrency: %d/%d\n",
		res.Index, backend, res.Active, res.Concurrent)
	d.cond.Broadcast()
}
