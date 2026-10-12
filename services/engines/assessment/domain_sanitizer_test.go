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
