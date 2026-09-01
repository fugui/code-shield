package models

import (
	commonModels "code-common/backend/models"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type User = commonModels.User
type Department = commonModels.Department
type SysAuditLog = commonModels.SysAuditLog
type AuditLevel = commonModels.AuditLevel

const (
	AuditLevelP0 = commonModels.AuditLevelP0
	AuditLevelP1 = commonModels.AuditLevelP1
	AuditLevelP2 = commonModels.AuditLevelP2
)

// ── GovernanceMode 枚举常量 ──
const (
	GovernanceModeDefectTracking   = "defect_tracking"   // 缺陷攻关模式
	GovernanceModeEntityAssessment = "entity_assessment" // 全量实体评估模式
)

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
	AIBackend       string         `gorm:"default:''" json:"ai_backend"`           // AI 后端: 为空时使用全局配置，可选 claude/opencode/codex
	TargetScope     string         `gorm:"default:'business'" json:"target_scope"` // 处理范围: all (全部), business (仅业务), test (仅测试)
	NotifyTemplate  string         `json:"notify_template"`                        // 邮件主题模板
	NotifyThreshold int            `gorm:"default:0" json:"notify_threshold"`      // score >= 此值才通知
	NotifyCc        datatypes.JSON `json:"notify_cc"`                              // 通知抄送邮箱列表 ["a@x.com","b@x.com"]
	Timeout         int            `gorm:"default:30" json:"timeout"`              // AI 执行超时（分钟）

	// ── 智能体协同与异构调度扩展 (阶段二) ──
	DebateEnabled        bool   `gorm:"default:true" json:"debate_enabled"`               // 是否启用三方对抗辩论流
	TierFastBackend      string `gorm:"size:64;default:''" json:"tier_fast_backend"`      // 指定 Tier 1 初筛后端 (空则遵循全局路由)
	TierReasoningBackend string `gorm:"size:64;default:''" json:"tier_reasoning_backend"` // 指定 Tier 2 强推理后端 (空则遵循全局路由)

	// ── 专项分析元数据扩展 ──
	IsCampaign     bool           `gorm:"default:false;index" json:"is_campaign"`                   // 是否启用为专项分析
	CampaignPath   string         `gorm:"size:100;default:''" json:"campaign_path"`                 // 路由别名 (空则默认同 Name)
	GovernanceMode string         `gorm:"size:50;default:'defect_tracking'" json:"governance_mode"` // defect_tracking / entity_assessment
	CampaignIcon   string         `gorm:"type:text" json:"campaign_icon"`                           // SVG 图标路径或图标类名
	CampaignConfig datatypes.JSON `json:"campaign_config"`                                          // 高级配置，结构定义见 CampaignConfigSchema

	IsActive  bool      `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CampaignConfigSchema 高级专项配置 Schema
type CampaignConfigSchema struct {
	Version           int               `json:"version"`              // Schema 版本号，当前为 1
	SeverityFilter    []string          `json:"severity_filter"`      // 需要展示的严重等级列表 (空=全部展示)
	NotifyOnNewDefect bool              `json:"notify_on_new_defect"` // 新缺陷入库时是否触发通知
	CustomLabels      map[string]string `json:"custom_labels"`        // 看板自定义标签 (如 {"metric": "合格率"})
}

// 模型写入校验 Hook
func (t *TaskType) BeforeCreate(tx *gorm.DB) error { return t.validate() }
func (t *TaskType) BeforeUpdate(tx *gorm.DB) error { return t.validate() }

func (t *TaskType) validate() error {
	if t.IsCampaign {
		if t.GovernanceMode == "" {
			t.GovernanceMode = GovernanceModeDefectTracking
		}
		switch t.GovernanceMode {
		case GovernanceModeDefectTracking, GovernanceModeEntityAssessment:
			// valid
		default:
			return fmt.Errorf("invalid governance_mode: %q, must be %q or %q",
				t.GovernanceMode, GovernanceModeDefectTracking, GovernanceModeEntityAssessment)
		}
		if len(t.CampaignConfig) > 0 {
			var cfg CampaignConfigSchema
			if err := json.Unmarshal(t.CampaignConfig, &cfg); err != nil {
				return fmt.Errorf("invalid campaign_config JSON: %w", err)
			}
		}
	}
	return nil
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

// TaskReport 通用任务报告
type TaskReport struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	RepoID          uint           `gorm:"index" json:"repo_id"`
	Repo            Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskTypeID      uint           `gorm:"index:idx_task_reports_type_status_created,priority:1;index" json:"task_type_id"`
	TaskType        TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	ParentID        uint           `gorm:"default:0;index" json:"parent_id"` // 0 if it is a parent or independent task
	ChunkName       string         `gorm:"default:''" json:"chunk_name"`     // Name of the directory or file group
	TotalChunks     int            `gorm:"default:0" json:"total_chunks"`
	ProcessedChunks int            `gorm:"default:0" json:"processed_chunks"`
	SuccessChunks   int            `gorm:"default:0" json:"success_chunks"`
	Status          string         `gorm:"default:pending;index:idx_task_reports_type_status_created,priority:2;index" json:"status"` // pending, queued, cloning, pre_processing, analyzing, post_processing, success, failed, skipped
	CloneStatus     string         `gorm:"default:pending" json:"clone_status"`
	AISummary       string         `json:"ai_summary"`
	ReportPath      string         `json:"report_path"`
	Score           int            `gorm:"default:0" json:"score"`
	Metrics         datatypes.JSON `json:"metrics"` // {"blocking":0,"critical":3,...}
	BaseCommit      string         `json:"base_commit"`
	HeadCommit      string         `json:"head_commit"`

	// ── 增量追踪与 Token 消耗统计 (阶段二/三) ──
	NewDefectsCount      int   `gorm:"default:0" json:"new_defects_count"`      // 本次新增缺陷数
	ExistedDefectsCount  int   `gorm:"default:0" json:"existed_defects_count"`  // 历史存量缺陷数
	ResolvedDefectsCount int   `gorm:"default:0" json:"resolved_defects_count"` // 本次已修复缺陷数
	Tier1Tokens          int64 `gorm:"default:0" json:"tier1_tokens"`           // Tier 1 快模型 Token 开销
	Tier2Tokens          int64 `gorm:"default:0" json:"tier2_tokens"`           // Tier 2 强推理模型 Token 开销

	CreatedAt time.Time `gorm:"index:idx_task_reports_type_status_created,priority:3;index" json:"created_at"`
}

// GetAbsReportPath 返回报告文件的绝对路径（如果存储的是相对路径，则使用 server.data_dir / storage.root 拼接）
func (r *TaskReport) GetAbsReportPath() string {
	if r.ReportPath == "" {
		return ""
	}
	if filepath.IsAbs(r.ReportPath) {
		// 1. 如果原始绝对路径文件存在，直接使用
		if _, err := os.Stat(r.ReportPath); err == nil {
			return r.ReportPath
		}
		// 2. 否则，如果路径中包含 "reports/"，截取相对路径并与当前 AppConfig.GetDataDir() 拼接做兼容性容错
		if idx := strings.Index(r.ReportPath, "reports/"); idx != -1 {
			relPath := r.ReportPath[idx:]
			absPath := filepath.Join(AppConfig.GetDataDir(), relPath)
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
		return r.ReportPath
	}
	return filepath.Join(AppConfig.GetDataDir(), r.ReportPath)
}

// GetReportDir 返回任务专属的报告存储目录
func (r *TaskReport) GetReportDir() string {
	absReport := r.GetAbsReportPath()
	if absReport != "" {
		return filepath.Dir(absReport)
	}
	return filepath.Join(AppConfig.GetDataDir(), "reports")
}

// GetSynthesisJSONPath 返回 Synthesis Findings JSON 文件路径（内建历史命名兼容）
func (r *TaskReport) GetSynthesisJSONPath() string {
	dir := r.GetReportDir()
	// 1. 标准命名
	p1 := filepath.Join(dir, "findings.json")
	if _, err := os.Stat(p1); err == nil {
		return p1
	}
	// 2. 规范命名 report-{id}-synthesis-{safeRepo}.json
	safeRepo := strings.ReplaceAll(r.Repo.Name, "/", "-")
	return filepath.Join(dir, fmt.Sprintf("report-%d-synthesis-%s.json", r.ID, safeRepo))
}

// GetSummaryJSONPath 返回 Summary Diagnostics JSON 文件路径（100% 确定性寻址，彻底消除 Glob）
func (r *TaskReport) GetSummaryJSONPath() string {
	dir := r.GetReportDir()
	// 1. 标准命名
	p1 := filepath.Join(dir, "diagnostics.json")
	if _, err := os.Stat(p1); err == nil {
		return p1
	}
	// 2. 规范命名 report-{id}-summary-{safeRepo}.json
	safeRepo := strings.ReplaceAll(r.Repo.Name, "/", "-")
	return filepath.Join(dir, fmt.Sprintf("report-%d-summary-%s.json", r.ID, safeRepo))
}

// GetExecutionLogPath 返回任务 AI 执行输出日志路径
func (r *TaskReport) GetExecutionLogPath() string {
	dir := r.GetReportDir()
	p1 := filepath.Join(dir, "execution.log")
	if _, err := os.Stat(p1); err == nil {
		return p1
	}
	absReport := r.GetAbsReportPath()
	if absReport != "" {
		p2 := absReport + ".output.txt"
		if _, err := os.Stat(p2); err == nil {
			return p2
		}
	}
	return p1
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

	// ── 智能体辩论与增量记忆扩展 (阶段二/三) ──
	Fingerprint     string `gorm:"size:64;index" json:"fingerprint"`               // 64位缺陷指纹 SHA-256
	DiffStatus      string `gorm:"size:32;default:'NEW';index" json:"diff_status"` // NEW / EXISTED / RESOLVED / REOPENED
	TriggerLine     string `gorm:"type:text" json:"trigger_line"`                  // 核心触发行（抗漂移指纹计算基准）
	ScopeSymbol     string `gorm:"size:256" json:"scope_symbol"`                   // AST 作用域符号（函数名/类名签名）
	HunterClaim     string `gorm:"type:text" json:"hunter_claim"`                  // 猎手初筛主张
	ChallengerArg   string `gorm:"type:text" json:"challenger_arg"`                // 辩护人抗辩意见
	JudgeVerdict    string `gorm:"type:text" json:"judge_verdict"`                 // 终审法官裁决
	CalibrationRule string `gorm:"size:128" json:"calibration_rule"`               // 命中的严重度校准规则

	CreatedAt time.Time `json:"created_at"`
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
	QueuePaused      bool       `gorm:"default:false" json:"queue_paused"`
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
	IsResume       bool            `gorm:"default:false" json:"is_resume"`                                // 是否为分片失败恢复任务（worker 走 ResumeFailedChunks）
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

// CampaignFinding 统一专项分析缺陷与实体评估记录模型（替代原 7 张独立分表）
type CampaignFinding struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	TaskTypeID   uint       `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:1;index:idx_camp_finding_repo_status,priority:2;index;not null" json:"task_type_id"`
	TaskType     TaskType   `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	RepoID       uint       `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:2;index:idx_camp_finding_repo_status,priority:1;index;not null" json:"repo_id"`
	Repo         Repository `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint       `gorm:"index" json:"task_report_id"`

	// 缺陷/实体本身属性
	FilePath    string `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:3;size:500;not null" json:"file_path"`
	LineNumber  string `gorm:"size:255" json:"line_number"`
	Title       string `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:4;size:500;not null" json:"title"` // 普通专项存缺陷标题，UT存测试用例名称
	Detail      string `gorm:"type:text" json:"detail"`
	Severity    string `gorm:"size:100;not null;index" json:"severity"` // 致命/严重/一般/建议/合格
	Category    string `gorm:"size:255;index" json:"category"`
	CodeSnippet string `gorm:"type:text" json:"code_snippet"`
	Suggestion  string `gorm:"type:text" json:"suggestion"`

	// 治理状态与审计跟踪
	Status     string         `gorm:"default:'open';size:50;index:idx_camp_finding_repo_status,priority:3;index" json:"status"` // open, analyzing, resolved, closed, invalid
	AssigneeID *uint          `json:"assignee_id"`
	Assignee   *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog  datatypes.JSON `json:"status_log"` // [{"status":"open","time":"...","user":"xxx","reason":"..."}]
	Feedback   string         `gorm:"type:text" json:"feedback"`
	CreatedAt  time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// ── 增量比对状态常量 (DiffStatus) ──
const (
	DiffStatusNew      = "NEW"      // 本次新增
	DiffStatusExisted  = "EXISTED"  // 历史存量
	DiffStatusResolved = "RESOLVED" // 本次已修复
	DiffStatusReopened = "REOPENED" // 历史缺陷复发
)

// ── 智能体辩论结论常量 (DebateVerdict) ──
const (
	DebateVerdictConfirmed   = "CONFIRMED"   // 确认存在
	DebateVerdictRejected    = "REJECTED"    // 判定误报
	DebateVerdictConditional = "CONDITIONAL" // 条件触发
)

// DefectFingerprintRecord 缺陷指纹持久化记录表 (SSOT 唯一真值中心)
type DefectFingerprintRecord struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	RepoID      uint   `gorm:"uniqueIndex:idx_repo_task_fp,priority:1;index;not null" json:"repo_id"`
	TaskTypeID  uint   `gorm:"uniqueIndex:idx_repo_task_fp,priority:2;index;not null" json:"task_type_id"`
	Fingerprint string `gorm:"uniqueIndex:idx_repo_task_fp,priority:3;size:64;not null" json:"fingerprint"` // SHA-256
	FilePath    string `gorm:"size:512;not null" json:"file_path"`
	ScopeSymbol string `gorm:"size:256" json:"scope_symbol"` // 函数/类/方法签名
	Category    string `gorm:"size:255" json:"category"`
	Status      string `gorm:"size:32;default:'ACTIVE';index" json:"status"` // ACTIVE, RESOLVED

	// 人工反馈状态
	FeedbackStatus string     `gorm:"size:32;default:'UNREVIEWED';index" json:"feedback_status"` // UNREVIEWED, FALSE_POSITIVE, WONT_FIX, CONFIRMED
	FeedbackReason string     `gorm:"type:text" json:"feedback_reason"`
	FeedbackUserID *uint      `json:"feedback_user_id"`
	FeedbackUser   *User      `gorm:"foreignKey:FeedbackUserID" json:"feedback_user,omitempty"`
	FeedbackAt     *time.Time `json:"feedback_at"`

	FirstTaskID uint           `json:"first_task_id"` // 引入该缺陷的任务 ID
	LastTaskID  uint           `json:"last_task_id"`  // 最近检出该缺陷的任务 ID
	FirstSeenAt time.Time      `json:"first_seen_at"`
	LastSeenAt  time.Time      `json:"last_seen_at"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TaskDebateLog 智能体三方对抗辩论轨迹表 (支持 TTL 自动清理与合规审计)
type TaskDebateLog struct {
	ID               uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskReportID     uint           `gorm:"index;not null" json:"task_report_id"`
	ChunkName        string         `gorm:"size:256;not null" json:"chunk_name"`
	CandidateID      string         `gorm:"size:64;not null" json:"candidate_id"`
	TriggerLine      string         `gorm:"type:text" json:"trigger_line"`
	HunterOutput     datatypes.JSON `gorm:"type:jsonb" json:"hunter_output"`
	ChallengerOutput datatypes.JSON `gorm:"type:jsonb" json:"challenger_output"`
	JudgeOutput      datatypes.JSON `gorm:"type:jsonb;not null" json:"judge_output"`
	Verdict          string         `gorm:"size:32;not null;index" json:"verdict"` // CONFIRMED, REJECTED, CONDITIONAL
	DurationMs       int            `json:"duration_ms"`
	TokenUsage       datatypes.JSON `gorm:"type:jsonb" json:"token_usage"` // {"hunter": 1200, "challenger": 800, "judge": 950}
	CreatedAt        time.Time      `gorm:"index" json:"created_at"`
}

// RepoFeedbackRule 代码仓人机反馈例外规则库表 (负样本沉淀知识库)
type RepoFeedbackRule struct {
	ID         uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	RepoID     uint       `gorm:"index:idx_fb_repo_task;not null" json:"repo_id"`
	Repo       Repository `gorm:"foreignKey:RepoID" json:"repo"`
	TaskTypeID uint       `gorm:"index:idx_fb_repo_task;not null" json:"task_type_id"`
	TaskType   TaskType   `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	ScopeType  string     `gorm:"size:32;default:'FILE'" json:"scope_type"`    // FILE (单文件), REPO (全仓), GLOBAL (全局)
	Pattern    string     `gorm:"size:512;not null" json:"pattern"`            // 文件路径正则或符号通配符
	RuleAction string     `gorm:"size:32;default:'IGNORE'" json:"rule_action"` // IGNORE (忽略), DOWNGRADE (降级)
	Reason     string     `gorm:"type:text;not null" json:"reason"`            // 豁免理由
	CreatedBy  string     `gorm:"size:64" json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
