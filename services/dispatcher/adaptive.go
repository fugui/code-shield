package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"

	"code-shield/models"
)

// canaryProbeEnvelope 用于兼容探针接口的多种返回结构（根对象或包含 data 数组）
type canaryProbeEnvelope struct {
	Data            json.RawMessage `json:"data"`
	ActiveRequests  *int            `json:"active_requests"`
	WaitingRequests *int            `json:"waiting_requests"`
}

type canaryMetricsData struct {
	ActiveRequests  int `json:"active_requests"`
	WaitingRequests int `json:"waiting_requests"`
}

// getAdaptiveStatusSnapshotLocked 获取自适应流控实时运行状态（需在持有 d.mu 时调用）
func (d *ModelDispatcher) getAdaptiveStatusSnapshotLocked() *AdaptiveStatusSnapshot {
	if !d.adaptiveCfg.Enabled {
		return nil
	}
	isFallback := d.adaptiveConsecutiveFailCount >= 3
	state := d.adaptiveState
	if isFallback {
		state = "fallback"
	}
	if state == "" {
		state = "optimal"
	}
	return &AdaptiveStatusSnapshot{
		ActiveRequests:   d.adaptiveLastActive,
		WaitingRequests:  d.adaptiveLastWaiting,
		CurrentScale:     d.adaptiveScale,
		State:            state,
		Message:          d.adaptiveMessage,
		LastProbeAt:      d.adaptiveLastProbeAt,
		ConsecutiveFails: d.adaptiveConsecutiveFailCount,
		IsFallback:       isFallback,
	}
}

// SetAdaptiveThrottle 线程安全地更新自适应流控配置并按需启动或停止探针协程
func (d *ModelDispatcher) SetAdaptiveThrottle(cfg models.AdaptiveThrottlingConfig) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.adaptiveCfg = cfg
	if d.adaptiveScale <= 0 {
		d.adaptiveScale = 1.0
	}
	enabled := cfg.Enabled && cfg.ProbeURL != ""
	shouldStart := enabled && !d.probeRunning
	shouldStop := !enabled && d.probeRunning

	if shouldStop && d.stopProbe != nil {
		select {
		case <-d.stopProbe:
		default:
			close(d.stopProbe)
		}
		d.stopProbe = make(chan struct{})
		d.probeRunning = false
	}
	d.mu.Unlock()

	if shouldStart {
		go d.startAdaptiveProbeWorker()
	}

	if d.cond != nil {
		d.cond.Broadcast()
	}
}

// adjustAIMDLocked 非对称加法递增/乘法递减拥塞控制算法实现（需在持有 d.mu 时调用）
// 判定规则严格依据架构设计规范：
// 1. 🚨 waiting > 3: 乘法断崖下调 scale = max(minScale, scale * 0.4)，抢在 429 前削峰
// 2. ⚠️ 1 <= waiting <= 3: 温和退避 scale = max(minScale, scale * 0.8)
// 3. 🟢 waiting == 0 && active >= 20: 黄金平衡，保持当前 Scale 不变
// 4. 🔵 waiting == 0 && active < 15: 闲置增压，连续 2 个周期稳定后执行 scale = min(maxScale, scale + 0.2)
func (d *ModelDispatcher) adjustAIMDLocked(active, waiting int) (float64, string, string) {
	minScale := d.adaptiveCfg.GetMinScale()
	maxScale := d.adaptiveCfg.GetMaxScale()
	criticalWait := d.adaptiveCfg.GetWaitingCriticalThreshold()
	activeLow := d.adaptiveCfg.GetActiveLowThreshold()
	activeHigh := d.adaptiveCfg.GetActiveHighThreshold()

	scale := d.adaptiveScale
	if scale <= 0 {
		scale = 1.0
	}

	var state, msg string

	if waiting > criticalWait {
		// 🚨 严重过载区: waiting > 3
		scale = math.Max(minScale, scale*0.4)
		d.adaptiveConsecutiveIdleCount = 0
		state = "overload"
		msg = fmt.Sprintf("🚨 严重过载：哨兵节点排队超标（活跃 %d，排队 %d），已断崖避让至 %.0f%% 并发速度，抢在 429 前削峰",
			active, waiting, math.Round(scale*100))
	} else if waiting >= 1 {
		// ⚠️ 轻度预警区: 1 <= waiting <= 3
		scale = math.Max(minScale, scale*0.8)
		d.adaptiveConsecutiveIdleCount = 0
		state = "warning"
		msg = fmt.Sprintf("⚠️ 队列预警：哨兵节点出现轻度积压（活跃 %d，排队 %d），已温和退让至 %.0f%% 并发速度防微杜渐",
			active, waiting, math.Round(scale*100))
	} else if active >= activeHigh {
		// 🟢 黄金平衡区: waiting == 0 且 active >= 20
		d.adaptiveConsecutiveIdleCount = 0
		state = "optimal"
		msg = fmt.Sprintf("🟢 黄金平衡：哨兵节点（活跃 %d，排队 %d），推理集群处于最佳利用水位，保持 %.0f%% 稳态吞吐",
			active, waiting, math.Round(scale*100))
	} else if active < activeLow {
		// 🔵 闲置增压区: waiting == 0 且 active < 15 (需连续 2 周期稳定)
		d.adaptiveConsecutiveIdleCount++
		if d.adaptiveConsecutiveIdleCount >= 2 {
			scale = math.Min(maxScale, scale+0.2)
			d.adaptiveConsecutiveIdleCount = 0
			state = "boost"
			msg = fmt.Sprintf("🔵 算力充沛：哨兵节点持续闲置（活跃 %d，排队 %d），加法增压调速至 %.0f%% 消化扫描任务",
				active, waiting, math.Round(scale*100))
		} else {
			state = "boost"
			msg = fmt.Sprintf("🔵 算力充沛：哨兵节点（活跃 %d，排队 %d），正进行第 1 周期平稳观测，准备增压",
				active, waiting)
		}
	} else {
		// 活跃度在 [activeLow, activeHigh) 之间且零排队：保持平稳
		d.adaptiveConsecutiveIdleCount = 0
		state = "optimal"
		msg = fmt.Sprintf("🟢 黄金平衡：哨兵节点（活跃 %d，排队 %d），保持 %.0f%% 稳态吞吐",
			active, waiting, math.Round(scale*100))
	}

	scale = math.Round(scale*100) / 100
	if scale < minScale {
		scale = minScale
	}
	if scale > maxScale {
		scale = maxScale
	}

	d.adaptiveScale = scale
	d.adaptiveState = state
	d.adaptiveMessage = msg
	d.adaptiveLastActive = active
	d.adaptiveLastWaiting = waiting
	d.adaptiveLastProbeAt = time.Now()

	return scale, state, msg
}

// handleProbeFailureLocked 处理探针探测失败与容灾降级（需在持有 d.mu 时调用）
func (d *ModelDispatcher) handleProbeFailureLocked(err error) {
	d.adaptiveConsecutiveFailCount++
	d.adaptiveConsecutiveIdleCount = 0
	d.adaptiveLastProbeAt = time.Now()

	if d.adaptiveConsecutiveFailCount >= 3 {
		d.adaptiveState = "fallback"
		d.adaptiveMessage = fmt.Sprintf("⚠️ 探针异常：哨兵节点连续 %d 次探测失败，已平滑回退至静态保底模式 (%v)",
			d.adaptiveConsecutiveFailCount, err)
		log.Printf("[Dispatcher] Adaptive probe continuous failure (count=%d): %v. Fallback to static mode.\n",
			d.adaptiveConsecutiveFailCount, err)
	} else {
		d.adaptiveMessage = fmt.Sprintf("⚠️ 探针抖动：哨兵节点探测失败（第 %d/3 次），维持当前流控状态",
			d.adaptiveConsecutiveFailCount)
		log.Printf("[Dispatcher] Adaptive probe transient error (attempt %d/3): %v\n",
			d.adaptiveConsecutiveFailCount, err)
	}
}

// fetchProbeMetrics 发起短超时 HTTP 请求拉取 Canary 探针指标
func fetchProbeMetrics(ctx context.Context, probeURL string, timeoutMs int) (int, int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, probeURL, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("create probe request failed: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Code-Shield-Canary-Probe/1.0")

	client := &http.Client{
		Timeout: time.Duration(timeoutMs) * time.Millisecond,
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("probe request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("probe HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return 0, 0, fmt.Errorf("read probe response body failed: %w", err)
	}

	var env canaryProbeEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, 0, fmt.Errorf("unmarshal probe response failed: %w", err)
	}

	// 1. 优先检查顶层 active_requests / waiting_requests
	if env.ActiveRequests != nil && env.WaitingRequests != nil {
		return *env.ActiveRequests, *env.WaitingRequests, nil
	}

	// 2. 检查 data 数组或对象
	if len(env.Data) > 0 {
		var list []canaryMetricsData
		if err := json.Unmarshal(env.Data, &list); err == nil && len(list) > 0 {
			return list[0].ActiveRequests, list[0].WaitingRequests, nil
		}

		var single canaryMetricsData
		if err := json.Unmarshal(env.Data, &single); err == nil {
			return single.ActiveRequests, single.WaitingRequests, nil
		}
	}

	return 0, 0, fmt.Errorf("no active/waiting metrics found in probe payload: %s", string(body))
}

// probeOnce 执行单次探针轮询与拥塞仲裁
func (d *ModelDispatcher) probeOnce() {
	d.mu.Lock()
	if !d.adaptiveCfg.Enabled || d.adaptiveCfg.ProbeURL == "" {
		d.mu.Unlock()
		return
	}
	probeURL := d.adaptiveCfg.ProbeURL
	timeoutMs := d.adaptiveCfg.GetTimeoutMs()
	oldScale := d.adaptiveScale
	d.mu.Unlock()

	active, waiting, err := fetchProbeMetrics(context.Background(), probeURL, timeoutMs)

	d.mu.Lock()
	defer d.mu.Unlock()

	// 检查在网络请求期间配置是否被禁用
	if !d.adaptiveCfg.Enabled {
		return
	}

	if err != nil {
		d.handleProbeFailureLocked(err)
	} else {
		wasFallback := d.adaptiveConsecutiveFailCount >= 3
		d.adaptiveConsecutiveFailCount = 0
		if wasFallback {
			log.Printf("[Dispatcher] Adaptive probe recovered from failures. Resuming adaptive control.\n")
		}
		newScale, state, msg := d.adjustAIMDLocked(active, waiting)
		if newScale != oldScale {
			log.Printf("[Dispatcher] Adaptive AIMD adjusted scale: %.2f -> %.2f (state=%s: %s)\n",
				oldScale, newScale, state, msg)
		}
	}

	info := d.getEffectiveScaleInfoLocked(time.Now())
	if info.EffectiveScale != d.lastEffectiveScale || info.ThrottleMode != d.lastThrottleMode {
		log.Printf("[Dispatcher] Throttle status switched: mode=%s, scale=%.2f (was mode=%s, scale=%.2f)\n",
			info.ThrottleMode, info.EffectiveScale, d.lastThrottleMode, d.lastEffectiveScale)
		d.lastEffectiveScale = info.EffectiveScale
		d.lastThrottleMode = info.ThrottleMode
		d.syncToDBLocked()
		d.cond.Broadcast()
	} else if d.adaptiveScale > oldScale {
		// 增压或扩容时，即刻唤醒等待槽位中的业务协程
		d.cond.Broadcast()
	}
}

// startAdaptiveProbeWorker 启动后台探针协程 (Background Probe Worker)
func (d *ModelDispatcher) startAdaptiveProbeWorker() {
	d.mu.Lock()
	if d.probeRunning {
		d.mu.Unlock()
		return
	}
	d.probeRunning = true
	stopCh := d.stopProbe
	interval := d.adaptiveCfg.GetIntervalSeconds()
	d.mu.Unlock()

	log.Printf("[Dispatcher] Background Canary Probe Worker started (interval=%ds)\n", interval)

	// 启动后立即探测一次
	d.probeOnce()

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			d.mu.Lock()
			d.probeRunning = false
			d.mu.Unlock()
			log.Println("[Dispatcher] Background Canary Probe Worker stopped")
			return
		case <-ticker.C:
			d.probeOnce()
		}
	}
}
