package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"encoding/json"
	"sync"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/dispatcher"
	"code-shield/services/engines"
	"code-shield/services/engines/planner"
	"code-shield/services/invoker"
)

// GetAIInvoker 根据名称获取经由调度器加权与租约管理的包装 AI 驱动实例
func GetAIInvoker(name string) invoker.AIInvoker {
	inv, ok := invoker.GetRawInvoker(name)
	if !ok || inv == nil {
		inv, _ = invoker.GetRawInvoker("claude")
	}
	return dispatcher.WrapInvoker(inv)
}

// ChunkDetails 记录单个分片（或单仓全量分析）的运行明细
type ChunkDetails struct {
	ChunkUID                 string    `json:"chunk_uid,omitempty"`
	ChunkName                string    `json:"chunk_name"`
	PrimaryUnitID            string    `json:"primary_unit_id,omitempty"`
	Files                    []string  `json:"files,omitempty"`
	Status                   string    `json:"status"` // "success" or "failed"
	Attempts                 int       `json:"attempts"`
	Retries                  int       `json:"retries"`
	ContractRepairs          int       `json:"contract_repairs"`
	ResourceFailovers        int       `json:"resource_failovers"`
	DriverFailovers          int       `json:"driver_failovers"`
	ResourceChain            []string  `json:"resource_chain,omitempty"`
	StageChain               []string  `json:"stage_chain,omitempty"`
	ErrorClasses             []string  `json:"error_classes,omitempty"`
	QueueWaitMS              []int64   `json:"queue_wait_ms,omitempty"`
	AttemptDurationSeconds   []float64 `json:"attempt_duration_seconds,omitempty"`
	SplitDepth               int       `json:"split_depth"`
	SplitCount               int       `json:"split_count"`
	ArtifactComplete         *bool     `json:"artifact_complete,omitempty"`
	ArtifactState            string    `json:"artifact_state,omitempty"`
	ArtifactQualityDegraded  bool      `json:"artifact_quality_degraded,omitempty"`
	UnresolvedIssueCount     int       `json:"unresolved_issue_count,omitempty"`
	NormalizedIssueCount     int       `json:"normalized_issue_count,omitempty"`
	SchemaRepairAttempts     int       `json:"schema_repair_attempts,omitempty"`
	SchemaRepairSuccesses    int       `json:"schema_repair_successes,omitempty"`
	JSONSyntaxRepairs        int       `json:"json_syntax_repairs,omitempty"`
	RepairBaselineKnown      bool      `json:"repair_baseline_known,omitempty"`
	RepairRepairedKnown      bool      `json:"repair_repaired_signature_known,omitempty"`
	RepairUnverified         bool      `json:"repair_unverified,omitempty"`
	RepairOutcome            string    `json:"repair_outcome,omitempty"`
	CandidateQuarantineCount int       `json:"candidate_quarantine_count,omitempty"`
	SchemaRepairIssues       []string  `json:"schema_repair_issues,omitempty"`
	ArtifactSchemaID         string    `json:"artifact_schema_id,omitempty"`
	ArtifactSchemaHash       string    `json:"artifact_schema_hash,omitempty"`
	ResponseFormatMode       string    `json:"response_format_mode,omitempty"`
	ResponseFormatFallbacks  int       `json:"response_format_fallbacks,omitempty"`
	StartTime                time.Time `json:"start_time"`
	EndTime                  time.Time `json:"end_time"`
	DurationSeconds          float64   `json:"duration_seconds"`
	ErrorMessage             string    `json:"error_message,omitempty"`
	ErrorClass               string    `json:"error_class,omitempty"`
	Resumed                  bool      `json:"resumed,omitempty"`
}

// AnalysisSummary 静态分析阶段的统计数据与明细
type AnalysisSummary struct {
	Status                   string                        `json:"status"` // "success" or "failed"
	StartTime                time.Time                     `json:"start_time"`
	EndTime                  time.Time                     `json:"end_time"`
	DurationSeconds          float64                       `json:"duration_seconds"`
	TotalChunks              int                           `json:"total_chunks"`
	SuccessChunks            int                           `json:"success_chunks"`
	FailedChunks             int                           `json:"failed_chunks"`
	TotalFindings            int                           `json:"total_findings"`
	Attempts                 int                           `json:"attempts"`
	Retries                  int                           `json:"retries"`
	ContractRepairs          int                           `json:"contract_repairs"`
	ResourceFailovers        int                           `json:"resource_failovers"`
	DriverFailovers          int                           `json:"driver_failovers"`
	SplitInvocations         int                           `json:"split_invocations"`
	RecoveredChunks          int                           `json:"recovered_chunks"`
	ArtifactComplete         bool                          `json:"artifact_complete"`
	ArtifactState            string                        `json:"artifact_state,omitempty"`
	ArtifactQualityDegraded  bool                          `json:"artifact_quality_degraded,omitempty"`
	UnresolvedIssueCount     int                           `json:"unresolved_issue_count,omitempty"`
	NormalizedIssueCount     int                           `json:"normalized_issue_count"`
	SchemaRepairAttempts     int                           `json:"schema_repair_attempts"`
	SchemaRepairSuccesses    int                           `json:"schema_repair_successes"`
	JSONSyntaxRepairs        int                           `json:"json_syntax_repairs"`
	RepairBaselineKnown      bool                          `json:"repair_baseline_known"`
	RepairRepairedKnown      bool                          `json:"repair_repaired_signature_known"`
	RepairUnverified         bool                          `json:"repair_unverified,omitempty"`
	RepairOutcome            string                        `json:"repair_outcome,omitempty"`
	CandidateQuarantineCount int                           `json:"candidate_quarantine_count"`
	ArtifactSchemaID         string                        `json:"artifact_schema_id,omitempty"`
	ArtifactSchemaHash       string                        `json:"artifact_schema_hash,omitempty"`
	ResponseFormatMode       string                        `json:"response_format_mode,omitempty"`
	ResponseFormatFallbacks  int                           `json:"response_format_fallbacks"`
	Category                 *engines.CategoryStageMetrics `json:"category,omitempty"`
	Chunks                   []ChunkDetails                `json:"chunks"`
}

// SynthesisSummary 报告综合生成阶段的统计数据
type SynthesisSummary struct {
	Status                 string    `json:"status"` // "success" or "failed"
	Attempts               int       `json:"attempts"`
	ResourceID             string    `json:"resource_id,omitempty"`
	ResourceFailovers      int       `json:"resource_failovers"`
	DriverFailovers        int       `json:"driver_failovers"`
	ResourceChain          []string  `json:"resource_chain,omitempty"`
	StageChain             []string  `json:"stage_chain,omitempty"`
	ErrorClasses           []string  `json:"error_classes,omitempty"`
	QueueWaitMS            []int64   `json:"queue_wait_ms,omitempty"`
	AttemptDurationSeconds []float64 `json:"attempt_duration_seconds,omitempty"`
	StartTime              time.Time `json:"start_time"`
	EndTime                time.Time `json:"end_time"`
	DurationSeconds        float64   `json:"duration_seconds"`
	ErrorMessage           string    `json:"error_message,omitempty"`
}

// MergingSummary 专项治理归并阶段的运行耗时与状态
type MergingSummary struct {
	Status          string    `json:"status"` // "success" or "failed" or "active"
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// TaskSummaryReport 定义单次扫描任务的结构化汇总度量快照
type TaskSummaryReport struct {
	TaskID             uint                         `json:"task_id"`
	RepoName           string                       `json:"repo_name"`
	TaskType           string                       `json:"task_type"`
	EngineMode         string                       `json:"engine_mode"`
	ScopeDecision      *planner.ScopeDecision       `json:"scope_decision,omitempty"`
	PlanReconciliation *coverage.PlanReconciliation `json:"plan_reconciliation,omitempty"`
	CoverageSummary    *coverage.ExecutionSummary   `json:"coverage_summary,omitempty"`
	Status             string                       `json:"status"` // "success" or "failed"
	DegradedReasons    []string                     `json:"degraded_reasons,omitempty"`
	StartTime          time.Time                    `json:"start_time"`
	EndTime            time.Time                    `json:"end_time"`
	DurationSeconds    float64                      `json:"duration_seconds"`
	Analysis           AnalysisSummary              `json:"analysis"`
	Synthesis          SynthesisSummary             `json:"synthesis"`
	Merging            MergingSummary               `json:"merging"`
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

// NormalizeLegacyChunkArtifactStates keeps old success summaries from being
// poisoned by Go's zero-value bool semantics. It is intentionally restricted to
// chunks that are already known to have succeeded.
func NormalizeLegacyChunkArtifactStates(details []ChunkDetails) {
	for i := range details {
		detail := &details[i]
		if detail.Status != "success" || detail.ArtifactComplete != nil {
			continue
		}

		complete := true
		detail.ArtifactComplete = &complete
		if detail.ArtifactState == "" {
			detail.ArtifactState = "legacy_success"
		}
	}
}

func ArtifactCompletePtr(value bool) *bool {
	return &value
}

func buildCoverageExecutionSummary(ctx *TaskContext) *coverage.ExecutionSummary {
	coverageSummary := coverage.Summary{}
	if ctx.Coverage != nil {
		coverageSummary = ctx.Coverage.Summary()
	}

	totalChunks := ctx.Summary.Analysis.TotalChunks
	if totalChunks == 0 {
		totalChunks = ctx.Report.TotalChunks
	}
	processedChunks := len(ctx.Summary.Analysis.Chunks)
	if processedChunks == 0 {
		processedChunks = ctx.Report.ProcessedChunks
	}
	successChunks := ctx.Summary.Analysis.SuccessChunks
	failedChunks := ctx.Summary.Analysis.FailedChunks
	if failedChunks == 0 {
		for _, item := range ctx.Summary.Analysis.Chunks {
			if item.Status != models.StatusSuccess {
				failedChunks++
			}
		}
	}

	missingAssessments := append([]string(nil), coverageSummary.MissingAssessments...)

	state := coverageSummary.State()
	hasCoverageDegradation := failedChunks > 0 || len(missingAssessments) > 0 || coverageSummary.CoverageDegraded
	if hasCoverageDegradation && (state == coverage.StateComplete || state == coverage.StateUnknown) {
		state = coverage.StatePartial
	}
	if coverageSummary.ManifestMissing {
		state = coverage.StateFailed
	}

	summary := &coverage.ExecutionSummary{
		CoverageState:         state,
		TotalChunks:           totalChunks,
		ProcessedChunks:       processedChunks,
		SuccessChunks:         successChunks,
		FailedChunks:          failedChunks,
		MissingAssessments:    missingAssessments,
		FailedFileDetails:     coverageSummary.FailedFileItems,
		FailedChunkDetails:    coverageSummary.FailedChunkItems,
		CoverageComplete:      coverageSummary.CoverageComplete && failedChunks == 0 && len(missingAssessments) == 0,
		CoverageDegraded:      coverageSummary.CoverageDegraded || hasCoverageDegradation,
		CoverageNotApplicable: coverageSummary.CoverageNotApplicable,
	}
	if len(summary.FailedChunkDetails) == 0 && failedChunks > 0 {
		for _, item := range ctx.Summary.Analysis.Chunks {
			if item.Status == models.StatusSuccess {
				continue
			}
			summary.FailedChunkDetails = append(summary.FailedChunkDetails, coverage.ExecutionFailure{
				ChunkUID:      item.ChunkUID,
				ChunkName:     item.ChunkName,
				PrimaryUnitID: item.PrimaryUnitID,
				FilePath:      firstFilePath(item.Files),
				Stage:         "analysis",
				ErrorClass:    item.ErrorClass,
				ErrorMessage:  item.ErrorMessage,
			})
		}
	}
	if len(summary.MissingAssessments) == 0 {
		summary.MissingAssessments = []string{}
	}
	enrichExecutionFailures(summary.FailedFileDetails, ctx.Summary.Analysis.Chunks)
	enrichExecutionFailures(summary.FailedChunkDetails, ctx.Summary.Analysis.Chunks)
	return summary
}

func enrichExecutionFailures(failures []coverage.ExecutionFailure, details []ChunkDetails) {
	for index := range failures {
		for _, item := range details {
			if item.ChunkUID != "" && item.ChunkUID != failures[index].ChunkUID {
				continue
			}
			if item.ChunkUID == "" && item.ChunkName != failures[index].ChunkName {
				continue
			}
			if item.ChunkUID != "" {
				failures[index].ChunkUID = item.ChunkUID
			}
			if item.ChunkName != "" {
				failures[index].ChunkName = item.ChunkName
			}
			if item.PrimaryUnitID != "" {
				failures[index].PrimaryUnitID = item.PrimaryUnitID
			}
			if filePath := firstFilePath(item.Files); filePath != "" && failures[index].FilePath == "" {
				failures[index].FilePath = filePath
			}
			if item.ErrorClass != "" && (failures[index].ErrorClass == "" ||
				strings.EqualFold(failures[index].ErrorClass, "unknown")) {
				failures[index].ErrorClass = item.ErrorClass
			}
			if item.ErrorMessage != "" {
				failures[index].ErrorMessage = item.ErrorMessage
			}
		}
	}
}

func ConvertEngineChunkDetails(items []engines.ChunkDetails) []ChunkDetails {
	converted := make([]ChunkDetails, len(items))
	for index, item := range items {
		converted[index] = ChunkDetails{
			ChunkUID:                 item.ChunkUID,
			ChunkName:                item.ChunkName,
			PrimaryUnitID:            item.PrimaryUnitID,
			StartTime:                item.StartTime,
			EndTime:                  item.EndTime,
			DurationSeconds:          item.DurationSeconds,
			Attempts:                 item.Attempts,
			Retries:                  item.Retries,
			ContractRepairs:          item.ContractRepairs,
			ResourceFailovers:        item.ResourceFailovers,
			DriverFailovers:          item.DriverFailovers,
			ResourceChain:            append([]string(nil), item.ResourceChain...),
			StageChain:               append([]string(nil), item.StageChain...),
			ErrorClasses:             append([]string(nil), item.ErrorClasses...),
			QueueWaitMS:              append([]int64(nil), item.QueueWaitMS...),
			AttemptDurationSeconds:   append([]float64(nil), item.AttemptDurationSeconds...),
			SplitDepth:               item.SplitDepth,
			SplitCount:               item.SplitCount,
			Resumed:                  item.Resumed,
			Status:                   item.Status,
			ErrorMessage:             item.ErrorMessage,
			ErrorClass:               item.ErrorClass,
			Files:                    append([]string(nil), item.Files...),
			ArtifactComplete:         item.ArtifactComplete,
			ArtifactState:            item.ArtifactState,
			ArtifactQualityDegraded:  item.ArtifactQualityDegraded,
			UnresolvedIssueCount:     item.UnresolvedIssueCount,
			NormalizedIssueCount:     item.NormalizedIssueCount,
			SchemaRepairAttempts:     item.SchemaRepairAttempts,
			SchemaRepairSuccesses:    item.SchemaRepairSuccesses,
			JSONSyntaxRepairs:        item.JSONSyntaxRepairs,
			RepairBaselineKnown:      item.RepairBaselineKnown,
			RepairRepairedKnown:      item.RepairRepairedKnown,
			RepairUnverified:         item.RepairUnverified,
			RepairOutcome:            item.RepairOutcome,
			CandidateQuarantineCount: item.CandidateQuarantineCount,
			SchemaRepairIssues:       item.SchemaRepairIssues,
			ArtifactSchemaID:         item.ArtifactSchemaID,
			ArtifactSchemaHash:       item.ArtifactSchemaHash,
			ResponseFormatMode:       item.ResponseFormatMode,
			ResponseFormatFallbacks:  item.ResponseFormatFallbacks,
		}
	}
	return converted
}

// RunningTaskInfo 封装正在执行的扫描任务实时状态（供任务看板与监控查看）
type RunningTaskInfo struct {
	ReportID        uint      `json:"report_id"`
	RepoID          uint      `json:"repo_id"`
	RepoName        string    `json:"repo_name"`
	RepoURL         string    `json:"repo_url"`
	TaskType        string    `json:"task_type"`
	TaskDisplayName string    `json:"task_display_name"`
	EngineMode      string    `json:"engine_mode"`
	Status          string    `json:"status"`
	StartTime       time.Time `json:"start_time"`
	DurationSec     int64     `json:"duration_seconds"`
	TotalChunks     int       `json:"total_chunks"`
	ProcessedChunks int       `json:"processed_chunks"`
	SuccessChunks   int       `json:"success_chunks"`
	Attempts        int       `json:"attempts"`
}

// TaskContext 封装单次任务流水线运行过程中的必要上下文
type TaskContext struct {
	Ctx               context.Context
	Cancel            context.CancelFunc
	Report            models.TaskReport
	TaskType          models.TaskType
	ExecutionSnapshot models.ExecutionContextSnapshot
	PromptPath        string
	PromptContent     string
	PromptContentHash string
	Repo              models.Repository
	CodesPath         string
	ReportPath        string
	JsonPath          string
	AutoNotify        bool
	RunParams         models.RunParams
	Attempts          int
	HasFailedChunks   bool
	ContractRepairs   int
	DriverFailovers   int
	Coverage          *coverage.Coverage
	ChunkPolicyID     string
	ChunkUID          string
	LastInvocation    *InvocationObservation
	Summary           TaskSummaryReport
	Findings          []models.AnalysisFinding

	Runtime           *TaskRuntimeTracker
}

// PhaseTimer 阶段打点计时器
type PhaseTimer struct {
	Name      string     `json:"name"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Duration  float64    `json:"duration_seconds"`
	Status    string     `json:"status"` // "running", "completed", "failed"
}

// TaskActivityEvent 实时执行事件
type TaskActivityEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Stage     string    `json:"stage"`
	Level     string    `json:"level"` // "info", "warn", "error", "success"
	Message   string    `json:"message"`
}

// PlannedChunkRuntime 记录分片在运行态的完整生命周期与指标
type PlannedChunkRuntime struct {
	ChunkUID        string    `json:"chunk_uid"`
	ChunkName       string    `json:"chunk_name"`
	PrimaryUnitID   string    `json:"primary_unit_id"`
	Files           []string  `json:"files"`
	Status          string    `json:"status"` // "pending", "running", "success", "failed"
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	FindingCount    int       `json:"finding_count"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// LiveFindingDTO 实时检出的候选缺陷简要
type LiveFindingDTO struct {
	ChunkName string `json:"chunk_name"`
	Severity  string `json:"severity"` // "high", "medium", "low"
	Title     string `json:"title"`
	FilePath  string `json:"file_path"`
	Line      string `json:"line"`
	Category  string `json:"category"`
	Detail    string `json:"detail"`
}

// LiveDebateDTO 三方对抗辩论关键动作
type LiveDebateDTO struct {
	Timestamp time.Time `json:"timestamp"`
	ChunkName string    `json:"chunk_name"`
	Agent     string    `json:"agent"`             // "hunter", "challenger", "judge"
	Verdict   string    `json:"verdict,omitempty"` // "confirmed", "disputed", "dismissed"
	Summary   string    `json:"summary"`
}

// TaskRuntimeTracker 封装任务运行期动态打点与事件追踪
type TaskRuntimeTracker struct {
	mu            sync.RWMutex
	PhaseTimers   map[string]*PhaseTimer
	RecentEvents  []TaskActivityEvent
	PlannedChunks []PlannedChunkRuntime
	LiveFindings  []LiveFindingDTO
	LiveDebates   []LiveDebateDTO
}

func (ctx *TaskContext) ensureRuntime() *TaskRuntimeTracker {
	if ctx.Runtime == nil {
		ctx.Runtime = &TaskRuntimeTracker{
			PhaseTimers: make(map[string]*PhaseTimer),
		}
	}
	return ctx.Runtime
}

func (ctx *TaskContext) StartPhase(phase, name, eventMsg string) {
	rt := ctx.ensureRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	now := time.Now()
	rt.PhaseTimers[phase] = &PhaseTimer{
		Name:      name,
		StartTime: now,
		Status:    "running",
	}
	if eventMsg != "" {
		rt.appendEventLocked(phase, "info", eventMsg, now)
	}
}

func (ctx *TaskContext) EndPhase(phase, status, eventMsg string) {
	rt := ctx.ensureRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	now := time.Now()
	if timer, ok := rt.PhaseTimers[phase]; ok {
		timer.EndTime = &now
		timer.Duration = now.Sub(timer.StartTime).Seconds()
		timer.Status = status
	}
	if eventMsg != "" {
		level := "info"
		if status == "failed" {
			level = "error"
		} else if status == "completed" {
			level = "success"
		}
		rt.appendEventLocked(phase, level, eventMsg, now)
	}
}

func (ctx *TaskContext) AddEvent(stage, level, message string) {
	rt := ctx.ensureRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.appendEventLocked(stage, level, message, time.Now())
}

func (rt *TaskRuntimeTracker) appendEventLocked(stage, level, message string, t time.Time) {
	rt.RecentEvents = append(rt.RecentEvents, TaskActivityEvent{
		Timestamp: t,
		Stage:     stage,
		Level:     level,
		Message:   message,
	})
	if len(rt.RecentEvents) > 50 {
		rt.RecentEvents = rt.RecentEvents[len(rt.RecentEvents)-50:]
	}
}

// RegisterPlannedChunks 预注册规划好的分片元数据
func (ctx *TaskContext) RegisterPlannedChunks(chunks []PlannedChunkRuntime) {
	rt := ctx.ensureRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.PlannedChunks = make([]PlannedChunkRuntime, len(chunks))
	copy(rt.PlannedChunks, chunks)
}

// MarkChunkStarted 标记分片开始执行
func (ctx *TaskContext) MarkChunkStarted(chunkName string) {
	rt := ctx.ensureRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	now := time.Now()
	for i := range rt.PlannedChunks {
		if rt.PlannedChunks[i].ChunkName == chunkName {
			rt.PlannedChunks[i].Status = "running"
			rt.PlannedChunks[i].StartTime = now
			break
		}
	}
}

// MarkChunkCompleted 标记分片执行完成
func (ctx *TaskContext) MarkChunkCompleted(chunkName, status string, duration float64, findingCount int, errMsg string) {
	rt := ctx.ensureRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	now := time.Now()
	for i := range rt.PlannedChunks {
		if rt.PlannedChunks[i].ChunkName == chunkName {
			rt.PlannedChunks[i].Status = status
			rt.PlannedChunks[i].EndTime = now
			rt.PlannedChunks[i].DurationSeconds = duration
			rt.PlannedChunks[i].FindingCount = findingCount
			rt.PlannedChunks[i].ErrorMessage = errMsg
			break
		}
	}
}

// AddLiveFindings 批量追加实时检出的候选缺陷
func (ctx *TaskContext) AddLiveFindings(findings []LiveFindingDTO) {
	if len(findings) == 0 {
		return
	}
	rt := ctx.ensureRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.LiveFindings = append(rt.LiveFindings, findings...)
	if len(rt.LiveFindings) > 200 {
		rt.LiveFindings = rt.LiveFindings[len(rt.LiveFindings)-200:]
	}
}

// AddLiveDebate 追加辩论事件
func (ctx *TaskContext) AddLiveDebate(event LiveDebateDTO) {
	rt := ctx.ensureRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	rt.LiveDebates = append(rt.LiveDebates, event)
	if len(rt.LiveDebates) > 100 {
		rt.LiveDebates = rt.LiveDebates[len(rt.LiveDebates)-100:]
	}
}

func (ctx *TaskContext) GetPhaseTimersSnapshot() map[string]PhaseTimer {
	if ctx.Runtime == nil {
		return nil
	}
	ctx.Runtime.mu.RLock()
	defer ctx.Runtime.mu.RUnlock()
	res := make(map[string]PhaseTimer, len(ctx.Runtime.PhaseTimers))
	now := time.Now()
	for k, v := range ctx.Runtime.PhaseTimers {
		cp := *v
		if cp.Status == "running" {
			cp.Duration = now.Sub(cp.StartTime).Seconds()
		}
		res[k] = cp
	}
	return res
}

func (ctx *TaskContext) GetRecentEventsSnapshot() []TaskActivityEvent {
	if ctx.Runtime == nil {
		return nil
	}
	ctx.Runtime.mu.RLock()
	defer ctx.Runtime.mu.RUnlock()
	res := make([]TaskActivityEvent, len(ctx.Runtime.RecentEvents))
	copy(res, ctx.Runtime.RecentEvents)
	return res
}

func (ctx *TaskContext) GetPlannedChunksSnapshot() []PlannedChunkRuntime {
	if ctx.Runtime == nil {
		return nil
	}
	ctx.Runtime.mu.RLock()
	defer ctx.Runtime.mu.RUnlock()
	res := make([]PlannedChunkRuntime, len(ctx.Runtime.PlannedChunks))
	now := time.Now()
	for i, c := range ctx.Runtime.PlannedChunks {
		cp := c
		if cp.Status == "running" && !cp.StartTime.IsZero() {
			cp.DurationSeconds = now.Sub(cp.StartTime).Seconds()
		}
		res[i] = cp
	}
	return res
}

func (ctx *TaskContext) GetLiveFindingsSnapshot() []LiveFindingDTO {
	if ctx.Runtime == nil {
		return nil
	}
	ctx.Runtime.mu.RLock()
	defer ctx.Runtime.mu.RUnlock()
	res := make([]LiveFindingDTO, len(ctx.Runtime.LiveFindings))
	copy(res, ctx.Runtime.LiveFindings)
	return res
}

func (ctx *TaskContext) GetLiveDebatesSnapshot() []LiveDebateDTO {
	if ctx.Runtime == nil {
		return nil
	}
	ctx.Runtime.mu.RLock()
	defer ctx.Runtime.mu.RUnlock()
	res := make([]LiveDebateDTO, len(ctx.Runtime.LiveDebates))
	copy(res, ctx.Runtime.LiveDebates)
	return res
}

// 确保 TaskContext 实现 engines.RuntimeObserver 接口
var _ engines.RuntimeObserver = (*TaskContext)(nil)

func (ctx *TaskContext) OnPlannedChunks(chunks []engines.RuntimeChunkUpdate) {
	planned := make([]PlannedChunkRuntime, len(chunks))
	for i, c := range chunks {
		planned[i] = PlannedChunkRuntime{
			ChunkUID:        c.ChunkUID,
			ChunkName:       c.ChunkName,
			PrimaryUnitID:   c.PrimaryUnitID,
			Files:           c.Files,
			Status:          c.Status,
			StartTime:       c.StartTime,
			EndTime:         c.EndTime,
			DurationSeconds: c.DurationSeconds,
			FindingCount:    c.FindingCount,
			ErrorMessage:    c.ErrorMessage,
		}
	}
	ctx.RegisterPlannedChunks(planned)
}

func (ctx *TaskContext) OnChunkStarted(chunkName string) {
	ctx.MarkChunkStarted(chunkName)
}

func (ctx *TaskContext) OnChunkCompleted(update engines.RuntimeChunkUpdate) {
	ctx.MarkChunkCompleted(update.ChunkName, update.Status, update.DurationSeconds, update.FindingCount, update.ErrorMessage)
}

func (ctx *TaskContext) OnLiveFindings(findings []engines.RuntimeFindingUpdate) {
	live := make([]LiveFindingDTO, len(findings))
	for i, f := range findings {
		live[i] = LiveFindingDTO{
			ChunkName: f.ChunkName,
			Severity:  f.Severity,
			Title:     f.Title,
			FilePath:  f.FilePath,
			Line:      f.Line,
			Category:  f.Category,
			Detail:    f.Detail,
		}
	}
	ctx.AddLiveFindings(live)
}

func (ctx *TaskContext) OnLiveDebate(event engines.RuntimeDebateUpdate) {
	ctx.AddLiveDebate(LiveDebateDTO{
		Timestamp: event.Timestamp,
		ChunkName: event.ChunkName,
		Agent:     event.Agent,
		Verdict:   event.Verdict,
		Summary:   event.Summary,
	})
}

type InvocationObservation struct {
	StartedAt       time.Time
	FinishedAt      time.Time
	Driver          string
	Backend         string
	ResourceID      string
	ModelName       string
	PromptHash      string
	ArtifactHash    string
	ArtifactPath    string
	QueueWaitMs     int64
	DurationMs      int64
	Attempts        int
	Continuations   int
	DriverFailovers int
}

func (ctx *TaskContext) LoadExecutionSnapshot() error {
	if ctx.Report.ExecutionSnapshotState == "" {
		return fmt.Errorf("execution snapshot is required for report %d", ctx.Report.ID)
	}
	if ctx.Report.ExecutionSnapshotState != "complete" {
		return fmt.Errorf("execution snapshot state is %q for report %d", ctx.Report.ExecutionSnapshotState, ctx.Report.ID)
	}
	snapshot := models.ExecutionContextSnapshot{}
	if err := json.Unmarshal(ctx.Report.ExecutionSnapshot, &snapshot); err != nil {
		return fmt.Errorf("decode execution snapshot for report %d: %w", ctx.Report.ID, err)
	}
	if snapshot.ScanProfileHash != ctx.Report.ScanProfileHash || snapshot.EngineMode != ctx.Report.EngineMode {
		return fmt.Errorf("execution snapshot identity mismatch for report %d", ctx.Report.ID)
	}
	if ctx.Report.TaxonomySchemaVersion > 1 && snapshot.TaxonomySchemaVersion != ctx.Report.TaxonomySchemaVersion {
		return fmt.Errorf("execution snapshot taxonomy schema mismatch for report %d", ctx.Report.ID)
	}
	if ctx.Report.TaxonomyHash != "" && snapshot.TaxonomyHash != ctx.Report.TaxonomyHash {
		return fmt.Errorf("execution snapshot taxonomy hash mismatch for report %d", ctx.Report.ID)
	}
	if snapshot.TaxonomyHash != "" {
		snapshot.Taxonomy.Hash = snapshot.TaxonomyHash
	}
	if snapshot.PromptContentHash == "" || snapshot.PromptContentHash != ctx.Report.PromptContentHash {
		return fmt.Errorf("execution snapshot prompt hash mismatch for report %d", ctx.Report.ID)
	}
	if filepath.IsAbs(snapshot.PromptContent) {
		return fmt.Errorf("execution snapshot prompt content is invalid for report %d", ctx.Report.ID)
	}
	ctx.ExecutionSnapshot = snapshot
	ctx.PromptContent = snapshot.PromptContent
	ctx.PromptContentHash = snapshot.PromptContentHash
	return nil
}

// Load 从数据库初始化加载关联数据
func (ctx *TaskContext) Load(reportID, taskTypeID uint) error {
	if models.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	if err := models.DB.Preload("Repo").First(&ctx.Report, reportID).Error; err != nil {
		return fmt.Errorf("report %d not found: %w", reportID, err)
	}
	if err := models.DB.First(&ctx.TaskType, taskTypeID).Error; err != nil {
		return fmt.Errorf("task type %d not found: %w", taskTypeID, err)
	}
	ctx.Repo = ctx.Report.Repo
	return nil
}

// ResolveRunParams 保存显式运行参数；默认值来自报告上的 scan profile 快照。
func (ctx *TaskContext) ResolveRunParams(input models.RunParams) {
	ctx.RunParams = input
}

// PrepareOutputPaths 创建输出目录并计算 Markdown 报告与 JSON Summary 的绝对路径
func (ctx *TaskContext) PrepareOutputPaths() {
	if ctx.Report.ReportPath != "" {
		ctx.ReportPath = ctx.Report.GetAbsReportPath()
		reportsDir := filepath.Dir(ctx.ReportPath)
		_ = os.MkdirAll(reportsDir, 0755)
		safeRepoName := strings.ReplaceAll(ctx.Repo.Name, "/", "-")
		ctx.JsonPath = filepath.Join(reportsDir, fmt.Sprintf("report-%d-summary-%s.json", ctx.Report.ID, safeRepoName))
		return
	}

	currentDate := time.Now().Format("2006-01-02")
	if !ctx.Report.CreatedAt.IsZero() {
		currentDate = ctx.Report.CreatedAt.Format("2006-01-02")
	}
	reportsDir := filepath.Join(models.AppConfig.GetDataDir(), "reports", ctx.TaskType.Name, currentDate)
	_ = os.MkdirAll(reportsDir, 0755)

	safeRepoName := strings.ReplaceAll(ctx.Repo.Name, "/", "-")
	ctx.ReportPath = filepath.Join(reportsDir, fmt.Sprintf("report-%d-report-%s.md", ctx.Report.ID, safeRepoName))
	ctx.JsonPath = filepath.Join(reportsDir, fmt.Sprintf("report-%d-summary-%s.json", ctx.Report.ID, safeRepoName))

	// 同步预填 ctx.Report 的内存路径属性，确保后续子系统（如 Manifest 预写）可直接获取标准的专属报告目录
	if rel, err := filepath.Rel(models.AppConfig.GetDataDir(), ctx.ReportPath); err == nil {
		ctx.Report.ReportPath = rel
	} else {
		ctx.Report.ReportPath = ctx.ReportPath
	}
}

func (ctx *TaskContext) ExecuteAnalysis(fileList []string) ([]models.AnalysisFinding, error) {
	return ExecuteAnalysis(ctx, fileList)
}

func (ctx *TaskContext) ExecuteAnalysisOnce(fileList []string) ([]models.AnalysisFinding, error) {
	return ExecuteAnalysisOnce(ctx, fileList)
}

func (ctx *TaskContext) PrepareAndSync(repoURL string) error {
	codesPath, err := PrepareAndSync(ctx.Ctx, ctx.Repo, ctx.Report.ID, repoURL)
	ctx.CodesPath = codesPath
	return err
}

func (ctx *TaskContext) Finalize(result TaskResult) error {
	return Finalize(ctx, result)
}

func (ctx *TaskContext) MarkFailed(errMsg string) {
	MarkFailed(ctx, errMsg)
}

func (ctx *TaskContext) ExecuteSynthesis(allFindings []models.AnalysisFinding) error {
	return ExecuteSynthesis(ctx, allFindings)
}

func (ctx *TaskContext) ExecuteSynthesisOnce(synthesisInputPath string, suffixPrompt string) error {
	return ExecuteSynthesisOnce(ctx, synthesisInputPath, suffixPrompt)
}
