package services

import (
	"code-shield/models"
	"encoding/json"
	"log"
	"time"
)

// SingleEngine 是默认的单次执行引擎，将整个代码仓作为一个整体提交给 AI 分析。
// 采用双阶段流程：分析（JSON） → 综合（Markdown）。
type SingleEngine struct{}

func (e *SingleEngine) Run(ctx *taskContext) error {
	var fileList []string
	var cfg ChunkConfig
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}

	targetScope := "all"
	if ctx.runParams.TargetScope != nil {
		targetScope = *ctx.runParams.TargetScope
	}

	// 如果配置了内容关键字或自定义排除路径，我们在单仓模式下进行文件列表过滤
	useFiltering := len(cfg.ContentKeywords) > 0 || len(cfg.ExcludePaths) > 0

	if useFiltering {
		filteredFiles, err := getFilteredFiles(ctx.codesPath, cfg, targetScope)
		if err != nil {
			return err
		}
		fileList = filteredFiles
		log.Printf("[SingleEngine] File filtering active: found %d matching files out of the repository\n", len(fileList))

		// 如果过滤后没有任何匹配文件，直接跳过 AI 分析阶段，生成空 findings 的报告
		if len(fileList) == 0 {
			log.Printf("[SingleEngine] No matching files found. Skipping AI analysis.\n")
			analysisStart := time.Now()
			chunkDetails := ChunkDetails{
				ChunkName:       "root",
				StartTime:       analysisStart,
				EndTime:         analysisStart,
				DurationSeconds: 0,
				Attempts:        1,
				Retries:         0,
				Status:          "success",
			}
			ctx.Summary.Analysis.Status = "success"
			ctx.Summary.Analysis.StartTime = analysisStart
			ctx.Summary.Analysis.EndTime = analysisStart
			ctx.Summary.Analysis.DurationSeconds = 0
			ctx.Summary.Analysis.TotalChunks = 1
			ctx.Summary.Analysis.SuccessChunks = 1
			ctx.Summary.Analysis.TotalFindings = 0
			ctx.Summary.Analysis.Chunks = []ChunkDetails{chunkDetails}

			// 写入 Summary 报告并进入综合阶段（综合阶段会输出无发现的最终 Markdown）
			ctx.writeSummaryReport()
			return ctx.executeSynthesis([]models.AnalysisFinding{})
		}
	}

	// Phase 1: 分析阶段 — AI 输出结构化 JSON 问题清单
	analysisStart := time.Now()
	// 如果不使用过滤，保持向后兼容性传递 nil
	var findings []models.AnalysisFinding
	var err error
	if useFiltering {
		findings, err = ctx.executeAnalysis(fileList)
	} else {
		findings, err = ctx.executeAnalysis(nil)
	}
	analysisEnd := time.Now()

	chunkDetails := ChunkDetails{
		ChunkName:       "root",
		StartTime:       analysisStart,
		EndTime:         analysisEnd,
		DurationSeconds: analysisEnd.Sub(analysisStart).Seconds(),
		Attempts:        ctx.Attempts,
		Retries:         0,
	}

	if err != nil {
		chunkDetails.Status = "failed"
		chunkDetails.ErrorMessage = err.Error()

		ctx.Summary.Analysis.Status = "failed"
		ctx.Summary.Analysis.StartTime = analysisStart
		ctx.Summary.Analysis.EndTime = analysisEnd
		ctx.Summary.Analysis.DurationSeconds = analysisEnd.Sub(analysisStart).Seconds()
		ctx.Summary.Analysis.TotalChunks = 1
		ctx.Summary.Analysis.FailedChunks = 1
		ctx.Summary.Analysis.Chunks = []ChunkDetails{chunkDetails}
		return err
	}

	chunkDetails.Status = "success"

	ctx.Summary.Analysis.Status = "success"
	ctx.Summary.Analysis.StartTime = analysisStart
	ctx.Summary.Analysis.EndTime = analysisEnd
	ctx.Summary.Analysis.DurationSeconds = analysisEnd.Sub(analysisStart).Seconds()
	ctx.Summary.Analysis.TotalChunks = 1
	ctx.Summary.Analysis.SuccessChunks = 1
	ctx.Summary.Analysis.TotalFindings = len(findings)
	ctx.Summary.Analysis.Chunks = []ChunkDetails{chunkDetails}

	// Save task summary report
	ctx.writeSummaryReport()

	// Phase 2: 综合阶段 — 基于 JSON 生成最终 Markdown 报告
	return ctx.executeSynthesis(findings)
}

func init() {
	RegisterEngine("single", &SingleEngine{})
}
