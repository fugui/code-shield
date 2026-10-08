package runner

import "errors"

// ErrSkipped 在前置条件未满足（例如代码无增量变更）时返回，通知外层队列优雅跳过本次扫描
var ErrSkipped = errors.New("task skipped by scope planner")

// ErrTaskCanceled 在任务启动前已经收到取消请求时返回，避免 worker 启动一个已被删除的任务。
var ErrTaskCanceled = errors.New("task canceled before execution")

var ErrResumeRequiresRescan = errors.New("RESUME_REQUIRES_RESCAN: no valid debate_full bundle checkpoint; rerun scan")
