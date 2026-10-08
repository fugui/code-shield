package debate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
)

func TestParseJSONFromAIOutput(t *testing.T) {
	// 测试包含 ```json 包裹的输出
	rawMarkdown := "下面是初筛结果：\n```json\n{\n  \"candidates\": [\n    {\n      \"candidate_id\": \"H-001\",\n      \"file_path\": \"src/posix.cc\",\n      \"cwe_category\": \"CWE-476\"\n    }\n  ]\n}\n```\n祝工作顺利！"

	var out HunterOutput
	err := parseJSONFromAIOutput(rawMarkdown, &out, t.TempDir())
	if err != nil {
		t.Fatalf("parseJSONFromAIOutput failed: %v", err)
	}
	if len(out.Candidates) != 1 {
		t.Fatalf("Expected 1 candidate, got %d", len(out.Candidates))
	}
	if out.Candidates[0].CandidateID != "H-001" {
		t.Errorf("Expected candidate ID H-001, got %s", out.Candidates[0].CandidateID)
	}

	// 测试自然语言前导与外层未包裹边界提取
	rawDirect := "这是判词：{\"final_verdicts\": [{\"candidate_id\": \"H-001\", \"verdict\": \"CONFIRMED\"}]} 结束"
	var judgeOut JudgeOutput
	err = parseJSONFromAIOutput(rawDirect, &judgeOut, t.TempDir())
	if err != nil {
		t.Fatalf("parseJSONFromAIOutput failed on rawDirect: %v", err)
	}
	if len(judgeOut.FinalVerdicts) != 1 {
		t.Fatalf("Expected 1 verdict, got %d", len(judgeOut.FinalVerdicts))
	}
}

func TestDebateEngine_FastPass(t *testing.T) {
	hunterOut := &HunterOutput{
		Candidates: []HunterCandidate{},
	}

	if len(hunterOut.Candidates) != 0 {
		t.Errorf("Expected 0 candidates for fast pass")
	}
}

func TestFallbackJudgeFromHunterKeepsFailureReason(t *testing.T) {
	rawReason := "Judge 调用失败: AI execution failed: \x1b[0m\n> shield-base-scanner · test-model\n\x1b[0m$ grep secret src/a.cpp"
	out := fallbackJudgeFromHunter(rawReason, &HunterOutput{
		Candidates: []HunterCandidate{{
			CandidateID:      "H-001",
			FilePath:         "src/a.cpp",
			LineRange:        "1-2",
			TriggerLine:      "state[1] = 2;",
			ScopeSymbol:      "Service::update",
			AttackHypothesis: "unsafe access",
			Category:         "测试分类",
		}},
	})
	if len(out.FinalVerdicts) != 1 {
		t.Fatalf("expected one fallback verdict, got %d", len(out.FinalVerdicts))
	}
	verdict := out.FinalVerdicts[0]
	rationale := verdict.JudgementRationale
	if !strings.Contains(rationale, "原因：Judge 调用失败") {
		t.Fatalf("fallback rationale must retain stable failure category: %q", rationale)
	}
	if strings.Contains(rationale, "AI execution failed") || strings.Contains(rationale, "shield-base-scanner") || strings.Contains(rationale, "grep secret") {
		t.Fatalf("fallback rationale must not expose raw CLI diagnostics: %q", rationale)
	}
	if verdict.SeverityPreliminary != "一般" {
		t.Fatalf("expected fallback severity 一般, got %q", verdict.SeverityPreliminary)
	}
	if verdict.EvidenceDegraded != true {
		t.Fatalf("expected degraded evidence marker to remain true")
	}
}

func TestDeriveConciseTitleKeepsFloatingPointLiteral(t *testing.T) {
	title := "浮点数比较缺陷：变量与阈值 1.05 直接等值比较，可能因精度误差失效"

	if got := DeriveConciseTitle(title, "CWE-000"); got != title {
		t.Fatalf("expected floating-point title to be preserved, got %q", got)
	}
}

func TestDebateEngine_VerdictConversion(t *testing.T) {
	judgeOut := &JudgeOutput{
		FinalVerdicts: []JudgeFinalVerdict{
			{
				CandidateID:        "H-001",
				Verdict:            models.DebateVerdictConfirmed,
				Category:           "内存管理问题-写越界",
				FilePath:           "src/format.cc",
				LineNumber:         "100-110",
				TriggerLine:        "memcpy(dst, src, len);",
				ScopeSymbol:        "format_buffer",
				Title:              "栈缓冲区写越界",
				JudgementRationale: "Hunter 指控属实，Challenger 辩护失败",
				CodeSnippet:        "memcpy(dst, src, len);",
				Suggestion:         "使用 safe_memcpy 增加长度检查",
			},
			{
				CandidateID:        "H-002",
				Verdict:            models.DebateVerdictRejected,
				Category:           "CWE-476 空指针",
				FilePath:           "src/posix.cc",
				Title:              "误报空指针",
				JudgementRationale: "Challenger 提供了外层非空断言证据，判定误报",
			},
		},
	}

	var confirmed []models.AnalysisFinding
	for _, jv := range judgeOut.FinalVerdicts {
		if jv.Verdict == models.DebateVerdictConfirmed || jv.Verdict == models.DebateVerdictConditional {
			confirmed = append(confirmed, models.AnalysisFinding{
				FilePath:    jv.FilePath,
				LineNumber:  jv.LineNumber,
				TriggerLine: jv.TriggerLine,
				ScopeSymbol: jv.ScopeSymbol,
				Category:    jv.Category,
				Title:       jv.Title,
				Detail:      jv.JudgementRationale,
				Suggestion:  jv.Suggestion,
			})
		}
	}

	if len(confirmed) != 1 {
		t.Fatalf("Expected 1 confirmed finding, got %d", len(confirmed))
	}
	if confirmed[0].Title != "栈缓冲区写越界" {
		t.Errorf("Expected confirmed title, got %s", confirmed[0].Title)
	}
}

type mockDebateInvoker struct{}

func (m *mockDebateInvoker) Name() string { return "mock-debate-persist" }
func (m *mockDebateInvoker) Invoke(req invoker.AIRequest) error {
	return os.WriteFile(req.OutputPath, []byte(`{"candidates":[]}`), 0644)
}

func TestCallAITier_ChunkDirPersistence(t *testing.T) {
	tempDir := t.TempDir()
	chunkDir := filepath.Join(tempDir, "debate-chunks-1-test")
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatalf("failed to create chunkDir: %v", err)
	}

	mockBackend := "mock-debate-persist"
	invoker.RegisterAIInvoker(mockBackend, &mockDebateInvoker{})

	targetOutPath := filepath.Join(chunkDir, "chunk-1-src_main-1-hunter.json")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	outStr, tokens, err := callAITier(ctx, mockBackend, "", "test hunter prompt", tempDir, targetOutPath, 1, aiTimeoutPolicy{})
	if err != nil {
		t.Fatalf("callAITier failed: %v", err)
	}

	if tokens <= 0 {
		t.Errorf("expected tokens > 0, got %d", tokens)
	}
	if outStr == "" {
		t.Errorf("expected non-empty outStr")
	}

	if _, err := os.Stat(targetOutPath); os.IsNotExist(err) {
		t.Fatalf("expected output file to persist at %s, but file not found", targetOutPath)
	}
}

func TestIsInvocationTimeoutClassification(t *testing.T) {
	if !isInvocationTimeout(invoker.NewClassifiedError(invoker.ErrorClassIdleTimeout, "AI execution idle timed out after 10m0s")) {
		t.Fatal("expected idle timeout to be split-retryable")
	}
	if isInvocationTimeout(invoker.NewClassifiedError(invoker.ErrorClassResourceBusy, "failed to acquire LLM server slot")) {
		t.Fatal("dispatcher wait timeout must not trigger bundle splitting")
	}
}
