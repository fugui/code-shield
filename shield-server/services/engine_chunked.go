package services

import (
	"bufio"
	"code-shield/models"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"gorm.io/gorm"
)

// ChunkConfig 定义分片引擎的配置参数
type ChunkConfig struct {
	MaxFiles    int `json:"max_files"`
	Depth       int `json:"depth"`
	Concurrency int `json:"concurrency"`
}

// ChunkedEngine 将代码仓按目录结构拆分成多个分片，逐个提交给 AI 分析后汇总。
type ChunkedEngine struct{}

func (e *ChunkedEngine) Run(ctx *taskContext) error {
	// ── 引擎前处理：解析配置并扫描分片 ──
	cfg := ChunkConfig{MaxFiles: 25, Depth: 1, Concurrency: 2}
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}

	targetScope := "all"
	if ctx.runParams.TargetScope != nil {
		targetScope = *ctx.runParams.TargetScope
	}
	chunks, err := scanAndChunk(ctx.codesPath, cfg, targetScope)
	if err != nil {
		return err
	}

	log.Printf("[ChunkedEngine] Chunked mode: Found %d chunks for repo %s\n", len(chunks), ctx.repo.Name)

	// ── 逐片执行分析阶段 ──
	// 取仓库名最后一段作为目录前缀，增强可读性
	nameParts := strings.Split(ctx.repo.Name, "/")
	repoShort := nameParts[len(nameParts)-1]
	chunkDir := filepath.Join(filepath.Dir(ctx.reportPath), fmt.Sprintf("chunks-%d-%s", ctx.report.ID, repoShort))
	os.MkdirAll(chunkDir, 0755)

	var allFindings []models.AnalysisFinding
	var mu sync.Mutex
	var wg sync.WaitGroup
	var chunkErrors []string
	var errMu sync.Mutex
	semaphore := make(chan struct{}, cfg.Concurrency)
	totalChunks := len(chunks)
	chunkIndex := 0

	// 记录总分片数
	models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Update("total_chunks", totalChunks)

	for name, files := range chunks {
		chunkIndex++
		currentIndex := chunkIndex

		wg.Add(1)
		semaphore <- struct{}{} // Acquire semaphore

		go func(chunkName string, chunkFiles []string, idx int) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release semaphore
			defer func() {
				// 原子性递增 processed_chunks
				models.DB.Model(&models.TaskReport{}).
					Where("id = ?", ctx.report.ID).
					UpdateColumn("processed_chunks", gorm.Expr("processed_chunks + ?", 1))
			}()

			safeName := strings.ReplaceAll(chunkName, "/", "-")
			chunkCtx := &taskContext{
				report:     ctx.report,
				taskType:   ctx.taskType,
				repo:       ctx.repo,
				codesPath:  ctx.codesPath,
				runParams:  ctx.runParams,
				reportPath: filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s.md", ctx.report.ID, safeName)),
				jsonPath:   filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s.json", ctx.report.ID, safeName)),
			}
			chunkCtx.report.ChunkName = chunkName

			log.Printf("[ChunkedEngine] Processing chunk %d/%d [%s] (%d files)\n", idx, totalChunks, chunkName, len(chunkFiles))

			// Phase 1: 分析阶段
			findings, err := chunkCtx.executeAnalysis(chunkFiles)
			if err != nil {
				log.Printf("[ChunkedEngine] Chunk [%s] analysis failed: %v\n", chunkName, err)
				errMu.Lock()
				chunkErrors = append(chunkErrors, fmt.Sprintf("Chunk [%s] failed: %v", chunkName, err))
				errMu.Unlock()
				return
			}

			mu.Lock()
			allFindings = append(allFindings, findings...)
			mu.Unlock()
		}(name, files, currentIndex)
	}

	wg.Wait()

	if len(chunkErrors) > 0 {
		aggregatedErr := fmt.Errorf("chunk analysis phase failed: %s", strings.Join(chunkErrors, "; "))

		// 写入到最终任务的 output.txt 并记录为 Error 或 Warning
		cliOutputPath := ctx.reportPath + ".output.txt"
		if f, createErr := os.Create(cliOutputPath); createErr == nil {
			if len(allFindings) == 0 {
				f.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution failed: %v\n", aggregatedErr))
			} else {
				f.WriteString(fmt.Sprintf("\n\n[Code-Shield Warning] Some chunks failed during analysis: %v\n", aggregatedErr))
			}
			f.Close()
		} else {
			log.Printf("[ChunkedEngine] Failed to create overall output.txt: %v\n", createErr)
		}

		// 如果所有分片都失败了且没有任何发现，则必须报错返回
		if len(allFindings) == 0 {
			return fmt.Errorf("all chunks failed: %w", aggregatedErr)
		}

		log.Printf("[ChunkedEngine] Warning: %d chunks failed, but proceeding to synthesis with %d findings from successful chunks\n", len(chunkErrors), len(allFindings))
	}

	if len(allFindings) == 0 && len(chunks) > 0 {
		log.Printf("[ChunkedEngine] Warning: all %d chunks produced no findings\n", len(chunks))
	}

	// ── Phase 2: 综合阶段 ──
	log.Printf("[ChunkedEngine] Starting synthesis for %d findings from %d chunks\n", len(allFindings), len(chunks))
	return ctx.executeSynthesis(allFindings)
}

// scanAndChunk 扫描 git 仓库中的文件并按目录深度分组
func scanAndChunk(codesPath string, cfg ChunkConfig, targetScope string) (map[string][]string, error) {
	cmd := exec.Command("git", "-C", codesPath, "ls-files")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	rawChunks := make(map[string][]string)

	for _, file := range files {
		if file == "" {
			continue
		}

		// 过滤非源码文件
		if !isSourceFile(file) {
			continue
		}

		// 根据 TargetScope 过滤文件
		isTest := isTestFile(file)
		if targetScope == "business" && isTest {
			continue
		}
		if targetScope == "test" && !isTest {
			continue
		}

		// 过滤自动生成的文件（如 Qt pyuic、protobuf 等）
		if isGeneratedFile(codesPath, file) {
			continue
		}

		parts := strings.Split(file, string(filepath.Separator))
		chunkName := "root"
		if len(parts) > 1 {
			depth := cfg.Depth
			if depth >= len(parts) {
				depth = len(parts) - 1
			}
			chunkName = filepath.Join(parts[:depth]...)
		}

		rawChunks[chunkName] = append(rawChunks[chunkName], file)
	}

	// 对超过 MaxFiles 的分片进行二次拆分
	chunks := make(map[string][]string)
	for name, fileList := range rawChunks {
		if cfg.MaxFiles > 0 && len(fileList) > cfg.MaxFiles {
			for i := 0; i < len(fileList); i += cfg.MaxFiles {
				end := i + cfg.MaxFiles
				if end > len(fileList) {
					end = len(fileList)
				}
				subName := fmt.Sprintf("%s-%d", name, i/cfg.MaxFiles+1)
				chunks[subName] = fileList[i:end]
			}
		} else {
			chunks[name] = fileList
		}
	}

	return chunks, nil
}

// sourceExtensions 定义需要分析的源码文件扩展名
var sourceExtensions = map[string]bool{
	// 通用编程语言
	".go": true, ".py": true, ".java": true, ".kt": true, ".scala": true,
	".js": true, ".ts": true, ".jsx": true, ".tsx": true, ".vue": true, ".svelte": true,
	".c": true, ".cpp": true, ".cc": true, ".cxx": true, ".h": true, ".hpp": true,
	".cs": true, ".rs": true, ".rb": true, ".php": true, ".swift": true, ".m": true,
	".dart": true, ".lua": true, ".r": true, ".pl": true, ".pm": true,
	// Shell / 脚本
	".sh": true, ".bash": true, ".zsh": true, ".bat": true, ".ps1": true,
	// // 配置 / 标记语言（可能含安全相关配置）
	// ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	// ".xml": true, ".json": true, ".jsonc": true,
	// // Web
	// ".html": true, ".css": true, ".scss": true, ".less": true,
	// 数据库
	".sql": true,
	// 其他
	".proto": true, ".graphql": true, ".gql": true,
	".tf": true, ".hcl": true,
	".dockerfile": true,
}

// isSourceFile 根据扩展名判断是否为源码文件
func isSourceFile(file string) bool {
	// 跳过 . 开头的目录（如 .github/, .vscode/, .idea/ 等）
	for _, part := range strings.Split(file, "/") {
		if strings.HasPrefix(part, ".") && part != "." {
			return false
		}
	}

	// 跳过常见的非源码目录
	lower := strings.ToLower(file)
	for _, skip := range []string{"vendor/", "node_modules/", "__pycache__/", "dist/", "build/"} {
		if strings.Contains(lower, skip) {
			return false
		}
	}

	ext := strings.ToLower(filepath.Ext(file))
	if ext == "" {
		// 无扩展名的特殊文件（如 Dockerfile, Makefile）
		base := strings.ToLower(filepath.Base(file))
		return base == "dockerfile" || base == "makefile" || base == "rakefile" || base == "gemfile"
	}

	return sourceExtensions[ext]
}

// isTestFile 根据文件名和路径判断是否为测试文件
func isTestFile(file string) bool {
	base := filepath.Base(file)
	lower := strings.ToLower(base)

	// Go: *_test.go
	if strings.HasSuffix(lower, "_test.go") {
		return true
	}
	// JS/TS: *.test.*, *.spec.*
	if strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") {
		return true
	}
	// Python: test_*.py, *_test.py
	if strings.HasSuffix(lower, ".py") && (strings.HasPrefix(lower, "test_") || strings.HasSuffix(strings.TrimSuffix(lower, ".py"), "_test")) {
		return true
	}
	// Java/Kotlin: *Test.java, *Spec.java, *Test.kt
	if strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Spec.java") || strings.HasSuffix(base, "Test.kt") {
		return true
	}

	// 测试目录
	lowerPath := strings.ToLower(file)
	for _, dir := range []string{"test/", "tests/", "__tests__/", "spec/", "testdata/"} {
		if strings.Contains(lowerPath, dir) {
			return true
		}
	}

	return false
}

// generatedMarkers 常见自动生成文件的标记（出现在文件头部前几行）
var generatedMarkers = []string{
	"# Form implementation generated from reading ui file", // Qt pyuic5/pyuic6
	"# Created by: PyQt",                                   // PyQt UI code generator
	"# WARNING! All changes made in this file will be lost", // Qt Designer
	"// Code generated by",                                  // Go generate / protobuf
	"// DO NOT EDIT",                                        // 通用自动生成标记
	"# This file is automatically generated",               // 通用 Python/Shell
	"/* This file is auto-generated",                        // 通用 C/C++/Java
	"// This code was generated by",                         // gRPC / Swagger
	"# Generated by the protocol buffer compiler",          // protobuf Python
}

// isGeneratedFile 读取文件头部前 10 行，检查是否包含自动生成标记
func isGeneratedFile(codesPath, file string) bool {
	f, err := os.Open(filepath.Join(codesPath, file))
	if err != nil {
		return false
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	for i := 0; i < 10; i++ {
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		for _, marker := range generatedMarkers {
			if strings.Contains(line, marker) {
				log.Printf("[ChunkedEngine] Skipping generated file: %s\n", file)
				return true
			}
		}
		if err == io.EOF {
			break
		}
	}
	return false
}

func init() {
	RegisterEngine("chunked", &ChunkedEngine{})
}
