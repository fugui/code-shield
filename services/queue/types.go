package queue

import (
	"code-shield/models"
)

// Task 描述工作队列中单个待执行的任务单元
type Task struct {
	RepoID     uint
	ReportID   uint
	RepoURL    string
	TaskTypeID uint
	AutoNotify bool
	LogID      uint             // ID of TaskExecutionLog
	RunParams  models.RunParams // 运行时参数（从 ScheduleConfig 传入）
	IsResume   bool             // true 时 worker 调用 ResumeFailedChunks 而非 RunTaskSync
}
