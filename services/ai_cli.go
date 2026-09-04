package services

import (
	"code-shield/models"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// AIRequest 封装一次 AI CLI 调用所需的全部参数（与具体 CLI 无关）
type AIRequest struct {
	ParentContext  context.Context // 父 context，支持提前取消
	WorkDir        string          // 执行目录（代码仓根目录）
	PromptFile     string          // 系统提示词文件的绝对路径（可选，为空时仅使用 PromptMsg）
	PromptMsg      string          // 用户提示消息
	InputFiles     []string        // 需要分析的文件列表（相对路径），AI 自行读取
	OutputPath     string          // AI 输出文档的目标路径
	TimeoutMin     int             // 执行超时（分钟），0 表示默认 60 分钟
	ModelName      string          // 新增：指定的模型名，例如 "glm5.1" 或 "models/qwen3.5"
	Temperature    *float64        // 新增：可选请求级温度覆盖（如 0.0 用于绝对确定性场景）
	ResponseFormat string          // 请求期望输出格式："json"（强制 json_object 结构化模式）、"text"/"markdown"（普通文本排版模式）或 ""（默认文本）
	Env            []string        // 附加/覆盖环境变量（例如 XDG_DATA_HOME）
}

// AIInvoker 定义了 AI CLI 调用的统一接口。
// 不同的 CLI 后端（claude、opencode）实现此接口。
type AIInvoker interface {
	// Invoke 执行 AI 任务，返回 nil 表示成功
	Invoke(req AIRequest) error
	// Name 返回此 CLI 后端的名称（用于日志）
	Name() string
}

// invokerRegistry 存储已注册的 AI CLI 实现
var invokerRegistry = map[string]AIInvoker{}

// RegisterAIInvoker 注册一个 AI CLI 后端
func RegisterAIInvoker(name string, invoker AIInvoker) {
	invokerRegistry[name] = invoker
}

// IsValidAIBackend 检查 backend 名称是否合法（支持空值跟随全局，或已注册的 Invoker）
func IsValidAIBackend(name string) bool {
	if name == "" {
		return true
	}
	_, ok := invokerRegistry[name]
	return ok
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
	}

	// 2. 调用真正的底层 AI 后端
	return w.delegate.Invoke(req)
}

// GetAIInvoker 根据名称返回对应的 AIInvoker，未找到则回退到 claude。
// 当调度器启用时，自动返回 DispatchingInvoker 包装实例。
func GetAIInvoker(name string) AIInvoker {
	var inv AIInvoker
	var ok bool
	if inv, ok = invokerRegistry[name]; !ok {
		log.Printf("[AI] WARNING: AI backend %q is not registered, falling back to claude\n", name)
		inv = invokerRegistry["claude"]
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

	invoker := GetAIInvoker(backend)
	log.Printf("[AI] Invoking %s to repair JSON: %s\n", invoker.Name(), jsonFilePath)

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
	}

	if err := invoker.Invoke(req); err != nil {
		return nil, fmt.Errorf("AI repair invocation failed: %w", err)
	}
	defer os.Remove(fixedPath)

	fixed, err := os.ReadFile(fixedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read repaired JSON: %w", err)
	}

	return cleanJSONFromAI(fixed), nil
}
