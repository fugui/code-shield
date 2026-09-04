package runner

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"code-shield/models"
	"code-shield/services/invoker"
)

// ToLineStr 将 AI 输出的 line_number（可能是数字或字符串）统一转为 string
func ToLineStr(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64: // JSON 数字默认解码为 float64
		return fmt.Sprintf("%d", int(val))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// CleanJSONFromAI 去除 Markdown 代码块标记（```json ... ```）并防御性提取纯 JSON
func CleanJSONFromAI(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))

	// 去除首尾 ```json 或 ``` 标记
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}

	// 如果外围存在自然语言文字说明，尝试截取 JSON 对象主体
	if !strings.HasPrefix(s, "{") {
		if start := strings.Index(s, "{"); start != -1 {
			if end := strings.LastIndex(s, "}"); end > start {
				s = s[start : end+1]
			}
		}
	}

	// 修复内部字符串值中未转义的引号
	s = FixUnescapedQuotes(s)

	return []byte(s)
}

// FixUnescapedQuotes 修复大模型生成的 JSON 中字符串值内未转义的引号（例如: {"title": "Use "proper" method"}）
// 使用状态机逐字符扫描，智能识别结构性分隔符与未转义的内容引号
func FixUnescapedQuotes(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}

	var buf strings.Builder
	buf.Grow(len(s) + 64)

	inString := false
	i := 0

	for i < len(s) {
		ch := s[i]

		// 保留已转义的字符
		if inString && ch == '\\' && i+1 < len(s) {
			buf.WriteByte(ch)
			buf.WriteByte(s[i+1])
			i += 2
			continue
		}

		if ch == '"' {
			if !inString {
				inString = true
				buf.WriteByte(ch)
				i++
				continue
			}

			// 当前在字符串内部，判断引号是结束闭合还是未转义内容
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}

			isStructural := false
			if j >= len(s) {
				isStructural = true
			} else {
				next := s[j]
				isStructural = next == ',' || next == ':' || next == '}' || next == ']'
			}

			if isStructural {
				inString = false
				buf.WriteByte(ch)
			} else {
				buf.WriteByte('\\')
				buf.WriteByte(ch)
			}
			i++
			continue
		}

		buf.WriteByte(ch)
		i++
	}

	return buf.String()
}

// CleanAnalysisTempFiles 清理分析尝试过程中产生的所有临时文件和错误同名目录
func CleanAnalysisTempFiles(jsonPath string) {
	os.Remove(jsonPath + ".output.txt")
	os.Remove(jsonPath + ".debug.log")
	ext := filepath.Ext(jsonPath)
	basePath := strings.TrimSuffix(jsonPath, ext)
	os.Remove(basePath + ".fixed" + ext)
	os.Remove(jsonPath)
	os.Remove(jsonPath + ".raw")
	os.RemoveAll(basePath)
}

// CleanSynthesisTempFiles 清理报告合成过程中产生的临时文件
func CleanSynthesisTempFiles(reportPath string) {
	os.Remove(reportPath)
	os.Remove(reportPath + ".output.txt")
	os.Remove(reportPath + ".debug.log")
}

// RecoverAIOutput 自动处理 AI CLI 将输出写入同名目录而非直接文件的异常（例如写到 chunk-1/chunk-1.json）
func RecoverAIOutput(expectedPath string) {
	if info, err := os.Stat(expectedPath); err == nil && !info.IsDir() {
		return
	}

	ext := filepath.Ext(expectedPath)
	baseName := filepath.Base(strings.TrimSuffix(expectedPath, ext))
	dirPath := strings.TrimSuffix(expectedPath, ext)

	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		return
	}

	candidatePath := filepath.Join(dirPath, baseName+ext)
	if _, err := os.Stat(candidatePath); err == nil {
		log.Printf("[Cleaner] Recovering AI output: moving %s → %s\n", candidatePath, expectedPath)
		if content, readErr := os.ReadFile(candidatePath); readErr == nil {
			if writeErr := os.WriteFile(expectedPath, content, 0644); writeErr == nil {
				os.RemoveAll(dirPath)
				return
			}
		}
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ext {
			fallbackPath := filepath.Join(dirPath, entry.Name())
			log.Printf("[Cleaner] Recovering AI output (fallback): moving %s → %s\n", fallbackPath, expectedPath)
			if content, readErr := os.ReadFile(fallbackPath); readErr == nil {
				if writeErr := os.WriteFile(expectedPath, content, 0644); writeErr == nil {
					os.RemoveAll(dirPath)
					return
				}
			}
			break
		}
	}

	log.Printf("[Cleaner] Cleaning up empty AI-created directory: %s\n", dirPath)
	os.RemoveAll(dirPath)
}

// SanitizeMarkdownReport 智能清洗大模型生成的 Markdown 报告，剥除外层代码围栏与解包 JSON
func SanitizeMarkdownReport(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))

	stripOuterFence := func(str string) string {
		str = strings.TrimSpace(str)
		if strings.HasPrefix(str, "```") {
			if idx := strings.Index(str, "\n"); idx != -1 {
				firstLine := strings.TrimSpace(str[:idx])
				if firstLine == "```" || firstLine == "```markdown" || firstLine == "```md" || firstLine == "```json" || firstLine == "```text" {
					if strings.HasSuffix(str, "```") {
						trimmed := strings.TrimSuffix(str, "```")
						return strings.TrimSpace(trimmed[idx+1:])
					}
				}
			}
		}
		return str
	}

	extractFromJSON := func(str string) string {
		str = strings.TrimSpace(str)
		if strings.HasPrefix(str, "{") && strings.HasSuffix(str, "}") {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(str), &obj); err == nil {
				for _, key := range []string{"report", "markdown", "content", "summary", "result", "text"} {
					if val, ok := obj[key].(string); ok && len(strings.TrimSpace(val)) > 0 {
						return strings.TrimSpace(val)
					}
				}
			}
		}
		return str
	}

	s = stripOuterFence(s)
	s = extractFromJSON(s)
	s = stripOuterFence(s)

	return []byte(s)
}

// RepairJSON 调用原生 AI 修复 JSON 语法错误，输出合法 JSON 字节切片
func RepairJSON(workDir, jsonFilePath, aiBackend string) ([]byte, error) {
	ext := filepath.Ext(jsonFilePath)
	fixedPath := strings.TrimSuffix(jsonFilePath, ext) + ".fixed" + ext

	backend := models.AppConfig.AI.ToolBackends.RepairJSON
	if backend == "" {
		backend = "native"
	}
	if !invoker.IsValidAIBackend(backend) {
		backend = aiBackend
		if backend == "" {
			backend = models.AppConfig.AI.Backend
		}
	}

	aiInv := GetAIInvoker(backend)
	log.Printf("[Cleaner] Invoking %s to repair JSON: %s\n", aiInv.Name(), jsonFilePath)

	rawContent, err := os.ReadFile(jsonFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read malformed JSON: %w", err)
	}

	repairMsg := "你是一个 JSON 语法修复工具。请修复以下内容中的语法错误（未转义引号、多余逗号、括号缺失等）。" +
		"只输出纯 JSON，不要 Markdown 代码块标记，不要任何解释文字，保持原始数据结构不变。"

	zeroTemp := 0.0
	req := invoker.AIRequest{
		WorkDir:        workDir,
		PromptMsg:      repairMsg + "\n\n" + string(rawContent),
		InputFiles:     []string{jsonFilePath},
		OutputPath:     fixedPath,
		TimeoutMin:     2,
		Temperature:    &zeroTemp,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
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
