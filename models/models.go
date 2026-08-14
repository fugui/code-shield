package models

import (
	commonModels "code-common/backend/models"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/datatypes"
)

type User = commonModels.User
type Department = commonModels.Department

type Repository struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	DepartmentID   uint           `json:"department_id"`
	Department     Department     `gorm:"foreignKey:DepartmentID" json:"department"`
	Name           string         `gorm:"uniqueIndex;not null" json:"name"`
	ProjectID      string         `gorm:"default:''" json:"project_id"`
	URL            string         `gorm:"not null" json:"url"`
	HTTPURL        string         `gorm:"default:''" json:"http_url"`
	OwnerID        uint           `json:"owner_id"`
	Owner          User           `gorm:"foreignKey:OwnerID" json:"owner"`
	Branch         string         `gorm:"default:master" json:"branch"`
	ServiceGroup   string         `gorm:"size:30" json:"service_group"`
	RelatedMembers datatypes.JSON `json:"related_members"` // Optional related members (receives CC emails)
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	LastCommitHash string         `json:"last_commit_hash"`
	ReportCount    int64          `gorm:"-" json:"report_count"`
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
	Name            string         `gorm:"uniqueIndex;not null" json:"name"`       // 唯一标识: "code_review", "memory_leak"
	DisplayName     string         `gorm:"not null" json:"display_name"`           // 中文名: "代码检视"
	Description     string         `json:"description"`                            // 任务说明
	EngineMode      string         `gorm:"default:single" json:"engine_mode"`      // 执行引擎模式: single, chunked
	EngineConfig    datatypes.JSON `json:"engine_config"`                          // 引擎配置 {"max_files": 50, "depth": 2}
	AIBackend       string         `gorm:"default:''" json:"ai_backend"`           // AI 后端: 为空时使用全局配置，可选 claude/opencode
	TargetScope     string         `gorm:"default:'business'" json:"target_scope"` // 处理范围: all (全部), business (仅业务), test (仅测试)
	NotifyTemplate  string         `json:"notify_template"`                        // 邮件主题模板
	NotifyThreshold int            `gorm:"default:0" json:"notify_threshold"`      // score >= 此值才通知
	NotifyCc        datatypes.JSON `json:"notify_cc"`                              // 通知抄送邮箱列表 ["a@x.com","b@x.com"]
	Timeout         int            `gorm:"default:30" json:"timeout"`              // AI 执行超时（分钟）
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
	ID              uint           `gorm:"primaryKey" json:"id"`
	RepoID          uint           `gorm:"index" json:"repo_id"`
	Repo            Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskTypeID      uint           `gorm:"index" json:"task_type_id"`
	TaskType        TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	ParentID        uint           `gorm:"default:0;index" json:"parent_id"` // 0 if it is a parent or independent task
	ChunkName       string         `gorm:"default:''" json:"chunk_name"`     // Name of the directory or file group
	TotalChunks     int            `gorm:"default:0" json:"total_chunks"`
	ProcessedChunks int            `gorm:"default:0" json:"processed_chunks"`
	SuccessChunks   int            `gorm:"default:0" json:"success_chunks"`
	Status          string         `gorm:"default:pending;index" json:"status"` // pending, queued, cloning, pre_processing, analyzing, post_processing, success, failed, skipped
	CloneStatus     string         `gorm:"default:pending" json:"clone_status"`
	AISummary       string         `json:"ai_summary"`
	ReportPath      string         `json:"report_path"`
	Score           int            `gorm:"default:0" json:"score"`
	Metrics         datatypes.JSON `json:"metrics"` // {"blocking":0,"critical":3,...}
	BaseCommit      string         `json:"base_commit"`
	HeadCommit      string         `json:"head_commit"`
	CreatedAt       time.Time      `gorm:"index" json:"created_at"`
}

// GetAbsReportPath 返回报告文件的绝对路径（如果存储的是相对路径，则使用 storage.root 拼接）
func (r *TaskReport) GetAbsReportPath() string {
	if r.ReportPath == "" {
		return ""
	}
	if filepath.IsAbs(r.ReportPath) {
		// 1. 如果原始绝对路径文件存在，直接使用
		if _, err := os.Stat(r.ReportPath); err == nil {
			return r.ReportPath
		}
		// 2. 否则，如果路径中包含 "reports/"，截取相对路径并与当前 AppConfig.Storage.Root 拼接做兼容性容错
		if idx := strings.Index(r.ReportPath, "reports/"); idx != -1 {
			relPath := r.ReportPath[idx:]
			absPath := filepath.Join(AppConfig.Storage.Root, relPath)
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
		return r.ReportPath
	}
	return filepath.Join(AppConfig.Storage.Root, r.ReportPath)
}

// AnalysisFinding 记录 AI 分析阶段输出的结构化问题
type AnalysisFinding struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	TaskReportID uint       `gorm:"index" json:"task_report_id"`    // 关联到 TaskReport
	TaskTypeID   uint       `gorm:"index" json:"task_type_id"`      // 哪个任务类型触发的
	RepoID       uint       `gorm:"index" json:"repo_id"`           // 来自哪个代码仓
	Severity     string     `gorm:"not null;index" json:"severity"` // 严重程度（致命/严重/一般/建议）
	Category     string     `gorm:"index" json:"category"`          // 问题分类（multithreading, memory_leak, library...）
	FilePath     string     `json:"file_path"`                      // 问题所在文件
	LineNumber   string     `json:"line_number"`                    // 行号（支持范围如 "100-125" 或多行 "41,42"）
	CodeSnippet  string     `gorm:"type:text" json:"code_snippet"`  // 问题发生处的原始代码片段
	Title        string     `gorm:"not null" json:"title"`          // 问题标题
	Detail       string     `gorm:"type:text" json:"detail"`        // 详细描述
	Suggestion   string     `gorm:"type:text" json:"suggestion"`    // 修复建议
	AssigneeID   *uint      `json:"assignee_id"`                    // 处理人 ID
	Assignee     *User      `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	Feedback     string     `gorm:"type:text" json:"feedback"` // 用户反馈内容
	FeedbackAt   *time.Time `json:"feedback_at"`               // 反馈时间
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
	Severity     string         `gorm:"size:50;not null;index" json:"severity"` // 合格、致命、严重、一般、建议
	Category     string         `gorm:"size:100;index" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50;index" json:"status"` // open (待处理), analyzing (问题分析), resolved (问题解决), closed (问题关闭), invalid (无效问题)
	AssigneeID   *uint          `json:"assignee_id"`
	Assignee     *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 用于记录时间节点：[{"status":"open","time":"2026-06-01...","user":"xxx"}]
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

const (
	StatusPending        = "pending"
	StatusQueued         = "queued"
	StatusRunning        = "running"
	StatusCloning        = "cloning"
	StatusPreProcessing  = "pre_processing"
	StatusAnalyzing      = "analyzing"
	StatusSynthesis      = "synthesis"
	StatusPostProcessing = "post_processing"
	StatusMerging        = "merging"
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
	AssigneeID   *uint      `json:"assignee_id"`
	Assignee     *User      `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type SystemConfig struct {
	ID               uint       `gorm:"primaryKey" json:"id"` // Always 1
	AutoNotify       bool       `gorm:"default:false" json:"auto_notify"`
	ConcurrencyScale float64    `gorm:"default:1.0" json:"concurrency_scale"`
	ScaleExpiresAt   *time.Time `json:"scale_expires_at"`
}

type ScheduleConfig struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Name         string         `gorm:"not null;default:''" json:"name"`
	CronExpr     string         `gorm:"not null;default:''" json:"cron_expr"`
	TaskTypeID   uint           `json:"task_type_id"`
	TaskType     TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	TargetMode   string         `gorm:"not null;default:'all'" json:"target_mode"` // "all", "service_group", "team", "specific"
	TargetValues datatypes.JSON `json:"target_values"`                             // JSON array
	AutoNotify   bool           `gorm:"default:true" json:"auto_notify"`
	IsActive     bool           `gorm:"default:true" json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	RunParams    datatypes.JSON `json:"run_params"` // 运行参数覆盖 {"ai_backend":"claude","target_scope":"business"}
	UpdatedAt    time.Time      `json:"updated_at"`
}

// TaskTriggerLog 记录面向人类的操作审计触发日志
type TaskTriggerLog struct {
	ID            uint            `gorm:"primaryKey" json:"id"`
	TriggerBatch  string          `gorm:"uniqueIndex;size:64;not null" json:"trigger_batch"` // 批次号 e.g. "TRG-20260731-XXXXXX"
	TriggerType   string          `gorm:"size:30;not null;index" json:"trigger_type"`        // "manual_single", "manual_batch", "cron_auto", "cron_manual"
	OperatorID    *uint           `gorm:"index" json:"operator_id"`                          // 操作人 ID (系统触发为 nil)
	Operator      *User           `gorm:"foreignKey:OperatorID" json:"operator,omitempty"`   // 关联 User
	OperatorName  string          `gorm:"size:100" json:"operator_name"`                     // 操作人姓名/Email 冗余快照
	TaskTypeID    uint            `gorm:"index" json:"task_type_id"`
	TaskType      TaskType        `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	TargetMode    string          `gorm:"size:30" json:"target_mode"`     // "single", "all", "service_group", "team", "missing_days", "specific"
	TargetSummary string          `gorm:"size:255" json:"target_summary"` // 目标摘要：“代码仓 repo-a”, “过去 7 天未扫代码仓”
	FilterParams  datatypes.JSON  `json:"filter_params"`                  // 筛选参数Json
	ScheduleID    *uint           `gorm:"index" json:"schedule_id"`       // 如果关联定时任务策略
	Schedule      *ScheduleConfig `gorm:"foreignKey:ScheduleID" json:"schedule,omitempty"`
	TotalRepos    int             `gorm:"default:0" json:"total_repos"`   // 本次触发涉及的代码仓总数
	SuccessCount  int             `gorm:"default:0" json:"success_count"` // 成功排队数
	SkipCount     int             `gorm:"default:0" json:"skip_count"`    // 跳过数
	ClientIP      string          `gorm:"size:50" json:"client_ip"`       // 操作客户端 IP
	Remark        string          `gorm:"type:text" json:"remark"`        // 审计备注
	CreatedAt     time.Time       `gorm:"index" json:"created_at"`
}

type TaskExecutionLog struct {
	ID             uint            `gorm:"primaryKey" json:"id"`
	ScheduleID     *uint           `gorm:"index" json:"schedule_id"`
	Schedule       *ScheduleConfig `gorm:"foreignKey:ScheduleID" json:"schedule"`
	TriggerLogID   *uint           `gorm:"index" json:"trigger_log_id"`
	TriggerLog     *TaskTriggerLog `gorm:"foreignKey:TriggerLogID" json:"trigger_log,omitempty"`
	RepoID         uint            `gorm:"index" json:"repo_id"`
	Repo           Repository      `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID   *uint           `gorm:"index" json:"task_report_id"`
	TaskReport     *TaskReport     `gorm:"foreignKey:TaskReportID" json:"task_report"`
	TaskTypeID     uint            `gorm:"index" json:"task_type_id"`
	TaskType       TaskType        `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	TriggerType    string          `gorm:"not null" json:"trigger_type"`                                  // "cron", "manual", "webhook"
	Status         string          `gorm:"default:pending;index" json:"status"`                           // "pending", "running", "success", "failed", "skipped"
	StatusPriority int             `gorm:"default:2;index:idx_status_priority_id" json:"status_priority"` // 1: running/analyzing, 2: pending, 3: completed/failed, 4: other
	ErrorMessage   string          `json:"error_message"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        *time.Time      `json:"end_time"`
	CreatedAt      time.Time       `gorm:"index" json:"created_at"`
}

// GetStatusPriority 返回状态排序优先级 (1=进行中, 2=待处理, 3=已结束, 4=其它)
func GetStatusPriority(status string) int {
	switch status {
	case "cloning", "pre_processing", "analyzing", "synthesis", "post_processing", "merging", "running":
		return 1
	case "pending":
		return 2
	case "success", "failed", "skipped":
		return 3
	default:
		return 4
	}
}

// CoredumpFinding 记录 "C/C++ Coredump 风险分析" (coredump_risk) 任务的扫描结果与跟踪
type CoredumpFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_repo_file_line_title;size:500;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_repo_file_line_title;size:255" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_repo_file_line_title;size:500;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:100;not null;index" json:"severity"` // 致命、严重、一般、建议
	Category     string         `gorm:"size:255;index" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50;index" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   *uint          `json:"assignee_id"`
	Assignee     *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// FloatFinding 记录 "Python 浮点数比较缺陷扫描" (float_comparison) 任务的扫描结果与跟踪
type FloatFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_float_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_float_repo_file_line_title;size:500;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_float_repo_file_line_title;size:255" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_float_repo_file_line_title;size:500;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:100;not null;index" json:"severity"` // 致命、严重、一般、建议
	Category     string         `gorm:"size:255;index" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50;index" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   *uint          `json:"assignee_id"`
	Assignee     *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// ThreadFinding 记录 "新建线程分析" (thread_create) 任务的扫描结果与跟踪
type ThreadFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_thread_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_thread_repo_file_line_title;size:500;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_thread_repo_file_line_title;size:255" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_thread_repo_file_line_title;size:500;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:100;not null;index" json:"severity"` // 合格、致命、严重、一般、建议
	Category     string         `gorm:"size:255;index" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50;index" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   *uint          `json:"assignee_id"`
	Assignee     *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// CjsonFinding 记录 "cJSON 内存泄漏扫描" (cjson_scan) 任务的扫描结果与跟踪
type CjsonFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_cjson_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_cjson_repo_file_line_title;size:500;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_cjson_repo_file_line_title;size:255" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_cjson_repo_file_line_title;size:500;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:100;not null;index" json:"severity"` // 致命、严重、一般、建议
	Category     string         `gorm:"size:255;index" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50;index" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   *uint          `json:"assignee_id"`
	Assignee     *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// UnorderedCollectionFinding 记录 "无序集合导出缺陷扫描" (unordered_collection_scan) 任务的扫描结果与跟踪
type UnorderedCollectionFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_unordered_col_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_unordered_col_repo_file_line_title;size:500;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_unordered_col_repo_file_line_title;size:255" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_unordered_col_repo_file_line_title;size:500;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:100;not null;index" json:"severity"` // 致命、严重、一般、建议
	Category     string         `gorm:"size:255;index" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50;index" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   *uint          `json:"assignee_id"`
	Assignee     *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// DeepReviewFinding 记录 "深度代码检视" (deep_review) 任务的扫描结果与跟踪
type DeepReviewFinding struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	RepoID       uint           `gorm:"uniqueIndex:idx_deep_repo_file_line_title;index" json:"repo_id"`
	Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint           `gorm:"index" json:"task_report_id"`
	FilePath     string         `gorm:"uniqueIndex:idx_deep_repo_file_line_title;size:500;not null" json:"file_path"`
	LineNumber   string         `gorm:"uniqueIndex:idx_deep_repo_file_line_title;size:255" json:"line_number"`
	Title        string         `gorm:"uniqueIndex:idx_deep_repo_file_line_title;size:500;not null" json:"title"`
	Detail       string         `gorm:"type:text" json:"detail"`
	Severity     string         `gorm:"size:100;not null;index" json:"severity"` // 致命、严重、一般、建议
	Category     string         `gorm:"size:255;index" json:"category"`
	CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
	Suggestion   string         `gorm:"type:text" json:"suggestion"`
	Status       string         `gorm:"default:'open';size:50;index" json:"status"` // open (待处理), analyzing (问题分析), resolved (已解决), closed (已关闭), invalid (忽略/误报)
	AssigneeID   *uint          `json:"assignee_id"`
	Assignee     *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog    datatypes.JSON `json:"status_log"` // 状态演进记录：[{"status":"open","time":"...","user":"xxx","comment":"xxx"}]
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}
