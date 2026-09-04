package services

import (
	"code-shield/models"
	"code-shield/services/invoker"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// 类型别名映射至底层 invoker 子包，保持根 services 包对外导出完全向后兼容
type (
	LLMWorkContext  = invoker.LLMWorkContext
	AIRequest       = invoker.AIRequest
	AIInvoker       = invoker.AIInvoker
	ClaudeInvoker   = invoker.ClaudeInvoker
	OpenCodeInvoker = invoker.OpenCodeInvoker
	CodexInvoker    = invoker.CodexInvoker
	AgyInvoker      = invoker.AgyInvoker
	NativeInvoker   = invoker.NativeInvoker
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

// RegisterAIInvoker 注册一个 AI CLI/API 驱动后端
func RegisterAIInvoker(name string, inv AIInvoker) {
	invoker.RegisterAIInvoker(name, inv)
}

// IsValidAIBackend 检查 backend 名称是否合法（支持空值跟随全局，或已注册的 Invoker）
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

// DispatchingInvoker 是 AIInvoker 的代理，自动在调用前后向 ModelDispatcher 申请/释放并发槽位
type DispatchingInvoker struct {
	delegate AIInvoker
}

func (w *DispatchingInvoker) Name() string {
	return w.delegate.Name()
}

func (w *DispatchingInvoker) Invoke(req AIRequest) error {
	backend := w.delegate.Name()
	ctx := req.ParentContext
	if ctx == nil {
		ctx = context.Background()
	}

	workCtx := req.WorkContext
	if workCtx == nil {
		workCtx = LLMWorkContextFromContext(ctx)
	}

	// 1. 申请 LLM 服务器资源（支持模型亲和性与容量加权优先分配）
	res, modelName, err := Dispatcher.AcquireWithPreference(ctx, backend, req.ModelName)
	if err != nil {
		return fmt.Errorf("failed to acquire LLM server slot: %w", err)
	}

	if res != nil {
		defer Dispatcher.Release(res, backend)
		if req.ModelName == "" && modelName != "" {
			req.ModelName = modelName
		}
		// 登记活跃槽位租约，并在调用退出时自动注销
		leaseID := Dispatcher.RegisterSlotLease(res, backend, modelName, workCtx)
		if leaseID != "" {
			defer Dispatcher.UnregisterSlotLease(leaseID)
		}
	}

	// 2. 调用真正的底层 AI 后端
	return w.delegate.Invoke(req)
}

// GetAIInvoker 根据名称返回对应的 AIInvoker，未找到则回退到 claude。
// 当调度器启用时，自动返回 DispatchingInvoker 包装实例。
func GetAIInvoker(name string) AIInvoker {
	inv, ok := invoker.GetRawInvoker(name)
	if !ok || inv == nil {
		log.Printf("[AI] WARNING: AI backend %q is not registered, falling back to claude\n", name)
		inv, _ = invoker.GetRawInvoker("claude")
	}

	if Dispatcher != nil && Dispatcher.enabled {
		return &DispatchingInvoker{delegate: inv}
	}
	return inv
}

// RepairJSON calls AI to fix syntax errors in a JSON file.
// workDir is the working directory for the AI invocation.
// jsonFilePath is the absolute path to the malformed JSON file.
// Returns the cleaned, repaired JSON bytes ready for json.Unmarshal.
func RepairJSON(workDir, jsonFilePath, aiBackend string) ([]byte, error) {
	ext := filepath.Ext(jsonFilePath)
	fixedPath := strings.TrimSuffix(jsonFilePath, ext) + ".fixed" + ext

	// 1. 确定后端：优先使用 ToolBackends 配置，若无效则回退至参数或全局配置
	backend := models.AppConfig.AI.ToolBackends.RepairJSON
	if backend == "" {
		backend = "native"
	}
	if !IsValidAIBackend(backend) {
		backend = aiBackend
		if backend == "" {
			backend = models.AppConfig.AI.Backend
		}
	}

	aiInv := GetAIInvoker(backend)
	log.Printf("[AI] Invoking %s to repair JSON: %s\n", aiInv.Name(), jsonFilePath)

	rawContent, err := os.ReadFile(jsonFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read malformed JSON: %w", err)
	}

	repairMsg := "你是一个 JSON 语法修复工具。请修复以下内容中的语法错误（未转义引号、多余逗号、括号缺失等）。" +
		"只输出纯 JSON，不要 Markdown 代码块标记，不要任何解释文字，保持原始数据结构不变。"

	zeroTemp := 0.0
	req := AIRequest{
		WorkDir:        workDir,
		PromptMsg:      repairMsg + "\n\n" + string(rawContent),
		InputFiles:     []string{jsonFilePath},
		OutputPath:     fixedPath,
		TimeoutMin:     2,
		Temperature:    &zeroTemp,
		ResponseFormat: "json",
		WorkContext: &LLMWorkContext{
			Stage:   "系统工具: JSON 语法修复",
			SubTask: fmt.Sprintf("修复输出文件 (%s)", filepath.Base(jsonFilePath)),
		},
	}

	if err := aiInv.Invoke(req); err != nil {
		return nil, fmt.Errorf("AI repair invocation failed: %w", err)
	}
	defer os.Remove(fixedPath)

	fixed, err := os.ReadFile(fixedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read repaired JSON: %w", err)
	}

	return fixed, nil
}
