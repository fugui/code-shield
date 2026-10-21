package dispatcher

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"code-shield/models"
)

func setupAdaptiveTestDispatcher(rawConcurrent int) *ModelDispatcher {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		stopProbe:     make(chan struct{}),
		enabled:       true,
		adaptiveScale: 1.0,
		adaptiveState: "optimal",
		adaptiveCfg: models.AdaptiveThrottlingConfig{
			Enabled:                  true,
			IntervalSeconds:          1,
			TimeoutMs:                500,
			ActiveLowThreshold:       15,
			ActiveHighThreshold:      20,
			WaitingCriticalThreshold: 3,
			MinScale:                 0.20,
			MaxScale:                 2.50,
		},
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{
			Index:      0,
			OpenCode:   "test-opencode",
			Claude:     "test-claude",
			Codex:      "test-codex",
			Agy:        "test-agy",
			Native:     "test-native",
			Concurrent: rawConcurrent,
			Active:     0,
		},
	}
	GlobalDispatcher = d
	return d
}

// TestAdaptive_AIMD_Algorithm 验证四态水力决策矩阵与边界约束
func TestAdaptive_AIMD_Algorithm(t *testing.T) {
	d := setupAdaptiveTestDispatcher(10)
	defer d.Close()

	d.mu.Lock()
	defer d.mu.Unlock()

	// 初始状态 scale = 1.0
	if d.adaptiveScale != 1.0 {
		t.Fatalf("expected initial scale 1.0, got %f", d.adaptiveScale)
	}

	// 1. 🚨 严重过载测试: waiting_requests = 4 (> 3) -> scale = max(0.20, 1.0 * 0.4) = 0.40
	scale, state, _ := d.adjustAIMDLocked(25, 4)
	if state != "overload" {
		t.Fatalf("expected state 'overload', got %s", state)
	}
	if math.Abs(scale-0.40) > 0.001 {
		t.Fatalf("expected scale 0.40 after fast cut, got %f", scale)
	}

	// 再次触发严重过载 -> scale = max(0.20, 0.40 * 0.4) = max(0.20, 0.16) = 0.20 (触底保底)
	scale, state, _ = d.adjustAIMDLocked(25, 5)
	if math.Abs(scale-0.20) > 0.001 {
		t.Fatalf("expected scale clamped to minScale 0.20, got %f", scale)
	}

	// 2. ⚠️ 轻度预警测试: 1 <= waiting <= 3 -> scale = max(0.20, scale * 0.8)
	// 先将 scale 设为 1.0
	d.adaptiveScale = 1.0
	scale, state, _ = d.adjustAIMDLocked(22, 2)
	if state != "warning" {
		t.Fatalf("expected state 'warning', got %s", state)
	}
	if math.Abs(scale-0.80) > 0.001 {
		t.Fatalf("expected scale 0.80 after soft backoff, got %f", scale)
	}

	// 3. 🟢 高位饱和稳态测试: scale = 0.80 < 1.0, waiting == 0, 但 active = 25 >= activeHigh (20)
	// 虽然零排队，但活跃度处于高位，严禁盲目加压回弹，系统应保持当前并发稳态观望，重置计数器
	scale, state, _ = d.adjustAIMDLocked(25, 0)
	if state != "optimal" {
		t.Fatalf("expected state 'optimal' for saturated active hold, got %s", state)
	}
	if math.Abs(scale-0.80) > 0.001 {
		t.Fatalf("expected scale to hold at 0.80 when active >= activeHigh, got %f", scale)
	}
	if d.adaptiveConsecutiveRecoveryCount != 0 {
		t.Fatalf("expected recovery count 0 during saturation hold, got %d", d.adaptiveConsecutiveRecoveryCount)
	}

	// 4. 🔄 安全基准自愈恢复测试: scale = 0.80 < 1.0, waiting == 0, 且 active = 16 < activeHigh (20)
	// 集群真正具备承载余量，持续观测 3 周期确认稳定后平稳回弹
	// 周期 1: 适度负载零排队观测 (count=1, scale 仍为 0.80)
	scale, state, _ = d.adjustAIMDLocked(16, 0)
	if state != "boost" {
		t.Fatalf("expected state 'boost', got %s", state)
	}
	if math.Abs(scale-0.80) > 0.001 {
		t.Fatalf("expected scale 0.80 in recovery cycle 1, got %f", scale)
	}
	if d.adaptiveConsecutiveRecoveryCount != 1 {
		t.Fatalf("expected recovery count 1, got %d", d.adaptiveConsecutiveRecoveryCount)
	}

	// 模拟中途活跃度突发冲高 (active = 22 >= 20)，验证打断防抖并重置计数器
	scale, state, _ = d.adjustAIMDLocked(22, 0)
	if state != "optimal" || math.Abs(scale-0.80) > 0.001 || d.adaptiveConsecutiveRecoveryCount != 0 {
		t.Fatalf("expected saturation to interrupt recovery and reset count, got count=%d", d.adaptiveConsecutiveRecoveryCount)
	}

	// 重新开始平稳观测 3 周期
	d.adjustAIMDLocked(16, 0) // count=1
	d.adjustAIMDLocked(16, 0) // count=2
	// 满 3 周期稳定确认，回弹 +0.10 -> scale = 0.80 + 0.10 = 0.90
	scale, state, _ = d.adjustAIMDLocked(16, 0)
	if math.Abs(scale-0.90) > 0.001 {
		t.Fatalf("expected scale 0.90 after 3-cycle recovery step, got %f", scale)
	}
	if d.adaptiveConsecutiveRecoveryCount != 0 {
		t.Fatalf("expected recovery count reset to 0, got %d", d.adaptiveConsecutiveRecoveryCount)
	}

	// 再经过 3 周期回弹至 1.00 标称基准 (0.90 + 0.10 = 1.00)
	d.adjustAIMDLocked(16, 0)                   // cycle 4 (count=1)
	d.adjustAIMDLocked(16, 0)                   // cycle 5 (count=2)
	scale, state, _ = d.adjustAIMDLocked(16, 0) // cycle 6 (count=3 -> 1.00, state=optimal)
	if state != "optimal" {
		t.Fatalf("expected state 'optimal' after reaching 1.0 baseline, got %s", state)
	}
	if math.Abs(scale-1.00) > 0.001 {
		t.Fatalf("expected scale to recover to 1.00, got %f", scale)
	}

	// 5. 🟢 黄金平衡稳态测试: waiting == 0 && scale >= 1.0 && active >= 20 -> 保持 1.00 不盲目超频
	scale, state, _ = d.adjustAIMDLocked(25, 0)
	if state != "optimal" {
		t.Fatalf("expected state 'optimal', got %s", state)
	}
	if math.Abs(scale-1.00) > 0.001 {
		t.Fatalf("expected scale to remain 1.00 in sweet spot, got %f", scale)
	}

	// 6. 🔵 闲置超频增压测试: waiting == 0 && active < 15
	// 第 1 周期: 满足闲置，但尚未连续三周期，scale 应保持不变
	scale, state, _ = d.adjustAIMDLocked(10, 0)
	if state != "boost" {
		t.Fatalf("expected state 'boost', got %s", state)
	}
	if math.Abs(scale-1.00) > 0.001 {
		t.Fatalf("expected scale to remain 1.00 in first idle cycle, got %f", scale)
	}
	if d.adaptiveConsecutiveIdleCount != 1 {
		t.Fatalf("expected consecutive idle count 1, got %d", d.adaptiveConsecutiveIdleCount)
	}

	// 第 2 周期: 满足闲置，但尚未连续三周期，scale 应保持不变
	scale, state, _ = d.adjustAIMDLocked(9, 0)
	if math.Abs(scale-1.00) > 0.001 {
		t.Fatalf("expected scale to remain 1.00 in second idle cycle, got %f", scale)
	}
	if d.adaptiveConsecutiveIdleCount != 2 {
		t.Fatalf("expected consecutive idle count 2, got %d", d.adaptiveConsecutiveIdleCount)
	}

	// 第 3 周期: 连续 3 周期闲置，触发加法超频爬坡 +0.10 -> scale = 1.00 + 0.10 = 1.10
	scale, state, _ = d.adjustAIMDLocked(8, 0)
	if state != "boost" {
		t.Fatalf("expected state 'boost', got %s", state)
	}
	if math.Abs(scale-1.10) > 0.001 {
		t.Fatalf("expected scale 1.10 after overdrive ramp-up, got %f", scale)
	}
	if d.adaptiveConsecutiveIdleCount != 0 {
		t.Fatalf("expected consecutive idle count reset to 0, got %d", d.adaptiveConsecutiveIdleCount)
	}

	// 验证爬坡封顶 maxScale (2.50)
	d.adaptiveScale = 2.45
	d.adjustAIMDLocked(5, 0)               // cycle 1
	d.adjustAIMDLocked(5, 0)               // cycle 2
	scale, _, _ = d.adjustAIMDLocked(5, 0) // cycle 3 -> min(2.50, 2.45 + 0.10) = 2.50
	if math.Abs(scale-2.50) > 0.001 {
		t.Fatalf("expected scale clamped to maxScale 2.50, got %f", scale)
	}
}

// TestAdaptive_ArbitrationPriority 验证仲裁优先级：Manual > Adaptive > WorkHours/Normal
func TestAdaptive_ArbitrationPriority(t *testing.T) {
	d := setupAdaptiveTestDispatcher(10)
	defer d.Close()

	d.workHours = models.WorkHoursConfig{
		Enabled:   true,
		Workdays:  []int{1, 2, 3, 4, 5},
		StartTime: "00:00",
		EndTime:   "23:59",
		Scale:     0.10,
	}

	now := time.Now()

	// 1. 自适应开启且正常时，优先级高于 WorkHours 静态限流
	d.mu.Lock()
	d.adaptiveScale = 1.50
	info := d.getEffectiveScaleInfoLocked(now)
	d.mu.Unlock()

	if info.ThrottleMode != "adaptive" {
		t.Fatalf("expected adaptive mode, got %s", info.ThrottleMode)
	}
	if math.Abs(info.EffectiveScale-1.50) > 0.001 {
		t.Fatalf("expected scale 1.50, got %f", info.EffectiveScale)
	}

	// 2. 管理员手动设置，优先级最高
	d.SetScale(0.50, 1*time.Hour)
	d.mu.Lock()
	infoManual := d.getEffectiveScaleInfoLocked(now)
	d.mu.Unlock()

	if infoManual.ThrottleMode != "manual" || !infoManual.IsManual {
		t.Fatalf("expected manual override to dominate, got mode=%s, isManual=%v",
			infoManual.ThrottleMode, infoManual.IsManual)
	}
	if math.Abs(infoManual.EffectiveScale-0.50) > 0.001 {
		t.Fatalf("expected scale 0.50, got %f", infoManual.EffectiveScale)
	}

	// 3. 解除管理员手动覆盖后，自动平滑切回 Adaptive
	d.RestoreManualScale()
	d.mu.Lock()
	infoRestored := d.getEffectiveScaleInfoLocked(now)
	d.mu.Unlock()

	if infoRestored.ThrottleMode != "adaptive" {
		t.Fatalf("expected to restore adaptive mode, got %s", infoRestored.ThrottleMode)
	}
	if math.Abs(infoRestored.EffectiveScale-1.50) > 0.001 {
		t.Fatalf("expected scale 1.50, got %f", infoRestored.EffectiveScale)
	}

	// 4. 当探针连续失败 3 次触发 Fallback 时，降级为底层 WorkHours 模式
	d.mu.Lock()
	d.adaptiveConsecutiveFailCount = 3
	infoFallback := d.getEffectiveScaleInfoLocked(now)
	d.mu.Unlock()

	if infoFallback.ThrottleMode != "work_hours" {
		t.Fatalf("expected fallback to work_hours mode, got %s", infoFallback.ThrottleMode)
	}
	if math.Abs(infoFallback.EffectiveScale-0.10) > 0.001 {
		t.Fatalf("expected fallback scale 0.10, got %f", infoFallback.EffectiveScale)
	}
}

// TestAdaptive_ProbeNetworkAndFallback 验证 HTTP 探针解析、连续失败回退与自愈恢复
func TestAdaptive_ProbeNetworkAndFallback(t *testing.T) {
	var shouldFail int32 = 0
	var activeVal int32 = 31
	var waitingVal int32 = 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&shouldFail) == 1 {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"server_id":        "pd-glm52-w4a8-xs",
					"timestamp":        "2026-10-20 20:00:00",
					"active_requests":  atomic.LoadInt32(&activeVal),
					"waiting_requests": atomic.LoadInt32(&waitingVal),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	d := setupAdaptiveTestDispatcher(10)
	defer d.Close()

	d.mu.Lock()
	d.adaptiveCfg.ProbeURL = server.URL
	d.adaptiveCfg.Enabled = true
	d.mu.Unlock()

	// 1. 正常探测一次
	d.probeOnce()

	d.mu.Lock()
	if d.adaptiveLastActive != 31 || d.adaptiveLastWaiting != 0 {
		t.Fatalf("expected active=31, waiting=0, got active=%d, waiting=%d",
			d.adaptiveLastActive, d.adaptiveLastWaiting)
	}
	if d.adaptiveConsecutiveFailCount != 0 {
		t.Fatalf("expected failure count 0, got %d", d.adaptiveConsecutiveFailCount)
	}
	d.mu.Unlock()

	// 2. 模拟服务异常，连续探测失败
	atomic.StoreInt32(&shouldFail, 1)

	d.probeOnce() // 失败 1 次
	d.mu.Lock()
	if d.adaptiveConsecutiveFailCount != 1 {
		t.Fatalf("expected failure count 1, got %d", d.adaptiveConsecutiveFailCount)
	}
	info1 := d.getEffectiveScaleInfoLocked(time.Now())
	d.mu.Unlock()
	if info1.ThrottleMode != "adaptive" {
		t.Fatalf("failure 1 should not trigger fallback yet, got mode %s", info1.ThrottleMode)
	}

	d.probeOnce() // 失败 2 次
	d.probeOnce() // 失败 3 次 -> 达到阈值，触发 Fallback

	d.mu.Lock()
	if d.adaptiveConsecutiveFailCount != 3 {
		t.Fatalf("expected failure count 3, got %d", d.adaptiveConsecutiveFailCount)
	}
	snapshot := d.getAdaptiveStatusSnapshotLocked()
	if !snapshot.IsFallback || snapshot.State != "fallback" {
		t.Fatalf("expected snapshot to reflect fallback, got %+v", snapshot)
	}
	infoFallback := d.getEffectiveScaleInfoLocked(time.Now())
	d.mu.Unlock()

	if infoFallback.ThrottleMode != "normal" {
		t.Fatalf("expected fallback to normal mode (since workHours is disabled), got %s", infoFallback.ThrottleMode)
	}

	// 3. 探针恢复正常
	atomic.StoreInt32(&shouldFail, 0)
	d.probeOnce()

	d.mu.Lock()
	if d.adaptiveConsecutiveFailCount != 0 {
		t.Fatalf("expected failure count reset to 0 after recovery, got %d", d.adaptiveConsecutiveFailCount)
	}
	infoRecovered := d.getEffectiveScaleInfoLocked(time.Now())
	d.mu.Unlock()

	if infoRecovered.ThrottleMode != "adaptive" {
		t.Fatalf("expected adaptive mode restored after recovery, got %s", infoRecovered.ThrottleMode)
	}
}
