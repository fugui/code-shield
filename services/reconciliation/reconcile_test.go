package reconciliation

import (
	"os"
	"testing"

	"code-shield/models"
	"code-shield/services/invoker"
)

func TestGenerateItemUID(t *testing.T) {
	fp := "12f5a8b9c0d1e2f3"
	uid1 := GenerateItemUID(19375, fp, 0)
	expected1 := "F19375-12f5a8b9"
	if uid1 != expected1 {
		t.Errorf("Expected %s, got %s", expected1, uid1)
	}

	uid2 := GenerateItemUID(19375, fp, 1)
	expected2 := "F19375-12f5a8b9-1"
	if uid2 != expected2 {
		t.Errorf("Expected %s, got %s", expected2, uid2)
	}
}

func TestNormalizeSeverityRange(t *testing.T) {
	// Same severity
	r1, c1 := NormalizeSeverityRange("建议", "建议")
	if r1 != "[\"建议\"]" || c1 {
		t.Errorf("Expected [\"建议\"] no conflict, got %s, conflict=%v", r1, c1)
	}

	// 建议 vs 致命 (Conflict: diff weight 4 - 1 = 3 >= 2)
	r2, c2 := NormalizeSeverityRange("建议", "致命")
	if r2 != "[\"建议\",\"致命\"]" || !c2 {
		t.Errorf("Expected [\"建议\",\"致命\"] with conflict, got %s, conflict=%v", r2, c2)
	}

	// 一般 vs 严重 (diff weight 3 - 2 = 1 < 2)
	r3, c3 := NormalizeSeverityRange("一般", "严重")
	if r3 != "[\"一般\",\"严重\"]" || c3 {
		t.Errorf("Expected [\"一般\",\"严重\"] no conflict, got %s, conflict=%v", r3, c3)
	}
}

func TestAnnealingPrunerRules(t *testing.T) {
	// 1. 低危建议项：连续 1 轮未复现 -> 保持 COVERAGE_GAP
	itemLow1 := SynthesisItem{
		FilePath:                "test.cpp",
		ConsecutiveMissedRounds: 0,
		Payload: models.AnalysisFinding{
			Severity: "建议",
		},
	}
	dec1 := EvaluateUnmatchedBaseItem(&itemLow1, 100, "", models.GovernanceModeFullLedger, true, false)
	if dec1.ShouldArchive || dec1.Lifecycle != LifecycleCoverageGap {
		t.Errorf("Expected low risk 1 round to stay in COVERAGE_GAP, got %+v", dec1)
	}

	// 2. 低危建议项：连续 2 轮未复现 -> 退火休眠 DORMANT_ARCHIVED
	itemLow2 := SynthesisItem{
		FilePath:                "test.cpp",
		ConsecutiveMissedRounds: 1, // evaluate will add 1 -> 2
		Payload: models.AnalysisFinding{
			Severity: "建议",
		},
	}
	dec2 := EvaluateUnmatchedBaseItem(&itemLow2, 100, "", models.GovernanceModeFullLedger, true, false)
	if !dec2.ShouldArchive || dec2.Lifecycle != LifecycleDormantArchived {
		t.Errorf("Expected low risk 2 rounds to archive DORMANT_ARCHIVED, got %+v", dec2)
	}

	// 3. 高危漏洞：连续 10 轮未复现 -> 【高危永不冷寂】，必须留在活动工作集！
	itemHigh := SynthesisItem{
		FilePath:                "critical.cpp",
		ConsecutiveMissedRounds: 10,
		Payload: models.AnalysisFinding{
			Severity: "致命",
		},
	}
	decHigh := EvaluateUnmatchedBaseItem(&itemHigh, 100, "", models.GovernanceModeFullLedger, true, false)
	if decHigh.ShouldArchive || decHigh.Lifecycle != LifecycleCoverageGap {
		t.Errorf("High risk must never dormant, got %+v", decHigh)
	}
}

func TestChangeFocusReconciliation(t *testing.T) {
	req := &ReconcileRequest{
		RepoID:          1,
		TaskTypeID:      1,
		CurrentReportID: 200,
		BaseReportID:    100,
		HeadCommit:      "c0ffee1",
		RepoUnchanged:   false,
		GovernanceMode:  models.GovernanceModeChangeFocus,
		ChangedFiles:    []string{"service/auth.go"},
		HunkRanges: map[string][]LineRange{
			"service/auth.go": {{Start: 50, End: 80}},
		},
	}

	baseItems := []SynthesisItem{
		// 历史条目 A: 在变动文件且在 Hunk 内 (55-60)
		{
			Fingerprint: "fp_auth_vuln",
			ItemUID:     "F100-auth01",
			FilePath:    "service/auth.go",
			LineNumber:  "55-60",
			Payload: models.AnalysisFinding{
				FilePath:    "service/auth.go",
				LineNumber:  "55-60",
				Title:       "未校验 Token 有效期",
				TriggerLine: "token.Verify()",
			},
		},
		// 历史条目 B: 在无关文件 (未触碰文件) -> 必须被 100% 旁路豁免！
		{
			Fingerprint: "fp_unrelated_vuln",
			ItemUID:     "F100-other01",
			FilePath:    "service/unrelated.go",
			LineNumber:  "10-20",
			Payload: models.AnalysisFinding{
				FilePath: "service/unrelated.go",
				Title:    "无关历史缺陷",
			},
		},
	}

	// 本次扫描：检出一个全新位置的缺陷 (NEW_IN_DIFF)
	currentFindings := []InternalFinding{
		{
			OriginalIndex: 0,
			NormPath:      "service/auth.go",
			StartLine:     70,
			EndLine:       75,
			CleanTrigger:  "log.print(user.password)",
			Fingerprint:   "fp_leak_pwd",
			ItemUID:       "F200-leak01",
			Payload: models.AnalysisFinding{
				FilePath:    "service/auth.go",
				LineNumber:  "70-75",
				Title:       "密码明文输出到日志",
				TriggerLine: "log.print(user.password)",
			},
		},
	}

	res, err := RunChangeFocusReconciliation(req, baseItems, currentFindings)
	if err != nil {
		t.Fatalf("RunChangeFocusReconciliation error: %v", err)
	}

	// 断言 1: 无关文件的缺陷绝对不应出现在结果中
	for _, it := range res.Ledger.Items {
		if it.FilePath == "service/unrelated.go" {
			t.Errorf("Unrelated file defect should be bypassed, but found: %+v", it)
		}
	}

	// 断言 2: 覆盖缺口必须为 0
	if res.Reconciliation.VanishedCoverageGap != 0 {
		t.Errorf("Change focus mode must have 0 COVERAGE_GAP, got %d", res.Reconciliation.VanishedCoverageGap)
	}

	// 断言 3: 本次引入新缺陷为 1 (NEW_IN_DIFF)
	if res.Ledger.Meta.NewIntroducedCount != 1 {
		t.Errorf("Expected 1 new introduced defect, got %d", res.Ledger.Meta.NewIntroducedCount)
	}

	// 断言 4: 变动 Hunk 内消失的历史条目 A 顺带核销为 RESOLVED_BY_CHANGE
	if res.Ledger.Meta.ResolvedHistoryCount != 1 {
		t.Errorf("Expected 1 resolved history defect, got %d", res.Ledger.Meta.ResolvedHistoryCount)
	}
}

func TestColdPoolResurrection(t *testing.T) {
	archived := []ArchivedItem{
		{
			Fingerprint: "fp_dormant_01",
			ItemUID:     "F100-dorm01",
			FilePath:    "util/helper.go",
			LineNumber:  "20-25",
			RoundsSeen:  []uint{90},
			Payload: models.AnalysisFinding{
				FilePath:    "util/helper.go",
				TriggerLine: "return compute(a, b)",
			},
		},
	}

	curr := InternalFinding{
		Fingerprint:  "fp_dormant_01",
		NormPath:     "util/helper.go",
		CleanTrigger: "returncompute(a,b)",
		ItemUID:      "F200-dorm01",
		Payload: models.AnalysisFinding{
			FilePath:    "util/helper.go",
			LineNumber:  "20-25",
			TriggerLine: "return compute(a, b)",
		},
	}

	resurrected, idx := TryResurrectFromColdPool(archived, &curr, 200)
	if resurrected == nil || idx != 0 {
		t.Fatalf("Expected resurrection from cold pool, got nil")
	}

	// 必须继承血统，标记为 EXISTED 而非 NEW
	if resurrected.DiffStatus != DiffStatusExisted {
		t.Errorf("Expected resurrected item to have DiffStatus EXISTED, got %s", resurrected.DiffStatus)
	}
	if resurrected.LifecycleStatus != LifecycleActive {
		t.Errorf("Expected LifecycleStatus ACTIVE, got %s", resurrected.LifecycleStatus)
	}
}

func TestCanonicalizeRelativePath(t *testing.T) {
	cases := []struct {
		input    string
		repoRoot string
		expected string
	}{
		{
			input:    "codes/scylladb/fmt/include/fmt/format.h",
			repoRoot: "/path/to/repo",
			expected: "include/fmt/format.h",
		},
		{
			input:    "./include/fmt/chrono.h",
			repoRoot: "",
			expected: "include/fmt/chrono.h",
		},
		{
			input:    "src/core/main.cpp",
			repoRoot: "",
			expected: "src/core/main.cpp",
		},
		{
			input:    "build/temp/sub/src/util.go",
			repoRoot: "",
			expected: "src/util.go",
		},
	}

	for _, c := range cases {
		actual := CanonicalizeRelativePath(c.input, c.repoRoot)
		if actual != c.expected {
			t.Errorf("CanonicalizeRelativePath(%q, %q) = %q, expected %q", c.input, c.repoRoot, actual, c.expected)
		}
	}
}

func TestCalculateSnippetOverlap(t *testing.T) {
	s1 := `template <typename T>
void print_arg(const T& val) {
    std::cout << val;
}`
	s2 := `template <typename T>
void print_arg(const T& val) {
    std::cout << val;
}`
	overlap := CalculateSnippetOverlap(s1, s2)
	if overlap != 1.0 {
		t.Errorf("Expected overlap 1.0 for identical snippets, got %f", overlap)
	}

	s3 := `template <typename T>
void print_arg(const T& val) {
    // some comment
    std::cout << val;
}`
	overlap3 := CalculateSnippetOverlap(s1, s3)
	if overlap3 < 0.70 {
		t.Errorf("Expected overlap >= 0.70, got %f", overlap3)
	}

	s4 := `int unrelated_function() { return 42; }`
	overlap4 := CalculateSnippetOverlap(s1, s4)
	if overlap4 > 0.30 {
		t.Errorf("Expected low overlap for unrelated snippet, got %f", overlap4)
	}
}

func TestSnippetOverlapTier3Funnel(t *testing.T) {
	// 模拟 printf.h:487 vs 488：触发代码单行 token 差异大，但上下文 snippet 重叠达 100%
	snippet := `template <typename Char>
void format_to(basic_format_args<Char> args) {
    parse_format_string(args);
}`

	base := InternalFinding{
		OriginalIndex: 0,
		Fingerprint:   "fp_printf_487",
		NormPath:      "include/fmt/printf.h",
		StartLine:     487,
		EndLine:       490,
		CleanTrigger:  "format_to(basic_format_args<Char> args)",
		Category:      "安全风险",
		Payload: models.AnalysisFinding{
			FilePath:    "include/fmt/printf.h",
			LineNumber:  "487-490",
			TriggerLine: "void format_to(basic_format_args<Char> args)",
			CodeSnippet: snippet,
		},
	}

	curr := InternalFinding{
		OriginalIndex: 0,
		Fingerprint:   "fp_printf_488",
		NormPath:      "include/fmt/printf.h",
		StartLine:     488,
		EndLine:       491,
		CleanTrigger:  "parse_format_string(args)", // 单行 token 与 base 差异较大
		Category:      "逻辑漏洞",
		Payload: models.AnalysisFinding{
			FilePath:    "include/fmt/printf.h",
			LineNumber:  "488-491",
			TriggerLine: "parse_format_string(args);",
			CodeSnippet: snippet, // snippet 完全重叠
		},
	}

	matchedCurrentMap, claimedBase, _ := RunDeterministicFunnel([]InternalFinding{base}, []InternalFinding{curr})
	if len(matchedCurrentMap) != 1 || !claimedBase[0] {
		t.Fatalf("Expected 1 match from funnel, got %d", len(matchedCurrentMap))
	}

	m := matchedCurrentMap[0]
	if m.MatchedTier != 3 {
		t.Errorf("Expected MatchedTier 3 (TierFuzzy), got %d", m.MatchedTier)
	}
	if m.Relation != RelationSame {
		t.Errorf("Expected RelationSame, got %s", m.Relation)
	}
}

type mockAIInvoker struct {
	responseJSON string
	err          error
}

func (m *mockAIInvoker) Invoke(req invoker.AIRequest) error {
	if m.err != nil {
		return m.err
	}
	return os.WriteFile(req.OutputPath, []byte(m.responseJSON), 0644)
}

func (m *mockAIInvoker) Name() string {
	return "mock-ai"
}

func TestArbitrateResidualsAI(t *testing.T) {
	// 模拟 std.h:671 vs 688：分类不同（安全风险 vs 逻辑漏洞），行差 17 行，无法被规则漏斗直接匹配
	base := InternalFinding{
		OriginalIndex: 0,
		ItemUID:       "F181-std01",
		NormPath:      "include/fmt/std.h",
		StartLine:     671,
		EndLine:       675,
		NormScope:     "format_as",
		Category:      "安全风险",
		Severity:      "致命",
		CleanTrigger:  "format_as(const T& value)",
		Payload: models.AnalysisFinding{
			FilePath:    "include/fmt/std.h",
			LineNumber:  "671-675",
			Title:       "递归解包未限制最大展开层数可能导致栈溢出",
			Detail:      "在 format_as 函数中缺少递归深度保护",
			CodeSnippet: "template <typename T> void format_as(...) { ... }",
		},
	}

	curr := InternalFinding{
		OriginalIndex: 0,
		ItemUID:       "F182-std01",
		NormPath:      "include/fmt/std.h",
		StartLine:     688,
		EndLine:       692,
		NormScope:     "format_as",
		Category:      "逻辑漏洞",
		Severity:      "严重",
		CleanTrigger:  "return format_as(val.get())",
		Payload: models.AnalysisFinding{
			FilePath:    "include/fmt/std.h",
			LineNumber:  "688-692",
			Title:       "解包过程存在无限递归调用风险",
			Detail:      "递归展开嵌套容器时未设置深度上限",
			CodeSnippet: "return format_as(val.get());",
		},
	}

	mockInv := &mockAIInvoker{
		responseJSON: `{
			"is_same_defect": true,
			"confidence": 0.95,
			"reason": "两份报告均指向同一递归解包缺少深度控制导致的潜在栈溢出隐患",
			"primary_uid": "F182-std01"
		}`,
	}

	results := ArbitrateResiduals([]InternalFinding{base}, []InternalFinding{curr}, mockInv)
	if len(results) != 1 {
		t.Fatalf("Expected 1 arbitration result, got %d", len(results))
	}

	r := results[0]
	if r.Relation != RelationSameSemantic {
		t.Errorf("Expected RelationSameSemantic, got %s", r.Relation)
	}
	if r.Confidence != 0.95 {
		t.Errorf("Expected Confidence 0.95, got %f", r.Confidence)
	}
	if r.SeverityRange != "[\"严重\",\"致命\"]" {
		t.Errorf("Expected SeverityRange [\"严重\",\"致命\"], got %s", r.SeverityRange)
	}
	if r.SeverityTriage {
		t.Errorf("Expected SeverityTriage false (严重 w=3 vs 致命 w=4 差异为1 < 2，无需打回人工)")
	}

	// 验证等级跨度悬殊 (如建议 w=1 vs 致命 w=4) 时触发 SeverityTriage
	base.Severity = "建议"
	resultsConflict := ArbitrateResiduals([]InternalFinding{base}, []InternalFinding{curr}, mockInv)
	if len(resultsConflict) != 1 || !resultsConflict[0].SeverityTriage {
		t.Errorf("Expected SeverityTriage true for 建议 vs 致命, got %+v", resultsConflict)
	}
}
