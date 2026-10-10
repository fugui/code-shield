package plugins

import (
	"fmt"
	"sync"

	"code-shield/models"
	"code-shield/services/coverage"
)

// Decision 枚举门禁评估与轻量复核决策结果
type Decision string

const (
	// DecisionFastPass 极简断言且无业务风险，快速放行，标记 pass（无需后续 LLM 介入）
	DecisionFastPass Decision = "FAST_PASS"
	// DecisionTier0Defect Tier 0 静态直接拦截，确定性缺陷（如空测试桩、恒真断言等）
	DecisionTier0Defect Decision = "DEFECT"
	// DecisionNeedVerify 命中可疑模式或复杂语法，需进一步复核或常规送审
	DecisionNeedVerify Decision = "NEED_VERIFY"
	// DecisionPass 无门禁命中，常规送审
	DecisionPass Decision = "PASS"
)

// StructuralFactHint 结构化事实提示，供轻量复核或主分析模型参考
type StructuralFactHint struct {
	Kind        string `json:"kind"`                  // e.g. "assertion", "helper_call", "context_manager", "outer_try"
	Line        int    `json:"line"`                  // 触发行号
	Expression  string `json:"expression"`            // 代码表达式摘要
	Description string `json:"description,omitempty"` // 事实描述
}

// RadarResult 门禁与探针评估结果
type RadarResult struct {
	Decision   Decision                `json:"decision"`
	Finding    *models.AnalysisFinding `json:"finding,omitempty"`
	Reason     string                  `json:"reason,omitempty"`
	FactHints  []StructuralFactHint    `json:"fact_hints,omitempty"`
	Metadata   map[string]any          `json:"metadata,omitempty"`
}

// PreflightGatePlugin 前置轻量门禁插件（负责执行 Tier 0 静态拦截、Fast Pass 极速放行与争议标记）
type PreflightGatePlugin interface {
	ID() string
	Inspect(codesPath string, unit coverage.PlanUnit, options map[string]any) RadarResult
}

// ContextEnricherPlugin 上下文因果增强插件（负责向分片中注入下游调用点切片与外层特征等）
type ContextEnricherPlugin interface {
	ID() string
	Enrich(codesPath string, target any, options map[string]any) error
}

// ThinVerifierPlugin 轻量快速复核插件（负责执行 Tier 1 Thin LLM 单轮语义仲裁）
type ThinVerifierPlugin interface {
	ID() string
	Verify(codesPath string, unit coverage.PlanUnit, hints []StructuralFactHint, promptTemplate string, timeoutSec float64) (RadarResult, error)
}

// TaskPluginDeclaration 任务元数据中单个插件的声明配置
type TaskPluginDeclaration struct {
	ID             string         `json:"id"`
	Options        map[string]any `json:"options,omitempty"`
	PromptFile     string         `json:"prompt_file,omitempty"`
	TimeoutSeconds float64        `json:"timeout_seconds,omitempty"`
}

// PluginsConfig 任务 meta.json 中的 plugins 配置块
type PluginsConfig struct {
	PreflightGate   *TaskPluginDeclaration `json:"preflight_gate,omitempty"`
	ContextEnricher *TaskPluginDeclaration `json:"context_enricher,omitempty"`
	ThinVerifier    *TaskPluginDeclaration `json:"thin_verifier,omitempty"`
}

// pluginRegistry 通用线程安全插件注册表容器
type pluginRegistry[T any] struct {
	mu      sync.RWMutex
	plugins map[string]T
}

func newPluginRegistry[T any]() *pluginRegistry[T] {
	return &pluginRegistry[T]{
		plugins: make(map[string]T),
	}
}

func (r *pluginRegistry[T]) register(id string, p T) error {
	if id == "" {
		return fmt.Errorf("plugin id cannot be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.plugins[id]; exists {
		return fmt.Errorf("plugin %q is already registered", id)
	}
	r.plugins[id] = p
	return nil
}

func (r *pluginRegistry[T]) get(id string) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, exists := r.plugins[id]
	if !exists {
		var zero T
		return zero, fmt.Errorf("plugin %q is not registered", id)
	}
	return p, nil
}

func (r *pluginRegistry[T]) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins = make(map[string]T)
}

var (
	gateRegistry     = newPluginRegistry[PreflightGatePlugin]()
	enricherRegistry = newPluginRegistry[ContextEnricherPlugin]()
	verifierRegistry = newPluginRegistry[ThinVerifierPlugin]()
)

// RegisterPreflightGate 注册前置门禁插件
func RegisterPreflightGate(gate PreflightGatePlugin) error {
	return gateRegistry.register(gate.ID(), gate)
}

// GetPreflightGate 获取前置门禁插件
func GetPreflightGate(id string) (PreflightGatePlugin, error) {
	return gateRegistry.get(id)
}

// RegisterContextEnricher 注册上下文因果增强插件
func RegisterContextEnricher(enricher ContextEnricherPlugin) error {
	return enricherRegistry.register(enricher.ID(), enricher)
}

// GetContextEnricher 获取上下文因果增强插件
func GetContextEnricher(id string) (ContextEnricherPlugin, error) {
	return enricherRegistry.get(id)
}

// RegisterThinVerifier 注册轻量单轮复核插件
func RegisterThinVerifier(verifier ThinVerifierPlugin) error {
	return verifierRegistry.register(verifier.ID(), verifier)
}

// GetThinVerifier 获取轻量单轮复核插件
func GetThinVerifier(id string) (ThinVerifierPlugin, error) {
	return verifierRegistry.get(id)
}

// ResetRegistriesForTest 清空已注册插件，仅用于单元测试隔离
func ResetRegistriesForTest() {
	gateRegistry.reset()
	enricherRegistry.reset()
	verifierRegistry.reset()
}
