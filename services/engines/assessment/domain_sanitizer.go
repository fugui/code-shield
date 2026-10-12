package assessment

import (
	"strings"
)

// SanitizeDomainPrompt 清洗领域规则内容，防止领域提示词污染通用评估协议：
// 1. 剔除手写的输出格式、JSON 说明、标准 JSON 样例等旧版协议章节（由 OutputContract 单一真实源统管）
// 2. 剔除独立的顶层 Role 或 Task 声明段落（由 Assessment Profile 统管）
func SanitizeDomainPrompt(content string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	skipping := false
	skipLevel := 0
	shallowSkip := false

	isOutputContractHeading := func(title string) bool {
		return strings.Contains(title, "输出格式") ||
			strings.Contains(title, "JSON 格式") ||
			strings.Contains(title, "标准 JSON") ||
			strings.Contains(title, "输出通道")
	}

	isMetaHeading := func(title string) bool {
		return strings.HasPrefix(title, "Role") ||
			strings.HasPrefix(title, "Task")
	}

	headingTitle := func(line string) string {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			return ""
		}
		title := strings.TrimLeft(trimmed, "#")
		return strings.TrimSpace(title)
	}

	headingLevel := func(line string) int {
		trimmed := strings.TrimSpace(line)
		level := 0
		for _, char := range trimmed {
			if char != '#' {
				break
			}
			level++
		}
		return level
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		title := headingTitle(trimmed)

		if title != "" {
			if isOutputContractHeading(title) {
				skipping = true
				skipLevel = headingLevel(trimmed)
				shallowSkip = false
				continue
			}
			if isMetaHeading(title) {
				skipping = true
				skipLevel = headingLevel(trimmed)
				shallowSkip = true
				continue
			}

			// 遇到了非跳过的其他标题
			if skipping {
				if shallowSkip || headingLevel(trimmed) <= skipLevel {
					skipping = false
					shallowSkip = false
					kept = append(kept, line)
					continue
				}
			}
		}

		if skipping {
			continue
		}
		kept = append(kept, line)
	}

	return strings.TrimSpace(strings.Join(kept, "\n"))
}
