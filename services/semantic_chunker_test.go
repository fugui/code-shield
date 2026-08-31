package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSemanticChunker_CrossDirectoryProjection(t *testing.T) {
	// 创建临时测试工作区
	tmpDir, err := os.MkdirTemp("", "code-shield-chunker-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 构建目录结构:
	// include/fmt/posix.h
	// include/fmt/format.h
	// src/posix.cc
	// src/format.cc
	// CMakeLists.txt
	os.MkdirAll(filepath.Join(tmpDir, "include", "fmt"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)

	_ = os.WriteFile(filepath.Join(tmpDir, "include", "fmt", "posix.h"), []byte("class buffered_file { int fileno(); };"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "include", "fmt", "format.h"), []byte("struct parse_arg_id { int id; };"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "src", "posix.cc"), []byte("#include <fmt/posix.h>\nint buffered_file::fileno() { return 1; }"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "src", "format.cc"), []byte("#include <fmt/format.h>\nvoid parse() {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "CMakeLists.txt"), []byte("set(FMT_USE_GRISU 0)\nadd_definitions(-DFMT_HEADER_ONLY=1)"), 0644)

	cfg := ChunkConfig{
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
		t.Errorf("Expected 'src' bundle to be created")
	}
}
