package dispatcher

import (
	"fmt"
	"sort"
	"time"

	"code-shield/services/invoker"
)

// LLMSlotLease 记录当前分配出去且正在运行中的单个算力槽位
type LLMSlotLease struct {
	LeaseID         string    `json:"lease_id"`
	ServerIndex     int       `json:"server_index"`
	ServerID        string    `json:"server_id"`
	Driver          string    `json:"driver"`
	Model           string    `json:"model"`
	ReportID        uint      `json:"report_id"`
	RepoName        string    `json:"repo_name"`
	TaskType        string    `json:"task_type"`
	Stage           string    `json:"stage"`
	SubTask         string    `json:"sub_task"`
	Detail          string    `json:"detail,omitempty"`
	StartTime       time.Time `json:"start_time"`
	DurationSeconds int64     `json:"duration_seconds"`
}

// RegisterSlotLease 登记一个分配出去的活跃算力槽位租约
func (d *ModelDispatcher) RegisterSlotLease(res *ModelResource, backend string, modelName string, workCtx *invoker.LLMWorkContext) string {
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
	if stage == "" {
		stage = "通用推理分析"
	}

	lease := &LLMSlotLease{
		LeaseID:         leaseID,
		ServerIndex:     serverIdx,
		ServerID:        serverID,
		Driver:          driver,
		Model:           modelName,
		ReportID:        reportID,
		RepoName:        repoName,
		TaskType:        taskType,
		Stage:           stage,
		SubTask:         subTask,
		Detail:          detail,
		StartTime:       time.Now(),
		DurationSeconds: 0,
	}

	d.activeLeases[leaseID] = lease
	return leaseID
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

	now := time.Now()
	leases := make([]LLMSlotLease, 0, len(d.activeLeases))
	for _, l := range d.activeLeases {
		item := *l
		item.DurationSeconds = int64(now.Sub(item.StartTime).Seconds())
		if item.DurationSeconds < 0 {
			item.DurationSeconds = 0
		}
		leases = append(leases, item)
	}

	// 排序：持续时长越长的置顶展示，方便运维人员一眼捕获慢推理和疑似卡死节点
	sort.Slice(leases, func(i, j int) bool {
		return leases[i].DurationSeconds > leases[j].DurationSeconds
	})

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
