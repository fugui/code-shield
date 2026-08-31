package services

import (
	"code-shield/models"
	"testing"
)

func TestCalibrateSeverityDeterministically(t *testing.T) {
	tests := []struct {
		name         string
		category     string
		verdict      string
		codeSnippet  string
		expectedSev  string
		expectedRule string
	}{
		{
			name:         "Grisu2 浮点栈越界写 - 宏隔离保护",
			category:     "内存管理问题-栈溢出/写越界",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "#if FMT_USE_GRISU\n  char buffer[100];\n  // write\n#endif",
			expectedSev:  "一般",
			expectedRule: "RULE_MEM_CORRUPTION_MACRO_GUARDED",
		},
		{
			name:         "通用栈写穿/堆溢出 - 默认可达",
			category:     "CWE-787: Out-of-bounds Write / 堆栈写越界",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "void write_data(char* dst) { memcpy(dst, src, len); }",
			expectedSev:  "严重",
			expectedRule: "RULE_MEM_CORRUPTION_DEFAULT_REACHABLE",
		},
		{
			name:         "parse_arg_id 堆越界读 - 默认可达确定性崩溃",
			category:     "CWE-125: Out-of-bounds Read / 词法分析读越界",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "do { c = *++it; } while (it != end);",
			expectedSev:  "高",
			expectedRule: "RULE_CRASH_DETERMINISTIC",
		},
		{
			name:         "buffered_file::fileno 空指针解引用",
			category:     "CWE-476: NULL Pointer Dereference / 空指针解引用",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "int fd = file_->fileno();",
			expectedSev:  "高",
			expectedRule: "RULE_CRASH_DETERMINISTIC",
		},
		{
			name:         "格式化宽度无上限分配 2GB - 内存分配 DoS",
			category:     "资源管理问题-未受限大内存分配 DoS/OOM",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "buffer.reserve(width);",
			expectedSev:  "一般",
			expectedRule: "RULE_RESOURCE_DOS_OR_FLAKY",
		},
		{
			name:         "条件性触发缺陷 (CONDITIONAL)",
			category:     "CWE-787: Out-of-bounds Write",
			verdict:      models.DebateVerdictConditional,
			codeSnippet:  "write_buf();",
			expectedSev:  "一般",
			expectedRule: "RULE_CONDITIONAL_MACRO_DOWNGRADE",
		},
		{
			name:         "架构坏味道与防御性缺失",
			category:     "架构规范-公共函数防御性校验缺失",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "void print_str(const char* s) { puts(s); }",
			expectedSev:  "建议",
			expectedRule: "RULE_ARCH_STYLE_SUGGESTION",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sev, rule := CalibrateSeverityDeterministically(tt.category, tt.verdict, tt.codeSnippet)
			if sev != tt.expectedSev {
				t.Errorf("Expected severity %q, got %q", tt.expectedSev, sev)
			}
			if rule != tt.expectedRule {
				t.Errorf("Expected rule %q, got %q", tt.expectedRule, rule)
			}
		})
	}
}

func TestCalibrateFindings(t *testing.T) {
	findings := []models.AnalysisFinding{
		{
			Title:       "栈写越界",
			Category:    "CWE-787 Out-of-bounds Write",
			CodeSnippet: "char buf[10]; buf[20] = 'a';",
		},
		{
			Title:       "空指针解引用",
			Category:    "CWE-476 Null Pointer Dereference",
			CodeSnippet: "ptr->call();",
		},
	}

	calibrated := CalibrateFindings(findings)
	if len(calibrated) != 2 {
		t.Fatalf("Expected 2 findings, got %d", len(calibrated))
	}
	if calibrated[0].Severity != "严重" {
		t.Errorf("Finding[0] expected 严重, got %s", calibrated[0].Severity)
	}
	if calibrated[1].Severity != "高" {
		t.Errorf("Finding[1] expected 高, got %s", calibrated[1].Severity)
	}
}
