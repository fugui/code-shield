package invoker

import (
	"context"
	"sort"
	"sync"
)

// LLMWorkContext 承载单次 LLM 调用的业务归属与当前微任务环节（用于算力看板透视）
type LLMWorkContext struct {
	ReportID uint   `json:"report_id"`
	RepoName string `json:"repo_name"`
	TaskType string `json:"task_type"`
	Stage    string `json:"stage"`    // 执行环节/阶段，如 "Tier 1: 初筛猎手"、"Tier 2: 裁判终审"、"Tier 3: 全仓汇总"、"系统工具: JSON 语法修复"
	SubTask  string `json:"sub_task"` // 当前微任务描述，如 "分片 2/5 (src/runtime/sys.go)"、"批次 1 (候选点 1~3 终审)"
	Detail   string `json:"detail,omitempty"`
}

type llmWorkContextKey struct{}

// WithLLMWorkContext 将 LLMWorkContext 注入 context.Context 中
func WithLLMWorkContext(ctx context.Context, work *LLMWorkContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, llmWorkContextKey{}, work)
}

// LLMWorkContextFromContext 从 context.Context 中提取 LLMWorkContext
func LLMWorkContextFromContext(ctx context.Context) *LLMWorkContext {
	if ctx == nil {
		return nil
	}
	if val, ok := ctx.Value(llmWorkContextKey{}).(*LLMWorkContext); ok {
		return val
	}
	return nil
}

// AIRequest 封装一次 AI 调用所需的全部参数（与具体驱动后端无关）
type AIRequest struct {
	ParentContext  context.Context // 父 context，支持提前取消
	WorkDir        string          // 执行目录（代码仓根目录）
	PromptFile     string          // 系统提示词文件的绝对路径（可选，为空时仅使用 PromptMsg）
	PromptMsg      string          // 用户提示消息
	InputFiles     []string        // 需要分析的文件列表（相对路径），AI 自行读取
	OutputPath     string          // AI 输出文档的目标路径
	TimeoutMin     int             // 执行超时（分钟），0 表示默认 60 分钟
	ModelName      string          // 指定的模型名，例如 "glm5.1" 或 "models/qwen3.5"
	Temperature    *float64        // 可选请求级温度覆盖（如 0.0 用于绝对确定性场景）
	ResponseFormat string          // 请求期望输出格式："json"（强制 json_object 结构化模式）、"text"/"markdown" 或 ""
	Env            []string        // 附加/覆盖环境变量（例如 XDG_DATA_HOME）
	WorkContext    *LLMWorkContext // 业务溯源与算力监控上下文
}

// AIInvoker 定义了底层 AI 驱动调用的统一接口。
type AIInvoker interface {
	// Invoke 执行 AI 任务，返回 nil 表示成功
	Invoke(req AIRequest) error
	// Name 返回此驱动后端的名称（用于日志和识别）
	Name() string
}

var (
	registryMu      sync.RWMutex
	invokerRegistry = map[string]AIInvoker{}
)

// RegisterAIInvoker 注册一个底层 AI 驱动
func RegisterAIInvoker(name string, inv AIInvoker) {
	registryMu.Lock()
	defer registryMu.Unlock()
	invokerRegistry[name] = inv
}

// GetRawInvoker 获取底层未加代理的原始 AIInvoker 实例
func GetRawInvoker(name string) (AIInvoker, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	inv, ok := invokerRegistry[name]
	return inv, ok
}

// IsValidAIBackend 检查 backend 名称是否合法（支持空值跟随全局，或已注册的 Invoker）
func IsValidAIBackend(name string) bool {
	if name == "" {
		return true
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := invokerRegistry[name]
	return ok
}

// ListRegisteredInvokers 列出当前所有已注册的 AI 驱动名称列表
func ListRegisteredInvokers() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	list := make([]string, 0, len(invokerRegistry))
	for k := range invokerRegistry {
		list = append(list, k)
	}
	sort.Strings(list)
	return list
}
