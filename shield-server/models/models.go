package models

import (
	"fmt"
	"path/filepath"
	"strings"
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
	ID           uint       `gorm:"primaryKey" json:"id"`
	Email        string     `gorm:"uniqueIndex;not null" json:"email"`
	Name         string     `gorm:"not null;default:''" json:"name"`
	Password     string     `gorm:"not null" json:"-"` // Omit password in JSON
	EmployeeID   string     `gorm:"default:''" json:"employee_id"`
	UniqueID     string     `gorm:"default:''" json:"unique_id"`
	EmployeeType string     `gorm:"default:''" json:"employee_type"`
	RegMethod    string     `gorm:"default:'local'" json:"reg_method"` // "local" or "sso"
	IsActive     bool       `gorm:"default:true" json:"is_active"`
	IsAdmin      bool       `gorm:"default:false" json:"is_admin"`
	LastLogin    *time.Time `json:"last_login"`
	CreatedAt    time.Time  `json:"created_at"`
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


// RunParams 定义任务执行时的运行参数。
// ScheduleConfig 中可设置此结构覆盖 TaskType 的默认值，nil 字段表示不覆盖。
type RunParams struct {
	AIBackend   *string `json:"ai_backend,omitempty"`   // nil = 不覆盖，使用 TaskType 默认
	TargetScope *string `json:"target_scope,omitempty"` // nil = 不覆盖，使用 TaskType 默认 ("all", "business", "test")
}

// TaskType 任务类型定义（管理员可配置）
type TaskType struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	Name            string         `gorm:"uniqueIndex;not null" json:"name"` // 唯一标识: "code_review", "memory_leak"
	DisplayName     string         `gorm:"not null" json:"display_name"`     // 中文名: "代码检视"
	Description     string         `json:"description"`                      // 任务说明
	EngineMode      string         `gorm:"default:single" json:"engine_mode"` // 执行引擎模式: single, chunked
	EngineConfig    datatypes.JSON `json:"engine_config"`                      // 引擎配置 {"max_files": 50, "depth": 2}
	AIBackend       string         `gorm:"default:''" json:"ai_backend"`       // AI 后端: 为空时使用全局配置，可选 claude/opencode
	TargetScope     string         `gorm:"default:'business'" json:"target_scope"` // 处理范围: all (全部), business (仅业务), test (仅测试)
	NotifyTemplate  string         `json:"notify_template"`                    // 邮件主题模板
	NotifyThreshold int            `gorm:"default:0" json:"notify_threshold"` // score >= 此值才通知
	NotifyCc        datatypes.JSON `json:"notify_cc"`                        // 通知抄送邮箱列表 ["a@x.com","b@x.com"]
	Timeout         int            `gorm:"default:30" json:"timeout"`        // AI 执行超时（分钟）
	IsActive        bool           `gorm:"default:true" json:"is_active"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// TaskDir 返回任务类型的文件目录（约定: tasks/<name-with-hyphens>/）
func (t *TaskType) TaskDir() string {
	return filepath.Join("tasks", strings.ReplaceAll(t.Name, "_", "-"))
}

// AnalysisPromptFile 分析阶段提示词文件路径（约定固定）
func (t *TaskType) AnalysisPromptFile() string {
	return filepath.Join(t.TaskDir(), "analysis_prompt.md")
}

// SynthesisPromptFile 综合报告阶段提示词文件路径（约定固定）
func (t *TaskType) SynthesisPromptFile() string {
	return filepath.Join(t.TaskDir(), "synthesis_prompt.md")
}

// PreconditionScript 前置检查脚本路径（约定固定）
func (t *TaskType) PreconditionScript() string {
	return filepath.Join(t.TaskDir(), "precondition")
}

// PostprocessScript 后置结果解析脚本路径（约定固定）
func (t *TaskType) PostprocessScript() string {
	return filepath.Join(t.TaskDir(), "postprocess")
}

// AgentName 返回指定阶段的 opencode agent 名称（约定: shield-<task-name>-<phase>）
// phase: "analysis" 或 "synthesis"
func (t *TaskType) AgentName(phase string) string {
	taskDir := strings.ReplaceAll(t.Name, "_", "-")
	return fmt.Sprintf("shield-%s-%s", taskDir, phase)
}


// TaskReport 通用任务报告
type TaskReport struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	RepoID      uint           `json:"repo_id"`
	Repo        Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskTypeID  uint           `json:"task_type_id"`
	TaskType    TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	ParentID        uint           `gorm:"default:0" json:"parent_id"`    // 0 if it is a parent or independent task
	ChunkName       string         `gorm:"default:''" json:"chunk_name"`  // Name of the directory or file group
	TotalChunks     int            `gorm:"default:0" json:"total_chunks"`
	ProcessedChunks int            `gorm:"default:0" json:"processed_chunks"`
	SuccessChunks   int            `gorm:"default:0" json:"success_chunks"`
	Status          string         `gorm:"default:pending" json:"status"` // pending, queued, cloning, pre_processing, analyzing, post_processing, success, failed, skipped
	CloneStatus string         `gorm:"default:pending" json:"clone_status"`
	AISummary   string         `json:"ai_summary"`
	ReportPath  string         `json:"report_path"`
	Score       int            `gorm:"default:0" json:"score"`
	Metrics     datatypes.JSON `json:"metrics"` // {"blocking":0,"critical":3,...}
	BaseCommit  string         `json:"base_commit"`
	HeadCommit  string         `json:"head_commit"`
	CreatedAt   time.Time      `json:"created_at"`
}

// AnalysisFinding 记录 AI 分析阶段输出的结构化问题
type AnalysisFinding struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	TaskReportID uint       `gorm:"index" json:"task_report_id"`          // 关联到 TaskReport
	TaskTypeID   uint       `gorm:"index" json:"task_type_id"`            // 哪个任务类型触发的
	RepoID       uint       `gorm:"index" json:"repo_id"`                 // 来自哪个代码仓
	Severity     string     `gorm:"not null" json:"severity"`             // 严重程度（阻塞/严重/主要/提示/建议）
	Category     string     `json:"category"`                             // 问题分类（multithreading, memory_leak, library...）
	FilePath     string     `json:"file_path"`                            // 问题所在文件
	LineNumber   string     `json:"line_number"`                          // 行号（支持范围如 "100-125" 或多行 "41,42"）
	CodeSnippet  string     `gorm:"type:text" json:"code_snippet"`        // 问题发生处的原始代码片段
	Title        string     `gorm:"not null" json:"title"`                // 问题标题
	Detail       string     `gorm:"type:text" json:"detail"`              // 详细描述
	Suggestion   string     `gorm:"type:text" json:"suggestion"`          // 修复建议
	Status       string     `gorm:"default:open" json:"status"`           // 处理状态: open, processing, closed
	AssigneeID   string     `json:"assignee_id"`                          // 处理人 ID
	Feedback     string     `gorm:"type:text" json:"feedback"`            // 用户反馈内容
	FeedbackAt   *time.Time `json:"feedback_at"`                          // 反馈时间
	CreatedAt    time.Time  `json:"created_at"`
}

// TestCaseFinding 记录 "测试用例有效性评估" (ut_effectiveness) 任务的测试用例级扫描结果
type TestCaseFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_repo_file_name;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_repo_file_name;size:500;not null" json:"file_path"`
	LineNumber   string         `json:"line_number"`
	TestCaseName string         `gorm:"uniqueIndex:idx_repo_file_name;size:255;not null;column:test_case_name" json:"test_case_name"` // 测试用例名称
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:50;not null" json:"severity"` // 合格、阻塞、严重、主要、提示、建议
	Category     string         `gorm:"size:100" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50" json:"status"` // open (待处理), analyzing (问题分析), resolved (问题解决), closed (问题关闭), invalid (无效问题)
	AssigneeID   string         `json:"assignee_id"`
	Assignee     *Member        `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 用于记录时间节点：[{"status":"open","time":"2026-06-01...","user":"xxx"}]
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
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
	LineNumber   string     `json:"line_number"`
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
	RunParams    datatypes.JSON `json:"run_params"`                      // 运行参数覆盖 {"ai_backend":"claude","target_scope":"business"}
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

// CoredumpFinding 记录 "C/C++ Coredump 风险分析" (coredump_risk) 任务的扫描结果与跟踪
type CoredumpFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_repo_file_line_title;size:255;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_repo_file_line_title;size:50" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_repo_file_line_title;size:255;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:50;not null" json:"severity"` // 阻塞、严重、主要、提示、建议
	Category     string         `gorm:"size:100" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   string         `json:"assignee_id"`
	Assignee     *Member        `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// FloatFinding 记录 "Python 浮点数比较缺陷扫描" (float_comparison) 任务的扫描结果与跟踪
type FloatFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_float_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_float_repo_file_line_title;size:255;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_float_repo_file_line_title;size:50" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_float_repo_file_line_title;size:255;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:50;not null" json:"severity"` // 阻塞、严重、主要、提示、建议
	Category     string         `gorm:"size:100" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   string         `json:"assignee_id"`
	Assignee     *Member        `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// ThreadFinding 记录 "新建线程分析" (thread_create) 任务的扫描结果与跟踪
type ThreadFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_thread_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_thread_repo_file_line_title;size:255;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_thread_repo_file_line_title;size:50" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_thread_repo_file_line_title;size:255;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:50;not null" json:"severity"` // 阻塞、严重、主要、提示、建议
	Category     string         `gorm:"size:100" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   string         `json:"assignee_id"`
	Assignee     *Member        `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// CjsonFinding 记录 "cJSON 内存泄漏扫描" (cjson_scan) 任务的扫描结果与跟踪
type CjsonFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_cjson_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_cjson_repo_file_line_title;size:255;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_cjson_repo_file_line_title;size:50" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_cjson_repo_file_line_title;size:255;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:50;not null" json:"severity"` // 阻塞、严重、主要、提示、建议
	Category     string         `gorm:"size:100" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   string         `json:"assignee_id"`
	Assignee     *Member        `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}
