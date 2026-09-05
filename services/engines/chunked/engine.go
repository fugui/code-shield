package chunked

import (
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
	"code-shield/services/engines/chunker"
)

// ChunkedEngine 将代码仓按目录结构拆分成多个分片，逐个提交给 AI 分析后汇总
type ChunkedEngine struct{}

func (e *ChunkedEngine) Name() string {
	return "chunked"
}

func (e *ChunkedEngine) Run(ctx *engines.EngineContext) (*engines.EngineResult, error) {
	cfg := engines.ChunkConfig{
		MaxFiles:    engines.DefaultChunkMaxFiles,
		Depth:       engines.DefaultChunkDepth,
		Concurrency: engines.DefaultChunkConcurrency,
	}
	if len(ctx.EngineConfig) > 0 {
		_ = json.Unmarshal(ctx.EngineConfig, &cfg)
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = engines.DefaultChunkMaxFiles
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = engines.DefaultChunkConcurrency
	}

	targetScope := "all"
	if ctx.RunParams.TargetScope != nil {
		targetScope = *ctx.RunParams.TargetScope
	}
	chunks, err := chunker.ScanAndChunk(ctx.CodesPath, cfg, targetScope)
	if err != nil {
		return nil, err
	}

	log.Printf("[ChunkedEngine] Found %d chunks for repo %s\n", len(chunks), ctx.RepoName)
	totalChunks := len(chunks)

	if ctx.ProgressReport != nil {
		ctx.ProgressReport(totalChunks, 0, 0)
	}

	nameParts := strings.Split(ctx.RepoName, "/")
	repoShort := nameParts[len(nameParts)-1]
	chunkDir := filepath.Join(filepath.Dir(ctx.ReportPath), fmt.Sprintf("chunks-%d-%s", ctx.ReportID, repoShort))
	_ = os.MkdirAll(chunkDir, 0755)

	var allFindings []models.AnalysisFinding
	var mu sync.Mutex
	var wg sync.WaitGroup
	var chunkErrors []string
	var errMu sync.Mutex
	semaphore := make(chan struct{}, cfg.Concurrency)
	chunkIndex := 0

	chunkDetailsList := make([]engines.ChunkDetails, totalChunks)
	processedChunks := 0
	successChunks := 0

loop:
	for name, files := range chunks {
		if ctx.Ctx.Err() != nil {
			break loop
		}

		chunkIndex++
		currentIndex := chunkIndex
		chunkName := name
		chunkFiles := files

		wg.Add(1)
		semaphore <- struct{}{}

		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()

			chunkStart := time.Now()
			log.Printf("[ChunkedEngine] [%d/%d] Starting chunk %q (%d files)...\n",
				currentIndex, totalChunks, chunkName, len(chunkFiles))

			var findings []models.AnalysisFinding
			var chunkErr error

			if ctx.AnalysisExecutor != nil {
				findings, chunkErr = ctx.AnalysisExecutor(chunkFiles)
			}

			chunkEnd := time.Now()
			durSec := chunkEnd.Sub(chunkStart).Seconds()

			detail := engines.ChunkDetails{
				ChunkName:       chunkName,
				StartTime:       chunkStart,
				EndTime:         chunkEnd,
				DurationSeconds: durSec,
				Attempts:        1,
				Retries:         0,
			}

			if chunkErr != nil {
				detail.Status = "failed"
				detail.Attempts = 4 // 1 initial + 3 retries
				detail.ErrorMessage = chunkErr.Error()
				errMu.Lock()
				chunkErrors = append(chunkErrors, fmt.Sprintf("chunk %q: %v", chunkName, chunkErr))
				errMu.Unlock()
				log.Printf("[ChunkedEngine] [%d/%d] Chunk %q failed (%.1fs): %v\n",
					currentIndex, totalChunks, chunkName, durSec, chunkErr)
			} else {
				detail.Status = "success"
				mu.Lock()
				allFindings = append(allFindings, findings...)
				mu.Unlock()
				log.Printf("[ChunkedEngine] [%d/%d] Chunk %q completed (%.1fs, %d findings)\n",
					currentIndex, totalChunks, chunkName, durSec, len(findings))
			}

			mu.Lock()
			chunkDetailsList[currentIndex-1] = detail
			processedChunks++
			if detail.Status == "success" {
				successChunks++
			}
			if ctx.ProgressReport != nil {
				ctx.ProgressReport(totalChunks, processedChunks, successChunks)
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	hasFailedChunks := len(chunkErrors) > 0
	if hasFailedChunks {
		log.Printf("[ChunkedEngine] Completed with %d failed chunk(s) out of %d\n", len(chunkErrors), totalChunks)
	}

	result := &engines.EngineResult{
		Findings:        allFindings,
		SummaryChunks:   chunkDetailsList,
		HasFailedChunks: hasFailedChunks,
	}

	if hasFailedChunks && len(allFindings) == 0 {
		return result, fmt.Errorf("all chunks failed: %s", strings.Join(chunkErrors, "; "))
	}

	if ctx.SynthesisExecutor != nil {
		scannedFileMap := make(map[string]bool)
		for _, files := range chunks {
			for _, f := range files {
				scannedFileMap[f] = true
			}
		}
		allScannedFiles := make([]string, 0, len(scannedFileMap))
		for f := range scannedFileMap {
			allScannedFiles = append(allScannedFiles, f)
		}

		if synthErr := ctx.SynthesisExecutor(allFindings, allScannedFiles); synthErr != nil {
			log.Printf("[ChunkedEngine] Warning: SynthesisExecutor failed: %v", synthErr)
		}
	}

	return result, nil
}

func init() {
	engines.RegisterEngine("chunked", &ChunkedEngine{})
}
