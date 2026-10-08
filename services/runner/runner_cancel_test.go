package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"code-shield/models"
)

func TestCancelRunningTaskPreventsStartupAfterDeletion(t *testing.T) {
	reportID := uint(time.Now().UnixNano())

	if CancelRunningTask(reportID) {
		t.Fatalf("task should not be active before runner registration")
	}

	if err := RunTaskSync(reportID, "", 0, false, models.RunParams{}); !errors.Is(err, ErrTaskCanceled) {
		t.Fatalf("expected ErrTaskCanceled, got %v", err)
	}
}

func TestCancelRunningTaskCancelsActiveContext(t *testing.T) {
	reportID := uint(time.Now().UnixNano())
	taskCtx, cancel := context.WithCancel(context.Background())
	ctx := &TaskContext{Ctx: taskCtx, Cancel: cancel}

	activeTasksMu.Lock()
	activeTasks[reportID] = ctx
	activeTasksMu.Unlock()
	defer func() {
		activeTasksMu.Lock()
		delete(activeTasks, reportID)
		activeTasksMu.Unlock()
		cancel()
	}()

	if !CancelRunningTask(reportID) {
		t.Fatalf("expected active task to be cancelled")
	}
	if ctx.Ctx.Err() == nil {
		t.Fatalf("expected task context to be cancelled")
	}
}
