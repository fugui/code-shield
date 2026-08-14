package services

import (
	"context"
	"sync"
	"testing"
	"time"
)

func setupTestDispatcher(rawConcurrent int) *ModelDispatcher {
	d := &ModelDispatcher{
		concurrencyScale: 1.0,
		stopHeartbeat:    make(chan struct{}),
		enabled:          true,
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{
			Index:      0,
			OpenCode:   "test-opencode",
			Claude:     "test-claude",
			Concurrent: rawConcurrent,
			Active:     0,
		},
	}
	Dispatcher = d
	return d
}

func TestDispatcher_CeilCalculation(t *testing.T) {
	// 当 rawConcurrent = 1, scale = 0.25 时，limit 应当向上取整为 1
	limit := calculateLimit(1, 0.25)
	if limit != 1 {
		t.Fatalf("expected calculateLimit(1, 0.25) == 1, got %d", limit)
	}

	// 当 scale = 0 时，limit 应当为 0（暂停）
	limit0 := calculateLimit(5, 0.0)
	if limit0 != 0 {
		t.Fatalf("expected calculateLimit(5, 0.0) == 0, got %d", limit0)
	}

	// 当 rawConcurrent = 2, scale = 0.5 时，limit 为 1
	limitHalf := calculateLimit(2, 0.5)
	if limitHalf != 1 {
		t.Fatalf("expected calculateLimit(2, 0.5) == 1, got %d", limitHalf)
	}
}

func TestDispatcher_AutoWakeupOnExpiration(t *testing.T) {
	d := setupTestDispatcher(2)
	defer func() {
		close(d.stopHeartbeat)
	}()

	// 1. 设置限速为 0%（完全暂停），持续 150 毫秒
	duration := 150 * time.Millisecond
	d.SetScale(0.0, duration)

	scale, expiresAt := d.GetScaleAndExpiration()
	if scale != 0.0 {
		t.Fatalf("expected scale 0.0, got %f", scale)
	}
	if expiresAt.IsZero() {
		t.Fatalf("expected non-zero expiration time")
	}

	acquiredChan := make(chan bool, 1)
	errChan := make(chan error, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 2. 启动协程尝试 Acquire，此时应当被阻塞
	start := time.Now()
	go func() {
		res, model, err := d.Acquire(ctx, "claude")
		if err != nil {
			errChan <- err
			return
		}
		if res != nil && model == "test-claude" {
			d.Release(res, "claude")
			acquiredChan <- true
		}
	}()

	// 3. 等待获取结果
	select {
	case err := <-errChan:
		t.Fatalf("Acquire returned error: %v", err)
	case <-acquiredChan:
		elapsed := time.Since(start)
		if elapsed < 100*time.Millisecond {
			t.Fatalf("Acquired too quickly before expiration (%v)", elapsed)
		}
		// 校验过期后 scale 是否已恢复为 1.0
		scaleAfter, _ := d.GetScaleAndExpiration()
		if scaleAfter != 1.0 {
			t.Fatalf("expected scale to be restored to 1.0, got %f", scaleAfter)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Acquire timed out! Goroutine was not woken up after throttle expiration.")
	}
}

func TestDispatcher_ContextCancellation(t *testing.T) {
	d := setupTestDispatcher(1)
	defer func() {
		close(d.stopHeartbeat)
	}()

	// 设置 scale = 0 永久暂停
	d.SetScale(0.0, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := d.Acquire(ctx, "claude")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Acquire did not exit promptly on context cancellation, took %v", elapsed)
	}
}

func TestDispatcher_ScaleClamp(t *testing.T) {
	d := setupTestDispatcher(1)
	defer func() {
		close(d.stopHeartbeat)
	}()

	d.SetScale(-1.0, 0)
	s, _ := d.GetScaleAndExpiration()
	if s != 0.0 {
		t.Fatalf("expected negative scale to be clamped to 0.0, got %f", s)
	}

	d.SetScale(5.0, 0)
	s, _ = d.GetScaleAndExpiration()
	if s != 3.0 {
		t.Fatalf("expected scale > 3.0 to be clamped to 3.0, got %f", s)
	}
}
