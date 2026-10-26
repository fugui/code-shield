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
	if !d.adaptiveInitialized {
		d.adaptiveInitialized = true
		if d.adaptiveScale <= 0 {
			d.adaptiveScale = 1.0
		}
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
// 判定规则严格依据架构设计规范与低水位微排队容忍策略：
// 1. 🚨 waiting > criticalWait (默认 3): 乘法断崖下调 scale = max(minScale, scale * 0.4)，抢在 429 前削峰
// 2. ⚠️ 确切积压或高位饱和排队 (waiting >= 2 或 waiting == 1 && active >= activeHigh): 温和退避 scale = max(minScale, scale * 0.8)
// 3. 🟡 瞬态入队微排队观望 (waiting == 1 && active < activeHigh): 活跃度未饱和，单个排队通常为引擎 Continuous Batching 调度切片毫秒级时延，不进行惩罚性退缩，维持当前 Scale 稳态观望
// 4. 🟢 waiting == 0 && active >= activeHigh: 高位饱和稳态，活跃度达到或超过上限，严禁盲目加压/回弹，维持当前并发稳态
// 5. 🔄 waiting == 0 && active < activeHigh && scale < 1.0: 安全基准自愈，确认有承载余量，连续 3 个周期稳定后按 +10% 平稳回弹至 1.0
// 6. 🔵 waiting == 0 && active < activeLow && scale >= 1.0: 闲置超频增压，连续 3 个周期稳定后执行 scale = min(maxScale, scale + 0.10)
// 7. 🟢 waiting == 0 && activeLow <= active < activeHigh && scale >= 1.0: 黄金平衡稳态，保持当前 Scale 不变
func (d *ModelDispatcher) adjustAIMDLocked(active, waiting int) (float64, string, string) {
	minScale := d.adaptiveCfg.GetMinScale()
	maxScale := d.adaptiveCfg.GetMaxScale()
	criticalWait := d.adaptiveCfg.GetWaitingCriticalThreshold()
	activeLow := d.adaptiveCfg.GetActiveLowThreshold()
	activeHigh := d.adaptiveCfg.GetActiveHighThreshold()

	if !d.adaptiveInitialized {
		d.adaptiveInitialized = true
		if d.adaptiveScale <= 0 {
			d.adaptiveScale = 1.0
		}
	}
	scale := d.adaptiveScale

	var state, msg string

	if waiting > criticalWait {
		// 🚨 严重过载区: waiting > 3
		scale = math.Max(minScale, scale*0.4)
		scale = math.Round(scale*100) / 100
		if minScale == 0 && scale <= 0.08 {
			scale = 0
		}
		d.adaptiveConsecutiveIdleCount = 0
		d.adaptiveConsecutiveRecoveryCount = 0
		state = "overload"
		if scale <= 0 {
			msg = fmt.Sprintf("🚨 严重过载：哨兵节点排队超标（活跃 %d，排队 %d），已断崖避让至 0%% 并发速度（扫描完全暂停），全力让路业务流量",
				active, waiting)
		} else {
			msg = fmt.Sprintf("🚨 严重过载：哨兵节点排队超标（活跃 %d，排队 %d），已断崖避让至 %.0f%% 并发速度，抢在 429 前削峰",
				active, waiting, math.Round(scale*100))
		}
	} else if waiting >= 2 || (waiting == 1 && active >= activeHigh) {
		// ⚠️ 确切积压与高位饱和排队预警区: 2 <= waiting <= 3 或 (waiting == 1 且 active >= activeHigh)
		scale = math.Max(minScale, scale*0.8)
		scale = math.Round(scale*100) / 100
		if minScale == 0 && scale <= 0.08 {
			scale = 0
		}
		d.adaptiveConsecutiveIdleCount = 0
		d.adaptiveConsecutiveRecoveryCount = 0
		state = "warning"
		if scale <= 0 {
			msg = fmt.Sprintf("⚠️ 队列预警：哨兵节点出现积压（活跃 %d，排队 %d），已温和退让至 0%% 并发速度（扫描完全暂停），全力让路业务流量",
				active, waiting)
		} else if waiting == 1 {
			msg = fmt.Sprintf("⚠️ 高位饱和排队：哨兵节点活跃度偏高且出现排队（活跃 %d >= %d，排队 1），已温和退让至 %.0f%% 并发速度避让",
				active, activeHigh, math.Round(scale*100))
		} else {
			msg = fmt.Sprintf("⚠️ 队列积压预警：哨兵节点出现排队积压（活跃 %d，排队 %d），已温和退让至 %.0f%% 并发速度防微杜渐",
				active, waiting, math.Round(scale*100))
		}
	} else if waiting == 1 && active < activeHigh {
		// 🟡 瞬态调度微排队观望区: waiting == 1 且 active < activeHigh
		// 活跃负载未达饱和高位，单个排队通常为推理引擎 Continuous Batching 调度切片毫秒级时延，不进行惩罚性退缩，维持当前 Scale 稳态观望；
		// 同时重置自愈与超频计数，避免在微排队时继续盲目扩容。
		d.adaptiveConsecutiveIdleCount = 0
		d.adaptiveConsecutiveRecoveryCount = 0
		state = "optimal"
		msg = fmt.Sprintf("🟢 瞬态微排队：哨兵节点活跃负载适中（活跃 %d < %d，排队 1），判定为调度切片正常排队，维持当前 %.0f%% 稳态观望暂不退让",
			active, activeHigh, math.Round(scale*100))
	} else if active >= activeHigh {
		// 🟢 高位饱和稳态区: waiting == 0 且 active >= activeHigh
		// 哨兵节点活跃负载已达到或超过上限阈值（如 active >= 20），算力处于高利用率饱和状态。
		// 无论此时处于限流状态 (scale < 1.0) 还是标称/超频状态 (scale >= 1.0)，均应保持当前并发稳态，
		// 严禁继续加压或激进回弹，防止在临界点重蹈覆辙引发排队。
		d.adaptiveConsecutiveIdleCount = 0
		d.adaptiveConsecutiveRecoveryCount = 0
		state = "optimal"
		if scale < 1.0 {
			msg = fmt.Sprintf("🟢 水位饱和：哨兵节点零排队但活跃度偏高（活跃 %d >= %d，排队 0），维持当前 %.0f%% 稳态观望暂不回弹",
				active, activeHigh, math.Round(scale*100))
		} else {
			msg = fmt.Sprintf("🟢 黄金平衡：哨兵节点（活跃 %d，排队 0），推理集群处于最佳利用水位，保持 %.0f%% 稳态吞吐",
				active, math.Round(scale*100))
		}
	} else if scale < 1.0 {
		// 🔄 安全基准自愈恢复区: waiting == 0 且 active < activeHigh 且 scale < 1.0
		// 确认集群无排队且活跃负载低于高阈值（active < activeHigh，存在承载余量），持续观测 3 个周期确认稳定后，每轮平稳回弹 +10%
		d.adaptiveConsecutiveIdleCount = 0
		d.adaptiveConsecutiveRecoveryCount++
		if d.adaptiveConsecutiveRecoveryCount >= 3 {
			scale = math.Min(1.0, scale+0.10)
			d.adaptiveConsecutiveRecoveryCount = 0
			if scale >= 1.0 {
				state = "optimal"
				msg = fmt.Sprintf("🟢 排队消退且负载适中：哨兵节点连续 3 周期平稳（活跃 %d < %d，排队 0），已自愈回归 100%% 标称基准速度", active, activeHigh)
			} else {
				state = "boost"
				msg = fmt.Sprintf("🔄 排队消退且负载适中：哨兵节点连续 3 周期平稳（活跃 %d < %d，排队 0），稳步回弹至 %.0f%% 恢复基准", active, activeHigh, math.Round(scale*100))
			}
		} else {
			state = "boost"
			msg = fmt.Sprintf("🔄 排队消退且负载适中：哨兵节点（活跃 %d < %d，排队 0），正进行第 %d/3 周期平稳观测，准备回弹", active, activeHigh, d.adaptiveConsecutiveRecoveryCount)
		}
	} else if active < activeLow {
		// 🔵 闲置超频增压区: waiting == 0 且 scale >= 1.0 且 active < 15 (需连续 3 周期稳定，每轮步进 +10%)
		d.adaptiveConsecutiveRecoveryCount = 0
		d.adaptiveConsecutiveIdleCount++
		if d.adaptiveConsecutiveIdleCount >= 3 {
			scale = math.Min(maxScale, scale+0.10)
			d.adaptiveConsecutiveIdleCount = 0
			state = "boost"
			msg = fmt.Sprintf("🔵 算力充沛：哨兵节点持续 3 周期闲置（活跃 %d < %d，排队 0），加法增压调速至 %.0f%% 消化扫描任务",
				active, activeLow, math.Round(scale*100))
		} else {
			state = "boost"
			msg = fmt.Sprintf("🔵 算力充沛：哨兵节点闲置（活跃 %d < %d，排队 0），正进行第 %d/3 周期平稳观测，准备增压",
				active, activeLow, d.adaptiveConsecutiveIdleCount)
		}
	} else {
		// 🟢 活跃度在 [activeLow, activeHigh) 之间且 scale >= 1.0 且零排队：保持平稳
		d.adaptiveConsecutiveIdleCount = 0
		d.adaptiveConsecutiveRecoveryCount = 0
		state = "optimal"
		msg = fmt.Sprintf("🟢 黄金平衡：哨兵节点（活跃 %d，排队 0），保持 %.0f%% 稳态吞吐",
			active, math.Round(scale*100))
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
	d.adaptiveConsecutiveRecoveryCount = 0
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
		// 仅在流控模式发生跨模态切换或非自适应模式调速时记录，避免与前面的 Adaptive AIMD adjusted scale 重复刷屏
		if info.ThrottleMode != d.lastThrottleMode || (info.ThrottleMode != "adaptive" && d.lastThrottleMode != "adaptive") {
			log.Printf("[Dispatcher] Throttle status switched: mode=%s, scale=%.2f (was mode=%s, scale=%.2f)\n",
				info.ThrottleMode, info.EffectiveScale, d.lastThrottleMode, d.lastEffectiveScale)
		}
		d.lastEffectiveScale = info.EffectiveScale
		d.lastThrottleMode = info.ThrottleMode
		d.syncToDBLocked()
		d.cond.Broadcast()
		TriggerSlotReleasedHook()
	} else if d.adaptiveScale > oldScale {
		// 增压或扩容时，即刻唤醒等待槽位中的业务协程与等待背压的 Worker
		d.cond.Broadcast()
		TriggerSlotReleasedHook()
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
