package services

import (
	"code-shield/models"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// AIRequest 封装一次 AI CLI 调用所需的全部参数（与具体 CLI 无关）
type AIRequest struct {
	WorkDir    string   // 执行目录（代码仓根目录）
	PromptFile string   // 系统提示词文件的绝对路径（可选，为空时仅使用 PromptMsg）
	PromptMsg  string   // 用户提示消息
	InputFiles []string // 需要分析的文件列表（相对路径），AI 自行读取
	OutputPath string   // AI 输出文档的目标路径
	TimeoutMin int      // 执行超时（分钟），0 表示默认 30 分钟
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

// GetAIInvoker 根据名称返回对应的 AIInvoker，未找到则回退到 claude
func GetAIInvoker(name string) AIInvoker {
	if inv, ok := invokerRegistry[name]; ok {
		return inv
	}
	return invokerRegistry["claude"]
}

// RepairJSON calls AI to fix syntax errors in a JSON file.
// workDir is the working directory for the AI invocation.
// jsonFilePath is the absolute path to the malformed JSON file.
// Returns the cleaned, repaired JSON bytes ready for json.Unmarshal.
func RepairJSON(workDir, jsonFilePath string) ([]byte, error) {
	ext := filepath.Ext(jsonFilePath)
	fixedPath := strings.TrimSuffix(jsonFilePath, ext) + ".fixed" + ext

	log.Printf("[AI] Invoking AI to repair JSON: %s\n", jsonFilePath)

	repairMsg := "你是一个 JSON 修复工具。请读取指定的 JSON 文件，修复其中的所有语法错误（如未转义的引号、尾部多余逗号、缺失的括号等），然后输出修复后的合法 JSON。" +
		"只输出纯 JSON，不要添加 Markdown 代码块标记，不要添加任何解释文字，保持原始数据结构和内容不变，只修复语法错误"

	invoker := GetAIInvoker(models.AppConfig.AI.Backend)
	if err := invoker.Invoke(AIRequest{
		WorkDir:    workDir,
		PromptMsg:  repairMsg,
		InputFiles: []string{jsonFilePath},
		OutputPath: fixedPath,
		TimeoutMin: 5,
	}); err != nil {
		return nil, fmt.Errorf("AI repair invocation failed: %w", err)
	}

	fixed, err := os.ReadFile(fixedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read repaired JSON: %w", err)
	}

	return cleanJSONFromAI(fixed), nil
}

