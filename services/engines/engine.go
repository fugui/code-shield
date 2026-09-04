package engines

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
)

// ChunkDetails 记录单个分片的执行明细
type ChunkDetails struct {
	ChunkName       string    `json:"chunk_name"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	Attempts        int       `json:"attempts"`
	Retries         int       `json:"retries"`
	Status          string    `json:"status"` // "success" or "failed"
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// EngineContext 纯内存化的引擎执行上下文，与持久层解耦
type EngineContext struct {
	Ctx               context.Context
	ReportID          uint
	RepoID            uint
	RepoName          string
	TaskTypeID        uint
	TaskTypeName      string
	CodesPath         string
	ReportPath        string
	JSONPath          string
	WorkDir           string
	EngineConfig      json.RawMessage
	RunParams         models.RunParams
	NegativeRules     []string                                                                             // 预加载的免扫/负样本例外规则
	ProgressReport    func(total, processed, success int)                                                  // 进度回调，解耦直接操作 DB
	Invoker           invoker.AIInvoker                                                                    // 算力分配后的调用驱动
	AIExecutor        func(fileList []string, customPromptSuffix, promptFilePath, outputPath string) error // 底层通用 AI 执行器
	AnalysisExecutor  func(fileList []string) ([]models.AnalysisFinding, error)                            // 单片/单仓分析执行器
	SynthesisExecutor func(findings []models.AnalysisFinding) error                                        // 报告综合生成执行器
}

// EngineResult 引擎纯内存计算与扫描输出结果
type EngineResult struct {
	Findings        []models.AnalysisFinding
	DebateLogs      []models.TaskDebateLog // 辩论日志纯内存返回，由 Runner 事务持久化
	SummaryChunks   []ChunkDetails
	HasFailedChunks bool
	HunterTokens    int64
	Tier2Tokens     int64
}

// TaskEngine 定义所有扫描与对抗引擎的标准抽象接口
type TaskEngine interface {
	Name() string
	Run(ctx *EngineContext) (*EngineResult, error)
}

var (
	engineRegistryMu sync.RWMutex
	engineRegistry   = map[string]TaskEngine{}
)

// RegisterEngine 注册执行引擎实现
func RegisterEngine(mode string, engine TaskEngine) {
	engineRegistryMu.Lock()
	defer engineRegistryMu.Unlock()
	engineRegistry[mode] = engine
}

// GetEngine 获取指定名称的执行引擎，缺省回退到 single
func GetEngine(mode string) TaskEngine {
	engineRegistryMu.RLock()
	defer engineRegistryMu.RUnlock()
	if e, ok := engineRegistry[mode]; ok {
		return e
	}
	return engineRegistry["single"]
}
