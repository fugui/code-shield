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

	// 3. 🟢 黄金平衡测试: waiting == 0 && active >= 20 -> 保持当前 scale
	scale, state, _ = d.adjustAIMDLocked(22, 0)
	if state != "optimal" {
		t.Fatalf("expected state 'optimal', got %s", state)
	}
	if math.Abs(scale-0.80) > 0.001 {
		t.Fatalf("expected scale to remain 0.80, got %f", scale)
	}

	// 4. 🔵 闲置增压测试: waiting == 0 && active < 15
	// 第 1 周期: 满足闲置，但尚未连续两周期，scale 应保持不变
	scale, state, _ = d.adjustAIMDLocked(10, 0)
	if state != "boost" {
		t.Fatalf("expected state 'boost', got %s", state)
	}
	if math.Abs(scale-0.80) > 0.001 {
		t.Fatalf("expected scale to remain 0.80 in first idle cycle, got %f", scale)
	}
	if d.adaptiveConsecutiveIdleCount != 1 {
		t.Fatalf("expected consecutive idle count 1, got %d", d.adaptiveConsecutiveIdleCount)
	}

	// 第 2 周期: 再次满足闲置，触发加法爬坡 +0.2 -> scale = 0.80 + 0.20 = 1.00
	scale, state, _ = d.adjustAIMDLocked(8, 0)
	if state != "boost" {
		t.Fatalf("expected state 'boost', got %s", state)
	}
	if math.Abs(scale-1.00) > 0.001 {
		t.Fatalf("expected scale 1.00 after additive ramp-up, got %f", scale)
	}
	if d.adaptiveConsecutiveIdleCount != 0 {
		t.Fatalf("expected consecutive idle count reset to 0, got %d", d.adaptiveConsecutiveIdleCount)
	}

	// 验证爬坡封顶 maxScale (2.50)
	d.adaptiveScale = 2.45
	d.adjustAIMDLocked(5, 0)               // cycle 1
	scale, _, _ = d.adjustAIMDLocked(5, 0) // cycle 2 -> min(2.50, 2.45 + 0.20) = 2.50
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
