package services

import "time"

// SingleEngine 是默认的单次执行引擎，将整个代码仓作为一个整体提交给 AI 分析。
// 采用双阶段流程：分析（JSON） → 综合（Markdown）。
type SingleEngine struct{}

func (e *SingleEngine) Run(ctx *taskContext) error {
	// Phase 1: 分析阶段 — AI 输出结构化 JSON 问题清单
	analysisStart := time.Now()
	findings, err := ctx.executeAnalysis(nil)
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

	// Phase 2: 综合阶段 — 基于 JSON 生成最终 Markdown 报告
	return ctx.executeSynthesis(findings)
}

func init() {
	RegisterEngine("single", &SingleEngine{})
}
