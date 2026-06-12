package services

import (
	"bufio"
	"code-shield/models"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ChunkConfig 定义分片引擎的配置参数
type ChunkConfig struct {
	MaxFiles       int      `json:"max_files"`
	Depth          int      `json:"depth"`
	Concurrency    int      `json:"concurrency"`
	FileExtensions []string `json:"file_extensions"` // 任务级文件扩展名白名单，为空时使用全局 sourceExtensions
}

// ChunkedEngine 将代码仓按目录结构拆分成多个分片，逐个提交给 AI 分析后汇总。
type ChunkedEngine struct{}

func (e *ChunkedEngine) Run(ctx *taskContext) error {
	// ── 引擎前处理：解析配置并扫描分片 ──
	cfg := ChunkConfig{MaxFiles: 20, Depth: 1, Concurrency: 4}
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
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
	overallStartTime := time.Now()

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

	chunkDetailsList := make([]ChunkDetails, totalChunks)

loop:
	for name, files := range chunks {
		if ctx.ctx.Err() != nil {
			break loop
		}

		chunkIndex++
		currentIndex := chunkIndex

		wg.Add(1)

		// Acquire semaphore, but abort if context gets cancelled
		select {
		case <-ctx.ctx.Done():
			wg.Done()
			break loop
		case semaphore <- struct{}{}:
		}

		if ctx.ctx.Err() != nil {
			select {
			case <-semaphore: // Release slot if acquired
			default:
			}
			wg.Done()
			break loop
		}

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
				ctx:        ctx.ctx,
				reportPath: filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s.md", ctx.report.ID, safeName)),
				jsonPath:   filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s.json", ctx.report.ID, safeName)),
			}
			chunkCtx.report.ChunkName = chunkName

			log.Printf("[ChunkedEngine] Processing chunk %d/%d [%s] (%d files)\n", idx, totalChunks, chunkName, len(chunkFiles))

			// Phase 1: 分析阶段
			chunkStartTime := time.Now()
			findings, err := chunkCtx.executeAnalysis(chunkFiles)
			chunkEndTime := time.Now()

			details := ChunkDetails{
				ChunkName:       chunkName,
				Files:           chunkFiles,
				Attempts:        chunkCtx.Attempts,
				Retries:         0, // 初始执行时，恢复轮数为 0
				StartTime:       chunkStartTime,
				EndTime:         chunkEndTime,
				DurationSeconds: chunkEndTime.Sub(chunkStartTime).Seconds(),
			}

			if err != nil {
				log.Printf("[ChunkedEngine] Chunk [%s] analysis failed: %v\n", chunkName, err)
				errMu.Lock()
				chunkErrors = append(chunkErrors, fmt.Sprintf("Chunk [%s] failed: %v", chunkName, err))
				errMu.Unlock()

				details.Status = "failed"
				details.ErrorMessage = err.Error()
				chunkDetailsList[idx-1] = details
				return
			}

			details.Status = "success"
			chunkDetailsList[idx-1] = details

			mu.Lock()
			allFindings = append(allFindings, findings...)
			mu.Unlock()
		}(name, files, currentIndex)
	}

	wg.Wait()

	if ctx.ctx.Err() != nil {
		return ctx.ctx.Err()
	}

	overallEndTime := time.Now()
	successfulChunks := 0
	failedChunks := 0
	for _, details := range chunkDetailsList {
		if details.Status == "success" {
			successfulChunks++
		} else {
			failedChunks++
		}
	}

	// 记录成功分片数
	models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Update("success_chunks", successfulChunks)

	// Populate analysis metrics on Summary
	ctx.Summary.Analysis.StartTime = overallStartTime
	ctx.Summary.Analysis.EndTime = overallEndTime
	ctx.Summary.Analysis.DurationSeconds = overallEndTime.Sub(overallStartTime).Seconds()
	ctx.Summary.Analysis.TotalChunks = totalChunks
	ctx.Summary.Analysis.SuccessChunks = successfulChunks
	ctx.Summary.Analysis.FailedChunks = failedChunks
	ctx.Summary.Analysis.TotalFindings = len(allFindings)
	ctx.Summary.Analysis.Chunks = chunkDetailsList

	if failedChunks > 0 {
		ctx.Summary.Analysis.Status = "failed"
	} else {
		ctx.Summary.Analysis.Status = "success"
	}

	// Save task summary report
	ctx.writeSummaryReport()

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
	ctx.findings = allFindings
	return ctx.executeSynthesis(allFindings)
}

// scanAndChunk 扫描 git 仓库中的文件并按目录深度分组
func scanAndChunk(codesPath string, cfg ChunkConfig, targetScope string) (map[string][]string, error) {
	cmd := exec.Command("git", "-C", codesPath, "ls-files")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	// 构建任务级扩展名白名单（为空时 isSourceFile 回退到全局白名单）
	var taskExtensions map[string]bool
	if len(cfg.FileExtensions) > 0 {
		taskExtensions = make(map[string]bool, len(cfg.FileExtensions))
		for _, ext := range cfg.FileExtensions {
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			taskExtensions[strings.ToLower(ext)] = true
		}
		log.Printf("[ChunkedEngine] Using task-level file extensions filter: %v\n", cfg.FileExtensions)
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	rawChunks := make(map[string][]string)

	for _, file := range files {
		if file == "" {
			continue
		}

		// 过滤非源码文件（任务级白名单优先于全局白名单）
		if !isSourceFile(file, taskExtensions) {
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

// isSourceFile 根据扩展名判断是否为源码文件。
// taskExtensions 为任务级白名单，非 nil 时优先使用；为 nil 时回退到全局 sourceExtensions。
func isSourceFile(file string, taskExtensions map[string]bool) bool {
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
		// 任务级白名单不包含无扩展名文件时直接跳过
		if taskExtensions != nil {
			return false
		}
		base := strings.ToLower(filepath.Base(file))
		return base == "dockerfile" || base == "makefile" || base == "rakefile" || base == "gemfile"
	}

	// 任务级白名单优先
	if taskExtensions != nil {
		return taskExtensions[ext]
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

	// C/C++: test_*.cpp/cc/c/cxx/h/hpp/hxx, *_test.cpp/cc/..., *_unittest.cpp/cc/...
	for _, ext := range []string{".cpp", ".cc", ".c", ".cxx", ".h", ".hpp", ".hxx"} {
		if strings.HasSuffix(lower, ext) {
			nameNoExt := strings.TrimSuffix(lower, ext)
			if strings.HasPrefix(nameNoExt, "test_") || strings.HasSuffix(nameNoExt, "_test") || strings.HasSuffix(nameNoExt, "_unittest") {
				return true
			}
		}
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
	"# Created by: PyQt", // PyQt UI code generator
	"# WARNING! All changes made in this file will be lost", // Qt Designer
	"// Code generated by",                        // Go generate / protobuf
	"// DO NOT EDIT",                              // 通用自动生成标记
	"# This file is automatically generated",      // 通用 Python/Shell
	"/* This file is auto-generated",              // 通用 C/C++/Java
	"// This code was generated by",               // gRPC / Swagger
	"# Generated by the protocol buffer compiler", // protobuf Python
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

// ResumeFailedChunks 读取指定报告的 chunk 执行摘要 JSON，找到失败的 chunk 进行重试，
// 全部完成后重新汇总 findings 并生成最终报告。
func ResumeFailedChunks(reportID uint) error {
	ctx := &taskContext{}

	// 1. 加载 report、taskType、repo
	var report models.TaskReport
	if err := models.DB.Preload("Repo").Preload("TaskType").First(&report, reportID).Error; err != nil {
		return fmt.Errorf("report %d not found: %w", reportID, err)
	}
	ctx.report = report
	ctx.taskType = report.TaskType
	ctx.repo = report.Repo

	// 2. 解析引擎配置
	cfg := ChunkConfig{MaxFiles: 20, Depth: 1, Concurrency: 4}
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}

	// 3. 构造上下文并准备路径
	taskCtx, cancel := context.WithCancel(context.Background())
	ctx.ctx = taskCtx
	ctx.cancel = cancel

	activeTasksMu.Lock()
	activeTasks[reportID] = ctx
	activeTasksMu.Unlock()
	defer func() {
		activeTasksMu.Lock()
		delete(activeTasks, reportID)
		activeTasksMu.Unlock()
		cancel()
	}()

	// 解析 RunParams（从关联的执行日志恢复）
	var execLog models.TaskExecutionLog
	if err := models.DB.Preload("Schedule").Where("task_report_id = ?", reportID).First(&execLog).Error; err == nil {
		if execLog.Schedule != nil && len(execLog.Schedule.RunParams) > 0 {
			var rp models.RunParams
			if err := json.Unmarshal(execLog.Schedule.RunParams, &rp); err == nil {
				ctx.resolveRunParams(rp)
			}
		}
	}
	if ctx.runParams.AIBackend == nil {
		ctx.resolveRunParams(models.RunParams{})
	}

	ctx.prepareOutputPaths()

	// 4. 更新状态为 analyzing，并重置已处理分片数为已成功分片数，以便前端正确显示重试进度
	models.DB.Model(&models.TaskReport{}).Where("id = ?", reportID).Updates(map[string]interface{}{
		"status":           models.StatusAnalyzing,
		"processed_chunks": report.SuccessChunks,
		"created_at":       time.Now(),
	})

	// 5. 同步代码
	if err := ctx.prepareAndSync(ctx.repo.URL); err != nil {
		ctx.markFailed(err.Error())
		return err
	}

	// 6. 读取 summary JSON
	summaryData, err := os.ReadFile(ctx.jsonPath)
	if err != nil {
		errMsg := fmt.Sprintf("failed to read chunk summary JSON (%s): %v", ctx.jsonPath, err)
		ctx.markFailed(errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	var taskSummary TaskSummaryReport
	if err := json.Unmarshal(summaryData, &taskSummary); err != nil {
		errMsg := fmt.Sprintf("failed to parse task summary JSON: %v", err)
		ctx.markFailed(errMsg)
		return fmt.Errorf("%s", errMsg)
	}
	ctx.Summary = taskSummary

	// 7. 提取失败的 chunk
	var failedChunks []ChunkDetails
	for _, chunk := range ctx.Summary.Analysis.Chunks {
		if chunk.Status == "failed" {
			failedChunks = append(failedChunks, chunk)
		}
	}

	if len(failedChunks) == 0 {
		log.Printf("[ResumeFailedChunks] No failed chunks found for ReportID %d, nothing to resume\n", reportID)
		return fmt.Errorf("no failed chunks to resume")
	}

	log.Printf("[ResumeFailedChunks] Found %d failed chunks to retry for ReportID %d\n", len(failedChunks), reportID)

	// 8. 准备 chunk 输出目录
	nameParts := strings.Split(ctx.repo.Name, "/")
	repoShort := nameParts[len(nameParts)-1]
	chunkDir := filepath.Join(filepath.Dir(ctx.reportPath), fmt.Sprintf("chunks-%d-%s", ctx.report.ID, repoShort))
	os.MkdirAll(chunkDir, 0755)

	// 9. 并发重试失败的 chunk
	var newFindings []models.AnalysisFinding
	var mu sync.Mutex
	var wg sync.WaitGroup
	var chunkErrors []string
	var errMu sync.Mutex
	semaphore := make(chan struct{}, cfg.Concurrency)

	// 更新 summary 中的 chunk 状态（用于追踪结果）
	chunkStatusMap := make(map[string]*ChunkDetails)
	for i := range ctx.Summary.Analysis.Chunks {
		chunkStatusMap[ctx.Summary.Analysis.Chunks[i].ChunkName] = &ctx.Summary.Analysis.Chunks[i]
	}

loop:
	for idx, failedChunk := range failedChunks {
		if ctx.ctx.Err() != nil {
			break loop
		}

		wg.Add(1)

		// Acquire semaphore, but abort if context gets cancelled
		select {
		case <-ctx.ctx.Done():
			wg.Done()
			break loop
		case semaphore <- struct{}{}:
		}

		if ctx.ctx.Err() != nil {
			select {
			case <-semaphore: // Release slot if acquired
			default:
			}
			wg.Done()
			break loop
		}

		go func(chunk ChunkDetails, chunkIdx int) {
			defer wg.Done()
			defer func() { <-semaphore }()
			defer func() {
				// 原子性递增已处理分片数以更新恢复进度
				models.DB.Model(&models.TaskReport{}).
					Where("id = ?", reportID).
					UpdateColumn("processed_chunks", gorm.Expr("processed_chunks + ?", 1))
			}()

			safeName := strings.ReplaceAll(chunk.ChunkName, "/", "-")
			chunkCtx := &taskContext{
				report:     ctx.report,
				taskType:   ctx.taskType,
				repo:       ctx.repo,
				codesPath:  ctx.codesPath,
				runParams:  ctx.runParams,
				ctx:        ctx.ctx,
				reportPath: filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s.md", ctx.report.ID, safeName)),
				jsonPath:   filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s.json", ctx.report.ID, safeName)),
			}
			chunkCtx.report.ChunkName = chunk.ChunkName

			log.Printf("[ResumeFailedChunks] Retrying chunk %d/%d [%s] (%d files)\n",
				chunkIdx+1, len(failedChunks), chunk.ChunkName, len(chunk.Files))

			// 清理上次失败 chunk 的临时文件
			cleanAnalysisTempFiles(chunkCtx.jsonPath)

			chunkStartTime := time.Now()
			findings, err := chunkCtx.executeAnalysis(chunk.Files)
			chunkEndTime := time.Now()

			// 更新 summary 中的 chunk 状态
			if detail, ok := chunkStatusMap[chunk.ChunkName]; ok {
				detail.StartTime = chunkStartTime
				detail.EndTime = chunkEndTime
				detail.DurationSeconds = chunkEndTime.Sub(chunkStartTime).Seconds()
				// 累加历史尝试次数与本次恢复尝试次数
				detail.Attempts = chunk.Attempts + chunkCtx.Attempts
				// 累加恢复轮数（增加 1）
				detail.Retries = chunk.Retries + 1

				if err != nil {
					detail.Status = "failed"
					detail.ErrorMessage = err.Error()
				} else {
					detail.Status = "success"
					detail.ErrorMessage = ""
				}
			}

			if err != nil {
				log.Printf("[ResumeFailedChunks] Chunk [%s] retry failed: %v\n", chunk.ChunkName, err)
				errMu.Lock()
				chunkErrors = append(chunkErrors, fmt.Sprintf("Chunk [%s] failed: %v", chunk.ChunkName, err))
				errMu.Unlock()
				return
			}

			mu.Lock()
			newFindings = append(newFindings, findings...)
			mu.Unlock()
		}(failedChunk, idx)
	}

	wg.Wait()

	if ctx.ctx.Err() != nil {
		return ctx.ctx.Err()
	}

	// 10. 更新 summary JSON
	successCount := 0
	failCount := 0
	for _, chunk := range ctx.Summary.Analysis.Chunks {
		if chunk.Status == "success" {
			successCount++
		} else {
			failCount++
		}
	}
	ctx.Summary.Analysis.SuccessChunks = successCount
	ctx.Summary.Analysis.FailedChunks = failCount
	ctx.Summary.Analysis.EndTime = time.Now()
	ctx.Summary.Analysis.DurationSeconds = ctx.Summary.Analysis.EndTime.Sub(ctx.Summary.Analysis.StartTime).Seconds()

	if failCount > 0 {
		ctx.Summary.Analysis.Status = "failed"
	} else {
		ctx.Summary.Analysis.Status = "success"
	}

	// 更新数据库中的 success_chunks
	models.DB.Model(&models.TaskReport{}).Where("id = ?", reportID).Update("success_chunks", successCount)

	// 保存中间 task summary report
	ctx.writeSummaryReport()

	// 11. 判断是否还有失败的 chunk
	var existingFindings []models.AnalysisFinding
	for _, chunk := range ctx.Summary.Analysis.Chunks {
		if chunk.Status == "success" {
			safeName := strings.ReplaceAll(chunk.ChunkName, "/", "-")
			chunkJsonPath := filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s.json", ctx.report.ID, safeName))
			findings, err := ctx.loadFindingsFromChunkFile(chunkJsonPath)
			if err != nil {
				log.Printf("[ResumeFailedChunks] Warning: failed to load findings for successful chunk [%s]: %v\n", chunk.ChunkName, err)
				continue
			}
			existingFindings = append(existingFindings, findings...)
		}
	}

	if len(chunkErrors) > 0 {
		if len(newFindings) == 0 {
			if len(existingFindings) == 0 {
				errMsg := fmt.Sprintf("resume failed: all retried chunks failed: %s", strings.Join(chunkErrors, "; "))
				ctx.markFailed(errMsg)
				return fmt.Errorf("%s", errMsg)
			}
		}
		log.Printf("[ResumeFailedChunks] Warning: %d chunks still failed, proceeding with available findings\n", len(chunkErrors))
	}

	// 12. 合并所有 findings（分片缓存文件中的 + 新重试成功的）
	var allFindings []models.AnalysisFinding
	allFindings = append(allFindings, existingFindings...)
	allFindings = append(allFindings, newFindings...)

	if len(allFindings) == 0 {
		log.Printf("[ResumeFailedChunks] Warning: no findings available for synthesis\n")
	}

	// 13. 重新生成综合报告
	log.Printf("[ResumeFailedChunks] Starting synthesis with %d total findings for ReportID %d\n", len(allFindings), reportID)
	ctx.findings = allFindings
	if err := ctx.executeSynthesis(allFindings); err != nil {
		ctx.markFailed(err.Error())
		return err
	}

	// 14. 后处理 + 最终化
	result := ctx.runPostProcess()
	return ctx.finalize(result)
}

func init() {
	RegisterEngine("chunked", &ChunkedEngine{})
}
