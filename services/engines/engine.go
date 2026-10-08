package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines/profile"
	"code-shield/services/invoker"
)

// ChunkDetails 记录单个分片的执行明细
type ChunkDetails struct {
	ChunkUID                 string                 `json:"chunk_uid,omitempty"`
	ChunkName                string                 `json:"chunk_name"`
	PrimaryUnitID            string                 `json:"primary_unit_id,omitempty"`
	StartTime                time.Time              `json:"start_time"`
	EndTime                  time.Time              `json:"end_time"`
	DurationSeconds          float64                `json:"duration_seconds"`
	Attempts                 int                    `json:"attempts"`
	Retries                  int                    `json:"retries"`
	ContractRepairs          int                    `json:"contract_repairs"`
	ResourceFailovers        int                    `json:"resource_failovers"`
	DriverFailovers          int                    `json:"driver_failovers"`
	ResourceChain            []string               `json:"resource_chain,omitempty"`
	ErrorClasses             []string               `json:"error_classes,omitempty"`
	QueueWaitMS              []int64                `json:"queue_wait_ms,omitempty"`
	AttemptDurationSeconds   []float64              `json:"attempt_duration_seconds,omitempty"`
	SplitDepth               int                    `json:"split_depth"`
	SplitCount               int                    `json:"split_count"`
	ArtifactComplete         *bool                  `json:"artifact_complete,omitempty"`
	ArtifactState            string                 `json:"artifact_state,omitempty"`
	ArtifactQualityDegraded  bool                   `json:"artifact_quality_degraded,omitempty"`
	UnresolvedIssueCount     int                    `json:"unresolved_issue_count,omitempty"`
	NormalizedIssueCount     int                    `json:"normalized_issue_count,omitempty"`
	SchemaRepairAttempts     int                    `json:"schema_repair_attempts,omitempty"`
	SchemaRepairSuccesses    int                    `json:"schema_repair_successes,omitempty"`
	JSONSyntaxRepairs        int                    `json:"json_syntax_repairs,omitempty"`
	RepairBaselineKnown      bool                   `json:"repair_baseline_known,omitempty"`
	RepairRepairedKnown      bool                   `json:"repair_repaired_signature_known,omitempty"`
	RepairUnverified         bool                   `json:"repair_unverified,omitempty"`
	RepairOutcome            string                 `json:"repair_outcome,omitempty"`
	CandidateQuarantineCount int                    `json:"candidate_quarantine_count,omitempty"`
	SchemaRepairIssues       []string               `json:"schema_repair_issues,omitempty"`
	ArtifactSchemaID         string                 `json:"artifact_schema_id,omitempty"`
	ArtifactSchemaHash       string                 `json:"artifact_schema_hash,omitempty"`
	ResponseFormatMode       string                 `json:"response_format_mode,omitempty"`
	ResponseFormatFallbacks  int                    `json:"response_format_fallbacks,omitempty"`
	Category                 CategoryStageMetrics   `json:"category,omitempty"`
	Status                   string                 `json:"status"` // "success" or "failed"
	ErrorMessage             string                 `json:"error_message,omitempty"`
	ErrorClass               string                 `json:"error_class,omitempty"`
	Files                    []string               `json:"files,omitempty"`
	PlannedFiles             []coverage.PlannedFile `json:"planned_files,omitempty"`
	Resumed                  bool                   `json:"resumed,omitempty"`
}

// EngineContext 纯内存化的引擎执行上下文，与持久层解耦
type EngineContext struct {
	Ctx                       context.Context
	ReportID                  uint
	RepoID                    uint
	RepoName                  string
	TaskTypeID                uint
	TaskTypeName              string
	TaskTypeKey               string              // 任务英文标识，如 "ut-effectiveness"
	EngineMode                string              // 引擎模式；当前只允许 debate_full
	AssessmentConfig          json.RawMessage     // assessment plugin configuration snapshot
	AssessmentProfile         string              // resolved assessment plugin registry key
	Profile                   profile.ScanProfile // 已校验的 scan profile 快照
	ChunkPolicyID             string
	TaskDir                   string                    // 任务文件目录，如 "tasks/ut-effectiveness"
	AnalysisPromptPath        string                    // 任务分析提示词文件绝对路径
	AnalysisPromptContent     string                    // immutable task prompt content; preferred over AnalysisPromptPath
	AnalysisPromptContentHash string                    // stable hash of AnalysisPromptContent
	AllowedCategories         []string                  // 受控标准分类白名单 (SSOT)
	Taxonomy                  models.CategoryTaxonomy   // frozen category taxonomy snapshot
	TaxonomyHash              string                    // stable hash shared with report/revision snapshot
	DomainFamily              string                    // 任务领域族群枚举
	DefenseDimensions         []models.DefenseDimension // 任务级专有抗辩维度 (优先级高于族群默认)
	TargetSemantics           models.TargetSemantics    // 任务评估对象语义快照
	DisplaySemantics          models.DisplaySemantics   // 任务展示语义快照
	CodesPath                 string
	ReportPath                string
	JSONPath                  string
	WorkDir                   string
	EngineConfig              json.RawMessage
	RunParams                 models.RunParams
	ScanPlan                  *coverage.ScanPlan
	PlanManifestHash          string
	PrimaryUnits              []coverage.PlanUnit
	ChangeBaseCommit          string
	ChangeHeadCommit          string
	DiffManifestHash          string
	Coverage                  *coverage.Coverage                                                                   // set by engines before synthesis
	NegativeRules             []string                                                                             // 预加载的免扫/负样本例外规则
	ProgressReport            func(total, processed, success int)                                                  // 进度回调，解耦直接操作 DB
	Invoker                   invoker.AIInvoker                                                                    // 算力分配后的调用驱动
	CategoryAliasRecorder     func(models.CategoryAliasUsage)                                                      // optional alias lifecycle recorder
	AIExecutor                func(fileList []string, customPromptSuffix, promptFilePath, outputPath string) error // 底层通用 AI 执行器
	AnalysisExecutor          func(fileList []string) ([]models.AnalysisFinding, error)                            // 单片/单仓分析执行器
	ChunkAnalysisExecutor     func(ChunkExecutionRequest) (ChunkExecutionResult, error)                            // 分片专用隔离执行器
}

// EngineResult 引擎纯内存计算与扫描输出结果
type EngineResult struct {
	Findings           []models.AnalysisFinding
	PlanReconciliation coverage.PlanReconciliation
	AssessmentRecords  []coverage.AssessmentRecord
	DebateLogs         []models.TaskDebateLog // 辩论日志纯内存返回，由 Runner 事务持久化
	SummaryChunks      []ChunkDetails
	HasFailedChunks    bool
	HunterTokens       int64
	Tier2Tokens        int64
	AnalysisMetrics    AnalysisStageMetrics
}

// AnalysisStageMetrics aggregates execution recovery facts. They are populated
// from per-chunk details so summary JSON and reports always use one source.
type AnalysisStageMetrics struct {
	Attempts                 int                  `json:"attempts"`
	Retries                  int                  `json:"retries"`
	ContractRepairs          int                  `json:"contract_repairs"`
	ResourceFailovers        int                  `json:"resource_failovers"`
	DriverFailovers          int                  `json:"driver_failovers"`
	QueueWaitMS              []int64              `json:"queue_wait_ms,omitempty"`
	AttemptDurationSeconds   []float64            `json:"attempt_duration_seconds,omitempty"`
	SplitInvocations         int                  `json:"split_invocations"`
	ResumedChunks            int                  `json:"resumed_chunks"`
	ArtifactComplete         bool                 `json:"artifact_complete"`
	ArtifactState            string               `json:"artifact_state,omitempty"`
	ArtifactQualityDegraded  bool                 `json:"artifact_quality_degraded,omitempty"`
	UnresolvedIssueCount     int                  `json:"unresolved_issue_count,omitempty"`
	NormalizedIssueCount     int                  `json:"normalized_issue_count"`
	SchemaRepairAttempts     int                  `json:"schema_repair_attempts"`
	SchemaRepairSuccesses    int                  `json:"schema_repair_successes"`
	JSONSyntaxRepairs        int                  `json:"json_syntax_repairs"`
	RepairBaselineKnown      bool                 `json:"repair_baseline_known"`
	RepairRepairedKnown      bool                 `json:"repair_repaired_signature_known"`
	RepairUnverified         bool                 `json:"repair_unverified,omitempty"`
	RepairOutcome            string               `json:"repair_outcome,omitempty"`
	CandidateQuarantineCount int                  `json:"candidate_quarantine_count"`
	ArtifactSchemaID         string               `json:"artifact_schema_id,omitempty"`
	ArtifactSchemaHash       string               `json:"artifact_schema_hash,omitempty"`
	ResponseFormatMode       string               `json:"response_format_mode,omitempty"`
	ResponseFormatFallbacks  int                  `json:"response_format_fallbacks"`
	Category                 CategoryStageMetrics `json:"category"`
}

type CategoryStageMetrics struct {
	Hunter CategoryHunterMetrics `json:"hunter"`
	Judge  CategoryJudgeMetrics  `json:"judge"`
}

func (metrics CategoryStageMetrics) Add(other CategoryStageMetrics) CategoryStageMetrics {
	metrics.Hunter.ModelCandidates += other.Hunter.ModelCandidates
	metrics.Hunter.ModelCodeValid += other.Hunter.ModelCodeValid
	metrics.Hunter.ModelCodeInvalid += other.Hunter.ModelCodeInvalid
	metrics.Hunter.ModelCodeAbsent += other.Hunter.ModelCodeAbsent
	metrics.Hunter.DeterministicRepairs += other.Hunter.DeterministicRepairs
	metrics.Hunter.LLMRepairAttempts += other.Hunter.LLMRepairAttempts
	metrics.Hunter.LLMRepairs += other.Hunter.LLMRepairs
	metrics.Hunter.ReviewRequired += other.Hunter.ReviewRequired
	metrics.Hunter.CategoryOnlyQuarantined += other.Hunter.CategoryOnlyQuarantined
	metrics.Hunter.ExtraFieldsIgnored += other.Hunter.ExtraFieldsIgnored
	metrics.Judge.ModelCandidates += other.Judge.ModelCandidates
	metrics.Judge.ModelCodeValid += other.Judge.ModelCodeValid
	metrics.Judge.ModelCodeInvalid += other.Judge.ModelCodeInvalid
	metrics.Judge.ModelCodeAbsent += other.Judge.ModelCodeAbsent
	metrics.Judge.DeterministicRepairs += other.Judge.DeterministicRepairs
	metrics.Judge.LLMRepairAttempts += other.Judge.LLMRepairAttempts
	metrics.Judge.LLMRepairs += other.Judge.LLMRepairs
	metrics.Judge.ReviewRequired += other.Judge.ReviewRequired
	metrics.Judge.Rejected += other.Judge.Rejected
	metrics.Judge.CategoryOnlyQuarantined += other.Judge.CategoryOnlyQuarantined
	metrics.Judge.ExtraFieldsIgnored += other.Judge.ExtraFieldsIgnored
	return metrics
}

type CategoryHunterMetrics struct {
	ModelCandidates         int `json:"model_candidates"`
	ModelCodeValid          int `json:"model_code_valid"`
	ModelCodeInvalid        int `json:"model_code_invalid"`
	ModelCodeAbsent         int `json:"model_code_absent"`
	DeterministicRepairs    int `json:"deterministic_repairs"`
	LLMRepairAttempts       int `json:"llm_repair_attempts"`
	LLMRepairs              int `json:"llm_repairs"`
	ReviewRequired          int `json:"review_required"`
	CategoryOnlyQuarantined int `json:"category_only_quarantined"`
	ExtraFieldsIgnored      int `json:"extra_fields_ignored"`
}

type CategoryJudgeMetrics struct {
	ModelCandidates         int `json:"model_candidates"`
	ModelCodeValid          int `json:"model_code_valid"`
	ModelCodeInvalid        int `json:"model_code_invalid"`
	ModelCodeAbsent         int `json:"model_code_absent"`
	DeterministicRepairs    int `json:"deterministic_repairs"`
	LLMRepairAttempts       int `json:"llm_repair_attempts"`
	LLMRepairs              int `json:"llm_repairs"`
	ReviewRequired          int `json:"review_required"`
	Rejected                int `json:"rejected"`
	CategoryOnlyQuarantined int `json:"category_only_quarantined"`
	ExtraFieldsIgnored      int `json:"extra_fields_ignored"`
}

func AggregateAnalysisMetrics(details []ChunkDetails) AnalysisStageMetrics {
	metrics := AnalysisStageMetrics{}
	metrics.RepairBaselineKnown = true
	metrics.RepairRepairedKnown = true
	metrics.ArtifactComplete, metrics.ArtifactState = AggregateArtifactComplete(details)
	for _, detail := range details {
		metrics.Attempts += detail.Attempts
		metrics.Retries += detail.Retries
		metrics.ContractRepairs += detail.ContractRepairs
		metrics.ResourceFailovers += detail.ResourceFailovers
		metrics.DriverFailovers += detail.DriverFailovers
		metrics.SplitInvocations += detail.splitInvocationCount()
		if detail.Resumed {
			metrics.ResumedChunks++
		}
		metrics.NormalizedIssueCount += detail.NormalizedIssueCount
		metrics.SchemaRepairAttempts += detail.SchemaRepairAttempts
		metrics.SchemaRepairSuccesses += detail.SchemaRepairSuccesses
		metrics.JSONSyntaxRepairs += detail.JSONSyntaxRepairs
		metrics.RepairBaselineKnown = metrics.RepairBaselineKnown && detail.RepairBaselineKnown
		metrics.RepairRepairedKnown = metrics.RepairRepairedKnown && detail.RepairRepairedKnown
		metrics.RepairUnverified = metrics.RepairUnverified || detail.RepairUnverified
		if detail.RepairOutcome != "" {
			metrics.RepairOutcome = detail.RepairOutcome
		}
		metrics.CandidateQuarantineCount += detail.CandidateQuarantineCount
		metrics.ResponseFormatFallbacks += detail.ResponseFormatFallbacks
		if detail.ArtifactSchemaID != "" {
			metrics.ArtifactSchemaID = detail.ArtifactSchemaID
		}
		if detail.ArtifactSchemaHash != "" {
			metrics.ArtifactSchemaHash = detail.ArtifactSchemaHash
		}
		if detail.ResponseFormatMode != "" {
			metrics.ResponseFormatMode = detail.ResponseFormatMode
		}
		metrics.Category = metrics.Category.Add(detail.Category)
	}
	// Quality metrics are independent from completeness. A missing legacy flag
	// must never be interpreted as an explicit false.
	for _, detail := range details {
		if detail.ArtifactQualityDegraded {
			metrics.ArtifactQualityDegraded = true
		}
		metrics.UnresolvedIssueCount += detail.UnresolvedIssueCount
	}
	if len(details) > 0 {
		return metrics
	}
	metrics.ArtifactComplete = true
	metrics.ArtifactState = "legacy_success"
	return metrics
}

func AggregateArtifactComplete(details []ChunkDetails) (bool, string) {
	hasKnown := false

	for _, detail := range details {
		if detail.ArtifactComplete == nil {
			continue
		}

		hasKnown = true
		if !*detail.ArtifactComplete {
			return false, "observed_incomplete"
		}
	}

	if hasKnown {
		return true, "observed_complete"
	}
	return true, "legacy_success"
}

func ArtifactCompletePtr(value bool) *bool {
	return &value
}

func (d ChunkDetails) splitInvocationCount() int {
	if d.SplitCount > 0 {
		return d.SplitCount
	}
	return d.SplitDepth
}

// ChunkExecutionRequest isolates one chunk invocation from sibling chunks.
type ChunkExecutionRequest struct {
	Index      int
	Name       string
	Files      []string
	OutputPath string
}

// ChunkExecutionResult carries retry metrics back to the engine. Attempts must
// reflect the actual number of business-level invocations, including success
// after retries.
type ChunkExecutionResult struct {
	Findings        []models.AnalysisFinding
	Attempts        int
	Retries         int
	ContractRepairs int
	DriverFailovers int
	OutputPath      string
	ErrorClass      string
	ChunkUID        string
	PrimaryUnitID   string

	ArtifactComplete         *bool
	ArtifactState            string
	ArtifactQualityDegraded  bool
	UnresolvedIssueCount     int
	NormalizedIssueCount     int
	SchemaRepairAttempts     int
	SchemaRepairSuccesses    int
	CandidateQuarantineCount int
}

// TaskEngine 定义所有扫描与对抗引擎的标准抽象接口
type TaskEngine interface {
	Name() string
	Run(ctx *EngineContext) (*EngineResult, error)
}

var (
	engineRegistryMu sync.RWMutex
	engineRegistry   = map[string]TaskEngine{}
)

// RegisterEngine 注册执行引擎实现
func RegisterEngine(mode string, engine TaskEngine) {
	engineRegistryMu.Lock()
	defer engineRegistryMu.Unlock()
	engineRegistry[mode] = engine
}

// GetEngine 获取已注册的执行引擎；未注册 mode 返回 nil，不允许隐式降级。
func GetEngine(mode string) TaskEngine {
	engineRegistryMu.RLock()
	defer engineRegistryMu.RUnlock()
	return engineRegistry[mode]
}

func EngineExists(mode string) bool {
	engineRegistryMu.RLock()
	defer engineRegistryMu.RUnlock()
	_, ok := engineRegistry[mode]
	return ok
}

func GetEngineStrict(mode string) (TaskEngine, error) {
	engineRegistryMu.RLock()
	defer engineRegistryMu.RUnlock()
	engine, ok := engineRegistry[mode]
	if !ok {
		return nil, fmt.Errorf("ENGINE_MODE_INVALID: engine mode %q is not registered", mode)
	}
	return engine, nil
}

// BuildScanCoverage derives an immutable coverage manifest from chunk details.
func BuildScanCoverage(ctx *EngineContext, details []ChunkDetails, plan *coverage.ScanPlan) *coverage.Coverage {
	chunks := make([]coverage.Chunk, 0, len(details))
	for _, detail := range details {
		chunks = append(chunks, coverage.Chunk{
			UID:          detail.ChunkName,
			Status:       detail.Status,
			Error:        detail.ErrorMessage,
			ErrorClass:   detail.ErrorClass,
			Files:        detail.Files,
			PlannedFiles: append([]coverage.PlannedFile(nil), detail.PlannedFiles...),
		})
	}
	if plan != nil {
		if len(plan.Excluded) > 0 {
			chunks = append(chunks, coverage.Chunk{
				UID:          "coverage-plan-excluded",
				Status:       coverage.StatusExcluded,
				PlannedFiles: plan.Excluded,
			})
		}
		if len(plan.UnchangedSkipped) > 0 {
			chunks = append(chunks, coverage.Chunk{
				UID:          "coverage-plan-unchanged-skipped",
				Status:       coverage.StatusUnchangedSkipped,
				PlannedFiles: plan.UnchangedSkipped,
			})
		}
		if len(plan.Unknown) > 0 {
			chunks = append(chunks, coverage.Chunk{
				UID:          "coverage-plan-unknown",
				Status:       coverage.StatusUnknown,
				PlannedFiles: plan.Unknown,
			})
		}
	}
	actual, commitVerified, worktreeClean, err := coverage.VerifyWorkspace(ctx.CodesPath, "")
	if err != nil {
		actual, commitVerified, worktreeClean = "", false, false
	}
	policyID := coverage.ChunkPolicyID(ctx.TaskTypeKey, ctx.EngineConfig)
	built := coverage.Build(ctx.CodesPath, actual, "v1", policyID, chunks)
	built.CommitVerified = commitVerified
	built.WorktreeClean = worktreeClean
	return &built
}
