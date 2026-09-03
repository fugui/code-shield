package services

import (
	"code-shield/models"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseJSONFromAIOutput(t *testing.T) {
	// 测试包含 ```json 包裹的输出
	rawMarkdown := "下面是初筛结果：\n```json\n{\n  \"candidates\": [\n    {\n      \"candidate_id\": \"H-001\",\n      \"file_path\": \"src/posix.cc\",\n      \"cwe_category\": \"CWE-476\"\n    }\n  ],\n  \"summary\": \"ok\"\n}\n```\n祝工作顺利！"

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
	// 验证 Hunter 结果为空时的快速放行逻辑
	hunterOut := &HunterOutput{
		Candidates: []HunterCandidate{},
		Summary:    "Clean code, no defect detected",
	}

	if len(hunterOut.Candidates) != 0 {
		t.Errorf("Expected 0 candidates for fast pass")
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

func TestCallAITier_ChunkDirPersistence(t *testing.T) {
	tempDir := t.TempDir()
	chunkDir := filepath.Join(tempDir, "debate-chunks-1-test")
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatalf("failed to create chunkDir: %v", err)
	}

	mockBackend := "mock-debate-persist"
	mockInv := &MockInvoker{}
	RegisterAIInvoker(mockBackend, mockInv)

	targetOutPath := filepath.Join(chunkDir, "chunk-1-src_main-1-hunter.json")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	outStr, tokens, err := callAITier(ctx, mockBackend, "", "test hunter prompt", tempDir, targetOutPath, 1)
	if err != nil {
		t.Fatalf("callAITier failed: %v", err)
	}

	if tokens <= 0 {
		t.Errorf("expected tokens > 0, got %d", tokens)
	}
	if outStr == "" {
		t.Errorf("expected non-empty outStr")
	}

	// 关键断言：文件必须持久化存在于 chunkDir 中，不能被作为 /tmp 临时文件删除
	if _, err := os.Stat(targetOutPath); os.IsNotExist(err) {
		t.Fatalf("expected output file to persist at %s, but file not found", targetOutPath)
	}

	content, err := os.ReadFile(targetOutPath)
	if err != nil {
		t.Fatalf("failed to read persisted chunk file: %v", err)
	}
	if len(content) == 0 {
		t.Fatalf("persisted chunk file is empty")
	}
}

func TestParseJSONFromAIOutput_MalformedEscapesAndNullBytes(t *testing.T) {
	// 模拟大模型输出含有不合规的反斜杠转义（\s, \0, \d）、Windows 路径（\test）、Unicode \u0000、字面量换行与尾部逗号
	malformedJSON := `{
  "candidates": [
    {
      "candidate_id": "H-001",
      "file_path": "src\dir\file.cc",
      "trigger_line": "if (*p == '\0') { regex = \d+; }",
      "cwe_category": "CWE-476",
      "title": "测试缺陷\u0000包含空字节",
      "attack_hypothesis": "成因描述第一行
成因描述第二行",
    },
  ],
}`

	var out HunterOutput
	err := parseJSONFromAIOutput(malformedJSON, &out, t.TempDir())
	if err != nil {
		t.Fatalf("parseJSONFromAIOutput should repair and parse malformed JSON successfully, got error: %v", err)
	}

	if len(out.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(out.Candidates))
	}
	cand := out.Candidates[0]
	if cand.CandidateID != "H-001" {
		t.Errorf("expected H-001, got %s", cand.CandidateID)
	}
	if cand.CWECategory != "CWE-476" {
		t.Errorf("expected CWE-476, got %s", cand.CWECategory)
	}
}

func TestSanitizeJSONForPostgresJSONB(t *testing.T) {
	// 测试包含 \u0000 及空字符的 jsonb 兼容性清洗
	raw := []byte(`{"hunter_output": "candidate\u0000detail", "code": "\x00test"}`)
	sanitized := SanitizeJSONForPostgresJSONB(raw)

	s := string(sanitized)
	if strings.Contains(s, `\u0000`) || strings.Contains(s, "\x00") {
		t.Errorf("sanitized JSON should not contain null bytes or \\u0000, got: %s", s)
	}
}
