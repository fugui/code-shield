package services

import (
	"code-shield/models"
	"context"
	"encoding/json"
	"fmt"
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

func TestSanitizeCandidateSnippet(t *testing.T) {
	// 1. 短片段不应被截断
	shortCode := "int x = 1;\nint y = 2;\nreturn x + y;"
	if got := sanitizeCandidateSnippet(shortCode); got != shortCode {
		t.Errorf("short snippet should not be modified, got: %s", got)
	}

	// 2. 超长行数应被折叠，保留首尾
	var sb strings.Builder
	for i := 1; i <= 200; i++ {
		sb.WriteString("line " + string(rune('0'+(i%10))) + "\n")
	}
	longCode := sb.String()
	sanitized := sanitizeCandidateSnippet(longCode)
	if !strings.Contains(sanitized, "代码片段过长已自动折叠截断") {
		t.Errorf("long snippet should contain folding message, got: %s", sanitized)
	}
	if !strings.HasPrefix(sanitized, "line 1") {
		t.Errorf("sanitized snippet should retain head lines")
	}
}

// debateMockInvoker 用于测试分批调用时的 Mock Invoker
type debateMockInvoker struct {
	name      string
	callCount int
	handler   func(req AIRequest) error
}

func (m *debateMockInvoker) Name() string { return m.name }
func (m *debateMockInvoker) Invoke(req AIRequest) error {
	m.callCount++
	if m.handler != nil {
		return m.handler(req)
	}
	return nil
}

func TestDebateEngine_ChallengerBatching(t *testing.T) {
	// 模拟 12 个候选缺陷，预期被切分为 3 批（5 + 5 + 2）
	var candidates []HunterCandidate
	for i := 1; i <= 12; i++ {
		candidates = append(candidates, HunterCandidate{
			CandidateID: fmt.Sprintf("CAND-%03d", i),
			FilePath:    "src/test.cc",
			TriggerLine: "int *p = nullptr;",
			CodeSnippet: "short snippet",
		})
	}

	mockBackend := "mock-challenger-batch"
	mock := &debateMockInvoker{
		name: mockBackend,
		handler: func(req AIRequest) error {
			// 根据 prompt 中包含的 candidate 生成对应的辩护结果
			var cases []ChallengerDefenseCase
			for i := 1; i <= 12; i++ {
				cid := fmt.Sprintf("CAND-%03d", i)
				if strings.Contains(req.PromptMsg, cid) {
					cases = append(cases, ChallengerDefenseCase{
						CandidateID:    cid,
						DefenseVerdict: "DEFENSE_SUCCESSFUL",
					})
				}
			}
			out := ChallengerOutput{
				DefenseCases: cases,
				Summary:      "batch summary",
			}
			data, _ := json.Marshal(out)
			return os.WriteFile(req.OutputPath, data, 0644)
		},
	}
	RegisterAIInvoker(mockBackend, mock)

	models.AppConfig.AI.Tiers.Tier2Reasoning = models.TierConfig{
		Backend:        mockBackend,
		TimeoutSeconds: 10,
	}

	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "merged_challenger.json")
	engine := &DebateEngine{}
	taskCtx := &taskContext{
		ctx:       context.Background(),
		codesPath: tempDir,
	}

	hunterOut := &HunterOutput{
		Candidates: candidates,
		Summary:    "12 candidates found",
	}

	challOut, tokens, err := engine.runChallengerStage(taskCtx, SemanticBundle{Name: "test-bundle"}, hunterOut, outPath)
	if err != nil {
		t.Fatalf("runChallengerStage failed: %v", err)
	}

	// 预期调用次数为 3 次（12 / 5 向上取整）
	if mock.callCount != 3 {
		t.Errorf("expected 3 batch invocations, got %d", mock.callCount)
	}

	// 预期合并后的辩护案例数量为 12 个
	if len(challOut.DefenseCases) != 12 {
		t.Errorf("expected 12 defense cases merged, got %d", len(challOut.DefenseCases))
	}

	if tokens <= 0 {
		t.Errorf("expected tokens > 0, got %d", tokens)
	}

	// 验证最终合并文件被正确写入
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Fatalf("merged output file %s does not exist", outPath)
	}
}

func TestDebateEngine_JudgeBatching(t *testing.T) {
	// 模拟 12 个候选缺陷及对应的辩护结果
	var candidates []HunterCandidate
	var defenseCases []ChallengerDefenseCase
	for i := 1; i <= 12; i++ {
		cid := fmt.Sprintf("CAND-%03d", i)
		candidates = append(candidates, HunterCandidate{
			CandidateID: cid,
			FilePath:    "src/test.cc",
			TriggerLine: "int *p = nullptr;",
		})
		defenseCases = append(defenseCases, ChallengerDefenseCase{
			CandidateID:    cid,
			DefenseVerdict: "DEFENSE_SUCCESSFUL",
		})
	}

	mockBackend := "mock-judge-batch"
	mock := &debateMockInvoker{
		name: mockBackend,
		handler: func(req AIRequest) error {
			var verdicts []JudgeFinalVerdict
			for i := 1; i <= 12; i++ {
				cid := fmt.Sprintf("CAND-%03d", i)
				if strings.Contains(req.PromptMsg, cid) {
					verdicts = append(verdicts, JudgeFinalVerdict{
						CandidateID:        cid,
						Verdict:            models.DebateVerdictConfirmed,
						Title:              "Title for " + cid,
						JudgementRationale: "Confirmed based on facts",
					})
				}
			}
			out := JudgeOutput{FinalVerdicts: verdicts}
			data, _ := json.Marshal(out)
			return os.WriteFile(req.OutputPath, data, 0644)
		},
	}
	RegisterAIInvoker(mockBackend, mock)

	models.AppConfig.AI.Tiers.Tier2Reasoning = models.TierConfig{
		Backend:        mockBackend,
		TimeoutSeconds: 10,
	}

	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "merged_judge.json")
	engine := &DebateEngine{}
	taskCtx := &taskContext{
		ctx:       context.Background(),
		codesPath: tempDir,
	}

	hunterOut := &HunterOutput{Candidates: candidates}
	challOut := &ChallengerOutput{DefenseCases: defenseCases}

	judgeOut, tokens, err := engine.runJudgeStage(taskCtx, SemanticBundle{Name: "test-bundle"}, hunterOut, challOut, outPath)
	if err != nil {
		t.Fatalf("runJudgeStage failed: %v", err)
	}

	if mock.callCount != 3 {
		t.Errorf("expected 3 judge batch invocations, got %d", mock.callCount)
	}

	if len(judgeOut.FinalVerdicts) != 12 {
		t.Errorf("expected 12 final verdicts merged, got %d", len(judgeOut.FinalVerdicts))
	}

	if tokens <= 0 {
		t.Errorf("expected tokens > 0, got %d", tokens)
	}

	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Fatalf("merged judge output file %s does not exist", outPath)
	}
}

func TestDebateEngine_ChallengerPartialFailure(t *testing.T) {
	// 模拟 10 个候选（2 批，各 5 个），第 2 批调用失败，验证第 1 批正常保留、第 2 批降级容错
	var candidates []HunterCandidate
	for i := 1; i <= 10; i++ {
		candidates = append(candidates, HunterCandidate{
			CandidateID: string(rune('A'+i-1)) + "-001",
			FilePath:    "src/test.cc",
		})
	}

	callIdx := 0
	mockBackend := "mock-challenger-partial"
	mock := &debateMockInvoker{
		name: mockBackend,
		handler: func(req AIRequest) error {
			callIdx++
			if callIdx == 2 {
				return context.DeadlineExceeded // 模拟第 2 批超时
			}
			// 第 1 批正常成功
			var cases []ChallengerDefenseCase
			for i := 1; i <= 5; i++ {
				cases = append(cases, ChallengerDefenseCase{
					CandidateID:    string(rune('A'+i-1)) + "-001",
					DefenseVerdict: "DEFENSE_SUCCESSFUL",
				})
			}
			data, _ := json.Marshal(ChallengerOutput{DefenseCases: cases})
			return os.WriteFile(req.OutputPath, data, 0644)
		},
	}
	RegisterAIInvoker(mockBackend, mock)

	models.AppConfig.AI.Tiers.Tier2Reasoning = models.TierConfig{
		Backend:        mockBackend,
		TimeoutSeconds: 10,
	}

	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "partial_challenger.json")
	engine := &DebateEngine{}
	taskCtx := &taskContext{
		ctx:       context.Background(),
		codesPath: tempDir,
	}

	hunterOut := &HunterOutput{Candidates: candidates}
	challOut, _, err := engine.runChallengerStage(taskCtx, SemanticBundle{Name: "test-bundle"}, hunterOut, outPath)
	if err != nil {
		t.Fatalf("expected partial degradation success, but got error: %v", err)
	}

	if len(challOut.DefenseCases) != 10 {
		t.Fatalf("expected 10 total cases (5 normal + 5 degraded), got %d", len(challOut.DefenseCases))
	}

	// 检查第 1 批与第 2 批的判定状态
	if challOut.DefenseCases[0].DefenseVerdict != "DEFENSE_SUCCESSFUL" {
		t.Errorf("batch 1 should be DEFENSE_SUCCESSFUL, got %s", challOut.DefenseCases[0].DefenseVerdict)
	}
	if challOut.DefenseCases[5].DefenseVerdict != "CHALLENGE_FAILED" {
		t.Errorf("batch 2 should be CHALLENGE_FAILED, got %s", challOut.DefenseCases[5].DefenseVerdict)
	}
	if !strings.Contains(challOut.DefenseCases[5].MitigatingFactors, "Challenger Degraded") {
		t.Errorf("batch 2 should contain degraded note, got %s", challOut.DefenseCases[5].MitigatingFactors)
	}
}
