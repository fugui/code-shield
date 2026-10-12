package assessment

import (
	"strings"
)

// SanitizeDomainPrompt 清洗领域规则内容，防止领域提示词污染通用评估协议：
// 1. 剔除手写的输出格式、JSON 说明、标准 JSON 样例等旧版协议章节（由 OutputContract 单一真实源统管）
// 2. 剔除独立的顶层 Role 或 Task/角色/任务/指令 声明段落（由统一框架统管）
// 3. 过滤正文中可能残留的与通用契约冲突的行级指令（如强行要求输出 findings 数组为 [] 等）
func SanitizeDomainPrompt(content string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	skipping := false
	skipLevel := 0
	shallowSkip := false

	stripLeadingNumbering := func(s string) string {
		trimmed := strings.TrimSpace(s)
		runes := []rune(trimmed)
		start := 0
		for start < len(runes) {
			r := runes[start]
			if (r >= '0' && r <= '9') || r == '.' || r == '、' || r == ' ' || r == '\t' ||
				r == '一' || r == '二' || r == '三' || r == '四' || r == '五' || r == '六' || r == '七' || r == '八' || r == '九' || r == '十' ||
				r == '(' || r == ')' || r == '（' || r == '）' {
				start++
			} else {
				break
			}
		}
		if start > 0 && start < len(runes) {
			return strings.TrimSpace(string(runes[start:]))
		}
		return trimmed
	}

	isOutputContractHeading := func(title string) bool {
		stripped := stripLeadingNumbering(title)
		return strings.Contains(stripped, "输出格式") ||
			strings.Contains(stripped, "输出约束") ||
			strings.Contains(stripped, "输出规范") ||
			strings.Contains(stripped, "输出要求") ||
			strings.Contains(stripped, "JSON 格式") ||
			strings.Contains(stripped, "标准 JSON") ||
			strings.Contains(stripped, "输出通道") ||
			strings.Contains(stripped, "Output Contract") ||
			strings.Contains(stripped, "Output Format")
	}

	isMetaHeading := func(title string) bool {
		stripped := stripLeadingNumbering(title)
		if strings.HasPrefix(stripped, "Role") ||
			strings.HasPrefix(stripped, "Task") ||
			strings.HasPrefix(stripped, "Objective") ||
			strings.HasPrefix(stripped, "Mission") {
			return true
		}
		if strings.HasPrefix(stripped, "角色") ||
			strings.HasPrefix(stripped, "任务") ||
			strings.HasPrefix(stripped, "目标") ||
			strings.HasPrefix(stripped, "背景") ||
			strings.HasSuffix(stripped, "分析指令") {
			return true
		}
		return false
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

	isConflictingInlineRule := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		return (strings.Contains(trimmed, "findings 数组") && strings.Contains(trimmed, "[]")) ||
			(strings.Contains(trimmed, "findings 设为") && strings.Contains(trimmed, "[]"))
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

		if isConflictingInlineRule(trimmed) {
			continue
		}

		kept = append(kept, line)
	}

	return strings.TrimSpace(strings.Join(kept, "\n"))
}
