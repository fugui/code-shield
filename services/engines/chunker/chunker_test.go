package chunker

import (
	"os"
	"path/filepath"
	"testing"

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
		MaxFiles:    10,
		Depth:       1,
		Concurrency: 2,
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
		MaxFiles:    0,
		Depth:       1,
		Concurrency: 2,
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
