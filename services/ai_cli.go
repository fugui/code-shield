package services

import (
	"code-shield/services/dispatcher"
	"code-shield/services/invoker"
	"code-shield/services/runner"
	"context"
	"log"
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

// DispatchingInvoker 是 AIInvoker 的代理，委托至 dispatcher 子包
type DispatchingInvoker = dispatcher.DispatchingInvoker

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

// RepairJSON calls AI to fix syntax errors in a JSON file, delegated to runner.RepairJSON.
func RepairJSON(workDir, jsonFilePath, aiBackend string) ([]byte, error) {
	return runner.RepairJSON(workDir, jsonFilePath, aiBackend)
}
