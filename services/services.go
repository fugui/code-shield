package services

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"code-shield/models"
	"code-shield/services/defects"
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
	ModelDispatcher     = dispatcher.ModelDispatcher
	ModelResource       = dispatcher.ModelResource
	ModelResourceStatus = dispatcher.ModelResourceStatus
	ThrottleInfo        = dispatcher.ThrottleInfo
	LLMSlotLease        = dispatcher.LLMSlotLease
	TierRouter          = dispatcher.TierRouter
	TierAcquisition     = dispatcher.TierAcquisition
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
	DefaultChunkMaxFiles    = engines.DefaultChunkMaxFiles
	DefaultChunkDepth       = engines.DefaultChunkDepth
	DefaultChunkConcurrency = engines.DefaultChunkConcurrency
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

// TaskEngine 兼容既有任务上下文的门面接口
type TaskEngine interface {
	Run(ctx *taskContext) error
}

// SingleEngine 兼容旧版调用的单仓分析引擎包装
type SingleEngine struct{}

func (e *SingleEngine) Run(ctx *taskContext) error {
	adapter := &engineAdapter{inner: engines.GetEngine("single")}
	return adapter.Run(ctx)
}

// ChunkedEngine 兼容旧版调用的分片并发引擎包装
type ChunkedEngine struct{}

func (e *ChunkedEngine) Run(ctx *taskContext) error {
	adapter := &engineAdapter{inner: engines.GetEngine("chunked")}
	return adapter.Run(ctx)
}

// DebateEngine 兼容旧版调用的辩论引擎包装
type DebateEngine struct {
	Mode string
}

func (e *DebateEngine) Run(ctx *taskContext) error {
	mode := e.Mode
	if mode == "" {
		mode = "debate_full"
	}
	adapter := &engineAdapter{inner: engines.GetEngine(mode)}
	return adapter.Run(ctx)
}

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

	engCtx := &engines.EngineContext{
		Ctx:           ctx.Ctx,
		ReportID:      ctx.Report.ID,
		RepoID:        ctx.Repo.ID,
		RepoName:      ctx.Repo.Name,
		TaskTypeID:    ctx.TaskType.ID,
		TaskTypeName:  ctx.TaskType.DisplayName,
		CodesPath:     ctx.CodesPath,
		ReportPath:    ctx.ReportPath,
		JSONPath:      ctx.JsonPath,
		EngineConfig:  json.RawMessage(ctx.TaskType.EngineConfig),
		RunParams:     ctx.RunParams,
		NegativeRules: GetNegativeRulesForScan(ctx.Repo.ID, ctx.TaskType.ID),
		ProgressReport: func(total, processed, success int) {
			runner.UpdateTaskProgress(ctx.Report.ID, total, processed, success, "")
		},
		AnalysisExecutor: func(fileList []string) ([]models.AnalysisFinding, error) {
			return runner.ExecuteAnalysis(ctx, fileList)
		},
		SynthesisExecutor: func(findings []models.AnalysisFinding, scannedFilesOpt ...[]string) error {
			findings = CalibrateFindings(findings)
			var scannedFiles []string
			if len(scannedFilesOpt) > 0 {
				scannedFiles = scannedFilesOpt[0]
			}
			findings, _ = DiffAndEnrichFindings(ctx.Repo.ID, ctx.Report.ID, ctx.TaskType.ID, scannedFiles, findings, ctx.CodesPath)
			ctx.Findings = findings
			return runner.ExecuteSynthesis(ctx, findings)
		},
	}

	runner.UpdateTaskStatus(ctx.Report.ID, models.StatusAnalyzing)

	actualEngine := a.inner
	if actualEngine == nil {
		actualEngine = engines.GetEngine(ctx.TaskType.EngineMode)
	}
	if actualEngine == nil {
		actualEngine = engines.GetEngine("single")
	}

	result, err := actualEngine.Run(engCtx)
	overallEndTime := time.Now()

	if result != nil {
		ctx.HasFailedChunks = result.HasFailedChunks
		ctx.Findings = result.Findings

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

		convertedChunks := make([]ChunkDetails, len(result.SummaryChunks))
		for i, sc := range result.SummaryChunks {
			convertedChunks[i] = ChunkDetails{
				ChunkName:       sc.ChunkName,
				StartTime:       sc.StartTime,
				EndTime:         sc.EndTime,
				DurationSeconds: sc.DurationSeconds,
				Attempts:        sc.Attempts,
				Retries:         sc.Retries,
				Status:          sc.Status,
				ErrorMessage:    sc.ErrorMessage,
			}
		}
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
			models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.Report.ID).Updates(updates)
		}
	}

	if err != nil {
		runner.MarkFailed(ctx, err.Error())
	}

	return err
}

var (
	engineRegistryMu sync.RWMutex
	legacyRegistry   = map[string]TaskEngine{}
)

// RegisterEngine 注册兼容版引擎实现
func RegisterEngine(mode string, engine TaskEngine) {
	engineRegistryMu.Lock()
	defer engineRegistryMu.Unlock()
	legacyRegistry[mode] = engine
}

// GetEngine 获取引擎实例，优先检查底层 engines 子包并包装适配
func GetEngine(mode string) TaskEngine {
	engineRegistryMu.RLock()
	if e, ok := legacyRegistry[mode]; ok {
		engineRegistryMu.RUnlock()
		return e
	}
	engineRegistryMu.RUnlock()

	modern := engines.GetEngine(mode)
	if modern != nil {
		return &engineAdapter{inner: modern}
	}

	return &engineAdapter{inner: engines.GetEngine("single")}
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
	SourceAnchor    = defects.SourceAnchor
	MigrationResult = defects.MigrationResult
)

// DiffAndEnrichFindings 执行跨任务缺陷增量比对与状态机打标，委托至 defects 子包
func DiffAndEnrichFindings(repoID uint, taskReportID uint, taskTypeID uint, scannedFiles []string, findings []models.AnalysisFinding, repoRootOpt ...string) ([]models.AnalysisFinding, error) {
	return defects.DiffAndEnrichFindings(repoID, taskReportID, taskTypeID, scannedFiles, findings, repoRootOpt...)
}

// ComputeCleanTokenHash 辅助计算代码段清洗后的哈希，委托至 defects 子包
func ComputeCleanTokenHash(body string) string {
	return defects.ComputeCleanTokenHash(body)
}

// CalculateDefectFingerprint 计算抗代码行号与上下文抖动的确定性源码强指纹 (L1 物理强指纹)，委托至 defects 子包
func CalculateDefectFingerprint(repoID uint, taskTypeID uint, filePath string, triggerLine string, scopeSymbol string, category ...string) string {
	return defects.CalculateDefectFingerprint(repoID, taskTypeID, filePath, triggerLine, scopeSymbol, category...)
}

// CalculateWeakScopeFingerprint 计算作用域弱指纹 (L2 弱指纹容错)，委托至 defects 子包
func CalculateWeakScopeFingerprint(repoID uint, taskTypeID uint, filePath string, scopeSymbol string, category ...string) string {
	return defects.CalculateWeakScopeFingerprint(repoID, taskTypeID, filePath, scopeSymbol, category...)
}

// CalculateTokenJaccard 计算两串代码 Token 的 2-gram Jaccard 相似度，委托至 defects 子包
func CalculateTokenJaccard(s1, s2 string) float64 {
	return defects.CalculateTokenJaccard(s1, s2)
}

// NormalizeTriggerLine 对引发漏洞的核心关键单一语句进行 Token 级规范化，委托至 defects 子包
func NormalizeTriggerLine(triggerLine string) string {
	return defects.NormalizeTriggerLine(triggerLine)
}

// ExtractScopeSymbol 多语言 AST 与正则作用域符号提取器，委托至 defects 子包
func ExtractScopeSymbol(filePath string, codeSnippet string) string {
	return defects.ExtractScopeSymbol(filePath, codeSnippet)
}

// RunFingerprintMigration 执行存量指纹原地物理重算与平滑升级，委托至 defects 子包
func RunFingerprintMigration(db *gorm.DB, repoRoot string, dryRun bool) (*MigrationResult, error) {
	return defects.RunFingerprintMigration(db, repoRoot, dryRun)
}

// CleanSourceToken 对代码行进行 Token 级去噪清洗，委托至 defects 子包
func CleanSourceToken(line string) string {
	return defects.CleanSourceToken(line)
}

// NormalizeScopeSymbol 规范化作用域符号，去除外层命名空间与 lambda 差异，委托至 defects 子包
func NormalizeScopeSymbol(rawScope string) string {
	return defects.NormalizeScopeSymbol(rawScope)
}

// LocateTriggerNearby 在 targetLine 前后指定窗口内滑动寻找最匹配 cleanTrigger 的物理行，委托至 defects 子包
func LocateTriggerNearby(lines []string, cleanTrigger string, targetLine int, windowSize int) int {
	return defects.LocateTriggerNearby(lines, cleanTrigger, targetLine, windowSize)
}

// LocateTriggerInLines 在整篇文件中模糊反查 cleanTrigger 所在真实行号，委托至 defects 子包
func LocateTriggerInLines(lines []string, cleanTrigger string) int {
	return defects.LocateTriggerInLines(lines, cleanTrigger)
}

// ExtractScopeAndBodyFromLines 从目标行向上逆向扫描提取物理函数作用域签名及函数体代码，委托至 defects 子包
func ExtractScopeAndBodyFromLines(filePath string, lines []string, targetLine int) (string, string) {
	return defects.ExtractScopeAndBodyFromLines(filePath, lines, targetLine)
}

// ComputeFileSHA256 计算物理文件的 SHA-256 快照哈希，委托至 defects 子包
func ComputeFileSHA256(fullPath string) (string, error) {
	return defects.ComputeFileSHA256(fullPath)
}

// ParseLineNumberRange 解析 "10-20" 或 "15" 格式的行号，返回起始行与结束行，委托至 defects 子包
func ParseLineNumberRange(rawLine string) (int, int) {
	return defects.ParseLineNumberRange(rawLine)
}

// EnrichSourceAnchor 从磁盘物理源文件中提取确定性特征与物理锚点，委托至 defects 子包
func EnrichSourceAnchor(repoRoot string, filePath string, rawLine string, rawTrigger string) (*SourceAnchor, error) {
	return defects.EnrichSourceAnchor(repoRoot, filePath, rawLine, rawTrigger)
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

// CalibrateFindings 批量校准缺陷列表的严重级别，委托至 governance 子包
func CalibrateFindings(findings []models.AnalysisFinding) []models.AnalysisFinding {
	return governance.CalibrateFindings(findings)
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

// MarkDefectFeedback 处理研发人员对缺陷的反馈（误报/不予修复/已确认），委托至 governance 子包
func MarkDefectFeedback(repoID uint, taskTypeID uint, fingerprint string, feedbackStatus string, reason string, userID *uint) error {
	return governance.MarkDefectFeedback(repoID, taskTypeID, fingerprint, feedbackStatus, reason, userID)
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

// ResumeFailedChunks 失败分片断点续跑，委托至 runner 子包
func ResumeFailedChunks(reportID uint) error {
	return runner.ResumeFailedChunks(reportID)
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
