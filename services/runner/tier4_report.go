package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
)

// Tier 4 defaults are intentionally deterministic and small.  The detailed
// findings are rendered locally; the remaining findings are always represented
// as one-line summaries in both the deterministic report and the AI payload.
const (
	tier4DetailedFindings   = 10
	tier4MaxSummaryRows     = 20
	tier4MaxSnippetBytes    = 800
	tier4MaxDetailBytes     = 1024
	tier4MaxSuggestionBytes = 1024
	tier4MaxAIInputBytes    = 64 * 1024
	tier4MaxAIOutputBytes   = 32 * 1024
	// The native text default is 32768 tokens, but Tier4 is intentionally a
	// compact polish pass.  Keep the prompt target below the 32KiB validator.
	tier4AIPromptOutputBytes = 24 * 1024
	tier4RequiredDetailItems = 8
)

type tier4Counts struct {
	Total      int `json:"total"`
	Fatal      int `json:"fatal"`
	Critical   int `json:"critical"`
	Minor      int `json:"minor"`
	Suggestion int `json:"suggestion"`
}

type tier4SummaryFinding struct {
	FilePath   string `json:"file_path"`
	LineNumber string `json:"line_number,omitempty"`
	Title      string `json:"title"`
	Severity   string `json:"severity"`
}

type tier4DetailedFinding struct {
	Severity    string `json:"severity"`
	Category    string `json:"category,omitempty"`
	FilePath    string `json:"file_path"`
	LineNumber  string `json:"line_number,omitempty"`
	Title       string `json:"title"`
	CodeSnippet string `json:"code_snippet,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Suggestion  string `json:"suggestion,omitempty"`
}

type tier4AIInput struct {
	Schema           string                 `json:"schema"`
	Report           tier4ReportMeta        `json:"report"`
	Statistics       tier4Counts            `json:"statistics"`
	Coverage         *coverage.Summary      `json:"coverage,omitempty"`
	DetailedFindings []tier4DetailedFinding `json:"detailed_findings"`
	SummaryFindings  []tier4SummaryFinding  `json:"summary_findings"`
}

type tier4ReportMeta struct {
	ID       uint   `json:"id"`
	RepoName string `json:"repo_name"`
	TaskType string `json:"task_type"`
	HeadHash string `json:"head_commit,omitempty"`
	Rendered string `json:"rendered_at"`
}

func tier4SeverityCounts(findings []models.AnalysisFinding) tier4Counts {
	counts := tier4Counts{Total: len(findings)}
	for _, finding := range findings {
		switch strings.ToLower(strings.TrimSpace(finding.Severity)) {
		case "致命", "fatal", "blocker", "阻塞", "blocking", "p0":
			counts.Fatal++
		case "严重", "critical", "major_error", "error", "p1", "高", "高危", "高风险", "high", "high_risk":
			counts.Critical++
		case "一般", "minor", "warning", "主要", "major", "提示", "info", "hint", "中", "中危", "中风险", "medium", "medium_risk", "p2":
			counts.Minor++
		default:
			counts.Suggestion++
		}
	}
	return counts
}

func tier4SeverityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "致命", "fatal", "blocker", "阻塞", "blocking", "p0":
		return 4
	case "严重", "critical", "major_error", "error", "p1", "高", "高危", "高风险", "high", "high_risk":
		return 3
	case "一般", "minor", "warning", "主要", "major", "提示", "info", "hint", "中", "中危", "中风险", "medium", "medium_risk", "p2":
		return 2
	default:
		return 1
	}
}

func sortTier4Findings(findings []models.AnalysisFinding) []models.AnalysisFinding {
	sorted := append([]models.AnalysisFinding(nil), findings...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := sorted[i], sorted[j]
		if rank := tier4SeverityRank(left.Severity) - tier4SeverityRank(right.Severity); rank != 0 {
			return rank > 0
		}
		if left.FilePath != right.FilePath {
			return left.FilePath < right.FilePath
		}
		return left.LineNumber < right.LineNumber
	})
	return sorted
}

func truncateUTF8Bytes(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	cut := value[:max]
	for len(cut) > 0 && !isUTF8BoundaryStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n...[truncated]..."
}

func tier4SingleLine(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}

func tier4CodeFence(snippet string) string {
	longestRun := 0
	currentRun := 0
	for _, r := range snippet {
		if r == '`' {
			currentRun++
			if currentRun > longestRun {
				longestRun = currentRun
			}
		} else {
			currentRun = 0
		}
	}
	fenceLength := longestRun + 1
	if fenceLength < 3 {
		fenceLength = 3
	}
	return strings.Repeat("`", fenceLength)
}

func isUTF8BoundaryStart(b byte) bool {
	return b < 0x80 || b >= 0xC0
}

// RenderTier4Report produces a complete, parseable Markdown report without
// using a model.  It is both the deterministic fast path and the mandatory
// fallback when the optional AI polish call fails.
func RenderTier4Report(ctx *TaskContext, findings []models.AnalysisFinding, scanCoverage *coverage.Coverage) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("task context is nil")
	}
	sorted := sortTier4Findings(findings)
	counts := tier4SeverityCounts(findings)

	var body bytes.Buffer
	body.WriteString("# Code-Shield 全仓态势汇总报告\n\n")
	fmt.Fprintf(&body, "- **代码仓**: %s\n", ctx.Repo.Name)
	fmt.Fprintf(&body, "- **任务类型**: %s\n", ctx.TaskType.DisplayName)
	fmt.Fprintf(&body, "- **报告 ID**: %d\n", ctx.Report.ID)
	fmt.Fprintf(&body, "- **生成时间**: %s\n", time.Now().Format("2006-01-02 15:04:05"))

	coverageSummary := coverage.Summary{}
	if scanCoverage != nil {
		coverageSummary = scanCoverage.Summary()
	}
	switch {
	case coverageSummary.CoverageNotApplicable:
		fmt.Fprintln(&body, "- **覆盖率**: 未命中扫描范围（计划文件 0），覆盖率评估不适用")
	case coverageSummary.ManifestMissing:
		fmt.Fprintln(&body, "- **覆盖率**: 覆盖率清单缺失，结果降级")
	default:
		fmt.Fprintf(&body, "- **覆盖率**: 扫描 %d / 计划 %d，完整 %t，降级 %t\n",
			coverageSummary.ScannedFiles, coverageSummary.PlannedFiles,
			coverageSummary.CoverageComplete, coverageSummary.Degraded)
	}
	if reconciliation := coverageSummary.PlanReconciliation; reconciliation != nil {
		fmt.Fprintf(&body, "- **Primary Plan 对账**: 计划 %d / 结论 %d，缺失 %d，孤儿 %d\n",
			reconciliation.PlannedUnits, reconciliation.MatchedUnits,
			len(reconciliation.MissingUnits), len(reconciliation.UnmatchedUnits))
	}
	if ctx.Summary.ScopeDecision != nil && ctx.Summary.ScopeDecision.ChangeOverview != nil {
		overview := ctx.Summary.ScopeDecision.ChangeOverview
		fmt.Fprintf(&body, "- **变更概览**: 新增 %d，修改 %d，删除 %d，重命名 %d，二进制 %d，hunk %d\n",
			overview.AddedFiles, overview.ModifiedFiles, overview.DeletedFiles,
			overview.RenamedFiles, overview.BinaryFiles, overview.ChangedHunks)
		degradedHunks := make([]string, 0)
		if scanCoverage != nil {
			for _, file := range scanCoverage.Files {
				if !file.DiffTouched || file.Status == coverage.StatusSuccess {
					continue
				}
				hunkRanges := strings.Join(file.HunkRanges, ", ")
				if hunkRanges == "" {
					hunkRanges = "-"
				}
				degradedHunks = append(degradedHunks, fmt.Sprintf("%s [%s]（%s）", file.Path, hunkRanges, file.Status))
			}
		}
		if len(degradedHunks) > 0 {
			fmt.Fprintln(&body, "- **降级 hunk**:")
			for _, hunk := range degradedHunks {
				fmt.Fprintf(&body, "  - %s\n", hunk)
			}
		}
	}

	body.WriteString("\n## 一、检视结果概要\n\n")
	fmt.Fprintf(&body, "致命：%d，严重：%d，一般：%d，建议：%d\n", counts.Fatal, counts.Critical, counts.Minor, counts.Suggestion)
	if verdictCounts := tier4AssessmentCounts(findings); len(verdictCounts) > 0 {
		fmt.Fprintf(&body, "变更结论：回归 %d，暴露存量 %d，条件成立 %d，非本次变更 %d，安全 %d，需人工 %d\n",
			verdictCounts["REGRESSION"], verdictCounts["EXPOSED_EXISTING"], verdictCounts["CONDITIONAL"],
			verdictCounts["NOT_CHANGE_RELATED"], verdictCounts["SAFE"], verdictCounts["NEEDS_HUMAN"])
	}
	body.WriteString("\n## 二、重点问题\n\n")
	if len(sorted) == 0 {
		body.WriteString("本次扫描未发现符合规则的缺陷。\n")
	} else {
		for i, finding := range sorted {
			if i >= tier4DetailedFindings {
				break
			}
			fmt.Fprintf(&body, "### %d. %s\n\n", i+1, tier4SingleLine(finding.Title))
			fmt.Fprintf(&body, "- **文件**: %s\n", tier4SingleLine(finding.FilePath))
			if finding.LineNumber != "" {
				fmt.Fprintf(&body, "- **行号**: %s\n", tier4SingleLine(finding.LineNumber))
			}
			fmt.Fprintf(&body, "- **严重度**: %s\n", tier4SingleLine(finding.Severity))
			if finding.Category != "" {
				fmt.Fprintf(&body, "- **分类**: %s\n", tier4SingleLine(finding.Category))
			}
			if detail := strings.TrimSpace(finding.Detail); detail != "" {
				fmt.Fprintf(&body, "\n%s\n", truncateUTF8Bytes(detail, tier4MaxDetailBytes))
			}
			if snippet := strings.TrimSpace(finding.CodeSnippet); snippet != "" {
				boundedSnippet := truncateUTF8Bytes(snippet, tier4MaxSnippetBytes)
				fence := tier4CodeFence(boundedSnippet)
				fmt.Fprintf(&body, "\n%s\n%s\n%s\n", fence, boundedSnippet, fence)
			}
			if suggestion := strings.TrimSpace(finding.Suggestion); suggestion != "" {
				fmt.Fprintf(&body, "\n**修复建议**：%s\n", truncateUTF8Bytes(suggestion, tier4MaxSuggestionBytes))
			}
			body.WriteString("\n")
		}
	}

	body.WriteString("## 三、其余问题清单\n\n")
	if len(sorted) <= tier4DetailedFindings {
		body.WriteString("无。\n")
	} else {
		summaryFindings := sorted[tier4DetailedFindings:]
		if len(summaryFindings) > tier4MaxSummaryRows {
			summaryFindings = summaryFindings[:tier4MaxSummaryRows]
		}
		body.WriteString("| 文件 | 行号 | 标题 | 严重度 |\n|---|---|---|---|\n")
		for _, finding := range summaryFindings {
			title := tier4SingleLine(finding.Title)
			path := tier4SingleLine(finding.FilePath)
			lineNumber := tier4SingleLine(finding.LineNumber)
			severity := tier4SingleLine(finding.Severity)
			fmt.Fprintf(&body, "| %s | %s | %s | %s |\n",
				path, lineNumber, title, severity)
		}
		if remainingCount := len(sorted) - tier4DetailedFindings - len(summaryFindings); remainingCount > 0 {
			fmt.Fprintf(&body, "...另有 %d 条详见完整报告导出\n", remainingCount)
		}
	}

	body.WriteString("\n## 四、总结与建议\n\n")
	if len(sorted) == 0 {
		body.WriteString("未发现相关类型缺陷，建议继续保持现有质量门禁。\n")
	} else {
		fmt.Fprintf(&body, "本轮共发现 %d 条活动问题，其中致命 %d 条、严重 %d 条。请优先处理上方重点问题；完整明细请查看 JSON 台账和报告详情页。\n",
			counts.Total, counts.Fatal, counts.Critical)
	}

	return bytes.TrimSpace(body.Bytes()), nil
}

func tier4AssessmentCounts(findings []models.AnalysisFinding) map[string]int {
	known := map[string]bool{
		"REGRESSION": true, "EXPOSED_EXISTING": true, "CONDITIONAL": true,
		"NOT_CHANGE_RELATED": true, "SAFE": true, "NEEDS_HUMAN": true,
	}
	counts := make(map[string]int)
	for _, finding := range findings {
		if known[finding.AssessmentStatus] {
			counts[finding.AssessmentStatus]++
		}
	}
	return counts
}

func writeTier4Report(path string, report []byte) error {
	if len(bytes.TrimSpace(report)) == 0 {
		return fmt.Errorf("deterministic tier4 report is empty")
	}
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create tier4 report temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(report); err != nil {
		return fmt.Errorf("write tier4 report temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync tier4 report temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close tier4 report temp file: %w", err)
	}
	if err := os.Chmod(tempPath, 0644); err != nil {
		return fmt.Errorf("chmod tier4 report temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish tier4 report: %w", err)
	}
	return nil
}

func buildTier4AIInput(ctx *TaskContext, findings []models.AnalysisFinding, scanCoverage *coverage.Coverage) ([]byte, error) {
	sorted := sortTier4Findings(findings)
	detailed := sorted
	if len(detailed) > tier4DetailedFindings {
		detailed = detailed[:tier4DetailedFindings]
	}
	boundedDetailed := make([]tier4DetailedFinding, 0, len(detailed))
	for _, finding := range detailed {
		boundedDetailed = append(boundedDetailed, tier4DetailedFinding{
			Severity:    strings.TrimSpace(finding.Severity),
			Category:    strings.TrimSpace(finding.Category),
			FilePath:    strings.TrimSpace(finding.FilePath),
			LineNumber:  strings.TrimSpace(finding.LineNumber),
			Title:       strings.TrimSpace(finding.Title),
			CodeSnippet: truncateUTF8Bytes(strings.TrimSpace(finding.CodeSnippet), tier4MaxSnippetBytes),
			Detail:      truncateUTF8Bytes(strings.TrimSpace(finding.Detail), tier4MaxDetailBytes),
			Suggestion:  truncateUTF8Bytes(strings.TrimSpace(finding.Suggestion), tier4MaxSuggestionBytes),
		})
	}

	summaryCount := len(sorted) - tier4DetailedFindings
	if summaryCount < 0 {
		summaryCount = 0
	}
	summaries := make([]tier4SummaryFinding, 0, summaryCount)
	summaryStart := tier4DetailedFindings
	if summaryStart > len(sorted) {
		summaryStart = len(sorted)
	}
	summaryFindings := sorted[summaryStart:]
	if len(summaryFindings) > tier4MaxSummaryRows {
		summaryFindings = summaryFindings[:tier4MaxSummaryRows]
	}
	for _, finding := range summaryFindings {
		summaries = append(summaries, tier4SummaryFinding{
			FilePath:   finding.FilePath,
			LineNumber: finding.LineNumber,
			Title:      finding.Title,
			Severity:   finding.Severity,
		})
	}

	var coverageSummary *coverage.Summary
	if scanCoverage != nil {
		summary := scanCoverage.Summary()
		coverageSummary = &summary
	}

	payload := tier4AIInput{
		Schema: "code-shield.tier4-synthesis-input.v1",
		Report: tier4ReportMeta{
			ID:       ctx.Report.ID,
			RepoName: ctx.Repo.Name,
			TaskType: ctx.TaskType.DisplayName,
			HeadHash: ctx.Report.HeadCommit,
			Rendered: time.Now().Format(time.RFC3339),
		},
		Statistics:       tier4SeverityCounts(findings),
		Coverage:         coverageSummary,
		DetailedFindings: boundedDetailed,
		SummaryFindings:  summaries,
	}
	return json.Marshal(payload)
}

// ValidateTier4AIReport guards the optional polish path.  Any mismatch causes
// the caller to keep the deterministic report rather than trusting model text.
func ValidateTier4AIReport(report []byte, aiInputPath string) error {
	trimmed := bytes.TrimSpace(report)
	if len(trimmed) == 0 {
		return fmt.Errorf("AI report is empty")
	}
	if len(trimmed) > tier4MaxAIOutputBytes {
		return fmt.Errorf("AI report is too large: %d bytes", len(trimmed))
	}
	if bytes.Contains(bytes.ToLower(trimmed), []byte("```json")) {
		return fmt.Errorf("AI report contains a JSON code fence")
	}

	raw, err := os.ReadFile(aiInputPath)
	if err != nil {
		return fmt.Errorf("read tier4 AI input: %w", err)
	}
	var input tier4AIInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Errorf("parse tier4 AI input: %w", err)
	}
	if input.Schema != "code-shield.tier4-synthesis-input.v1" {
		// ExecuteSynthesisOnce is also used by legacy callers.  Only enforce
		// the strict Tier 4 contract when the bounded payload was produced by
		// the new Tier 4 builder.
		return nil
	}

	expectedStats := fmt.Sprintf("致命：%d，严重：%d，一般：%d，建议：%d",
		input.Statistics.Fatal, input.Statistics.Critical, input.Statistics.Minor, input.Statistics.Suggestion)
	if !strings.Contains(string(trimmed), expectedStats) {
		return fmt.Errorf("AI statistics mismatch: expected %q", expectedStats)
	}

	required := tier4RequiredDetailItems
	if len(input.DetailedFindings) < required {
		required = len(input.DetailedFindings)
	}
	found := 0
	for _, finding := range input.DetailedFindings {
		if strings.Contains(string(trimmed), finding.FilePath) {
			found++
		}
	}
	if found < required {
		return fmt.Errorf("AI report only contains %d of %d required detailed findings", found, required)
	}
	return nil
}
