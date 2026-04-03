package models

import (
	"time"

	"gorm.io/datatypes"
)

type Member struct {
	ID         string    `gorm:"primaryKey;column:id" json:"id"` // 员工的字符串ID
	Name       string    `gorm:"not null" json:"name"`           // 姓名
	Email      string    `gorm:"default:''" json:"email"`        // 邮箱地址
	Department string    `gorm:"default:''" json:"department"`   // 部门名称
	CreatedAt  time.Time `json:"created_at"`
}

type User struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Username  string     `gorm:"uniqueIndex;not null" json:"username"`
	Name      string     `gorm:"not null;default:''" json:"name"`
	Password  string     `gorm:"not null" json:"-"` // Omit password in JSON
	IsActive  bool       `gorm:"default:true" json:"is_active"`
	IsAdmin   bool       `gorm:"default:false" json:"is_admin"`
	LastLogin *time.Time `json:"last_login"`
	CreatedAt time.Time  `json:"created_at"`
}

type Team struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	LeaderID  string    `json:"leader_id"`
	Leader    Member    `gorm:"foreignKey:LeaderID" json:"leader"`
	CreatedAt time.Time `json:"created_at"`
}

type Repository struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TeamID         uint      `json:"team_id"`
	Team           Team      `gorm:"foreignKey:TeamID" json:"team"`
	Name           string    `gorm:"uniqueIndex;not null" json:"name"`
	URL            string    `gorm:"not null" json:"url"`
	OwnerID        string    `json:"owner_id"`
	Owner          Member    `gorm:"foreignKey:OwnerID" json:"owner"`
	Branch         string    `gorm:"default:main" json:"branch"`
	ServiceGroup   string         `gorm:"size:30" json:"service_group"`
	RelatedMembers datatypes.JSON `json:"related_members"` // Optional related members (receives CC emails)
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	LastCommitHash string         `json:"last_commit_hash"`
	CreatedAt      time.Time      `json:"created_at"`
}

// TaskType 任务类型定义（管理员可配置）
type TaskType struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	Name               string    `gorm:"uniqueIndex;not null" json:"name"`        // 唯一标识: "code_review", "memory_leak"
	DisplayName        string    `gorm:"not null" json:"display_name"`            // 中文名: "代码检视"
	Description        string    `json:"description"`                             // 任务说明
	PromptFile         string    `json:"prompt_file"`                             // prompt 文件路径
	PreconditionScript string    `json:"precondition_script"`                     // 前置检查脚本路径
	PostprocessScript  string    `json:"postprocess_script"`                      // 后置结果解析脚本路径
	NotifyTemplate     string    `json:"notify_template"`                         // 邮件主题模板
	NotifyThreshold    int       `gorm:"default:0" json:"notify_threshold"`       // score >= 此值才通知
	Timeout            int       `gorm:"default:30" json:"timeout"`               // AI 执行超时（分钟）
	IsActive           bool      `gorm:"default:true" json:"is_active"`
	IsBuiltin          bool      `gorm:"default:false" json:"is_builtin"`         // 内置任务不可删除
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// TaskReport 通用任务报告
type TaskReport struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	RepoID      uint           `json:"repo_id"`
	Repo        Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskTypeID  uint           `json:"task_type_id"`
	TaskType    TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	Status      string         `gorm:"default:pending" json:"status"` // pending, queued, cloning, pre_processing, analyzing, post_processing, success, failed, skipped
	CloneStatus string         `gorm:"default:pending" json:"clone_status"`
	AISummary   string         `json:"ai_summary"`
	ReportPath  string         `json:"report_path"`
	Score       int            `gorm:"default:0" json:"score"`
	Metrics     datatypes.JSON `json:"metrics"` // {"blocking":0,"critical":3,...}
	BaseCommit  string         `json:"base_commit"`
	HeadCommit  string         `json:"head_commit"`
	CreatedAt   time.Time      `json:"created_at"`
}

const (
	StatusPending        = "pending"
	StatusQueued         = "queued"
	StatusCloning        = "cloning"
	StatusPreProcessing  = "pre_processing"
	StatusAnalyzing      = "analyzing"
	StatusPostProcessing = "post_processing"
	StatusSuccess        = "success"
	StatusFailed         = "failed"
	StatusSkipped        = "skipped"
)

// KeyIssue 核心问题追踪
type KeyIssue struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	RepoID       uint       `json:"repo_id"`
	TaskReportID uint       `json:"task_report_id"`
	Repo         Repository `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReport   TaskReport `gorm:"foreignKey:TaskReportID" json:"task_report"`
	IssueType    string     `gorm:"not null" json:"issue_type"` // multithreading, lock, memory_leak, library
	Title        string     `gorm:"not null" json:"title"`
	FilePath     string     `json:"file_path"`
	LineNumber   int        `json:"line_number"`
	Status       string     `gorm:"default:open" json:"status"` // open, in_progress, resolved
	AssigneeID   string     `json:"assignee_id"`
	Assignee     Member     `gorm:"foreignKey:AssigneeID" json:"assignee"`
	CreatedAt    time.Time  `json:"created_at"`
}

type SystemConfig struct {
	ID         uint `gorm:"primaryKey" json:"id"` // Always 1
	AutoNotify bool `gorm:"default:false" json:"auto_notify"`
}

type ScheduleConfig struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Name         string         `gorm:"not null" json:"name"`
	CronExpr     string         `gorm:"not null" json:"cron_expr"`
	TaskTypeID   uint           `json:"task_type_id"`
	TaskType     TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	TargetMode   string         `gorm:"not null" json:"target_mode"`     // "all", "service_group", "team", "specific"
	TargetValues datatypes.JSON `json:"target_values"`                   // JSON array
	AutoNotify   bool           `gorm:"default:true" json:"auto_notify"`
	IsActive     bool           `gorm:"default:true" json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type TaskExecutionLog struct {
	ID             uint            `gorm:"primaryKey" json:"id"`
	ScheduleID     *uint           `json:"schedule_id"`
	Schedule       *ScheduleConfig `gorm:"foreignKey:ScheduleID" json:"schedule"`
	RepoID         uint            `json:"repo_id"`
	Repo           Repository      `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID   *uint           `json:"task_report_id"`
	TaskReport     *TaskReport     `gorm:"foreignKey:TaskReportID" json:"task_report"`
	TaskTypeID     uint            `json:"task_type_id"`
	TaskType       TaskType        `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	TriggerType    string          `gorm:"not null" json:"trigger_type"`  // "cron", "manual", "webhook"
	Status         string          `gorm:"default:pending" json:"status"` // "pending", "running", "success", "failed", "skipped"
	ErrorMessage   string          `json:"error_message"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        *time.Time      `json:"end_time"`
	CreatedAt      time.Time       `json:"created_at"`
}
