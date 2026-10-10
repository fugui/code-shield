package assessment

import (
	"os"
	"path/filepath"
	"strings"

	"code-shield/services/coverage"
)

// maxSnippetLines 定义回退读取源码行时的上限防护，防止异常大范围区间撑爆存储
const maxSnippetLines = 150

// ExtractIssueSnippet 提取问题的源码片段：
// 1. 优先使用大模型评测直接产出的 issueCode；
// 2. 若 issueCode 为空，回退读取代码仓文件并在 [StartLine, EndLine] 区间内切片；
// 3. 各种边界异常下保证安全返回空字符串，不产生 panic。
func ExtractIssueSnippet(ctx AssessmentContext, unit coverage.PlanUnit, issueCode string) string {
	if trimmed := strings.TrimSpace(issueCode); trimmed != "" {
		return trimmed
	}

	if ctx.EngineContext == nil || strings.TrimSpace(ctx.EngineContext.CodesPath) == "" || strings.TrimSpace(unit.Path) == "" {
		return ""
	}

	filePath := filepath.Join(ctx.EngineContext.CodesPath, filepath.FromSlash(unit.Path))
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)
	if totalLines == 0 {
		return ""
	}

	start := unit.StartLine
	end := unit.EndLine

	// 1. 指定了合法的起止区间 [start, end]
	if start > 0 && end >= start {
		if start > totalLines {
			return ""
		}
		if end > totalLines {
			end = totalLines
		}
		if end-start+1 > maxSnippetLines {
			end = start + maxSnippetLines - 1
		}
		return strings.TrimSpace(strings.Join(lines[start-1:end], "\n"))
	}

	// 2. 仅指定了起始行 start
	if start > 0 && start <= totalLines {
		end = start + 30
		if end > totalLines {
			end = totalLines
		}
		return strings.TrimSpace(strings.Join(lines[start-1:end], "\n"))
	}

	return ""
}
