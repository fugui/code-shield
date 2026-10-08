package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/defectlifecycle"
	"code-shield/services/dispatcher"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/debate"
	"code-shield/services/governance"
	"code-shield/services/invoker"
	"code-shield/services/queue"
	"code-shield/services/runner"

	"gorm.io/gorm"
)

// ==============================================================================
// 1. Invoker CLI / LLM 适配器门面 (向后兼容)
// ==============================================================================

type (
	LLMWorkContext     = invoker.LLMWorkContext
	AIRequest          = invoker.AIRequest
	AIInvoker          = invoker.AIInvoker
	ClaudeInvoker      = invoker.ClaudeInvoker
	OpenCodeInvoker    = invoker.OpenCodeInvoker
	CodexInvoker       = invoker.CodexInvoker
	AgyInvoker         = invoker.AgyInvoker
	NativeInvoker      = invoker.NativeInvoker
	DispatchingInvoker = dispatcher.DispatchingInvoker
)

const BaseScannerAgentName = invoker.BaseScannerAgentName

// WithLLMWorkContext 将 LLMWorkContext 注入 context.Context 中
func WithLLMWorkContext(ctx context.Context, work *LLMWorkContext) context.Context {
	return invoker.WithLLMWorkContext(ctx, work)
}

// LLMWorkContextFromContext 从 context.Context 中提取 LLMWorkContext
func LLMWorkContextFromContext(ctx context.Context) *LLMWorkContext {
	return invoker.LLMWorkContextFromContext(ctx)
}

// RegisterAIInvoker 动态注册 AI 执行器驱动
func RegisterAIInvoker(name string, inv AIInvoker) {
	invoker.RegisterAIInvoker(name, inv)
}

// IsValidAIBackend 检查 backend 名称是否合法
func IsValidAIBackend(name string) bool {
	return invoker.IsValidAIBackend(name)
}

// BuildPromptPayload 组装通用的 Prompt 规约、分片提示及输入文件清单
func BuildPromptPayload(req AIRequest, includePromptFile bool) (string, error) {
	return invoker.BuildPromptPayload(req, includePromptFile)
}

// RunCLIProcess 统一管理所有 AI CLI 的执行、超时、进程组治理与 Mock 降级
func RunCLIProcess(cliName string, args []string, req AIRequest, mockSummary string) error {
	return invoker.RunCLIProcess(cliName, args, req, mockSummary)
}

// EnsureBaseAgent 确保全局 OpenCode 基座 Agent 存在且最新
func EnsureBaseAgent() error {
	return invoker.EnsureBaseAgent()
}

// CleanupLegacyTaskAgents 清理旧版 OpenCode 遗留 Agent
func CleanupLegacyTaskAgents() {
	invoker.CleanupLegacyTaskAgents()
}

// GetAIInvoker 根据名称返回对应的 AIInvoker，未找到则回退到 claude。
// 当调度器启用时，自动返回经过调度器并发配额管理的包装实例。
func GetAIInvoker(name string) AIInvoker {
	inv, ok := invoker.GetRawInvoker(name)
	if !ok || inv == nil {
		log.Printf("[AI] WARNING: AI backend %q is not registered, falling back to claude\n", name)
		inv, _ = invoker.GetRawInvoker("claude")
	}

	return dispatcher.WrapInvoker(inv)
}

// RepairJSON 委托至 runner.RepairJSON
func RepairJSON(workDir, jsonFilePath, aiBackend string) ([]byte, error) {
	return runner.RepairJSON(workDir, jsonFilePath, aiBackend)
}

// ==============================================================================
// 2. Dispatcher 算力调度与槽位租赁门面
// ==============================================================================

type (
	ModelDispatcher           = dispatcher.ModelDispatcher
	ModelResource             = dispatcher.ModelResource
	ModelResourceStatus       = dispatcher.ModelResourceStatus
	ThrottleInfo              = dispatcher.ThrottleInfo
	LLMSlotLease              = dispatcher.LLMSlotLease
	DispatcherMetricsSnapshot = dispatcher.DispatcherMetricsSnapshot
	DispatcherDebugSnapshot   = dispatcher.DispatcherDebugSnapshot
	PoolMetricsSnapshot       = dispatcher.PoolMetricsSnapshot
	TierMetricsSnapshot       = dispatcher.TierMetricsSnapshot
	TierRouter                = dispatcher.TierRouter
	TierAcquisition           = dispatcher.TierAcquisition
)

// Dispatcher 为多 LLM 并发分配器的全局单例引用
var Dispatcher *ModelDispatcher = dispatcher.GlobalDispatcher

// InitModelDispatcher 初始化全局并发调度器并同步单例引用
func InitModelDispatcher() {
	dispatcher.InitModelDispatcher()
	Dispatcher = dispatcher.GlobalDispatcher
}

// GetTierRouter 获取算力分级路由器门面
func GetTierRouter() *TierRouter {
	return dispatcher.GetTierRouter()
}

// ==============================================================================
// 3. Engines 计算引擎与分片编排门面
// ==============================================================================

const (
	DefaultChunkMaxFiles = engines.DefaultChunkMaxFiles
	DefaultChunkDepth    = engines.DefaultChunkDepth
)

type (
	EngineContext         = engines.EngineContext
	EngineResult          = engines.EngineResult
	ChunkConfig           = engines.ChunkConfig
	SemanticBundle        = chunker.SemanticBundle
	HunterCandidate       = debate.HunterCandidate
	HunterOutput          = debate.HunterOutput
	DefenseArgument       = debate.DefenseArgument
	ChallengerDefenseCase = debate.ChallengerDefenseCase
	ChallengerOutput      = debate.ChallengerOutput
	JudgeFinalVerdict     = debate.JudgeFinalVerdict
	JudgeOutput           = debate.JudgeOutput
	DebateTicket          = debate.DebateTicket
	DebateTicketResult    = debate.DebateTicketResult
)

// 兼容既有单元测试与调用方的辅助函数别名
func scanAndChunk(codesPath string, cfg ChunkConfig, targetScope string) (map[string][]string, error) {
	return chunker.ScanAndChunk(codesPath, cfg, targetScope)
}

func getFilteredFiles(codesPath string, cfg ChunkConfig, targetScope string) ([]string, error) {
	return chunker.GetFilteredFiles(codesPath, cfg, targetScope)
}

func isSourceFile(file string, taskExtensions map[string]bool) bool {
	return chunker.IsSourceFile(file, taskExtensions)
}

func isTestFile(file string) bool {
	return chunker.IsTestFile(file)
}

func deriveConciseTitle(rawTitle, fallbackCategory string) string {
	return debate.DeriveConciseTitle(rawTitle, fallbackCategory)
}

// engineAdapter 桥接 engines.TaskEngine 与既有 taskContext
type engineAdapter struct {
	inner engines.TaskEngine
}

func (a *engineAdapter) Run(ctx *taskContext) error {
	overallStartTime := time.Now()

	var engCtx *engines.EngineContext
	chunkPolicyID := coverage.ChunkPolicyID(ctx.TaskType.Name, ctx.TaskType.EngineConfig)
	ctx.ChunkPolicyID = chunkPolicyID
	engCtx = &engines.EngineContext{
		Ctx:                ctx.Ctx,
		ReportID:           ctx.Report.ID,
		RepoID:             ctx.Repo.ID,
		RepoName:           ctx.Repo.Name,
		TaskTypeID:         ctx.TaskType.ID,
		TaskTypeName:       ctx.TaskType.DisplayName,
		TaskTypeKey:        ctx.TaskType.Name,
		EngineMode:         ctx.TaskType.EngineMode,
		AssessmentConfig:   json.RawMessage(ctx.TaskType.AssessmentConfig),
		TaskDir:            ctx.TaskType.TaskDir(),
		AnalysisPromptPath: models.AppConfig.GetAbsPath(ctx.TaskType.AnalysisPromptFile()),
		AllowedCategories:  ctx.TaskType.GetAllowedCategories(),
		Taxonomy:           taxonomySnapshot(ctx.TaskType),
		DomainFamily:       ctx.TaskType.GetDomainFamily(),
		DefenseDimensions:  ctx.TaskType.GetDefenseDimensions(),
		CodesPath:          ctx.CodesPath,
		WorkDir:            ctx.CodesPath,
		ReportPath:         ctx.ReportPath,
		JSONPath:           ctx.JsonPath,
		EngineConfig:       json.RawMessage(ctx.TaskType.EngineConfig),
		ChunkPolicyID:      chunkPolicyID,
		RunParams:          ctx.RunParams,
		NegativeRules:      GetNegativeRulesForScan(ctx.Repo.ID, ctx.TaskType.ID),
		CategoryAliasRecorder: func(usage models.CategoryAliasUsage) {
			if models.DB == nil {
				return
			}
			usage.TaskTypeID = ctx.TaskType.ID
			usage.LastReportID = ctx.Report.ID
			_ = governance.RecordCategoryAliasHit(models.DB, usage)
		},
		ProgressReport: func(total, processed, success int) {
			runner.UpdateTaskProgress(ctx.Report.ID, total, processed, success, "")
		},
		AnalysisExecutor: func(fileList []string) ([]models.AnalysisFinding, error) {
			return runner.ExecuteAnalysis(ctx, fileList)
		},
		ChunkAnalysisExecutor: func(req engines.ChunkExecutionRequest) (engines.ChunkExecutionResult, error) {
			return runner.ExecuteChunkAnalysis(ctx, req)
		},
	}

	runner.UpdateTaskStatus(ctx.Report.ID, models.StatusAnalyzing)

	actualEngine := a.inner
	if actualEngine == nil {
		return fmt.Errorf("ENGINE_MODE_INVALID: engine mode %q is not registered", ctx.TaskType.EngineMode)
	}

	result, err := actualEngine.Run(engCtx)
	overallEndTime := time.Now()

	if result != nil {
		ctx.HasFailedChunks = result.HasFailedChunks
		ctx.Coverage = engCtx.Coverage
		ctx.Findings = CalibrateFindings(result.Findings)

		successfulChunks := 0
		failedChunks := 0
		for _, sc := range result.SummaryChunks {
			if sc.Status == "success" {
				successfulChunks++
			} else {
				failedChunks++
			}
		}

		ctx.Summary.Analysis.StartTime = overallStartTime
		ctx.Summary.Analysis.EndTime = overallEndTime
		ctx.Summary.Analysis.DurationSeconds = overallEndTime.Sub(overallStartTime).Seconds()
		ctx.Summary.Analysis.TotalChunks = len(result.SummaryChunks)
		ctx.Summary.Analysis.SuccessChunks = successfulChunks
		ctx.Summary.Analysis.FailedChunks = failedChunks
		ctx.Summary.Analysis.TotalFindings = len(result.Findings)
		ctx.Summary.Analysis.Attempts = result.AnalysisMetrics.Attempts
		ctx.Summary.Analysis.Retries = result.AnalysisMetrics.Retries
		ctx.Summary.Analysis.ContractRepairs = result.AnalysisMetrics.ContractRepairs
		ctx.Summary.Analysis.ResourceFailovers = result.AnalysisMetrics.ResourceFailovers
		ctx.Summary.Analysis.DriverFailovers = result.AnalysisMetrics.DriverFailovers
		ctx.Summary.Analysis.SplitInvocations = result.AnalysisMetrics.SplitInvocations
		ctx.Summary.Analysis.RecoveredChunks = result.AnalysisMetrics.ResumedChunks
		ctx.Summary.Analysis.ArtifactComplete = result.AnalysisMetrics.ArtifactComplete
		ctx.Summary.Analysis.ArtifactState = result.AnalysisMetrics.ArtifactState
		ctx.Summary.Analysis.ArtifactQualityDegraded = result.AnalysisMetrics.ArtifactQualityDegraded
		ctx.Summary.Analysis.UnresolvedIssueCount = result.AnalysisMetrics.UnresolvedIssueCount
		ctx.Summary.Analysis.NormalizedIssueCount = result.AnalysisMetrics.NormalizedIssueCount
		ctx.Summary.Analysis.SchemaRepairAttempts = result.AnalysisMetrics.SchemaRepairAttempts
		ctx.Summary.Analysis.SchemaRepairSuccesses = result.AnalysisMetrics.SchemaRepairSuccesses
		ctx.Summary.Analysis.CandidateQuarantineCount = result.AnalysisMetrics.CandidateQuarantineCount

		convertedChunks := runner.ConvertEngineChunkDetails(result.SummaryChunks)
		ctx.Summary.Analysis.Chunks = convertedChunks

		if failedChunks > 0 {
			ctx.Summary.Analysis.Status = "failed"
		} else {
			ctx.Summary.Analysis.Status = "success"
		}

		runner.WriteSummaryReport(ctx)

		// 持久化辩论轨迹
		if len(result.DebateLogs) > 0 && models.DB != nil {
			for _, dl := range result.DebateLogs {
				dl.TaskReportID = ctx.Report.ID
				if dbErr := models.DB.Create(&dl).Error; dbErr != nil {
					log.Printf("[EngineAdapter] Warning: failed to persist DebateLog for %s: %v", dl.CandidateID, dbErr)
				}
			}
		}

		// 累加 Token 消耗
		if (result.HunterTokens > 0 || result.Tier2Tokens > 0) && models.DB != nil {
			updates := map[string]interface{}{}
			if result.HunterTokens > 0 {
				updates["tier1_tokens"] = gorm.Expr("tier1_tokens + ?", result.HunterTokens)
			}
			if result.Tier2Tokens > 0 {
				updates["tier2_tokens"] = gorm.Expr("tier2_tokens + ?", result.Tier2Tokens)
			}
			if _, updateErr := models.UpdateActiveTaskReport(models.DB, ctx.Report.ID, updates); updateErr != nil {
				log.Printf("[EngineAdapter] Warning: failed to persist token usage for report %d: %v", ctx.Report.ID, updateErr)
			}
		}
	}

	if err != nil {
		runner.MarkFailed(ctx, err.Error())
		return err
	}

	if models.DB != nil {
		if _, persistErr := defectlifecycle.PersistScanFacts(defectlifecycle.ScanInput{
			Report:   ctx.Report,
			Repo:     ctx.Repo,
			RepoRoot: ctx.CodesPath,
			TaskType: ctx.TaskType,
			Findings: ctx.Findings,
			Coverage: ctx.Coverage,
		}); persistErr != nil {
			runner.MarkFailed(ctx, persistErr.Error())
			return persistErr
		}
	}

	if synthErr := runner.ExecuteSynthesis(ctx, ctx.Findings, ctx.Coverage); synthErr != nil {
		runner.MarkFailed(ctx, synthErr.Error())
		return synthErr
	}

	return err
}

func taxonomySnapshot(taskType models.TaskType) models.CategoryTaxonomy {
	if taxonomy := taskType.GetCategoryTaxonomy(); taxonomy != nil {
		return *taxonomy
	}
	return models.CategoryTaxonomy{}
}

// BuildSemanticBundles 构建语义感知分片，委托至 chunker
func BuildSemanticBundles(codesPath string, cfg ChunkConfig, targetScope string, negativeRules []string) ([]SemanticBundle, error) {
	return chunker.BuildSemanticBundles(codesPath, cfg, targetScope, negativeRules)
}

// ProjectAndGroupFiles 跨目录同名投影归并，委托至 chunker
func ProjectAndGroupFiles(files []string, cfg ChunkConfig) []SemanticBundle {
	return chunker.ProjectAndGroupFiles(files, cfg)
}

// ExtractGlobalMacros 扫描提取构建宏，委托至 chunker
func ExtractGlobalMacros(codesPath string) map[string]string {
	return chunker.ExtractGlobalMacros(codesPath)
}

// ExtractHeaderOutline 提取公共头文件大纲，委托至 chunker
func ExtractHeaderOutline(codesPath string, files []string) string {
	return chunker.ExtractHeaderOutline(codesPath, files)
}

// ==============================================================================
// 4. Defects 缺陷指纹与物理锚点门面
// ==============================================================================

type (
	SourceAnchor = defectlifecycle.SourceAnchor
)

// ComputeCleanTokenHash 辅助计算代码段清洗后的哈希，委托至 defects 子包
func ComputeCleanTokenHash(body string) string {
	return defectlifecycle.ComputeCleanTokenHash(body)
}

// CalculateTokenJaccard 计算两串代码 Token 的 2-gram Jaccard 相似度，委托至 defects 子包
func CalculateTokenJaccard(s1, s2 string) float64 {
	return defectlifecycle.CalculateTokenJaccard(s1, s2)
}

// CleanSourceToken 对代码行进行 Token 级去噪清洗，委托至 defects 子包
func CleanSourceToken(line string) string {
	return defectlifecycle.CleanSourceToken(line)
}

// NormalizeScopeSymbol 规范化作用域符号，去除外层命名空间与 lambda 差异，委托至 defects 子包
func NormalizeScopeSymbol(rawScope string) string {
	return defectlifecycle.NormalizeScopeSymbol(rawScope)
}

// LocateTriggerNearby 在 targetLine 前后指定窗口内滑动寻找最匹配 cleanTrigger 的物理行，委托至 defects 子包
func LocateTriggerNearby(lines []string, cleanTrigger string, targetLine int, windowSize int) int {
	return defectlifecycle.LocateTriggerNearby(lines, cleanTrigger, targetLine, windowSize)
}

// LocateTriggerInLines 在整篇文件中模糊反查 cleanTrigger 所在真实行号，委托至 defects 子包
func LocateTriggerInLines(lines []string, cleanTrigger string) int {
	return defectlifecycle.LocateTriggerInLines(lines, cleanTrigger)
}

// ExtractScopeAndBodyFromLines 从目标行向上逆向扫描提取物理函数作用域签名及函数体代码，委托至 defects 子包
func ExtractScopeAndBodyFromLines(filePath string, lines []string, targetLine int) (string, string) {
	return defectlifecycle.ExtractScopeAndBodyFromLines(filePath, lines, targetLine)
}

// ComputeFileSHA256 计算物理文件的 SHA-256 快照哈希，委托至 defects 子包
func ComputeFileSHA256(fullPath string) (string, error) {
	return defectlifecycle.ComputeFileSHA256(fullPath)
}

// ParseLineNumberRange 解析 "10-20" 或 "15" 格式的行号，返回起始行与结束行，委托至 defects 子包
func ParseLineNumberRange(rawLine string) (int, int) {
	return defectlifecycle.ParseLineNumberRange(rawLine)
}

// EnrichSourceAnchor 从磁盘物理源文件中提取确定性特征与物理锚点，委托至 defects 子包
func EnrichSourceAnchor(repoRoot string, filePath string, rawLine string, rawTrigger string) (*SourceAnchor, error) {
	return defectlifecycle.EnrichSourceAnchor(repoRoot, filePath, rawLine, rawTrigger)
}

// ==============================================================================
// 5. Governance 专项治理与反馈沉淀门面
// ==============================================================================

type (
	ExtractedFeedbackRule = governance.ExtractedFeedbackRule
)

// DefaultFallbackCategory 通用兜底分类
const DefaultFallbackCategory = governance.DefaultFallbackCategory

// CalibrateSeverityDeterministically 依据确定性规则决策树计算严重级别，委托给 governance 子包
func CalibrateSeverityDeterministically(category string, verdict string, codeSnippet string) (string, string) {
	return governance.CalibrateSeverityDeterministically(category, verdict, codeSnippet)
}

// CalibrateSeverityWithTaxonomy 按任务受控分类策略确定性计算严重级别
func CalibrateSeverityWithTaxonomy(
	taxonomy models.CategoryTaxonomy,
	categoryCode string,
	category string,
	verdict string,
	codeSnippet string,
) (string, string) {
	return governance.CalibrateSeverityWithTaxonomy(taxonomy, categoryCode, category, verdict, codeSnippet)
}

// CalibrateFindings 批量校准缺陷列表的严重级别，委托至 governance 子包
func CalibrateFindings(findings []models.AnalysisFinding) []models.AnalysisFinding {
	return governance.CalibrateFindings(findings)
}

// CalibrateFindingsWithTaxonomy 批量按受控分类策略校准缺陷列表
func CalibrateFindingsWithTaxonomy(taxonomy models.CategoryTaxonomy, findings []models.AnalysisFinding) []models.AnalysisFinding {
	return governance.CalibrateFindingsWithTaxonomy(taxonomy, findings)
}

// SanitizeCategory 将 Category 规范化吸附至白名单，委托至 governance 子包
func SanitizeCategory(rawCategory string, allowedCategories []string) string {
	return governance.SanitizeCategory(rawCategory, allowedCategories)
}

// ExtractFeedbackRuleViaNative 使用 Native Thin LLM 提炼误报特征规则，委托至 governance 子包
func ExtractFeedbackRuleViaNative(filePath, codeSnippet, defectTitle, userReason string) (*ExtractedFeedbackRule, error) {
	backend := models.AppConfig.AI.ToolBackends.FeedbackExtraction
	if backend == "" {
		backend = "native"
	}
	if !IsValidAIBackend(backend) {
		backend = models.AppConfig.AI.Backend
	}
	if backend == "" {
		backend = "native"
	}

	inv := GetAIInvoker(backend)
	return governance.ExtractFeedbackRule(inv, filePath, codeSnippet, defectTitle, userReason)
}

// GetNegativeRulesForScan 获取指定仓库和任务类型在扫描时应注入的负样本规则列表，委托至 governance 子包
func GetNegativeRulesForScan(repoID uint, taskTypeID uint) []string {
	return governance.GetNegativeRulesForScan(repoID, taskTypeID)
}

// ==============================================================================
// 6. Runner 任务流水线与 Hook 门面
// ==============================================================================

type (
	TaskResult        = runner.TaskResult
	ChunkDetails      = runner.ChunkDetails
	AnalysisSummary   = runner.AnalysisSummary
	SynthesisSummary  = runner.SynthesisSummary
	MergingSummary    = runner.MergingSummary
	TaskSummaryReport = runner.TaskSummaryReport
	RunningTaskInfo   = runner.RunningTaskInfo
	taskContext       = runner.TaskContext
)

// TaskHook is a callback function run when a task finishes successfully
type TaskHook func(ctx *taskContext, findings []models.AnalysisFinding) error

var (
	taskHooksMu sync.RWMutex
	taskHooks   = make(map[string][]TaskHook)
)

// RegisterTaskHook registers a postprocess hook for a specific task type name
func RegisterTaskHook(taskTypeName string, hook TaskHook) {
	taskHooksMu.Lock()
	defer taskHooksMu.Unlock()
	taskHooks[taskTypeName] = append(taskHooks[taskTypeName], hook)
}

// executeHooks runs all hooks registered for the current task type
func executeHooks(ctx *taskContext, findings []models.AnalysisFinding) {
	if ctx.TaskType.IsCampaign {
		log.Printf("[TaskHooks] Running generic campaign hook for %q (GovernanceMode: %s, Report ID: %d)",
			ctx.TaskType.Name, ctx.TaskType.GovernanceMode, ctx.Report.ID)
		if err := handleGenericCampaignHook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Generic campaign hook for %q failed: %v", ctx.TaskType.Name, err)
		}
	}

	taskHooksMu.RLock()
	hooks, ok := taskHooks[ctx.TaskType.Name]
	taskHooksMu.RUnlock()
	if !ok {
		return
	}
	log.Printf("[TaskHooks] Running %d custom hooks for task type %q (Report ID: %d)", len(hooks), ctx.TaskType.Name, ctx.Report.ID)
	for i, hook := range hooks {
		if err := hook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Hook %d for %q failed: %v", i, ctx.TaskType.Name, err)
		}
	}
}

// handleGenericCampaignHook 将任务执行上下文适配并转接至治理领域的 HandleGenericCampaign
func handleGenericCampaignHook(ctx *taskContext, findings []models.AnalysisFinding) error {
	backend := models.AppConfig.AI.ToolBackends.FindingMatch
	if backend == "" {
		backend = "native"
	}
	if !IsValidAIBackend(backend) {
		backend = models.AppConfig.AI.Backend
	}
	if backend == "" {
		backend = "claude"
	}

	inv := GetAIInvoker(backend)

	campCtx := &governance.CampaignContext{
		Ctx:             ctx.Ctx,
		TaskType:        ctx.TaskType,
		Repo:            ctx.Repo,
		Report:          ctx.Report,
		CodesPath:       ctx.CodesPath,
		HasFailedChunks: ctx.HasFailedChunks,
		Invoker:         inv,
	}

	return governance.HandleGenericCampaign(campCtx, findings)
}

// SanitizeFindingTitle 规范化缺陷标题，委托给 governance 子包
func SanitizeFindingTitle(title string) string {
	return governance.SanitizeFindingTitle(title)
}

// RunTaskSync 同步驱动单次扫描任务，委托至 runner 子包
func RunTaskSync(reportID uint, repoURL string, taskTypeID uint, autoNotify bool, runParams models.RunParams) error {
	return runner.RunTaskSync(reportID, repoURL, taskTypeID, autoNotify, runParams)
}

// CancelRunningTask 取消正在执行的任务，委托至 runner 子包
func CancelRunningTask(reportID uint) bool {
	return runner.CancelRunningTask(reportID)
}

// CancelAllRunningTasks 取消所有正在执行的任务，委托至 runner 子包
func CancelAllRunningTasks() {
	runner.CancelAllRunningTasks()
}

// GetRunningTasks 获取当前运行中任务列表快照，委托至 runner 子包
func GetRunningTasks() []RunningTaskInfo {
	return runner.GetRunningTasks()
}

// NotifyTaskResult 结果邮件与 Webhook 通告，委托至 runner 子包
func NotifyTaskResult(repo models.Repository, taskType models.TaskType, result TaskResult, specificRecipientEmail string, reportID uint, reportPath string) {
	runner.NotifyTaskResult(repo, taskType, result, specificRecipientEmail, reportID, reportPath)
}

// 门面清洗与辅助函数，保持单元测试透明兼容
func cleanJSONFromAI(raw []byte) []byte {
	return runner.CleanJSONFromAI(raw)
}

func fixUnescapedQuotes(s string) string {
	return runner.FixUnescapedQuotes(s)
}

func cleanAnalysisTempFiles(jsonPath string) {
	runner.CleanAnalysisTempFiles(jsonPath)
}

func cleanSynthesisTempFiles(reportPath string) {
	runner.CleanSynthesisTempFiles(reportPath)
}

func recoverAIOutput(expectedPath string) {
	runner.RecoverAIOutput(expectedPath)
}

func sanitizeMarkdownReport(raw []byte) []byte {
	return runner.SanitizeMarkdownReport(raw)
}

func toLineStr(v interface{}) string {
	return runner.ToLineStr(v)
}

func cleanStaleGitLocks(codesPath string) {
	runner.CleanStaleGitLocks(codesPath)
}

func getRepoSyncLock(repoPath string) *sync.Mutex {
	return runner.GetRepoSyncLock(repoPath)
}

// ==============================================================================
// 7. Queue 队列与工作池门面
// ==============================================================================

// ErrSkipped 在前置条件未满足时返回，映射自 runner 子包
var ErrSkipped = runner.ErrSkipped

// Task 任务模型别名，完全兼容外部引用
type Task = queue.Task

// IsQueuePaused 查询当前队列是否处于暂停派发状态，委托至 queue 子包
func IsQueuePaused() bool {
	return queue.IsQueuePaused()
}

// SetQueuePaused 设置调度开关，并在恢复派发时即时唤醒 Worker，委托至 queue 子包
func SetQueuePaused(paused bool) {
	queue.SetQueuePaused(paused)
}

// InitQueueState 在服务启动时从 DB 加载初始队列状态，委托至 queue 子包
func InitQueueState() {
	queue.InitQueueState()
}

// NotifyWorker 发送唤醒信号，委托至 queue 子包
func NotifyWorker() {
	queue.NotifyWorker()
}

// StartWorkerPool starts the background workers，委托至 queue 子包
func StartWorkerPool(workers int) {
	queue.StartWorkerPool(workers)
}

// ResizeWorkerPool 热更新任务 Worker 并发数，委托至 queue 子包
func ResizeWorkerPool(workers int) {
	queue.ResizeWorkerPool(workers)
}

// WorkerPoolSize 返回当前目标任务 Worker 数，委托至 queue 子包
func WorkerPoolSize() int {
	return queue.WorkerPoolSize()
}

// EnqueueTask adds a new task to the queue，委托至 queue 子包
func EnqueueTask(scheduleID *uint, repoID uint, repoURL string, taskTypeID uint, autoNotify bool, triggerType string, runParams models.RunParams) {
	queue.EnqueueTask(scheduleID, repoID, repoURL, taskTypeID, autoNotify, triggerType, runParams)
}

// EnqueueTaskWithTriggerLog supports linking a parent TaskTriggerLog，委托至 queue 子包
func EnqueueTaskWithTriggerLog(scheduleID *uint, triggerLogID *uint, repoID uint, repoURL string, taskTypeID uint, autoNotify bool, triggerType string, runParams models.RunParams) bool {
	return queue.EnqueueTaskWithTriggerLog(scheduleID, triggerLogID, repoID, repoURL, taskTypeID, autoNotify, triggerType, runParams)
}

// EnqueueResumeTask 将恢复任务放入队列排队执行，委托至 queue 子包
func EnqueueResumeTask(report models.TaskReport) error {
	return queue.EnqueueResumeTask(report)
}

// IsBundleResumeEngine 判断引擎模式是否支持 bundle checkpoint 复用
func IsBundleResumeEngine(engineMode string) bool {
	return runner.IsBundleResumeEngine(engineMode)
}

// UpdateTaskExecutionLog 更新任务执行日志状态，委托至 queue 子包
func UpdateTaskExecutionLog(logID uint, status string, errMsg string) {
	queue.UpdateTaskExecutionLog(logID, status, errMsg)
}

// RecoverPendingTasks 在进程启动时调用处理挂起任务，委托至 queue 子包
func RecoverPendingTasks(action string) {
	queue.RecoverPendingTasks(action)
}

// CleanReportFiles 清理物理报告文件，委托至 queue 子包
func CleanReportFiles(taskTypeName string, reportID uint) {
	queue.CleanReportFiles(taskTypeName, reportID)
}

// CleanExpiredTempArtifacts 清理过期临时构件，委托至 queue 子包
func CleanExpiredTempArtifacts(retentionDays int) {
	queue.CleanExpiredTempArtifacts(retentionDays)
}
