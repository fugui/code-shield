package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/dispatcher"
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
	ChunkName       string    `json:"chunk_name"`
	Files           []string  `json:"files,omitempty"`
	Status          string    `json:"status"` // "success" or "failed"
	Attempts        int       `json:"attempts"`
	Retries         int       `json:"retries"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// AnalysisSummary 静态分析阶段的统计数据与明细
type AnalysisSummary struct {
	Status          string         `json:"status"` // "success" or "failed"
	StartTime       time.Time      `json:"start_time"`
	EndTime         time.Time      `json:"end_time"`
	DurationSeconds float64        `json:"duration_seconds"`
	TotalChunks     int            `json:"total_chunks"`
	SuccessChunks   int            `json:"success_chunks"`
	FailedChunks    int            `json:"failed_chunks"`
	TotalFindings   int            `json:"total_findings"`
	Chunks          []ChunkDetails `json:"chunks"`
}

// SynthesisSummary 报告综合生成阶段的统计数据
type SynthesisSummary struct {
	Status          string    `json:"status"` // "success" or "failed"
	Attempts        int       `json:"attempts"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	ErrorMessage    string    `json:"error_message,omitempty"`
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
	TaskID          uint             `json:"task_id"`
	RepoName        string           `json:"repo_name"`
	TaskType        string           `json:"task_type"`
	EngineMode      string           `json:"engine_mode"`
	Status          string           `json:"status"` // "success" or "failed"
	StartTime       time.Time        `json:"start_time"`
	EndTime         time.Time        `json:"end_time"`
	DurationSeconds float64          `json:"duration_seconds"`
	Analysis        AnalysisSummary  `json:"analysis"`
	Synthesis       SynthesisSummary `json:"synthesis"`
	Merging         MergingSummary   `json:"merging"`
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
	Ctx             context.Context
	Cancel          context.CancelFunc
	Report          models.TaskReport
	TaskType        models.TaskType
	Repo            models.Repository
	CodesPath       string
	ReportPath      string
	JsonPath        string
	AutoNotify      bool
	RunParams       models.RunParams
	Attempts        int
	HasFailedChunks bool
	Summary         TaskSummaryReport
	Findings        []models.AnalysisFinding
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

// ResolveRunParams 将外部传入的 RunParams 与 TaskType 默认值合并
func (ctx *TaskContext) ResolveRunParams(input models.RunParams) {
	ctx.RunParams = input
	if ctx.RunParams.TargetScope == nil {
		ctx.RunParams.TargetScope = &ctx.TaskType.TargetScope
	}
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
}

func (ctx *TaskContext) ExecuteAnalysis(fileList []string) ([]models.AnalysisFinding, error) {
	return ExecuteAnalysis(ctx, fileList)
}

func (ctx *TaskContext) ExecuteAnalysisOnce(fileList []string) ([]models.AnalysisFinding, error) {
	return ExecuteAnalysisOnce(ctx, fileList)
}

func (ctx *TaskContext) CheckPrecondition() (bool, error) {
	return CheckPrecondition(ctx.Ctx, ctx.Report.ID, ctx.TaskType, ctx.CodesPath)
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
