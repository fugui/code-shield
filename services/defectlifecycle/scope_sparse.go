package defectlifecycle

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"code-shield/models"
)

// filterSparseScope 将全量计划范围条目裁剪为稀疏落库集合与外置压缩 Manifest 字节
func filterSparseScope(input ScanInput, fullScope []models.ScanScopeEntry) (sparse []models.ScanScopeEntry, manifestGz []byte) {
	// 1. 若配置中显式禁用了稀疏存储，全量落库
	if !models.AppConfig.Retention.EnableSparseScope {
		manifestGz, _ = compressScopeManifest(fullScope)
		return fullScope, manifestGz
	}

	// 2. 从本轮 Findings 中构建规范化命中文件集合
	findingPaths := make(map[string]bool, len(input.Findings))
	for _, f := range input.Findings {
		norm := NormalizePath(input.RepoRoot, f.FilePath)
		if norm != "" {
			findingPaths[norm] = true
		}
	}

	// 3. 遍历计划全集，严格过滤关键稀疏事实（失败、变更、命中缺陷）
	sparse = make([]models.ScanScopeEntry, 0, 64)
	for _, entry := range fullScope {
		hasFindings := findingPaths[entry.NormPath]
		isFailed := entry.Outcome == ScopeFailed
		isTouched := entry.DiffTouched

		if isFailed || isTouched || hasFindings {
			sparse = append(sparse, entry)
		}
	}

	// 4. 全量数据序列化并 Gzip 压缩为 Manifest
	manifestGz, _ = compressScopeManifest(fullScope)
	return sparse, manifestGz
}

// compressScopeManifest 将全量 Scope 条目序列化并 Gzip 压缩
func compressScopeManifest(entries []models.ScanScopeEntry) ([]byte, error) {
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshal scope manifest: %w", err)
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return nil, fmt.Errorf("gzip scope manifest: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// decompressScopeManifest 解压并反序列化 Scope 清单
func decompressScopeManifest(gzData []byte) ([]models.ScanScopeEntry, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gzData))
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer zr.Close()

	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("read gzip data: %w", err)
	}

	var entries []models.ScanScopeEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal scope manifest: %w", err)
	}

	return entries, nil
}

// manifestFinalPath 获取报告对应的 Manifest 最终持久化路径
func manifestFinalPath(report *models.TaskReport) string {
	if report == nil {
		return ""
	}
	return report.GetScopeManifestPath()
}

// writeTempManifest 将压缩的 Manifest 写入临时文件，供事务提交成功后原子 Rename
func writeTempManifest(report *models.TaskReport, manifestGz []byte) (string, error) {
	finalPath := manifestFinalPath(report)
	if finalPath == "" {
		return "", fmt.Errorf("empty manifest final path")
	}

	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create manifest directory %s: %w", dir, err)
	}

	tempPath := fmt.Sprintf("%s.tmp.%d", finalPath, time.Now().UnixNano())
	if err := os.WriteFile(tempPath, manifestGz, 0644); err != nil {
		return "", fmt.Errorf("write temp manifest %s: %w", tempPath, err)
	}

	return tempPath, nil
}
