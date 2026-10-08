package reports

import (
	"encoding/json"
	"strings"
	"time"

	"code-shield/services/coverage"
	"code-shield/services/engines/planner"
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
	ID                       uint                         `json:"id"`
	RepoID                   uint                         `json:"repo_id"`
	RepoName                 string                       `json:"repo_name"`
	RepoURL                  string                       `json:"repo_url"`
	Branch                   string                       `json:"branch"`
	TaskTypeID               uint                         `json:"task_type_id"`
	TaskTypeName             string                       `json:"task_type_name"`
	TaskTypeDisplay          string                       `json:"task_type_display"`
	EngineMode               string                       `json:"engine_mode"`
	ScopeDecision            *planner.ScopeDecision       `json:"scope_decision,omitempty"`
	PlanReconciliation       *coverage.PlanReconciliation `json:"plan_reconciliation,omitempty"`
	CoverageSummary          *coverage.ExecutionSummary   `json:"coverage_summary,omitempty"`
	ScanProfile              any                          `json:"scan_profile,omitempty"`
	ScanProfileHash          string                       `json:"scan_profile_hash,omitempty"`
	PromptVersion            string                       `json:"prompt_version,omitempty"`
	EngineConfigHash         string                       `json:"engine_config_hash,omitempty"`
	PlannerVersion           string                       `json:"planner_version,omitempty"`
	AssessmentConfigHash     string                       `json:"assessment_config_hash,omitempty"`
	PromptContentHash        string                       `json:"prompt_content_hash,omitempty"`
	CategorySchemaHash       string                       `json:"category_schema_hash,omitempty"`
	TaxonomySchemaVersion    int                          `json:"taxonomy_schema_version,omitempty"`
	TaxonomyHash             string                       `json:"taxonomy_hash,omitempty"`
	DomainFamily             string                       `json:"domain_family,omitempty"`
	DomainLabel              string                       `json:"domain_label,omitempty"`
	DefenseDimensions        json.RawMessage              `json:"defense_dimensions,omitempty"`
	TargetSemantics          json.RawMessage              `json:"target_semantics,omitempty"`
	DisplaySemantics         json.RawMessage              `json:"display_semantics,omitempty"`
	GovernanceMode           string                       `json:"governance_mode"`
	ExecutionSnapshotVersion int                          `json:"execution_snapshot_version,omitempty"`
	ExecutionSnapshotState   string                       `json:"execution_snapshot_state,omitempty"`
	Status                   string                       `json:"status"`
	Score                    int                          `json:"score"`
	Rating                   string                       `json:"rating"` // 优/良/中/差
	TotalChunks              int                          `json:"total_chunks"`
	ProcessedChunks          int                          `json:"processed_chunks"`
	SuccessChunks            int                          `json:"success_chunks"`
	CoverageNotApplicable    bool                         `json:"coverage_not_applicable"`
	CoverageComplete         bool                         `json:"coverage_complete"`
	CoverageDegraded         bool                         `json:"coverage_degraded"`
	CoverageReasons          []string                     `json:"coverage_reasons,omitempty"`
	ChangedCoverageFiles     []coverage.File              `json:"changed_coverage_files,omitempty"`
	DurationSeconds          float64                      `json:"duration_seconds"`
	BaseCommit               string                       `json:"base_commit"`
	HeadCommit               string                       `json:"head_commit"`

	// ── Token 统计 ──
	Tier1Tokens int64 `json:"tier1_tokens"`
	Tier2Tokens int64 `json:"tier2_tokens"`

	CreatedAt time.Time `json:"created_at"`
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
	AssessmentStats map[string]int `json:"assessment_stats,omitempty"`
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
	ID                      uint   `json:"id"`
	TaskReportID            uint   `json:"task_report_id"`
	TaskTypeID              uint   `json:"task_type_id"`
	RepoID                  uint   `json:"repo_id"`
	Severity                string `json:"severity"`         // Canonical severity
	SeverityDisplay         string `json:"severity_display"` // 中文展示
	Category                string `json:"category"`
	CategoryCode            string `json:"category_code,omitempty"`
	CategorySource          string `json:"category_source,omitempty"`
	CategoryStatus          string `json:"category_status,omitempty"`
	ClassificationRationale string `json:"classification_rationale,omitempty"`
	TaxonomyHash            string `json:"taxonomy_hash,omitempty"`
	FilePath                string `json:"file_path"`
	LineNumber              string `json:"line_number"`
	Title                   string `json:"title"`
	Detail                  string `json:"detail"`
	CodeSnippet             string `json:"code_snippet,omitempty"`
	Suggestion              string `json:"suggestion,omitempty"`
	Status                  string `json:"status"`
	StatusDisplay           string `json:"status_display"`
	AssigneeID              *uint  `json:"assignee_id,omitempty"`
	AssigneeName            string `json:"assignee_name,omitempty"`
	LatestComment           string `json:"latest_comment,omitempty"`

	// ── 缺陷指纹、增量状态与智能体辩论链 ──
	PrimaryUnitID       string                 `json:"primary_unit_id,omitempty"`
	AssessmentStatus    string                 `json:"assessment_status,omitempty"`
	AssessmentOutcome   string                 `json:"assessment_outcome,omitempty"`
	AssessmentArtifact  map[string]interface{} `json:"assessment_artifact,omitempty"`
	TriggerLine         string                 `json:"trigger_line,omitempty"`
	ScopeSymbol         string                 `json:"scope_symbol,omitempty"`
	HunterClaim         string                 `json:"hunter_claim,omitempty"`
	ChallengerArg       string                 `json:"challenger_arg,omitempty"`
	JudgeVerdict        string                 `json:"judge_verdict,omitempty"`
	ObservationGroupUID string                 `json:"observation_group_uid,omitempty"`
	DefectID            *uint                  `json:"defect_id,omitempty"`
	Verdict             string                 `json:"verdict,omitempty"`
	MatchTier           string                 `json:"match_tier,omitempty"`
	Confidence          float64                `json:"confidence,omitempty"`

	Note string `json:"note,omitempty"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
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
	ChunkName                string    `json:"chunk_name"`
	Status                   string    `json:"status"` // success, failed
	DurationSeconds          float64   `json:"duration_seconds"`
	Attempts                 int       `json:"attempts"`
	FilesCount               int       `json:"files_count"`
	FindingsCount            int       `json:"findings_count"`
	ErrorMessage             string    `json:"error_message,omitempty"`
	ErrorClass               string    `json:"error_class,omitempty"`
	ContractRepairs          int       `json:"contract_repairs"`
	ResourceFailovers        int       `json:"resource_failovers"`
	DriverFailovers          int       `json:"driver_failovers"`
	ResourceChain            []string  `json:"resource_chain,omitempty"`
	ErrorClasses             []string  `json:"error_classes,omitempty"`
	QueueWaitMS              []int64   `json:"queue_wait_ms,omitempty"`
	AttemptDurationSeconds   []float64 `json:"attempt_duration_seconds,omitempty"`
	SplitDepth               int       `json:"split_depth"`
	SplitCount               int       `json:"split_count"`
	Resumed                  bool      `json:"resumed,omitempty"`
	ArtifactComplete         bool      `json:"artifact_complete"`
	ArtifactState            string    `json:"artifact_state,omitempty"`
	ArtifactQualityDegraded  bool      `json:"artifact_quality_degraded,omitempty"`
	UnresolvedIssueCount     int       `json:"unresolved_issue_count,omitempty"`
	NormalizedIssueCount     int       `json:"normalized_issue_count"`
	SchemaRepairAttempts     int       `json:"schema_repair_attempts"`
	SchemaRepairSuccesses    int       `json:"schema_repair_successes"`
	JSONSyntaxRepairs        int       `json:"json_syntax_repairs"`
	RepairBaselineKnown      bool      `json:"repair_baseline_known,omitempty"`
	RepairRepairedKnown      bool      `json:"repair_repaired_signature_known,omitempty"`
	RepairUnverified         bool      `json:"repair_unverified,omitempty"`
	RepairOutcome            string    `json:"repair_outcome,omitempty"`
	CandidateQuarantineCount int       `json:"candidate_quarantine_count"`
	SchemaRepairIssues       []string  `json:"schema_repair_issues,omitempty"`
	ArtifactSchemaID         string    `json:"artifact_schema_id,omitempty"`
	ArtifactSchemaHash       string    `json:"artifact_schema_hash,omitempty"`
	ResponseFormatMode       string    `json:"response_format_mode,omitempty"`
	ResponseFormatFallbacks  int       `json:"response_format_fallbacks,omitempty"`
	Files                    []string  `json:"files,omitempty"`
}

// DiagnosticsDTO 运行轨迹与诊断 DTO
type DiagnosticsDTO struct {
	Meta                     ReportMetaDTO               `json:"meta"`
	PipelineSteps            []PipelineStep              `json:"pipeline_steps"`
	TotalDuration            float64                     `json:"total_duration"`
	AnalysisDuration         float64                     `json:"analysis_duration"`
	Attempts                 int                         `json:"attempts"`
	Retries                  int                         `json:"retries"`
	ContractRepairs          int                         `json:"contract_repairs"`
	ResourceFailovers        int                         `json:"resource_failovers"`
	DriverFailovers          int                         `json:"driver_failovers"`
	SplitInvocations         int                         `json:"split_invocations"`
	RecoveredChunks          int                         `json:"recovered_chunks"`
	ArtifactComplete         bool                        `json:"artifact_complete"`
	ArtifactState            string                      `json:"artifact_state,omitempty"`
	ArtifactQualityDegraded  bool                        `json:"artifact_quality_degraded,omitempty"`
	UnresolvedIssueCount     int                         `json:"unresolved_issue_count,omitempty"`
	NormalizedIssueCount     int                         `json:"normalized_issue_count"`
	SchemaRepairAttempts     int                         `json:"schema_repair_attempts"`
	SchemaRepairSuccesses    int                         `json:"schema_repair_successes"`
	JSONSyntaxRepairs        int                         `json:"json_syntax_repairs"`
	RepairBaselineKnown      bool                        `json:"repair_baseline_known"`
	RepairRepairedKnown      bool                        `json:"repair_repaired_signature_known"`
	RepairUnverified         bool                        `json:"repair_unverified,omitempty"`
	RepairOutcome            string                      `json:"repair_outcome,omitempty"`
	CandidateQuarantineCount int                         `json:"candidate_quarantine_count"`
	ArtifactSchemaID         string                      `json:"artifact_schema_id,omitempty"`
	ArtifactSchemaHash       string                      `json:"artifact_schema_hash,omitempty"`
	ResponseFormatMode       string                      `json:"response_format_mode,omitempty"`
	ResponseFormatFallbacks  int                         `json:"response_format_fallbacks"`
	Category                 *CategoryDiagnosticsSummary `json:"category,omitempty"`
	DegradedReasons          []string                    `json:"degraded_reasons,omitempty"`
	Chunks                   []ChunkDiagnosticDetail     `json:"chunks"`
	Synthesis                *SynthesisDiagnosticSummary `json:"synthesis,omitempty"`
	RawOutputLog             string                      `json:"raw_output_log"`
	LogTruncated             bool                        `json:"log_truncated"`
	TotalLogLines            int                         `json:"total_log_lines"`
	ErrorMessage             string                      `json:"error_message,omitempty"`
}

type SynthesisDiagnosticSummary struct {
	Status                 string    `json:"status,omitempty"`
	Attempts               int       `json:"attempts"`
	ResourceID             string    `json:"resource_id,omitempty"`
	ResourceFailovers      int       `json:"resource_failovers"`
	DriverFailovers        int       `json:"driver_failovers"`
	ResourceChain          []string  `json:"resource_chain,omitempty"`
	ErrorClasses           []string  `json:"error_classes,omitempty"`
	QueueWaitMS            []int64   `json:"queue_wait_ms,omitempty"`
	AttemptDurationSeconds []float64 `json:"attempt_duration_seconds,omitempty"`
	DurationSeconds        float64   `json:"duration_seconds"`
	ErrorMessage           string    `json:"error_message,omitempty"`
}

type CategoryDiagnosticsSummary struct {
	Hunter CategoryHunterDiagnostics `json:"hunter"`
	Judge  CategoryJudgeDiagnostics  `json:"judge"`
}

type CategoryHunterDiagnostics struct {
	ModelCandidates         int `json:"model_candidates"`
	ModelCodeValid          int `json:"model_code_valid"`
	ModelCodeInvalid        int `json:"model_code_invalid"`
	ModelCodeAbsent         int `json:"model_code_absent"`
	DeterministicRepairs    int `json:"deterministic_repairs"`
	LLMRepairAttempts       int `json:"llm_repair_attempts"`
	LLMRepairs              int `json:"llm_repairs"`
	ReviewRequired          int `json:"review_required"`
	CategoryOnlyQuarantined int `json:"category_only_quarantined"`
}

type CategoryJudgeDiagnostics struct {
	ModelCandidates         int `json:"model_candidates"`
	ModelCodeValid          int `json:"model_code_valid"`
	ModelCodeInvalid        int `json:"model_code_invalid"`
	ModelCodeAbsent         int `json:"model_code_absent"`
	ReviewRequired          int `json:"review_required"`
	CategoryOnlyQuarantined int `json:"category_only_quarantined"`
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
