package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"code-shield/services/invoker"
)

// LLMSlotLease 记录当前分配出去且正在运行中的单个算力槽位
type LLMSlotLease struct {
	LeaseID          string                     `json:"lease_id"`
	ServerIndex      int                        `json:"server_index"`
	ServerID         string                     `json:"server_id"`
	Driver           string                     `json:"driver"`
	Model            string                     `json:"model"`
	ReportID         uint                       `json:"report_id"`
	RepoName         string                     `json:"repo_name"`
	TaskType         string                     `json:"task_type"`
	Stage            string                     `json:"stage"`
	SubTask          string                     `json:"sub_task"`
	Detail           string                     `json:"detail,omitempty"`
	TierName         string                     `json:"tier_name,omitempty"`
	QueueStartedAt   *time.Time                 `json:"queue_started_at,omitempty"`
	QueueWaitSeconds float64                    `json:"queue_wait_seconds,omitempty"`
	StartTime        time.Time                  `json:"start_time"`
	DurationSeconds  int64                      `json:"duration_seconds"`
	Status           string                     `json:"status"`
	ErrorClass       string                     `json:"error_class,omitempty"`
	EndTime          *time.Time                 `json:"end_time,omitempty"`
	ErrorMessage     string                     `json:"error_message,omitempty"`
	Diagnostics      *invoker.CLIDiagnostics    `json:"diagnostics,omitempty"`
	Observability    *invoker.CallObservability `json:"-"`
}

// RegisterSlotLease 登记一个分配出去的活跃算力槽位租约
func (d *ModelDispatcher) RegisterSlotLease(res *ModelResource, backend string, modelName string, workCtx *invoker.LLMWorkContext, observability ...*invoker.CallObservability) string {
	tierName := ""
	if workCtx != nil {
		tierName = workCtx.TierName
	}
	return d.RegisterSlotLeaseWithQueue(res, backend, modelName, workCtx, tierName, time.Time{}, observability...)
}

// RegisterSlotLeaseWithQueue also records how long the caller waited for the
// physical slot before this execution lease began.
func (d *ModelDispatcher) RegisterSlotLeaseWithQueue(res *ModelResource, backend string, modelName string, workCtx *invoker.LLMWorkContext, metricTierName string, queueStartedAt time.Time, observability ...*invoker.CallObservability) string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.activeLeases == nil {
		d.activeLeases = make(map[string]*LLMSlotLease)
	}

	d.leaseSeq++
	leaseID := fmt.Sprintf("lease-%d-%d", time.Now().UnixNano(), d.leaseSeq)

	driver := backend
	if res != nil && res.Driver != "" {
		driver = res.Driver
	}
	serverID := ""
	serverIdx := -1
	if res != nil {
		serverID = res.ID
		serverIdx = res.Index
		if modelName == "" {
			modelName = res.ModelName(backend)
		}
		if modelName == "" {
			modelName = res.Model
		}
	}

	var reportID uint
	var repoName, taskType, stage, subTask, detail string
	if workCtx != nil {
		reportID = workCtx.ReportID
		repoName = workCtx.RepoName
		taskType = workCtx.TaskType
		stage = workCtx.Stage
		subTask = workCtx.SubTask
		detail = workCtx.Detail
	}
	tierName := CanonicalTierName(metricTierName)
	if tierName == systemTierName && workCtx != nil && workCtx.TierName != "" {
		tierName = CanonicalTierName(workCtx.TierName)
	}
	if stage == "" {
		stage = "通用推理分析"
	}

	startTime := time.Now()
	queueWaitSeconds := 0.0
	var queueStartedAtPtr *time.Time
	if !queueStartedAt.IsZero() {
		queueWaitSeconds = startTime.Sub(queueStartedAt).Seconds()
		if queueWaitSeconds < 0 {
			queueWaitSeconds = 0
		}
		startedAt := queueStartedAt
		queueStartedAtPtr = &startedAt
	}

	lease := &LLMSlotLease{
		LeaseID:          leaseID,
		ServerIndex:      serverIdx,
		ServerID:         serverID,
		Driver:           driver,
		Model:            modelName,
		ReportID:         reportID,
		RepoName:         repoName,
		TaskType:         taskType,
		Stage:            stage,
		SubTask:          subTask,
		Detail:           detail,
		TierName:         tierName,
		QueueStartedAt:   queueStartedAtPtr,
		QueueWaitSeconds: queueWaitSeconds,
		StartTime:        startTime,
		DurationSeconds:  0,
		Status:           string(invoker.CallRunning),
	}
	if len(observability) > 0 && observability[0] != nil {
		lease.Observability = observability[0]
		lease.Observability.LeaseID = leaseID
	}

	d.activeLeases[leaseID] = lease
	return leaseID
}

// CompleteSlotLease marks a lease finished and keeps a bounded in-memory copy
// so an operator can inspect the just-finished console without persistence.
func (d *ModelDispatcher) CompleteSlotLease(leaseID string, invokeErr error) {
	if d == nil || leaseID == "" {
		return
	}

	d.mu.Lock()
	lease, ok := d.activeLeases[leaseID]
	if ok {
		delete(d.activeLeases, leaseID)
		now := time.Now()
		lease.EndTime = &now
		lease.DurationSeconds = int64(now.Sub(lease.StartTime).Seconds())
		if lease.DurationSeconds < 0 {
			lease.DurationSeconds = 0
		}
		switch {
		case invokeErr == nil:
			lease.Status = string(invoker.CallSucceeded)
		case errors.Is(invokeErr, context.Canceled):
			lease.Status = string(invoker.CallCanceled)
			lease.ErrorMessage = invokeErr.Error()
		case errors.Is(invokeErr, context.DeadlineExceeded):
			lease.Status = string(invoker.CallFailed)
			lease.ErrorMessage = "context deadline exceeded"
		default:
			lease.Status = string(invoker.CallFailed)
			lease.ErrorMessage = invokeErr.Error()
		}
		if lease.Observability != nil {
			lease.Diagnostics = lease.Observability.CLIDiagnosticsSnapshot()
			if invokeErr == nil {
				lease.ErrorClass = ""
				if lease.Diagnostics != nil {
					lease.Diagnostics.ErrorClass = ""
				}
			} else {
				lease.ErrorClass = string(invoker.ClassifyError(invokeErr))
				if lease.Diagnostics != nil {
					lease.Diagnostics.ErrorClass = invoker.ClassifyError(invokeErr)
				}
			}
		}
		if lease.Observability != nil {
			lease.Observability.Finish(invokeErr)
			lease.Observability.ReleasePromptProvider()
		}
		if !d.skipSuccessfulLeases || lease.Status != string(invoker.CallSucceeded) {
			d.recentLeases = append([]*LLMSlotLease{lease}, d.recentLeases...)
			if len(d.recentLeases) > maxRecentLeases {
				d.recentLeases = d.recentLeases[:maxRecentLeases]
			}
		}
	}
	d.mu.Unlock()
}

// SetRecordSuccessfulLeases updates whether future successful leases are
// retained. It intentionally does not mutate the existing history ring.
func (d *ModelDispatcher) SetRecordSuccessfulLeases(enabled bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.skipSuccessfulLeases = !enabled
}

// RecordSuccessfulLeases reports the current completion-history policy.
func (d *ModelDispatcher) RecordSuccessfulLeases() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.skipSuccessfulLeases
}

// GetLeaseByID returns an active or recently completed lease for the
// super-admin-only diagnostic API.
func (d *ModelDispatcher) GetLeaseByID(leaseID string) (LLMSlotLease, bool) {
	if d == nil || leaseID == "" {
		return LLMSlotLease{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if lease, ok := d.activeLeases[leaseID]; ok {
		item := *lease
		item.DurationSeconds = int64(time.Since(lease.StartTime).Seconds())
		if item.DurationSeconds < 0 {
			item.DurationSeconds = 0
		}
		return item, true
	}
	for _, lease := range d.recentLeases {
		if lease.LeaseID == leaseID {
			return *lease, true
		}
	}
	return LLMSlotLease{}, false
}

// UnregisterSlotLease 销毁归还的活跃算力槽位租约
func (d *ModelDispatcher) UnregisterSlotLease(leaseID string) {
	if d == nil || leaseID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.activeLeases, leaseID)
}

// GetActiveLeases 返回当前所有活跃槽位租约快照（按运行时长从大到小排序，排障优先）
func (d *ModelDispatcher) GetActiveLeases() []LLMSlotLease {
	if d == nil {
		return []LLMSlotLease{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getActiveLeasesLocked(time.Now())
}

func (d *ModelDispatcher) getActiveLeasesLocked(now time.Time) []LLMSlotLease {
	leases := make([]LLMSlotLease, 0, len(d.activeLeases))
	for _, l := range d.activeLeases {
		item := *l
		item.DurationSeconds = int64(now.Sub(item.StartTime).Seconds())
		if item.DurationSeconds < 0 {
			item.DurationSeconds = 0
		}
		if item.Status == "" {
			item.Status = string(invoker.CallRunning)
		}
		leases = append(leases, item)
	}

	// 排序：持续时长越长的置顶展示，方便运维人员一眼捕获慢推理和疑似卡死节点
	sort.Slice(leases, func(i, j int) bool {
		return leases[i].DurationSeconds > leases[j].DurationSeconds
	})

	return leases
}

// GetRecentLeases 返回最近完成的算力槽位租约快照（最新完成在前，仅内存，上限 50 条）
func (d *ModelDispatcher) GetRecentLeases() []LLMSlotLease {
	if d == nil {
		return []LLMSlotLease{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getRecentLeasesLocked()
}

func (d *ModelDispatcher) getRecentLeasesLocked() []LLMSlotLease {
	leases := make([]LLMSlotLease, 0, len(d.recentLeases))
	for _, item := range d.recentLeases {
		lease := *item
		if lease.EndTime != nil {
			lease.DurationSeconds = int64(lease.EndTime.Sub(lease.StartTime).Seconds())
			if lease.DurationSeconds < 0 {
				lease.DurationSeconds = 0
			}
		}
		leases = append(leases, lease)
	}
	return leases
}

// ResetActiveSlots 紧急运维自愈方法：将所有算力节点的活跃槽位 Active 强制重置为 0，并广播唤醒所有等待者。
func (d *ModelDispatcher) ResetActiveSlots() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	totalCleared := 0
	for _, res := range d.resources {
		totalCleared += res.Active
		res.Active = 0
	}
	d.activeLeases = make(map[string]*LLMSlotLease)
	d.cond.Broadcast()
	return totalCleared
}
