package dispatcher

import (
	"log"
	"time"

	"code-shield/models"
)

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

// getEffectiveScaleInfoLocked 计算当前生效流控（需在持有 d.mu 时调用）
func (d *ModelDispatcher) getEffectiveScaleInfoLocked(now time.Time) ThrottleInfo {
	cfg := d.workHours
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

			// 检查流控模式或生效比例是否发生突变
			if info.EffectiveScale != d.lastEffectiveScale || info.ThrottleMode != d.lastThrottleMode {
				log.Printf("[Dispatcher] Throttle status switched: mode=%s, scale=%.2f (was mode=%s, scale=%.2f)\n",
					info.ThrottleMode, info.EffectiveScale, d.lastThrottleMode, d.lastEffectiveScale)
				d.lastEffectiveScale = info.EffectiveScale
				d.lastThrottleMode = info.ThrottleMode
				d.syncToDBLocked()
				d.cond.Broadcast()
			} else {
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

	d.cond.Broadcast()
}

// SetScale 设置手动并发折扣比率与持续时间
func (d *ModelDispatcher) SetScale(scale float64, duration time.Duration) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if scale < 0 {
		scale = 0
	}
	if scale > 3.0 {
		scale = 3.0
	}

	if d.scaleTimer != nil {
		d.scaleTimer.Stop()
		d.scaleTimer = nil
	}

	if scale == 1.0 && duration == 0 {
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

	d.cond.Broadcast()
}

// ReloadResources 动态热重载算力资源池（零停机，就地复用在途任务指针并平滑继承活跃任务 Active 计数）
func (d *ModelDispatcher) ReloadResources(llmCfg models.LLMConfig) {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.workHours = models.AppConfig.AI.WorkHoursThrottle

	existingByKey := make(map[string]*ModelResource, len(d.resources))
	for _, r := range d.resources {
		existingByKey[r.ResourceKey()] = r
	}

	var newResources []*ModelResource
	for i, res := range llmCfg.Resources {
		concurrent := res.Concurrent
		if concurrent <= 0 {
			concurrent = 5
		}

		key := computeResourceConfigKey(res)
		var mr *ModelResource
		if existing, ok := existingByKey[key]; ok {
			mr = existing
			mr.Index = i
			mr.ID = res.ID
			mr.Driver = res.Driver
			mr.Model = res.Model
			mr.Concurrent = concurrent
			mr.Endpoints = res.Endpoints
			mr.OpenCode, mr.Claude, mr.Codex, mr.Agy, mr.Native = "", "", "", "", ""
		} else {
			mr = &ModelResource{
				Index:      i,
				ID:         res.ID,
				Driver:     res.Driver,
				Model:      res.Model,
				Concurrent: concurrent,
				Endpoints:  res.Endpoints,
				Active:     0,
			}
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

		newResources = append(newResources, mr)
	}

	d.resources = newResources
	if d.activeLeases == nil {
		d.activeLeases = make(map[string]*LLMSlotLease)
	}
	if len(d.resources) > 0 {
		d.enabled = true
	} else {
		d.enabled = false
	}

	log.Printf("[Dispatcher] [HotReload] Dynamically reloaded %d compute resources in memory", len(d.resources))
	d.cond.Broadcast()
}
