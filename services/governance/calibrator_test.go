package governance

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
			expectedSev:  "致命",
			expectedRule: "RULE_MEM_CORRUPTION_DEFAULT_REACHABLE",
		},
		{
			name:         "parse_arg_id 堆越界读 - 默认可达确定性崩溃",
			category:     "CWE-125: Out-of-bounds Read / 词法分析读越界",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "do { c = *++it; } while (it != end);",
			expectedSev:  "严重",
			expectedRule: "RULE_CRASH_DETERMINISTIC",
		},
		{
			name:         "buffered_file::fileno 空指针解引用",
			category:     "CWE-476: NULL Pointer Dereference / 空指针解引用",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "int fd = file_->fileno();",
			expectedSev:  "严重",
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
			name:         "预期结果不完整断言",
			category:     "断言有效性-预期结果不完整",
			verdict:      models.DebateVerdictConfirmed,
			codeSnippet:  "TEST(Calculator, Add) { EXPECT_EQ(Calculator::Add(1, 2), 3); }",
			expectedSev:  "一般",
			expectedRule: "RULE_INCOMPLETE_EXPECTED_RESULTS",
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
	if calibrated[0].Severity != "致命" {
		t.Errorf("Finding[0] expected 致命, got %s", calibrated[0].Severity)
	}
	if calibrated[1].Severity != "严重" {
		t.Errorf("Finding[1] expected 严重, got %s", calibrated[1].Severity)
	}
}

func TestCalibrateSeverityWithTaxonomy(t *testing.T) {
	taxonomy := models.CategoryTaxonomy{
		SchemaVersion: 3,
		Categories: []models.CategoryDefinition{
			{Code: "FLOAT_BUSINESS_THRESHOLD", Label: "关键业务阈值误判", DefaultSeverity: "严重"},
			{Code: "CJSON_PARSE_LEAK", Label: "cJSON 解析结果未释放", DefaultSeverity: "一般"},
		},
	}

	tests := []struct {
		name         string
		code         string
		category     string
		verdict      string
		expectedSev  string
		expectedRule string
	}{
		{
			name:         "governed code uses business severity",
			code:         "FLOAT_BUSINESS_THRESHOLD",
			category:     "anything",
			verdict:      models.DebateVerdictConfirmed,
			expectedSev:  "严重",
			expectedRule: "RULE_TAXONOMY_FLOAT_BUSINESS_THRESHOLD",
		},
		{
			name:         "governed label uses business severity",
			code:         "",
			category:     "cJSON 解析结果未释放",
			verdict:      models.DebateVerdictConfirmed,
			expectedSev:  "一般",
			expectedRule: "RULE_TAXONOMY_CJSON_PARSE_LEAK",
		},
		{
			name:         "conditional defect still downgrades",
			code:         "FLOAT_BUSINESS_THRESHOLD",
			category:     "关键业务阈值误判",
			verdict:      models.DebateVerdictConditional,
			expectedSev:  "一般",
			expectedRule: "RULE_CONDITIONAL_MACRO_DOWNGRADE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sev, rule := CalibrateSeverityWithTaxonomy(taxonomy, tt.code, tt.category, tt.verdict, "")
			if sev != tt.expectedSev || rule != tt.expectedRule {
				t.Fatalf("severity/rule = %q/%q, want %q/%q", sev, rule, tt.expectedSev, tt.expectedRule)
			}
		})
	}
}

func TestCalibrateFindingsWithTaxonomy(t *testing.T) {
	taxonomy := models.CategoryTaxonomy{
		SchemaVersion: 3,
		Categories: []models.CategoryDefinition{
			{Code: "UNORDERED_CONTRACT_ORDER", Label: "跨系统契约顺序不稳定", DefaultSeverity: "严重"},
		},
	}
	findings := []models.AnalysisFinding{
		{CategoryCode: "UNORDERED_CONTRACT_ORDER", Category: "旧标签", CategoryDetail: "unknown"},
		{Category: "CWE-787: Out-of-bounds Write"},
	}

	calibrated := CalibrateFindingsWithTaxonomy(taxonomy, findings)
	if calibrated[0].Severity != "严重" || calibrated[0].CalibrationRule != "RULE_TAXONOMY_UNORDERED_CONTRACT_ORDER" {
		t.Fatalf("governed finding = %q/%q", calibrated[0].Severity, calibrated[0].CalibrationRule)
	}
	if calibrated[1].Severity != "致命" || calibrated[1].CalibrationRule != "RULE_MEM_CORRUPTION_DEFAULT_REACHABLE" {
		t.Fatalf("fallback finding = %q/%q", calibrated[1].Severity, calibrated[1].CalibrationRule)
	}
}

func TestCalibrateFindingsSkipsValidAssessments(t *testing.T) {
	findings := []models.AnalysisFinding{
		{
			Title:            "测试用例合格",
			Severity:         "合格",
			Category:         "无问题",
			AssessmentStatus: "valid",
		},
		{
			Title:            "非线程创建命中",
			Severity:         "合格",
			Category:         "无问题",
			AssessmentStatus: "not_thread_creation",
		},
	}

	calibrated := CalibrateFindings(findings)
	if calibrated[0].Severity != "合格" {
		t.Errorf("valid assessment severity expected 合格, got %s", calibrated[0].Severity)
	}
	if calibrated[1].Severity != "合格" {
		t.Errorf("not_thread_creation severity expected 合格, got %s", calibrated[1].Severity)
	}
}
