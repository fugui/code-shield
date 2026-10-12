package assessment

import (
	"strings"
	"testing"
)

func TestSanitizeDomainPrompt(t *testing.T) {
	raw := `# Role: 资深自动化测试与质量评估专家

## Task
请对测试代码文件进行深度审计。

## 审计维度与判定标准
### 1. 断言有效性
空测试属于严重问题。

## 输出格式与约束
1. 输出形式：纯 JSON
### JSON 格式说明
{"findings": []}
### 标准 JSON 真实示例
{"summary": "ok"}

## 补充原则
必须严格执行。`

	sanitized := SanitizeDomainPrompt(raw)

	if strings.Contains(sanitized, "Role") {
		t.Errorf("expected Role to be stripped, got: %s", sanitized)
	}
	if strings.Contains(sanitized, "Task") {
		t.Errorf("expected Task to be stripped, got: %s", sanitized)
	}
	if strings.Contains(sanitized, "输出格式") {
		t.Errorf("expected 输出格式 to be stripped, got: %s", sanitized)
	}
	if strings.Contains(sanitized, "JSON 格式") {
		t.Errorf("expected JSON 格式 to be stripped, got: %s", sanitized)
	}
	if strings.Contains(sanitized, "findings") {
		t.Errorf("expected findings to be stripped, got: %s", sanitized)
	}
	if !strings.Contains(sanitized, "## 审计维度与判定标准") {
		t.Errorf("expected 审计维度与判定标准 to be kept, got: %s", sanitized)
	}
	if !strings.Contains(sanitized, "空测试属于严重问题") {
		t.Errorf("expected inner content to be kept, got: %s", sanitized)
	}
	if !strings.Contains(sanitized, "## 补充原则") {
		t.Errorf("expected 补充原则 to be kept, got: %s", sanitized)
	}
}

func TestSanitizeDomainPrompt_ChineseAndNumberedHeadings(t *testing.T) {
	raw := `# C/C++ Coredump 潜在缺陷检测 分析指令

## 1. 角色与任务定义
你是一个 C/C++ 专家。

## 2. 扫描聚焦领域
### 2.1 内存越界
指针移动超出边界。

## 3. 分析与判定原则
1. 拒绝虚报。
2. 无代码响应：如果未提供源码，findings 数组必须为 []，并在 summary 中说明。
3. 遵循 C++17 标准。

## 4. 输出格式与规范
输出合法 JSON。`

	sanitized := SanitizeDomainPrompt(raw)

	if strings.Contains(sanitized, "角色与任务定义") {
		t.Errorf("expected 角色与任务定义 to be stripped, got: %s", sanitized)
	}
	if strings.Contains(sanitized, "输出格式与规范") {
		t.Errorf("expected 输出格式与规范 to be stripped, got: %s", sanitized)
	}
	if strings.Contains(sanitized, "findings 数组必须为 []") {
		t.Errorf("expected inline conflicting rule to be stripped, got: %s", sanitized)
	}
	if !strings.Contains(sanitized, "## 2. 扫描聚焦领域") {
		t.Errorf("expected 扫描聚焦领域 to be kept, got: %s", sanitized)
	}
	if !strings.Contains(sanitized, "指针移动超出边界") {
		t.Errorf("expected inner content to be kept, got: %s", sanitized)
	}
	if !strings.Contains(sanitized, "1. 拒绝虚报。") {
		t.Errorf("expected 拒绝虚报 to be kept, got: %s", sanitized)
	}
	if !strings.Contains(sanitized, "3. 遵循 C++17 标准。") {
		t.Errorf("expected C++17 标准 to be kept, got: %s", sanitized)
	}
}
