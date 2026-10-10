package assessment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines"
)

func TestExtractIssueSnippet(t *testing.T) {
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "test.cc")
	lines := make([]string, 200)
	for i := 0; i < 200; i++ {
		lines[i] = "line " + string(rune('A'+(i%26)))
	}
	if err := os.WriteFile(sourceFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("write file error: %v", err)
	}

	ctx := AssessmentContext{
		EngineContext: &engines.EngineContext{
			CodesPath: tempDir,
		},
	}

	// 1. 优先使用 issueCode
	t.Run("prioritize issueCode", func(t *testing.T) {
		unit := coverage.PlanUnit{
			Path:      "test.cc",
			StartLine: 1,
			EndLine:   5,
		}
		got := ExtractIssueSnippet(ctx, unit, "  int a = 1;  \n  int b = 2;  ")
		want := "int a = 1;  \n  int b = 2;"
		if got != want {
			t.Fatalf("ExtractIssueSnippet() = %q, want %q", got, want)
		}
	})

	// 2. issueCode 为空时，按行号切片回退读取
	t.Run("fallback to file range", func(t *testing.T) {
		unit := coverage.PlanUnit{
			Path:      "test.cc",
			StartLine: 2,
			EndLine:   4,
		}
		got := ExtractIssueSnippet(ctx, unit, "")
		want := strings.Join(lines[1:4], "\n")
		if got != want {
			t.Fatalf("ExtractIssueSnippet() = %q, want %q", got, want)
		}
	})

	// 3. 超大行数上限保护 (不超过 maxSnippetLines)
	t.Run("max lines truncation protection", func(t *testing.T) {
		unit := coverage.PlanUnit{
			Path:      "test.cc",
			StartLine: 1,
			EndLine:   180,
		}
		got := ExtractIssueSnippet(ctx, unit, "")
		gotLines := strings.Split(got, "\n")
		if len(gotLines) > maxSnippetLines {
			t.Fatalf("lines count = %d, exceeds max %d", len(gotLines), maxSnippetLines)
		}
	})

	// 4. 文件不存在时安全返回空字符串
	t.Run("file not found safe fallback", func(t *testing.T) {
		unit := coverage.PlanUnit{
			Path:      "not_exist.cc",
			StartLine: 1,
			EndLine:   5,
		}
		got := ExtractIssueSnippet(ctx, unit, "")
		if got != "" {
			t.Fatalf("ExtractIssueSnippet() = %q, want empty", got)
		}
	})

	// 5. 行号越界时安全返回空字符串
	t.Run("line number out of bounds", func(t *testing.T) {
		unit := coverage.PlanUnit{
			Path:      "test.cc",
			StartLine: 500,
			EndLine:   600,
		}
		got := ExtractIssueSnippet(ctx, unit, "")
		if got != "" {
			t.Fatalf("ExtractIssueSnippet() = %q, want empty", got)
		}
	})
}
