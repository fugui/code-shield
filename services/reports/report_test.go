package reports

import (
	"bytes"
	"code-shield/models"
	"testing"
	"time"
)

func TestNormalizeSeverity(t *testing.T) {
	cases := []struct {
		raw      string
		expected string
	}{
		{"致命", SeverityFatal},
		{"fatal", SeverityFatal},
		{"阻塞", SeverityFatal},
		{"P0", SeverityFatal},
		{"严重", SeverityCritical},
		{"critical", SeverityCritical},
		{"高风险", SeverityCritical},
		{"P1", SeverityCritical},
		{"一般", SeverityMajor},
		{"major", SeverityMajor},
		{"中风险", SeverityMajor},
		{"P2", SeverityMajor},
		{"提示", SeverityMinor},
		{"minor", SeverityMinor},
		{"低风险", SeverityMinor},
		{"P3", SeverityMinor},
		{"建议", SeveritySuggestion},
		{"suggestion", SeveritySuggestion},
		{"合格", SeverityPass},
		{"pass", SeverityPass},
		{"unknown_string", SeverityMinor},
	}

	for _, c := range cases {
		got := NormalizeSeverity(c.raw)
		if got != c.expected {
			t.Errorf("NormalizeSeverity(%q) = %q, expected %q", c.raw, got, c.expected)
		}
	}
}

func TestCalculateRating(t *testing.T) {
	if CalculateRating(95) != "风险估值" {
		t.Errorf("expected 风险估值 for score 95")
	}
}

func TestGetStatusChinese(t *testing.T) {
	if GetStatusChinese("open", false) != "待处理" {
		t.Errorf("expected 待处理 for open in defect mode")
	}
	if GetStatusChinese("open", true) != "待复核" {
		t.Errorf("expected 待复核 for open in entity mode")
	}
	if GetStatusChinese("resolved", false) != "已解决" {
		t.Errorf("expected 已解决 for resolved in defect mode")
	}
	if GetStatusChinese("resolved", true) != "已整改" {
		t.Errorf("expected 已整改 for resolved in entity mode")
	}
}

func TestBuildExportFilename(t *testing.T) {
	report := &models.TaskReport{
		ID: 1028,
		Repo: models.Repository{
			Name: "fugui/code-bench",
		},
		TaskType: models.TaskType{
			Name: "code_review",
		},
	}

	filename := BuildExportFilename(report, "findings", "xlsx")
	if filename == "" {
		t.Errorf("expected non-empty filename")
	}
	expectedPrefix := "fugui-code-bench_1028_code-review_findings_"
	if len(filename) < len(expectedPrefix) || filename[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected filename prefix %s, got %s", expectedPrefix, filename)
	}
}

func TestMarkdownExporter(t *testing.T) {
	report := &models.TaskReport{
		ID:        1001,
		AISummary: "### 综述内容\n\n测试综述",
		Repo: models.Repository{
			Name: "test/repo",
		},
		TaskType: models.TaskType{
			Name: "test_task",
		},
	}

	exporter := &MarkdownExporter{}
	var buf bytes.Buffer
	err := exporter.Export(report, &buf, ExportOptions{})
	if err != nil {
		t.Fatalf("export markdown failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("测试综述")) {
		t.Errorf("expected markdown to contain summary content")
	}
}

func TestExcelExporterCSV(t *testing.T) {
	report := &models.TaskReport{
		ID: 1002,
		Repo: models.Repository{
			Name: "test/repo",
		},
		TaskType: models.TaskType{
			Name:           "ut_test",
			GovernanceMode: models.GovernanceModeEntityAssessment,
		},
		CreatedAt: time.Now(),
	}

	exporter := &ExcelExporter{IsCSV: true}
	var buf bytes.Buffer
	err := exporter.Export(report, &buf, ExportOptions{})
	if err != nil {
		t.Fatalf("export csv failed: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte{0xEF, 0xBB, 0xBF}) {
		t.Errorf("expected UTF-8 BOM prefix")
	}
}

func TestExtractLatestComment(t *testing.T) {
	// 1. 空字节
	if got := ExtractLatestComment(nil); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}

	// 2. 只有系统原因
	systemLogs := []byte(`[{"status":"open","time":"2026-08-25 10:00:00","user":"system","reason":"Initial scan discovery"}]`)
	if got := ExtractLatestComment(systemLogs); got != "Initial scan discovery" {
		t.Errorf("expected 'Initial scan discovery', got %q", got)
	}

	// 3. 人工填写了意见
	userLogs := []byte(`[
		{"status":"open","time":"2026-08-25 10:00:00","user":"system","reason":"Initial scan discovery"},
		{"status":"analyzing","time":"2026-08-25 11:00:00","user":"张三","comment":"排查中，确认为边界缺陷"}
	]`)
	if got := ExtractLatestComment(userLogs); got != "排查中，确认为边界缺陷" {
		t.Errorf("expected '排查中，确认为边界缺陷', got %q", got)
	}

	// 4. 后续流转未填写意见（优先保留最近一次填写的意见）
	userLogsWithSubsequentEmpty := []byte(`[
		{"status":"open","time":"2026-08-25 10:00:00","user":"system","reason":"Initial scan discovery"},
		{"status":"analyzing","time":"2026-08-25 11:00:00","user":"张三","comment":"排查中，确认为边界缺陷"},
		{"status":"resolved","time":"2026-08-25 12:00:00","user":"李四","comment":""}
	]`)
	if got := ExtractLatestComment(userLogsWithSubsequentEmpty); got != "排查中，确认为边界缺陷" {
		t.Errorf("expected '排查中，确认为边界缺陷', got %q", got)
	}
}
