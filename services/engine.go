package services

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"code-shield/models"
	"code-shield/services/engines"
	_ "code-shield/services/engines/chunked"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/debate"
	_ "code-shield/services/engines/single"

	"gorm.io/gorm"
)

const (
	DefaultChunkMaxFiles    = engines.DefaultChunkMaxFiles
	DefaultChunkDepth       = engines.DefaultChunkDepth
	DefaultChunkConcurrency = engines.DefaultChunkConcurrency
)

// 导出共享配置类型别名
type ChunkConfig = engines.ChunkConfig

// ChunkedEngine 兼容旧版调用的分片并发引擎包装
type ChunkedEngine struct{}

func (e *ChunkedEngine) Run(ctx *taskContext) error {
	adapter := &engineAdapter{inner: engines.GetEngine("chunked")}
	return adapter.Run(ctx)
}

// SingleEngine 兼容旧版调用的单仓分析引擎包装
type SingleEngine struct{}

func (e *SingleEngine) Run(ctx *taskContext) error {
	adapter := &engineAdapter{inner: engines.GetEngine("single")}
	return adapter.Run(ctx)
}

// DebateEngine 兼容旧版调用的辩论引擎包装
type DebateEngine struct {
	Mode string
}

func (e *DebateEngine) Run(ctx *taskContext) error {
	mode := e.Mode
	if mode == "" {
		mode = "debate_full"
	}
	adapter := &engineAdapter{inner: engines.GetEngine(mode)}
	return adapter.Run(ctx)
}

// 兼容既有单元测试与调用方的辅助函数别名
func scanAndChunk(codesPath string, cfg ChunkConfig, targetScope string) (map[string][]string, error) {
	return chunker.ScanAndChunk(codesPath, cfg, targetScope)
}

func getFilteredFiles(codesPath string, cfg ChunkConfig, targetScope string) ([]string, error) {
	return chunker.GetFilteredFiles(codesPath, cfg, targetScope)
}

func isSourceFile(file string, taskExtensions map[string]bool) bool {
	return chunker.IsSourceFile(file, taskExtensions)
}

func isTestFile(file string) bool {
	return chunker.IsTestFile(file)
}

func deriveConciseTitle(rawTitle, fallbackCategory string) string {
	return debate.DeriveConciseTitle(rawTitle, fallbackCategory)
}

// TaskEngine 兼容既有任务上下文的门面接口
type TaskEngine interface {
	Run(ctx *taskContext) error
}

// engineAdapter 桥接 engines.TaskEngine 与既有 taskContext
type engineAdapter struct {
	inner engines.TaskEngine
}

func (a *engineAdapter) Run(ctx *taskContext) error {
	overallStartTime := time.Now()

	engCtx := &engines.EngineContext{
		Ctx:           ctx.ctx,
		ReportID:      ctx.report.ID,
		RepoID:        ctx.repo.ID,
		RepoName:      ctx.repo.Name,
		TaskTypeID:    ctx.taskType.ID,
		TaskTypeName:  ctx.taskType.DisplayName,
		CodesPath:     ctx.codesPath,
		ReportPath:    ctx.reportPath,
		JSONPath:      ctx.jsonPath,
		EngineConfig:  json.RawMessage(ctx.taskType.EngineConfig),
		RunParams:     ctx.runParams,
		NegativeRules: GetNegativeRulesForScan(ctx.repo.ID, ctx.taskType.ID),
		ProgressReport: func(total, processed, success int) {
			if models.DB != nil {
				models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Updates(map[string]interface{}{
					"total_chunks":     total,
					"processed_chunks": processed,
					"success_chunks":   success,
				})
			}
		},
		AnalysisExecutor: func(fileList []string) ([]models.AnalysisFinding, error) {
			return ctx.executeAnalysis(fileList)
		},
		SynthesisExecutor: func(findings []models.AnalysisFinding) error {
			// 严重级别校准与跨扫描增量比对
			findings = CalibrateFindings(findings)
			findings, _ = DiffAndEnrichFindings(ctx.repo.ID, ctx.report.ID, ctx.taskType.ID, nil, findings, ctx.codesPath)
			ctx.findings = findings
			return ctx.executeSynthesis(findings)
		},
	}

	result, err := a.inner.Run(engCtx)

	overallEndTime := time.Now()

	if result != nil {
		ctx.hasFailedChunks = result.HasFailedChunks
		ctx.findings = result.Findings

		successfulChunks := 0
		failedChunks := 0
		for _, sc := range result.SummaryChunks {
			if sc.Status == "success" {
				successfulChunks++
			} else {
				failedChunks++
			}
		}

		ctx.Summary.Analysis.StartTime = overallStartTime
		ctx.Summary.Analysis.EndTime = overallEndTime
		ctx.Summary.Analysis.DurationSeconds = overallEndTime.Sub(overallStartTime).Seconds()
		ctx.Summary.Analysis.TotalChunks = len(result.SummaryChunks)
		ctx.Summary.Analysis.SuccessChunks = successfulChunks
		ctx.Summary.Analysis.FailedChunks = failedChunks
		ctx.Summary.Analysis.TotalFindings = len(result.Findings)

		convertedChunks := make([]ChunkDetails, len(result.SummaryChunks))
		for i, sc := range result.SummaryChunks {
			convertedChunks[i] = ChunkDetails{
				ChunkName:       sc.ChunkName,
				StartTime:       sc.StartTime,
				EndTime:         sc.EndTime,
				DurationSeconds: sc.DurationSeconds,
				Attempts:        sc.Attempts,
				Retries:         sc.Retries,
				Status:          sc.Status,
				ErrorMessage:    sc.ErrorMessage,
			}
		}
		ctx.Summary.Analysis.Chunks = convertedChunks

		if failedChunks > 0 {
			ctx.Summary.Analysis.Status = "failed"
		} else {
			ctx.Summary.Analysis.Status = "success"
		}

		ctx.writeSummaryReport()

		// 持久化辩论日志
		if len(result.DebateLogs) > 0 && models.DB != nil {
			for _, dl := range result.DebateLogs {
				dl.TaskReportID = ctx.report.ID
				if dbErr := models.DB.Create(&dl).Error; dbErr != nil {
					log.Printf("[EngineAdapter] Warning: failed to persist DebateLog for %s: %v", dl.CandidateID, dbErr)
				}
			}
		}

		// 累加 Token 消耗
		if (result.HunterTokens > 0 || result.Tier2Tokens > 0) && models.DB != nil {
			updates := map[string]interface{}{}
			if result.HunterTokens > 0 {
				updates["tier1_tokens"] = gorm.Expr("tier1_tokens + ?", result.HunterTokens)
			}
			if result.Tier2Tokens > 0 {
				updates["tier2_tokens"] = gorm.Expr("tier2_tokens + ?", result.Tier2Tokens)
			}
			models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Updates(updates)
		}
	}

	return err
}

var (
	engineRegistryMu sync.RWMutex
	legacyRegistry   = map[string]TaskEngine{}
)

// RegisterEngine 注册兼容版引擎实现
func RegisterEngine(mode string, engine TaskEngine) {
	engineRegistryMu.Lock()
	defer engineRegistryMu.Unlock()
	legacyRegistry[mode] = engine
}

// GetEngine 获取引擎实例，优先检查底层 engines 子包并包装适配
func GetEngine(mode string) TaskEngine {
	engineRegistryMu.RLock()
	if e, ok := legacyRegistry[mode]; ok {
		engineRegistryMu.RUnlock()
		return e
	}
	engineRegistryMu.RUnlock()

	modern := engines.GetEngine(mode)
	if modern != nil {
		return &engineAdapter{inner: modern}
	}

	return &engineAdapter{inner: engines.GetEngine("single")}
}
