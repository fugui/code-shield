package defectlifecycle

import (
	"fmt"
	"os"
	"testing"

	"code-shield/models"
)

func TestFilterSparseScopeAndManifest(t *testing.T) {
	// 确保配置中开启稀疏存储
	models.AppConfig.Retention.EnableSparseScope = true

	fullScope := []models.ScanScopeEntry{}
	// 添加 20 个健康且未变更的文件
	for i := 1; i <= 20; i++ {
		fullScope = append(fullScope, models.ScanScopeEntry{
			NormPath:    fmt.Sprintf("src/healthy_%d.cpp", i),
			Outcome:     ScopeScanned,
			DiffTouched: false,
		})
	}
	// 添加 1 个变更文件
	fullScope = append(fullScope, models.ScanScopeEntry{
		NormPath:    "src/modified.cpp",
		Outcome:     ScopeScanned,
		DiffTouched: true,
	})
	// 添加 1 个失败文件
	fullScope = append(fullScope, models.ScanScopeEntry{
		NormPath:    "src/failed.cpp",
		Outcome:     ScopeFailed,
		FailReason:  "compile error",
		DiffTouched: false,
	})
	// 添加 1 个报出缺陷的文件
	fullScope = append(fullScope, models.ScanScopeEntry{
		NormPath:    "src/finding.cpp",
		Outcome:     ScopeScanned,
		DiffTouched: false,
	})

	input := ScanInput{
		RepoRoot: "/repo",
		Findings: []models.AnalysisFinding{
			{FilePath: "/repo/src/finding.cpp"},
		},
	}

	sparse, manifestGz := filterSparseScope(input, fullScope)

	// 稀疏切片应仅保留：1 变更 + 1 失败 + 1 命中缺陷 = 3 个
	if len(sparse) != 3 {
		t.Fatalf("expected 3 sparse entries, got %d", len(sparse))
	}

	paths := make(map[string]bool)
	for _, entry := range sparse {
		paths[entry.NormPath] = true
	}
	if !paths["src/modified.cpp"] || !paths["src/failed.cpp"] || !paths["src/finding.cpp"] {
		t.Errorf("sparse entries do not contain expected files: %+v", paths)
	}

	// 验证压缩包解压后包含全部 23 个条目
	decompressed, err := decompressScopeManifest(manifestGz)
	if err != nil {
		t.Fatalf("decompress manifest failed: %v", err)
	}
	if len(decompressed) != 23 {
		t.Errorf("expected 23 entries in decompressed manifest, got %d", len(decompressed))
	}
}

func TestWriteTempManifest(t *testing.T) {
	tempDir := t.TempDir()
	models.AppConfig.Retention.ScopeManifestDir = tempDir

	report := &models.TaskReport{ID: 999}
	data := []byte("dummy manifest content")

	tempPath, err := writeTempManifest(report, data)
	if err != nil {
		t.Fatalf("write temp manifest failed: %v", err)
	}
	defer os.Remove(tempPath)

	if _, err := os.Stat(tempPath); err != nil {
		t.Fatalf("temp manifest file does not exist: %v", err)
	}

	finalPath := manifestFinalPath(report)
	if err := os.Rename(tempPath, finalPath); err != nil {
		t.Fatalf("rename to final path failed: %v", err)
	}
	defer os.Remove(finalPath)

	readBack, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read back final manifest failed: %v", err)
	}
	if string(readBack) != string(data) {
		t.Errorf("content mismatch, got %s want %s", string(readBack), string(data))
	}
}
