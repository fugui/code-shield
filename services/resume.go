package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"code-shield/models"
	"code-shield/services/engines"

	"gorm.io/gorm"
)

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
	cfg := engines.ChunkConfig{
		MaxFiles:    engines.DefaultChunkMaxFiles,
		Depth:       engines.DefaultChunkDepth,
		Concurrency: engines.DefaultChunkConcurrency,
	}
	if len(ctx.taskType.EngineConfig) > 0 {
		_ = json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = engines.DefaultChunkMaxFiles
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = engines.DefaultChunkConcurrency
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
	if ctx.runParams.TargetScope == nil {
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
	_ = os.MkdirAll(chunkDir, 0755)

	// 9. 并发重试失败的 chunk
	var newFindings []models.AnalysisFinding
	var mu sync.Mutex
	var wg sync.WaitGroup
	var chunkErrors []string
	var errMu sync.Mutex
	semaphore := make(chan struct{}, cfg.Concurrency)

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

		select {
		case <-ctx.ctx.Done():
			wg.Done()
			break loop
		case semaphore <- struct{}{}:
		}

		if ctx.ctx.Err() != nil {
			select {
			case <-semaphore:
			default:
			}
			wg.Done()
			break loop
		}

		go func(chunk ChunkDetails, chunkIdx int) {
			defer wg.Done()
			defer func() { <-semaphore }()
			defer func() {
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

			cleanAnalysisTempFiles(chunkCtx.jsonPath)

			chunkStartTime := time.Now()
			findings, err := chunkCtx.executeAnalysis(chunk.Files)
			chunkEndTime := time.Now()

			if detail, ok := chunkStatusMap[chunk.ChunkName]; ok {
				detail.StartTime = chunkStartTime
				detail.EndTime = chunkEndTime
				detail.DurationSeconds = chunkEndTime.Sub(chunkStartTime).Seconds()
				detail.Attempts = chunk.Attempts + chunkCtx.Attempts
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

	models.DB.Model(&models.TaskReport{}).Where("id = ?", reportID).Update("success_chunks", successCount)
	ctx.writeSummaryReport()

	// 11. 提取所有成功分片的发现
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
		if len(newFindings) == 0 && len(existingFindings) == 0 {
			errMsg := fmt.Sprintf("resume failed: all retried chunks failed: %s", strings.Join(chunkErrors, "; "))
			ctx.markFailed(errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		log.Printf("[ResumeFailedChunks] Warning: %d chunks still failed, proceeding with available findings\n", len(chunkErrors))
	}

	var allFindings []models.AnalysisFinding
	allFindings = append(allFindings, existingFindings...)
	allFindings = append(allFindings, newFindings...)

	ctx.findings = allFindings
	if err := ctx.executeSynthesis(allFindings); err != nil {
		ctx.markFailed(err.Error())
		return err
	}

	result := ctx.runPostProcess()
	return ctx.finalize(result)
}
