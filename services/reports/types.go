package reports

import (
	"strings"
	"time"
)

// Canonical Severity 常量定义
const (
	SeverityFatal      = "fatal"
	SeverityCritical   = "critical"
	SeverityMajor      = "major"
	SeverityMinor      = "minor"
	SeveritySuggestion = "suggestion"
	SeverityPass       = "pass"
)

// NormalizeSeverity 归一化严重级别字符串
func NormalizeSeverity(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	switch s {
	case "fatal", "致命", "阻塞", "blocking", "p0":
		return SeverityFatal
	case "critical", "严重", "高风险", "high", "high_risk", "p1":
		return SeverityCritical
	case "major", "一般", "中风险", "medium", "主要", "p2":
		return SeverityMajor
	case "minor", "提示", "低风险", "low", "次要", "p3", "info":
		return SeverityMinor
	case "suggestion", "建议":
		return SeveritySuggestion
	case "pass", "合格", "通过":
		return SeverityPass
	default:
		if strings.Contains(s, "致命") || strings.Contains(s, "阻塞") {
			return SeverityFatal
		}
		if strings.Contains(s, "严重") || strings.Contains(s, "高") {
			return SeverityCritical
		}
		if strings.Contains(s, "一般") || strings.Contains(s, "中") {
			return SeverityMajor
		}
		if strings.Contains(s, "合格") || strings.Contains(s, "通过") {
			return SeverityPass
		}
		return SeverityMinor
	}
}

// GetSeverityChinese 返回规范化的中文展示
func GetSeverityChinese(sev string) string {
	switch NormalizeSeverity(sev) {
	case SeverityFatal:
		return "致命"
	case SeverityCritical:
		return "严重"
	case SeverityMajor:
		return "一般"
	case SeverityMinor:
		return "提示"
	case SeveritySuggestion:
		return "建议"
	case SeverityPass:
		return "合格"
	default:
		return "提示"
	}
}

// GetStatusChinese 返回流转状态的中文展示
func GetStatusChinese(status string, isEntityMode bool) string {
	switch strings.ToLower(status) {
	case "open":
		if isEntityMode {
			return "待复核"
		}
		return "待处理"
	case "analyzing":
		if isEntityMode {
			return "复核中"
		}
		return "问题分析"
	case "resolved":
		if isEntityMode {
			return "已整改"
		}
		return "已解决"
	case "closed":
		return "已关闭"
	case "pass":
		return "合格"
	case "fail":
		return "不合格"
	case "invalid":
		if isEntityMode {
			return "无效用例"
		}
		return "忽略/误报"
	default:
		return status
	}
}

// ReportMetaDTO 任务报告元数据
type ReportMetaDTO struct {
	ID              uint      `json:"id"`
	RepoID          uint      `json:"repo_id"`
	RepoName        string    `json:"repo_name"`
	RepoURL         string    `json:"repo_url"`
	Branch          string    `json:"branch"`
	TaskTypeID      uint      `json:"task_type_id"`
	TaskTypeName    string    `json:"task_type_name"`
	TaskTypeDisplay string    `json:"task_type_display"`
	EngineMode      string    `json:"engine_mode"`
	GovernanceMode  string    `json:"governance_mode"`
	Status          string    `json:"status"`
	Score           int       `json:"score"`
	Rating          string    `json:"rating"` // 优/良/中/差
	TotalChunks     int       `json:"total_chunks"`
	ProcessedChunks int       `json:"processed_chunks"`
	SuccessChunks   int       `json:"success_chunks"`
	DurationSeconds float64   `json:"duration_seconds"`
	BaseCommit      string    `json:"base_commit"`
	HeadCommit      string    `json:"head_commit"`
	CreatedAt       time.Time `json:"created_at"`
}

// KPIMetrics 统计指标
type KPIMetrics struct {
	TotalFindings   int            `json:"total_findings"`
	FatalCount      int            `json:"fatal_count"`
	CriticalCount   int            `json:"critical_count"`
	MajorCount      int            `json:"major_count"`
	MinorCount      int            `json:"minor_count"`
	SuggestionCount int            `json:"suggestion_count"`
	PassCount       int            `json:"pass_count"`
	PassRate        float64        `json:"pass_rate,omitempty"`
	CategoryStats   map[string]int `json:"category_stats"`
	StatusStats     map[string]int `json:"status_stats"`
}

// ReportSummaryDTO 总结概览 DTO
type ReportSummaryDTO struct {
	Meta               ReportMetaDTO `json:"meta"`
	MarkdownContent    string        `json:"markdown_content"`
	Metrics            KPIMetrics    `json:"metrics"`
	KeyRecommendations []string      `json:"key_recommendations,omitempty"`
}

// FindingItemDTO 结构化问题/实体项 DTO
type FindingItemDTO struct {
	ID              uint       `json:"id"`
	TaskReportID    uint       `json:"task_report_id"`
	TaskTypeID      uint       `json:"task_type_id"`
	RepoID          uint       `json:"repo_id"`
	Severity        string     `json:"severity"`         // Canonical severity
	SeverityDisplay string     `json:"severity_display"` // 中文展示
	Category        string     `json:"category"`
	FilePath        string     `json:"file_path"`
	LineNumber      string     `json:"line_number"`
	Title           string     `json:"title"`
	Detail          string     `json:"detail"`
	CodeSnippet     string     `json:"code_snippet,omitempty"`
	Suggestion      string     `json:"suggestion,omitempty"`
	Status          string     `json:"status"`
	StatusDisplay   string     `json:"status_display"`
	AssigneeID      *uint      `json:"assignee_id,omitempty"`
	AssigneeName    string     `json:"assignee_name,omitempty"`
	LatestComment   string     `json:"latest_comment,omitempty"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
}

// FindingsPageDTO 清单分页 DTO
type FindingsPageDTO struct {
	Items      []FindingItemDTO `json:"items"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
	TotalPages int              `json:"totalPages"`
	Metrics    KPIMetrics       `json:"metrics"`
}

// PipelineStep 时序流单步
type PipelineStep struct {
	Name            string  `json:"name"`
	Status          string  `json:"status"` // success, failed, running, skipped
	DurationSeconds float64 `json:"duration_seconds"`
}

// ChunkDiagnosticDetail 分片诊断信息
type ChunkDiagnosticDetail struct {
	ChunkName       string   `json:"chunk_name"`
	Status          string   `json:"status"` // success, failed
	DurationSeconds float64  `json:"duration_seconds"`
	Attempts        int      `json:"attempts"`
	FilesCount      int      `json:"files_count"`
	FindingsCount   int      `json:"findings_count"`
	ErrorMessage    string   `json:"error_message,omitempty"`
	Files           []string `json:"files,omitempty"`
}

// DiagnosticsDTO 运行轨迹与诊断 DTO
type DiagnosticsDTO struct {
	Meta             ReportMetaDTO           `json:"meta"`
	PipelineSteps    []PipelineStep          `json:"pipeline_steps"`
	TotalDuration    float64                 `json:"total_duration"`
	AnalysisDuration float64                 `json:"analysis_duration"`
	Chunks           []ChunkDiagnosticDetail `json:"chunks"`
	RawOutputLog     string                  `json:"raw_output_log"`
	LogTruncated     bool                    `json:"log_truncated"`
	TotalLogLines    int                     `json:"total_log_lines"`
	ErrorMessage     string                  `json:"error_message,omitempty"`
}

// ReportAggregateDTO 全量聚合 DTO
type ReportAggregateDTO struct {
	Meta        ReportMetaDTO    `json:"meta"`
	Summary     ReportSummaryDTO `json:"summary"`
	Findings    []FindingItemDTO `json:"findings"`
	Diagnostics DiagnosticsDTO   `json:"diagnostics"`
}

// FindingsQuery 详细清单查询参数
type FindingsQuery struct {
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
	Severity   string `json:"severity"`
	Status     string `json:"status"`
	Category   string `json:"category"`
	Keyword    string `json:"keyword"`
	SortField  string `json:"sort_field"`
	SortOrder  string `json:"sort_order"`
	AssigneeID string `json:"assignee_id"`
}
