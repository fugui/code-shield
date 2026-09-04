package dispatcher

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"code-shield/models"
)

// ModelResource 代表单台 LLM 服务器的模型资源以及并发追踪
type ModelResource struct {
	Index         int
	ID            string
	Driver        string
	Model         string
	OpenCode      string
	Claude        string
	Codex         string
	Agy           string
	Native        string
	Concurrent    int
	Active        int // 当前正在运行的并发数
	CurrentWeight int // 运行时平滑加权轮询 (SWRR) 动态权重
	Endpoints     []models.ResourceEndpointConfig
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
	case "agy":
		return r.Agy
	case "native":
		return r.Native
	default:
		return ""
	}
}

// ResourceKey 返回当前算力节点的全局唯一业务标识键。
func (r *ModelResource) ResourceKey() string {
	if r == nil {
		return ""
	}
	if r.ID != "" {
		return "id:" + r.ID
	}
	if r.Driver != "" || r.Model != "" {
		return "driver:" + r.Driver + "/" + r.Model
	}
	return "models:" + r.OpenCode + "|" + r.Claude + "|" + r.Codex + "|" + r.Agy + "|" + r.Native
}

// computeResourceConfigKey 计算 ComputeResourceConfig 对应的业务标识键
func computeResourceConfigKey(res models.ComputeResourceConfig) string {
	if res.ID != "" {
		return "id:" + res.ID
	}
	if res.Driver != "" || res.Model != "" {
		return "driver:" + res.Driver + "/" + res.Model
	}
	var openCode, claude, codex, agy, native string
	switch res.Driver {
	case "opencode":
		openCode = res.Model
	case "claude":
		claude = res.Model
	case "codex":
		codex = res.Model
	case "agy":
		agy = res.Model
	case "native":
		native = res.Model
	default:
		native = res.Model
	}
	return "models:" + openCode + "|" + claude + "|" + codex + "|" + agy + "|" + native
}

// ThrottleInfo 封装当前流控全景状态信息
type ThrottleInfo struct {
	EffectiveScale  float64                        `json:"effective_scale"`   // 最终生效比例 (0.0 ~ 3.0)
	ThrottleMode    string                         `json:"throttle_mode"`     // "work_hours", "manual", "normal"
	ManualScale     float64                        `json:"manual_scale"`      // 手动设置的比例
	ScaleExpiresAt  *time.Time                     `json:"scale_expires_at"`  // 手动过期时间
	IsManual        bool                           `json:"is_manual"`         // 是否正处于手动覆盖中
	IsWorkHours     bool                           `json:"is_work_hours"`     // 当前是否命中工作时间
	WorkHoursConfig models.WorkHoursThrottleConfig `json:"work_hours_config"` // 工作时间自动限流配置
}

// ModelResourceStatus 封装单台 LLM 服务器的状态快照
type ModelResourceStatus struct {
	Index      int                             `json:"index"`
	ID         string                          `json:"id"`
	Driver     string                          `json:"driver"`
	Model      string                          `json:"model"`
	OpenCode   string                          `json:"opencode"`
	Claude     string                          `json:"claude"`
	Codex      string                          `json:"codex"`
	Agy        string                          `json:"agy"`
	Native     string                          `json:"native"`
	Concurrent int                             `json:"concurrent"`
	Active     int                             `json:"active"`
	Limit      int                             `json:"limit"`
	Endpoints  []models.ResourceEndpointConfig `json:"endpoints,omitempty"`
}

// ModelDispatcher 负责协调跨不同物理/逻辑 LLM 服务器的 AI 并发
type ModelDispatcher struct {
	mu                 sync.Mutex
	cond               *sync.Cond
	resources          []*ModelResource
	enabled            bool
	manualScale        float64                        // 管理员手动设置的并发折扣系数 [0.0, 3.0]
	scaleExpiresAt     time.Time                      // 手动设置失效时间
	scaleTimer         *time.Timer                    // 到期自动恢复定时器
	stopHeartbeat      chan struct{}                  // 停止心跳通道
	workHours          models.WorkHoursThrottleConfig // 内部纳管的工作时间限流配置
	unsupportedWarned  map[string]time.Time           // 记录各 backend 最近一次直通降级告警时间
	lastEffectiveScale float64                        // 记录上一时刻生效的 scale
	lastThrottleMode   string                         // 记录上一时刻模式
	activeLeases       map[string]*LLMSlotLease
	leaseSeq           uint64
}

// GlobalDispatcher 为多 LLM 并发分配器的全局单例
var GlobalDispatcher *ModelDispatcher

// GetGlobal 获取全局并发调度器单例
func GetGlobal() *ModelDispatcher {
	return GlobalDispatcher
}

// Close 优雅停止调度器后台心跳通道
func (d *ModelDispatcher) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	select {
	case <-d.stopHeartbeat:
	default:
		close(d.stopHeartbeat)
	}
}

// SetWorkHoursThrottle 线程安全地更新调度器工作时间限流配置
func (d *ModelDispatcher) SetWorkHoursThrottle(cfg models.WorkHoursThrottleConfig) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.workHours = cfg
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

// InitModelDispatcher 初始化全局并发调度器
func InitModelDispatcher() {
	if GlobalDispatcher != nil {
		GlobalDispatcher.Close()
	}

	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		activeLeases:  make(map[string]*LLMSlotLease),
		workHours:     models.AppConfig.AI.WorkHoursThrottle,
	}
	d.cond = sync.NewCond(&d.mu)

	if len(models.AppConfig.LLM.Resources) > 0 {
		for i, res := range models.AppConfig.LLM.Resources {
			concurrent := res.Concurrent
			if concurrent <= 0 {
				concurrent = 5
			}
			mr := &ModelResource{
				Index:      i,
				ID:         res.ID,
				Driver:     res.Driver,
				Model:      res.Model,
				Concurrent: concurrent,
				Endpoints:  res.Endpoints,
			}
			switch res.Driver {
			case "opencode":
				mr.OpenCode = res.Model
			case "claude":
				mr.Claude = res.Model
			case "codex":
				mr.Codex = res.Model
			case "agy":
				mr.Agy = res.Model
			case "native":
				mr.Native = res.Model
				if len(res.Endpoints) > 0 {
					epConcurrent := 0
					for _, ep := range res.Endpoints {
						if ep.Concurrent > 0 {
							epConcurrent += ep.Concurrent
						}
					}
					if epConcurrent > 0 {
						mr.Concurrent = epConcurrent
					}
				}
			default:
				mr.Native = res.Model
			}
			d.resources = append(d.resources, mr)
		}
	} else {
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
				Agy:        mc.Agy,
				Native:     mc.Native,
				Concurrent: concurrent,
			})
		}

		hasNativeResource := false
		for _, r := range d.resources {
			if r.Native != "" {
				hasNativeResource = true
				break
			}
		}

		nativeCfg := models.AppConfig.AI.Native
		if !hasNativeResource && (nativeCfg.BaseURL != "" || nativeCfg.Endpoint != "" || len(nativeCfg.Endpoints) > 0) {
			nativeConcurrent := 0
			if len(nativeCfg.Endpoints) > 0 {
				for _, ep := range nativeCfg.Endpoints {
					if ep.Concurrent > 0 {
						nativeConcurrent += ep.Concurrent
					} else {
						nativeConcurrent += 20
					}
				}
			}
			if nativeConcurrent <= 0 {
				nativeConcurrent = 20
			}
			defaultModel := nativeCfg.DefaultModel
			if defaultModel == "" {
				defaultModel = "glm-4-flash"
			}
			d.resources = append(d.resources, &ModelResource{
				Index:      len(d.resources),
				Native:     defaultModel,
				Concurrent: nativeConcurrent,
			})
		}
	}

	if len(d.resources) > 0 {
		d.enabled = true
		log.Printf("[Dispatcher] Initialized with %d custom LLM servers\n", len(d.resources))
		for _, r := range d.resources {
			log.Printf("  - Server #%d: opencode=%s, claude=%s, codex=%s, agy=%s, native=%s, concurrent=%d\n", r.Index, r.OpenCode, r.Claude, r.Codex, r.Agy, r.Native, r.Concurrent)
		}
	} else {
		d.enabled = false
		log.Println("[Dispatcher] No custom models configured, dispatcher is disabled (falling back to default backend settings)")
	}

	if models.DB != nil {
		var cfg models.SystemConfig
		if err := models.DB.First(&cfg, 1).Error; err == nil {
			if cfg.ConcurrencyScale > 0 || cfg.ScaleExpiresAt != nil {
				if cfg.ScaleExpiresAt != nil && cfg.ScaleExpiresAt.After(time.Now()) {
					dur := time.Until(*cfg.ScaleExpiresAt)
					d.manualScale = cfg.ConcurrencyScale
					d.scaleExpiresAt = *cfg.ScaleExpiresAt
					d.scaleTimer = time.AfterFunc(dur, func() {
						d.RestoreManualScale()
					})
					log.Printf("[Dispatcher] Restored active manual throttle from DB: scale=%.2f, expires in %v (at %s)\n",
						d.manualScale, dur.Round(time.Second), d.scaleExpiresAt.Format("15:04:05"))
				} else if cfg.ConcurrencyScale != 1.0 && cfg.ScaleExpiresAt != nil {
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

	initialInfo := d.getEffectiveScaleInfoLocked(time.Now())
	d.lastEffectiveScale = initialInfo.EffectiveScale
	d.lastThrottleMode = initialInfo.ThrottleMode
	log.Printf("[Dispatcher] Initial throttle state: mode=%s, effective_scale=%.2f\n",
		initialInfo.ThrottleMode, initialInfo.EffectiveScale)

	go d.startHeartbeat()

	GlobalDispatcher = d
}

// IsEnabled 返回当前调度器是否已启用
func (d *ModelDispatcher) IsEnabled() bool {
	if d == nil {
		return false
	}
	return d.enabled
}

// Acquire 动态请求一个支持指定后端类型的空闲 LLM 模型资源槽位
func (d *ModelDispatcher) Acquire(ctx context.Context, backend string) (*ModelResource, string, error) {
	return d.AcquireWithPreference(ctx, backend, "")
}

// AcquireWithPreference 动态请求支持指定后端类型的空闲 LLM 模型资源槽位。
func (d *ModelDispatcher) AcquireWithPreference(ctx context.Context, backend string, preferredModel string) (*ModelResource, string, error) {
	if d == nil || !d.enabled {
		return nil, "", nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	hasSupportedServer := false
	for _, res := range d.resources {
		if res.ModelName(backend) != "" {
			hasSupportedServer = true
			break
		}
	}
	if !hasSupportedServer {
		if last, ok := d.unsupportedWarned[backend]; !ok || time.Since(last) >= time.Minute {
			log.Printf("[Dispatcher] WARNING: no server resource supports backend %q; falling back to passthrough (concurrency throttling bypassed)", backend)
			if d.unsupportedWarned == nil {
				d.unsupportedWarned = make(map[string]time.Time)
			}
			d.unsupportedWarned[backend] = time.Now()
		}
		return nil, "", nil
	}

	waitDone := make(chan struct{})
	defer close(waitDone)
	go func() {
		select {
		case <-ctx.Done():
			d.mu.Lock()
			d.cond.Broadcast()
			d.mu.Unlock()
		case <-waitDone:
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}

		if !d.enabled {
			return nil, "", nil
		}

		info := d.getEffectiveScaleInfoLocked(time.Now())
		scale := info.EffectiveScale

		var bestRes *ModelResource
		bestLoadRatio := 999.0
		minActive := 999999
		hasSupportedServerInLoop := false

		if preferredModel != "" {
			for _, res := range d.resources {
				model := res.ModelName(backend)
				if model == "" {
					continue
				}
				hasSupportedServerInLoop = true
				if model == preferredModel || res.Model == preferredModel || res.ID == preferredModel {
					limit := calculateLimit(res.Concurrent, scale)
					if limit > 0 && res.Active < limit {
						bestRes = res
						break
					}
				}
			}
		}

		if bestRes == nil {
			for _, res := range d.resources {
				if res.ModelName(backend) == "" {
					continue
				}
				hasSupportedServerInLoop = true
				limit := calculateLimit(res.Concurrent, scale)
				if res.Active >= limit || limit <= 0 {
					continue
				}
				loadRatio := float64(res.Active) / float64(limit)
				if bestRes == nil || loadRatio < bestLoadRatio || (loadRatio == bestLoadRatio && res.Active < minActive) {
					bestRes = res
					bestLoadRatio = loadRatio
					minActive = res.Active
				}
			}
		}

		if !hasSupportedServerInLoop {
			return nil, "", nil
		}

		if bestRes != nil {
			bestRes.Active++
			limit := calculateLimit(bestRes.Concurrent, scale)
			log.Printf("[Dispatcher] [Acquire] Server #%d [%s] allocated for backend %s (model: %s). Concurrency: %d/%d (Scale: %.2f [%s], Raw: %d)\n",
				bestRes.Index, bestRes.ResourceKey(), backend, bestRes.ModelName(backend), bestRes.Active, limit, scale, info.ThrottleMode, bestRes.Concurrent)
			return bestRes, bestRes.ModelName(backend), nil
		}

		d.cond.Wait()
	}
}

// Release 释放指定的模型资源槽位，并通知其他等待中的任务。
func (d *ModelDispatcher) Release(res *ModelResource, backend string) {
	if d == nil || !d.enabled || res == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if res.Active > 0 {
		res.Active--
	}

	var cur *ModelResource
	for _, r := range d.resources {
		if r == res {
			cur = r
			break
		}
	}

	if cur == nil {
		targetKey := res.ResourceKey()
		for _, r := range d.resources {
			if r.ResourceKey() == targetKey {
				cur = r
				break
			}
		}
		if cur != nil && cur.Active > 0 {
			cur.Active--
		}
	}

	targetForLog := cur
	if targetForLog == nil {
		targetForLog = res
	}

	info := d.getEffectiveScaleInfoLocked(time.Now())
	limit := calculateLimit(targetForLog.Concurrent, info.EffectiveScale)
	log.Printf("[Dispatcher] [Release] Server #%d [%s] released for backend %s (model: %s). Concurrency: %d/%d (Scale: %.2f [%s], Raw: %d)\n",
		targetForLog.Index, targetForLog.ResourceKey(), backend, targetForLog.ModelName(backend), targetForLog.Active, limit, info.EffectiveScale, info.ThrottleMode, targetForLog.Concurrent)
	d.cond.Broadcast()
}

// PickBestCandidateResource 在多个候选 Resource ID / Driver 中采用 SWRR 与实时可用容量动态择优
func (d *ModelDispatcher) PickBestCandidateResource(candidateIDs []string) (string, string) {
	if d == nil || !d.enabled || len(candidateIDs) == 0 {
		return "", ""
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	info := d.getEffectiveScaleInfoLocked(time.Now())
	scale := info.EffectiveScale

	var candidates []*ModelResource
	seenKeys := make(map[string]bool)
	for _, id := range candidateIDs {
		for _, res := range d.resources {
			matches := (res.ID != "" && res.ID == id) ||
				(res.Driver != "" && res.Driver == id) ||
				(res.ModelName(id) != "")
			if !matches {
				continue
			}
			key := res.ResourceKey()
			if !seenKeys[key] {
				seenKeys[key] = true
				candidates = append(candidates, res)
			}
		}
	}

	if len(candidates) == 0 {
		return "", ""
	}

	type availableCandidate struct {
		res    *ModelResource
		weight int
	}
	var availList []availableCandidate
	var totalWeight int

	var fallbackRes *ModelResource
	bestLoadRatio := 999.0

	for _, res := range candidates {
		limit := calculateLimit(res.Concurrent, scale)
		loadRatio := float64(res.Active)
		if limit > 0 {
			loadRatio = float64(res.Active) / float64(limit)
		}

		if fallbackRes == nil || loadRatio < bestLoadRatio {
			fallbackRes = res
			bestLoadRatio = loadRatio
		}

		if limit > 0 && res.Active < limit {
			availSlots := limit - res.Active
			if availSlots < 1 {
				availSlots = 1
			}
			availList = append(availList, availableCandidate{
				res:    res,
				weight: availSlots,
			})
			totalWeight += availSlots
		}
	}

	if len(availList) == 0 {
		if fallbackRes != nil {
			return resolveResourceDriverAndModel(fallbackRes, candidateIDs)
		}
		return "", ""
	}

	if len(availList) == 1 {
		return resolveResourceDriverAndModel(availList[0].res, candidateIDs)
	}

	var selectedRes *ModelResource
	maxCurrentWeight := -999999999

	for i := range availList {
		ac := &availList[i]
		ac.res.CurrentWeight += ac.weight
		if ac.res.CurrentWeight > maxCurrentWeight {
			maxCurrentWeight = ac.res.CurrentWeight
			selectedRes = ac.res
		}
	}

	if selectedRes != nil {
		selectedRes.CurrentWeight -= totalWeight
		return resolveResourceDriverAndModel(selectedRes, candidateIDs)
	}

	return resolveResourceDriverAndModel(availList[0].res, candidateIDs)
}

// resolveResourceDriverAndModel 解析资源的有效 driver 和 model 名称
func resolveResourceDriverAndModel(res *ModelResource, candidateIDs []string) (string, string) {
	driver := res.Driver
	model := res.Model
	if driver == "" {
		for _, id := range candidateIDs {
			if res.ModelName(id) != "" {
				driver = id
				model = res.ModelName(id)
				break
			}
		}
	}
	if model == "" && driver != "" {
		model = res.ModelName(driver)
	}
	return driver, model
}

// GetScaleAndExpiration 获取当前的并发折扣比率与过期时间
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

// GetThrottleInfo 获取全景流控信息
func (d *ModelDispatcher) GetThrottleInfo() ThrottleInfo {
	if d == nil {
		return ThrottleInfo{EffectiveScale: 1.0, ThrottleMode: "normal"}
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.getEffectiveScaleInfoLocked(time.Now())
}

// GetResourcesStatus 返回所有 LLM 服务器当前的并发状态快照
func (d *ModelDispatcher) GetResourcesStatus() []ModelResourceStatus {
	if d == nil || !d.enabled {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	info := d.getEffectiveScaleInfoLocked(time.Now())
	var list []ModelResourceStatus
	for _, r := range d.resources {
		limit := calculateLimit(r.Concurrent, info.EffectiveScale)
		list = append(list, ModelResourceStatus{
			Index:      r.Index,
			ID:         r.ID,
			Driver:     r.Driver,
			Model:      r.Model,
			OpenCode:   r.OpenCode,
			Claude:     r.Claude,
			Codex:      r.Codex,
			Agy:        r.Agy,
			Native:     r.Native,
			Concurrent: r.Concurrent,
			Active:     r.Active,
			Limit:      limit,
			Endpoints:  r.Endpoints,
		})
	}
	return list
}
