package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines/planner"
	"code-shield/services/invoker"
)

func TestRenderTier4ReportDetailedLimitAndFallback(t *testing.T) {
	tempDir := t.TempDir()
	reportPath := filepath.Join(tempDir, "report.md")
	findings := make([]models.AnalysisFinding, 0, 11)
	for i := 0; i < 11; i++ {
		findings = append(findings, models.AnalysisFinding{
			Severity:    "严重",
			Category:    "memory",
			FilePath:    filepath.Join("src", "file.go"),
			LineNumber:  "1",
			Title:       "issue",
			CodeSnippet: "code",
			Detail:      "detail",
			Suggestion:  "fix",
		})
	}
	ctx := &TaskContext{
		ReportPath: reportPath,
		Repo:       models.Repository{Name: "demo/repo"},
		TaskType:   models.TaskType{DisplayName: "Coredump Risk"},
		Report:     models.TaskReport{ID: 123},
	}

	report, err := RenderTier4Report(ctx, findings, nil)
	if err != nil {
		t.Fatalf("RenderTier4Report failed: %v", err)
	}
	text := string(report)
	if got := strings.Count(text, "### "); got != 10 {
		t.Fatalf("expected 10 detailed sections, got %d", got)
	}
	if got := strings.Count(text, "| src"); got != 1 {
		t.Fatalf("expected 1 summary row, got %d", got)
	}
	if !strings.Contains(text, "致命：0，严重：11，一般：0，建议：0") {
		t.Fatalf("statistics mismatch:\n%s", text)
	}
	if err := writeTier4Report(reportPath, report); err != nil {
		t.Fatalf("writeTier4Report failed: %v", err)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("deterministic report not written: %v", err)
	}
}

func TestTier4ReportAndAIInputSummaryTruncation(t *testing.T) {
	findings := make([]models.AnalysisFinding, 0, 436)
	appendFindings := func(count int, severity string) {
		for i := 0; i < count; i++ {
			findings = append(findings, models.AnalysisFinding{
				Severity:   severity,
				FilePath:   filepath.Join("src", fmt.Sprintf("file-%03d.go", i)),
				LineNumber: fmt.Sprintf("%d", i+1),
				Title:      "issue",
			})
		}
	}
	appendFindings(58, "严重")
	appendFindings(192, "一般")
	appendFindings(24, "建议")
	appendFindings(162, "合格")

	ctx := &TaskContext{
		ReportPath: filepath.Join(t.TempDir(), "report.md"),
		Repo:       models.Repository{Name: "demo/repo"},
		TaskType:   models.TaskType{DisplayName: "Coredump Risk"},
		Report:     models.TaskReport{ID: 124},
	}

	report, err := RenderTier4Report(ctx, findings, nil)
	if err != nil {
		t.Fatalf("RenderTier4Report failed: %v", err)
	}
	reportText := string(report)
	summaryTableStart := strings.Index(reportText, "## 三、其余问题清单\n")
	if summaryTableStart < 0 {
		t.Fatalf("summary table missing:\n%s", reportText)
	}
	summaryTableEnd := strings.Index(reportText[summaryTableStart:], "\n## 四、总结与建议\n")
	if summaryTableEnd < 0 {
		t.Fatalf("summary section end missing:\n%s", reportText)
	}
	summaryTable := reportText[summaryTableStart : summaryTableStart+summaryTableEnd]
	if got := strings.Count(summaryTable, "\n| src/"); got > 20 {
		t.Fatalf("summary table has %d rows, want <= 20", got)
	}
	if !strings.Contains(summaryTable, "...另有 406 条详见完整报告导出\n") {
		t.Fatalf("summary table missing truncation notice:\n%s", summaryTable)
	}
	if !strings.HasSuffix(summaryTable, "...另有 406 条详见完整报告导出\n") {
		t.Fatalf("truncation notice is not at the summary table end:\n%s", summaryTable)
	}

	raw, err := buildTier4AIInput(ctx, findings, nil)
	if err != nil {
		t.Fatalf("buildTier4AIInput failed: %v", err)
	}
	if len(raw) >= 64*1024 {
		t.Fatalf("AI input is %d bytes, want < %d", len(raw), 64*1024)
	}
	var payload tier4AIInput
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("parse tier4 AI input failed: %v", err)
	}
	if len(payload.SummaryFindings) != 20 {
		t.Fatalf("AI summary findings length is %d, want 20", len(payload.SummaryFindings))
	}
}

func TestRenderTier4ReportIncludesPlanDriftAndChangeOverview(t *testing.T) {
	tempDir := t.TempDir()
	reportPath := filepath.Join(tempDir, "report.md")
	findings := []models.AnalysisFinding{
		{Severity: "严重", FilePath: "src/a.cpp", LineNumber: "10", Title: "regression", AssessmentStatus: "REGRESSION"},
		{Severity: "合格", FilePath: "src/b.cpp", LineNumber: "20", Title: "safe", AssessmentStatus: "SAFE"},
	}
	scanCoverage := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		PlanReconciliation: &coverage.PlanReconciliation{
			PlannedUnits: 2, MatchedUnits: 1,
			MissingUnits:   []string{"change-hunk-2"},
			UnmatchedUnits: []string{"orphan-hunk"},
		},
		Files: []coverage.File{
			{Path: "src/a.cpp", DiffTouched: true, Status: coverage.StatusSuccess, HunkRanges: []string{"10-12"}},
			{Path: "src/c.cpp", DiffTouched: true, Status: coverage.StatusFailed, HunkRanges: []string{"1-3"}},
		},
	}
	ctx := &TaskContext{
		ReportPath: reportPath,
		Repo:       models.Repository{Name: "demo/repo"},
		TaskType:   models.TaskType{DisplayName: "Change Review"},
		Report:     models.TaskReport{ID: 124},
		Summary: TaskSummaryReport{
			ScopeDecision: &planner.ScopeDecision{
				Decision: planner.DecisionProceedDegraded,
				ChangeOverview: &planner.ChangeOverview{
					AddedFiles: 1, ModifiedFiles: 1, ChangedHunks: 2,
					Files: []planner.ChangeOverviewFile{
						{Path: "src/a.cpp", ChangeKind: "modified", Language: "cpp", HunkRanges: []string{"10-12"}},
						{Path: "src/c.cpp", ChangeKind: "added", Language: "cpp", HunkRanges: []string{"1-3"}},
					},
				},
			},
		},
	}

	report, err := RenderTier4Report(ctx, findings, scanCoverage)
	if err != nil {
		t.Fatalf("RenderTier4Report failed: %v", err)
	}
	text := string(report)
	for _, expected := range []string{
		"Primary Plan 对账**: 计划 2 / 结论 1，缺失 1，孤儿 1",
		"变更概览**: 新增 1，修改 1",
		"src/c.cpp [1-3]（failed）",
		"变更结论：回归 1，暴露存量 0，条件成立 0，非本次变更 0，安全 1，需人工 0",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("report missing %q:\n%s", expected, text)
		}
	}
}

func TestExecuteSynthesisAIFailureUsesDeterministicReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"llm unavailable"}`))
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.Scanner.Debate.Tiers.Tier4Synthesis = models.TierBindingConfig{
		Resource:       "native",
		TimeoutSeconds: 300,
	}
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints:      []models.ResourceEndpointConfig{{Name: "default", BaseURL: server.URL, Model: "glm-4-flash", Concurrent: 20}},
		MaxRetries:     1,
		RetryBackoffMs: 1,
	}

	tempDir := t.TempDir()
	reportPath := filepath.Join(tempDir, "report.md")
	findings := []models.AnalysisFinding{
		{Severity: "致命", FilePath: "src/a.cpp", LineNumber: "10", Title: "null deref"},
		{Severity: "严重", FilePath: "src/b.cpp", LineNumber: "20", Title: "index overflow"},
	}
	ctx := &TaskContext{
		ReportPath: reportPath,
		Repo:       models.Repository{Name: "demo/repo"},
		TaskType:   models.TaskType{DisplayName: "Coredump Risk", Timeout: 5},
		Report:     models.TaskReport{ID: 456},
		Findings:   findings,
	}

	if err := ExecuteSynthesis(ctx, findings); err != nil {
		t.Fatalf("ExecuteSynthesis should degrade instead of fail: %v", err)
	}
	if ctx.Summary.Synthesis.Status != "degraded" {
		t.Fatalf("expected degraded status, got %q", ctx.Summary.Synthesis.Status)
	}
	content, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("deterministic report missing: %v", err)
	}
	if !strings.Contains(string(content), "null deref") || !strings.Contains(string(content), "index overflow") {
		t.Fatalf("deterministic report lost findings:\n%s", content)
	}
}

type tierRecoveryInvoker struct {
	name  string
	calls int
	fail  func(call int) error
}

func (inv *tierRecoveryInvoker) Name() string { return inv.name }

func (inv *tierRecoveryInvoker) Invoke(req invoker.AIRequest) error {
	inv.calls++
	if inv.fail != nil {
		if err := inv.fail(inv.calls); err != nil {
			return err
		}
	}
	return os.WriteFile(req.OutputPath, []byte("# Code-Shield 安全扫描报告\n\n本次报告完整。\n"), 0644)
}

func TestExecuteSynthesisIdleTimeoutRetriesSameResource(t *testing.T) {
	previousConfig := models.AppConfig
	t.Cleanup(func() { models.AppConfig = previousConfig })

	inv := &tierRecoveryInvoker{name: "synthesis-stable-resource"}
	inv.fail = func(call int) error {
		if call == 1 {
			return invoker.NewClassifiedError(invoker.ErrorClassIdleTimeout, "idle timeout")
		}
		return nil
	}
	invoker.RegisterAIInvoker(inv.name, inv)
	models.AppConfig.Scanner.Debate.Tiers.Tier4Synthesis = models.TierBindingConfig{
		Resource:              inv.name,
		Resources:             []string{inv.name},
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		Recovery: &models.TierRecoveryConfig{
			MaxTotalAttempts:       2,
			MaxAttemptsPerResource: 2,
			RetryBackoffMs:         1,
			RetryOn:                []string{"idle_timeout"},
		},
	}

	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "synthesis-input.json")
	if err := os.WriteFile(inputPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("write synthesis input: %v", err)
	}
	ctx := &TaskContext{
		Ctx:        context.Background(),
		Report:     models.TaskReport{ID: 42},
		TaskType:   models.TaskType{Name: "security_audit", DisplayName: "安全审计", Timeout: 1},
		CodesPath:  tempDir,
		ReportPath: filepath.Join(tempDir, "report.md"),
	}
	err := ExecuteSynthesisOnce(ctx, inputPath, "")
	if err != nil {
		t.Fatalf("synthesis retry failed: %v", err)
	}
	if inv.calls != 2 {
		t.Fatalf("invoker calls=%d, want same-resource fresh retry", inv.calls)
	}
	summary := ctx.Summary.Synthesis
	if summary.Attempts != 2 || summary.ResourceFailovers != 0 {
		t.Fatalf("summary=%+v, want two attempts and zero resource failovers", summary)
	}
	if len(summary.ResourceChain) != 2 || summary.ResourceChain[0] != summary.ResourceChain[1] {
		t.Fatalf("resource chain=%#v, want repeated stable resource", summary.ResourceChain)
	}
	if len(summary.ErrorClasses) != 2 || summary.ErrorClasses[0] != "idle_timeout" || summary.ErrorClasses[1] != "none" {
		t.Fatalf("error classes=%#v, want attempt-level audit", summary.ErrorClasses)
	}
}

func TestExecuteSynthesisStopsWhenStageBudgetExhausted(t *testing.T) {
	previousConfig := models.AppConfig
	t.Cleanup(func() { models.AppConfig = previousConfig })

	inv := &tierRecoveryInvoker{name: "synthesis-budget-resource"}
	invoker.RegisterAIInvoker(inv.name, inv)
	models.AppConfig.Scanner.Debate.Tiers.Tier4Synthesis = models.TierBindingConfig{
		Resource:              inv.name,
		Resources:             []string{inv.name},
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		Recovery: &models.TierRecoveryConfig{
			MaxTotalAttempts:       2,
			MaxAttemptsPerResource: 2,
		},
	}

	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := &TaskContext{
		Ctx:        parent,
		Report:     models.TaskReport{ID: 43},
		TaskType:   models.TaskType{Name: "security_audit", DisplayName: "安全审计", Timeout: 1},
		CodesPath:  t.TempDir(),
		ReportPath: filepath.Join(t.TempDir(), "report.md"),
	}
	if err := ExecuteSynthesisOnce(ctx, filepath.Join(ctx.CodesPath, "synthesis-input.json"), ""); err == nil {
		t.Fatal("expected budget exhaustion error")
	}
	summary := ctx.Summary.Synthesis
	if inv.calls != 0 || summary.Attempts != 0 || len(summary.ErrorClasses) != 0 || len(summary.ResourceChain) != 0 {
		t.Fatalf("calls=%d summary=%+v, want no attempt metrics without invocation", inv.calls, summary)
	}
}

func TestBuildTier4AIInputUsesBoundedDetailedDTO(t *testing.T) {
	findings := []models.AnalysisFinding{{
		Severity:      "致命",
		Category:      "memory",
		FilePath:      "src/a.cpp",
		LineNumber:    "10",
		Title:         "null deref",
		CodeSnippet:   strings.Repeat("x", 1200),
		Detail:        strings.Repeat("d", 1200),
		Suggestion:    strings.Repeat("s", 1200),
		HunterClaim:   "secret hunter narrative",
		ChallengerArg: "secret challenger narrative",
		JudgeVerdict:  "secret judge narrative",
	}}
	ctx := &TaskContext{
		Repo:     models.Repository{Name: "demo/repo"},
		TaskType: models.TaskType{DisplayName: "Coredump Risk"},
		Report:   models.TaskReport{ID: 1},
	}

	raw, err := buildTier4AIInput(ctx, findings, nil)
	if err != nil {
		t.Fatalf("buildTier4AIInput failed: %v", err)
	}
	payload := string(raw)
	for _, forbidden := range []string{"secret hunter narrative", "secret challenger narrative", "secret judge narrative", "secret feedback", "hunter_claim"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("AI payload leaked restricted field %q:\n%s", forbidden, payload)
		}
	}
	for _, field := range []string{"detailed_findings", "summary_findings", "null deref"} {
		if !strings.Contains(payload, field) {
			t.Fatalf("AI payload missing %q:\n%s", field, payload)
		}
	}
}
