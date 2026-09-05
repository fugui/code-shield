package single

import (
	"encoding/json"
	"log"
	"time"

	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
)

// SingleEngine 将整个代码仓作为一个整体提交给 AI 分析
type SingleEngine struct{}

func (e *SingleEngine) Name() string {
	return "single"
}

func (e *SingleEngine) Run(ctx *engines.EngineContext) (*engines.EngineResult, error) {
	var fileList []string
	var cfg engines.ChunkConfig
	if len(ctx.EngineConfig) > 0 {
		_ = json.Unmarshal(ctx.EngineConfig, &cfg)
	}

	targetScope := "all"
	if ctx.RunParams.TargetScope != nil {
		targetScope = *ctx.RunParams.TargetScope
	}

	useFiltering := len(cfg.ContentKeywords) > 0 || len(cfg.ExcludePaths) > 0
	if useFiltering {
		filteredFiles, err := chunker.GetFilteredFiles(ctx.CodesPath, cfg, targetScope)
		if err != nil {
			return nil, err
		}
		fileList = filteredFiles
		log.Printf("[SingleEngine] File filtering active: found %d matching files out of the repository\n", len(fileList))

		if len(fileList) == 0 {
			log.Printf("[SingleEngine] No matching files found. Skipping AI analysis.\n")
			analysisStart := time.Now()
			chunkDetails := engines.ChunkDetails{
				ChunkName:       "root",
				StartTime:       analysisStart,
				EndTime:         analysisStart,
				DurationSeconds: 0,
				Attempts:        1,
				Retries:         0,
				Status:          "success",
			}
			result := &engines.EngineResult{
				Findings:        []models.AnalysisFinding{},
				SummaryChunks:   []engines.ChunkDetails{chunkDetails},
				HasFailedChunks: false,
			}
			if ctx.ProgressReport != nil {
				ctx.ProgressReport(1, 1, 1)
			}
			if ctx.SynthesisExecutor != nil {
				_ = ctx.SynthesisExecutor(result.Findings, fileList)
			}
			return result, nil
		}
	}

	if ctx.ProgressReport != nil {
		ctx.ProgressReport(1, 0, 0)
	}

	analysisStart := time.Now()
	var findings []models.AnalysisFinding
	var err error

	if ctx.AnalysisExecutor != nil {
		if useFiltering {
			findings, err = ctx.AnalysisExecutor(fileList)
		} else {
			findings, err = ctx.AnalysisExecutor(nil)
		}
	}

	analysisEnd := time.Now()
	chunkDetails := engines.ChunkDetails{
		ChunkName:       "root",
		StartTime:       analysisStart,
		EndTime:         analysisEnd,
		DurationSeconds: analysisEnd.Sub(analysisStart).Seconds(),
		Attempts:        1,
		Retries:         0,
	}

	if err != nil {
		chunkDetails.Status = "failed"
		chunkDetails.ErrorMessage = err.Error()
		if ctx.ProgressReport != nil {
			ctx.ProgressReport(1, 1, 0)
		}
		return &engines.EngineResult{
			Findings:        nil,
			SummaryChunks:   []engines.ChunkDetails{chunkDetails},
			HasFailedChunks: true,
		}, err
	}

	chunkDetails.Status = "success"
	if ctx.ProgressReport != nil {
		ctx.ProgressReport(1, 1, 1)
	}

	result := &engines.EngineResult{
		Findings:        findings,
		SummaryChunks:   []engines.ChunkDetails{chunkDetails},
		HasFailedChunks: false,
	}

	if ctx.SynthesisExecutor != nil {
		if synthErr := ctx.SynthesisExecutor(findings, fileList); synthErr != nil {
			log.Printf("[SingleEngine] Error: SynthesisExecutor failed: %v", synthErr)
			return result, synthErr
		}
	}

	return result, nil
}

func init() {
	engines.RegisterEngine("single", &SingleEngine{})
}
