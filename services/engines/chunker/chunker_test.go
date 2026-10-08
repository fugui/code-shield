package chunker

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines"
)

func TestSemanticChunker_CrossDirectoryProjection(t *testing.T) {
	tmpDir := t.TempDir()

	os.MkdirAll(filepath.Join(tmpDir, "include", "fmt"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)

	_ = os.WriteFile(filepath.Join(tmpDir, "include", "fmt", "posix.h"), []byte("class buffered_file { int fileno(); };"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "include", "fmt", "format.h"), []byte("struct parse_arg_id { int id; };"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "src", "posix.cc"), []byte("#include <fmt/posix.h>\nint buffered_file::fileno() { return 1; }"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "src", "format.cc"), []byte("#include <fmt/format.h>\nvoid parse() {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "CMakeLists.txt"), []byte("set(FMT_USE_GRISU 0)\nadd_definitions(-DFMT_HEADER_ONLY=1)"), 0644)

	cfg := engines.ChunkConfig{
		MaxFiles: 10,
		Depth:    1,
	}

	bundles, err := BuildSemanticBundles(tmpDir, cfg, "all", []string{"rule-test"})
	if err != nil {
		t.Fatalf("BuildSemanticBundles failed: %v", err)
	}

	if len(bundles) == 0 {
		t.Fatalf("Expected bundles, got 0")
	}

	// 验证宏提取
	firstBundle := bundles[0]
	if firstBundle.MacroContext["FMT_USE_GRISU"] != "0" {
		t.Errorf("Expected FMT_USE_GRISU=0, got %s", firstBundle.MacroContext["FMT_USE_GRISU"])
	}
	if firstBundle.MacroContext["FMT_HEADER_ONLY"] != "1" {
		t.Errorf("Expected FMT_HEADER_ONLY=1, got %s", firstBundle.MacroContext["FMT_HEADER_ONLY"])
	}

	// 验证 src 分片中成功投影包含对应头文件
	var foundSrcChunk bool
	for _, b := range bundles {
		if b.Name == "src" {
			foundSrcChunk = true
			if len(b.HeaderFiles) == 0 {
				t.Errorf("Expected src bundle to have paired header files, got 0")
			}
		}
	}
	if !foundSrcChunk {
		t.Errorf("Expected src chunk with projected header, but not found")
	}
}

func TestPlanFilesRecordsExclusionReasons(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"src/keep.cpp":        "int keep() { return 0; }",
		"src/keyword.cpp":     "needle",
		"src/skip.cpp":        "int skip() { return 0; }",
		"src/example_test.go": "func TestExample() {}",
		"docs/readme.txt":     "readme",
	}
	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	unreadable := filepath.Join(tmpDir, "src", "unreadable.cpp")
	if err := os.Symlink(filepath.Join("missing-target"), unreadable); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanFiles(tmpDir, engines.ChunkConfig{
		ExcludePaths:    []string{"skip"},
		ContentKeywords: []string{"needle"},
	}, "business")
	if err != nil {
		t.Fatalf("PlanFiles failed: %v", err)
	}
	if len(plan.Selected) != 1 || plan.Selected[0].Path != "src/keyword.cpp" {
		t.Fatalf("unexpected selected files: %+v", plan.Selected)
	}
	reasons := map[string]string{}
	for _, item := range plan.Excluded {
		reasons[item.Path] = item.Reason
	}
	expected := map[string]string{
		"src/skip.cpp":        coverage.ReasonExcludePath,
		"src/example_test.go": coverage.ReasonTargetScope,
		"docs/readme.txt":     coverage.ReasonNotSourceFile,
	}
	for path, reason := range expected {
		if reasons[path] != reason {
			t.Fatalf("expected %s=%s, got %+v", path, reason, reasons)
		}
	}
	if len(plan.UnchangedSkipped) != 0 {
		t.Fatalf("non-incremental scan should not skip files: %+v", plan.UnchangedSkipped)
	}
	if len(plan.Unknown) != 1 || plan.Unknown[0].Path != "src/unreadable.cpp" || plan.Unknown[0].Reason != coverage.ReasonContentFilterError {
		t.Fatalf("content read failure must be unknown, got %+v", plan.Unknown)
	}
}

func TestListRepositoryFilesPreservesUnicodePaths(t *testing.T) {
	tmpDir := t.TempDir()
	relativePath := "docs/【swp】支持.md"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(tmpDir, relativePath)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, relativePath), []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"add", "--", relativePath},
	} {
		cmd := exec.Command("git", append([]string{"-C", tmpDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	files, err := listRepositoryFiles(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.ToSlash(relativePath)
	for _, file := range files {
		if file == expected {
			return
		}
	}
	t.Fatalf("expected %q in %q", expected, files)
}

func TestPlanFilesRecordsIncrementalSkipsAndDiffTouch(t *testing.T) {
	tmpDir := t.TempDir()
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := git("init"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "base.cpp"), []byte("int base() { return 0; }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "changed.cpp"), []byte("int changed() { return 0; }"), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := git("add", "base.cpp", "changed.cpp"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := git("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "initial"); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "changed.cpp"), []byte("int changed() { return 1; }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "unchanged.cpp"), []byte("int unchanged() { return 0; }"), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanFiles(tmpDir, engines.ChunkConfig{DiffBase: "HEAD"}, "all")
	if err != nil {
		t.Fatalf("PlanFiles failed: %v", err)
	}
	selected := map[string]coverage.PlannedFile{}
	for _, item := range plan.Selected {
		selected[item.Path] = item
	}
	changed, ok := selected["changed.cpp"]
	if !ok || !changed.DiffTouched {
		t.Fatalf("expected changed.cpp to be selected and diff-touched: %+v", plan.Selected)
	}
	if _, ok := selected["unchanged.cpp"]; ok {
		t.Fatalf("unchanged.cpp should not be selected: %+v", plan.Selected)
	}
	unchangedSkipped := map[string]coverage.PlannedFile{}
	for _, item := range plan.UnchangedSkipped {
		unchangedSkipped[item.Path] = item
	}
	if _, ok := unchangedSkipped["unchanged.cpp"]; !ok || unchangedSkipped["unchanged.cpp"].DiffTouched {
		t.Fatalf("unexpected unchanged skipped files: %+v", plan.UnchangedSkipped)
	}
}

func TestSemanticChunker_MaxFilesDefault8(t *testing.T) {
	tmpDir := t.TempDir()

	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(srcDir, 0755)

	// 创建 18 个源文件，断言默认按 8 个文件拆分后生成 3 个分片 (8 + 8 + 2)
	for i := 1; i <= 18; i++ {
		filePath := filepath.Join(srcDir, filepath.FromSlash(filepath.Clean(filepath.Join(".", "file_"+string(rune('a'+i-1))+".cpp"))))
		_ = os.WriteFile(filePath, []byte("int test() { return 0; }"), 0644)
	}

	// 传入 MaxFiles: 0，测试自动生效默认值 8
	cfg := engines.ChunkConfig{
		MaxFiles: 0,
		Depth:    1,
	}

	bundles, err := BuildSemanticBundles(tmpDir, cfg, "all", nil)
	if err != nil {
		t.Fatalf("BuildSemanticBundles failed: %v", err)
	}

	if len(bundles) != 3 {
		t.Fatalf("Expected 3 bundles for 18 files with default MaxFiles=8, got %d", len(bundles))
	}

	// 验证前两个分片大小为 8，第三个分片大小为 2
	if len(bundles[0].AllFiles) != 8 {
		t.Errorf("Expected bundle 0 to have 8 files, got %d", len(bundles[0].AllFiles))
	}
	if len(bundles[1].AllFiles) != 8 {
		t.Errorf("Expected bundle 1 to have 8 files, got %d", len(bundles[1].AllFiles))
	}
	if len(bundles[2].AllFiles) != 2 {
		t.Errorf("Expected bundle 2 to have 2 files, got %d", len(bundles[2].AllFiles))
	}
}

func TestSplitSemanticBundleInHalfKeepsHeaderPairs(t *testing.T) {
	bundle := SemanticBundle{
		Name:         "src",
		AllFiles:     []string{"src/a.cc", "include/a.h", "src/b.cc", "include/b.h"},
		MacroContext: map[string]string{"USE_X": "1"},
	}

	halves := SplitSemanticBundleInHalf(bundle)
	if len(halves) != 2 {
		t.Fatalf("expected 2 halves, got %d", len(halves))
	}
	if got := append(append([]string{}, halves[0].PrimaryFiles...), halves[0].HeaderFiles...); len(got) != 2 {
		t.Fatalf("expected first half to contain two files, got %v", got)
	}
	if got := append(append([]string{}, halves[1].PrimaryFiles...), halves[1].HeaderFiles...); len(got) != 2 {
		t.Fatalf("expected second half to contain two files, got %v", got)
	}
	if !reflect.DeepEqual(halves[0].PrimaryFiles, []string{"src/a.cc"}) ||
		!reflect.DeepEqual(halves[0].HeaderFiles, []string{"include/a.h"}) {
		t.Fatalf("header pair was not preserved: %+v", halves[0])
	}
}

func TestSplitSemanticBundleInHalfDoesNotCreateEmptyHalf(t *testing.T) {
	bundle := SemanticBundle{
		Name:     "src",
		AllFiles: []string{"src/a.cc", "src/b.cc", "include/a.h"},
	}

	halves := SplitSemanticBundleInHalf(bundle)
	if len(halves) != 2 {
		t.Fatalf("expected 2 halves, got %d", len(halves))
	}
	for i, half := range halves {
		if len(half.AllFiles) == 0 {
			t.Fatalf("half %d is empty", i+1)
		}
	}
	total := len(halves[0].AllFiles) + len(halves[1].AllFiles)
	if total != len(bundle.AllFiles) {
		t.Fatalf("expected %d files across halves, got %d", len(bundle.AllFiles), total)
	}
}

func TestIsSourceFileAndTestFile(t *testing.T) {
	if !IsSourceFile("main.go", nil) {
		t.Errorf("expected main.go to be source file")
	}
	if IsSourceFile(".hidden/main.go", nil) {
		t.Errorf("expected .hidden to be skipped")
	}
	if IsSourceFile("vendor/dep.go", nil) {
		t.Errorf("expected vendor to be skipped")
	}
	if !IsTestFile("main_test.go") {
		t.Errorf("expected main_test.go to be test file")
	}
	if IsTestFile("main.go") {
		t.Errorf("expected main.go not to be test file")
	}
}
