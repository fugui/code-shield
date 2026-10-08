package models

import (
	"code-common/backend/configutil"
	commonModels "code-common/backend/models"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"net/url"

	"gopkg.in/yaml.v3"
)

type FieldMappingConfig = commonModels.FieldMappingConfig
type OAuth2Config = commonModels.OAuth2Config
type DatabaseConfig = commonModels.DatabaseConfig

// ==============================================================================
// 1. 大模型算力供给层结构体 (LLM Compute Resources)
// ==============================================================================

// ResourceEndpointConfig 原生算力端点配置
type ResourceEndpointConfig struct {
	Name       string `yaml:"name" json:"name"`
	BaseURL    string `yaml:"base_url" json:"base_url"`
	APIKey     string `yaml:"api_key" json:"api_key"`
	Model      string `yaml:"model" json:"model"`
	Concurrent int    `yaml:"concurrent" json:"concurrent"`
}

// ComputeResourceConfig 算力节点实体定义
type ComputeResourceConfig struct {
	ID                      string                   `yaml:"id" json:"id"`
	Driver                  string                   `yaml:"driver" json:"driver"` // agy / opencode / claude / codex / native
	ResponseFormatJSON      bool                     `yaml:"response_format_json" json:"response_format_json"`
	EnableThinking          bool                     `yaml:"enable_thinking" json:"enable_thinking"`
	MaxTokens               int                      `yaml:"max_tokens" json:"max_tokens"`
	MaxRetries              int                      `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMs          int                      `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
	AttemptTimeoutSeconds   int                      `yaml:"attempt_timeout_seconds" json:"attempt_timeout_seconds"`
	FirstByteTimeoutSeconds int                      `yaml:"first_byte_timeout_seconds" json:"first_byte_timeout_seconds"`
	IdleTimeoutSeconds      int                      `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
	MaxOutputBytes          int                      `yaml:"max_output_bytes" json:"max_output_bytes"`
	Endpoints               []ResourceEndpointConfig `yaml:"endpoints" json:"endpoints"`

	legacyBaseURL    string
	legacyAPIKey     string
	legacyModel      string
	legacyConcurrent int
}

func (c *ComputeResourceConfig) UnmarshalYAML(value *yaml.Node) error {
	type computeResourceConfig ComputeResourceConfig
	var raw computeResourceConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*c = ComputeResourceConfig(raw)
	for i := 0; i+1 < len(value.Content); i += 2 {
		switch strings.ToLower(strings.TrimSpace(value.Content[i].Value)) {
		case "base_url":
			c.legacyBaseURL = value.Content[i+1].Value
		case "api_key":
			c.legacyAPIKey = value.Content[i+1].Value
		case "model":
			c.legacyModel = value.Content[i+1].Value
		case "concurrent":
			if err := value.Content[i+1].Decode(&c.legacyConcurrent); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *ComputeResourceConfig) UnmarshalJSON(data []byte) error {
	type computeResourceConfig ComputeResourceConfig
	var raw computeResourceConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = ComputeResourceConfig(raw)
	var legacy struct {
		BaseURL    string `json:"base_url"`
		APIKey     string `json:"api_key"`
		Model      string `json:"model"`
		Concurrent int    `json:"concurrent"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	c.legacyBaseURL = legacy.BaseURL
	c.legacyAPIKey = legacy.APIKey
	c.legacyModel = legacy.Model
	c.legacyConcurrent = legacy.Concurrent
	return nil
}

// MarshalJSON 序列化算力节点时回填旧版顶层连接字段。
// 端点池重构后这些字段只被记录到私有兼容字段，
// 若不在此处回填，/api/admin/config/full 会丢失顶层 concurrent 与 model：
// 配置页“模型名”保存后刷新显示为空，且写回可能冲掉节点模型配置。
func (c ComputeResourceConfig) MarshalJSON() ([]byte, error) {
	type computeResourceConfig ComputeResourceConfig
	type computeResourceJSON struct {
		computeResourceConfig
		BaseURL    string `json:"base_url,omitempty"`
		APIKey     string `json:"api_key,omitempty"`
		Model      string `json:"model,omitempty"`
		Concurrent int    `json:"concurrent"`
	}
	return json.Marshal(computeResourceJSON{
		computeResourceConfig: computeResourceConfig(c),
		BaseURL:               c.legacyBaseURL,
		APIKey:                c.legacyAPIKey,
		Model:                 c.ResourceModel(),
		Concurrent:            c.ResourceConcurrent(),
	})
}

// LLMConfig 大模型算力供给层配置
type LLMConfig struct {
	DefaultResource string                  `yaml:"default_resource" json:"default_resource"`
	DebugLogs       bool                    `yaml:"debug_logs" json:"debug_logs"`
	Resources       []ComputeResourceConfig `yaml:"resources" json:"resources"`
}

// ==============================================================================
// 2. 智能扫描引擎与任务调度结构体 (Scanner & Debate Pipeline Engine)
// ==============================================================================

// WorkHoursConfig 工作时间限流时段配置
type WorkHoursConfig struct {
	Enabled   bool    `yaml:"enabled" json:"enabled"`
	Workdays  []int   `yaml:"workdays" json:"workdays"`     // 1=周一, ..., 5=周五, 6=周六, 7=周日
	StartTime string  `yaml:"start_time" json:"start_time"` // "09:00"
	EndTime   string  `yaml:"end_time" json:"end_time"`     // "22:00"
	Scale     float64 `yaml:"scale" json:"scale"`           // 0.10 代表 10%
}

// ThrottlingConfig 全局流控策略
type ThrottlingConfig struct {
	WorkHours WorkHoursConfig `yaml:"work_hours" json:"work_hours"`
}

// TierBindingConfig 阶梯绑定配置 (支持单一资源或多资源池化绑定)
type TierBindingConfig struct {
	Resource                string              `yaml:"resource" json:"resource"`
	Resources               []string            `yaml:"resources" json:"resources"`
	TimeoutSeconds          int                 `yaml:"timeout_seconds" json:"timeout_seconds"`
	AttemptTimeoutSeconds   int                 `yaml:"attempt_timeout_seconds" json:"attempt_timeout_seconds"`
	FirstByteTimeoutSeconds int                 `yaml:"first_byte_timeout_seconds" json:"first_byte_timeout_seconds"`
	IdleTimeoutSeconds      int                 `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
	MaxOutputBytes          int                 `yaml:"max_output_bytes" json:"max_output_bytes"`
	TimeoutRecovery         string              `yaml:"timeout_recovery" json:"timeout_recovery"`
	MaxSplitDepth           int                 `yaml:"max_split_depth" json:"max_split_depth"`
	Recovery                *TierRecoveryConfig `yaml:"recovery" json:"recovery,omitempty"`
}

// TierRecoveryConfig defines stage-level recovery budgets and error routing.
type TierRecoveryConfig struct {
	MaxTotalAttempts       int      `yaml:"max_total_attempts" json:"max_total_attempts"`
	MaxAttemptsPerResource int      `yaml:"max_attempts_per_resource" json:"max_attempts_per_resource"`
	MaxCandidateFailovers  int      `yaml:"max_candidate_failovers" json:"max_candidate_failovers"`
	RetryBackoffMs         int      `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
	MaxBackoffSeconds      int      `yaml:"max_backoff_seconds" json:"max_backoff_seconds"`
	SplitOn                []string `yaml:"split_on" json:"split_on"`
	RetryOn                []string `yaml:"retry_on" json:"retry_on"`
	CandidateFailoverOn    []string `yaml:"candidate_failover_on" json:"candidate_failover_on"`
	ContractRepairOn       []string `yaml:"contract_repair_on" json:"contract_repair_on"`
}

// GetResources 获取该阶段绑定的所有候选算力节点列表（兼容旧版单选与新版多选）
func (tb *TierBindingConfig) GetResources() []string {
	if len(tb.Resources) > 0 {
		return tb.Resources
	}
	if tb.Resource != "" {
		return []string{tb.Resource}
	}
	return nil
}

func (tb *TierBindingConfig) HasConfig() bool {
	return tb.Resource != "" || len(tb.Resources) > 0
}

// DebateTiersConfig 辩论阶梯流水线配置 (4 层组织映射 4 大角色，向前/后平滑兼容)
type DebateTiersConfig struct {
	Tier1Hunter     TierBindingConfig `yaml:"tier1_hunter" json:"tier1_hunter"`                             // Hunter 初筛角色
	Tier2Challenger TierBindingConfig `yaml:"tier2_challenger,omitempty" json:"tier2_challenger,omitempty"` // Challenger 辩护对抗角色
	Tier3Judge      TierBindingConfig `yaml:"tier3_judge,omitempty" json:"tier3_judge,omitempty"`           // Judge 终审法官角色
	Tier4Synthesis  TierBindingConfig `yaml:"tier4_synthesis,omitempty" json:"tier4_synthesis,omitempty"`   // Synthesis 全仓汇总角色
}

// DebateConfig 辩论流水线流控与阶梯配置
type DebateConfig struct {
	Enabled                    bool              `yaml:"enabled" json:"enabled"`
	FastPassEnabled            bool              `yaml:"fast_pass_enabled" json:"fast_pass_enabled"`
	MaxCandidatesPerChunk      int               `yaml:"max_candidates_per_chunk" json:"max_candidates_per_chunk"`
	StageTimeoutSeconds        int               `yaml:"stage_timeout_seconds" json:"stage_timeout_seconds"`
	LogRetentionDays           int               `yaml:"log_retention_days" json:"log_retention_days"`
	BackpressureThreshold      int               `yaml:"backpressure_threshold" json:"backpressure_threshold"`
	BackpressureTimeoutSeconds int               `yaml:"backpressure_timeout_seconds" json:"backpressure_timeout_seconds"`
	Tiers                      DebateTiersConfig `yaml:"tiers" json:"tiers"`
}

// ToolsConfig 微任务工具路由
type ToolsConfig struct {
	DefaultResource string            `yaml:"default_resource" json:"default_resource"`
	Overrides       map[string]string `yaml:"overrides" json:"overrides"`
}

// AnalysisRetryConfig controls business-level retries around one generic
// analysis chunk/whole-repo invocation. Error classes match
// invoker.ErrorClass values, but models intentionally depends only on strings.
type AnalysisRetryConfig struct {
	MaxRetries        int      `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMs    int      `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
	RetryableErrors   []string `yaml:"retryable_errors" json:"retryable_errors"`
	MaxBackoffSeconds int      `yaml:"max_backoff_seconds" json:"max_backoff_seconds"`
}

// ScannerConfig 智能扫描引擎与任务调度配置
type ScannerConfig struct {
	WorkerCount      int                 `yaml:"worker_count" json:"worker_count"`
	ChunkConcurrency int                 `yaml:"chunk_concurrency" json:"chunk_concurrency"`
	MaxQueueSize     int                 `yaml:"max_queue_size" json:"max_queue_size"`
	MockOnMissingCLI *bool               `yaml:"mock_on_missing_cli" json:"mock_on_missing_cli"`
	Analysis         AnalysisRetryConfig `yaml:"analysis" json:"analysis"`
	Artifact         ArtifactGuardConfig `yaml:"artifact" json:"artifact"`
	Resume           ResumeConfig        `yaml:"resume" json:"resume"`
	Throttling       ThrottlingConfig    `yaml:"throttling" json:"throttling"`
	OpenCode         OpenCodeConfig      `yaml:"opencode" json:"opencode"`
	Debate           DebateConfig        `yaml:"debate" json:"debate"`
	Tools            ToolsConfig         `yaml:"tools" json:"tools"`
	Determinism      DeterminismConfig   `yaml:"determinism" json:"determinism"`
}

// ResumeConfig controls backward-compatible interpretation of legacy scan
// checkpoints. The default is deliberately conservative.
type ResumeConfig struct {
	// LegacyV2Policy controls bundle checkpoints written by the previous
	// schema version. "reject" replays them; "accept_success_as_complete" may
	// only be enabled when callers know those checkpoints predate candidate
	// salvage.
	LegacyV2Policy string `yaml:"legacy_v2_policy" json:"legacy_v2_policy"`
}

func (c Config) AcceptLegacyBundleV2Checkpoints() bool {
	return strings.EqualFold(strings.TrimSpace(c.Scanner.Resume.LegacyV2Policy), "accept_success_as_complete")
}

// ArtifactGuardConfig controls deterministic normalization and bounded recovery
// for AI scanner artifacts.
type ArtifactGuardConfig struct {
	NormalizationEnabled            *bool   `yaml:"normalization_enabled" json:"normalization_enabled"`
	MaxSchemaRepairAttempts         int     `yaml:"max_schema_repair_attempts" json:"max_schema_repair_attempts"`
	SchemaRepairTimeoutSeconds      int     `yaml:"schema_repair_timeout_seconds" json:"schema_repair_timeout_seconds"`
	SchemaRepairResource            string  `yaml:"schema_repair_resource" json:"schema_repair_resource"`
	AssessmentContractRepairEnabled *bool   `yaml:"assessment_contract_repair_enabled" json:"assessment_contract_repair_enabled"`
	AssessmentJSONSchemaMode        string  `yaml:"assessment_json_schema_mode" json:"assessment_json_schema_mode"`
	AllowCandidateSalvage           *bool   `yaml:"allow_candidate_salvage" json:"allow_candidate_salvage"`
	MaxQuarantinedCandidateRatio    float64 `yaml:"max_quarantined_candidate_ratio" json:"max_quarantined_candidate_ratio"`
	CategoryRepairEnabled           *bool   `yaml:"category_repair_enabled" json:"category_repair_enabled"`
	CategoryRepairResource          string  `yaml:"category_repair_resource" json:"category_repair_resource"`
	CategoryRepairTimeoutSeconds    int     `yaml:"category_repair_timeout_seconds" json:"category_repair_timeout_seconds"`
	CategoryRepairMaxCandidates     int     `yaml:"category_repair_max_candidates" json:"category_repair_max_candidates"`
}

func (c Config) ArtifactGuardEnabled() bool {
	return c.Scanner.Artifact.NormalizationEnabled == nil || *c.Scanner.Artifact.NormalizationEnabled
}

func (c Config) MaxSchemaRepairAttempts() int {
	if value := c.Scanner.Artifact.MaxSchemaRepairAttempts; value > 0 {
		return value
	}
	return 2
}

func (c Config) SchemaRepairTimeoutSeconds() int {
	if value := c.Scanner.Artifact.SchemaRepairTimeoutSeconds; value > 0 {
		return value
	}
	return 600
}

func (c Config) SchemaRepairResource() string {
	if value := strings.TrimSpace(c.Scanner.Artifact.SchemaRepairResource); value != "" {
		return value
	}
	if value := strings.TrimSpace(c.AI.ToolBackends.RepairJSON); value != "" {
		return value
	}
	return "native"
}

func (c Config) AssessmentContractRepairEnabled() bool {
	return c.Scanner.Artifact.AssessmentContractRepairEnabled != nil && *c.Scanner.Artifact.AssessmentContractRepairEnabled
}

func (c Config) AssessmentJSONSchemaMode() string {
	value := strings.ToLower(strings.TrimSpace(c.Scanner.Artifact.AssessmentJSONSchemaMode))
	switch value {
	case "json_object", "auto", "strict":
		return value
	default:
		return "off"
	}
}

func (c Config) AllowCandidateSalvage() bool {
	return c.Scanner.Artifact.AllowCandidateSalvage == nil || *c.Scanner.Artifact.AllowCandidateSalvage
}

func (c Config) MaxQuarantinedCandidateRatio() float64 {
	if value := c.Scanner.Artifact.MaxQuarantinedCandidateRatio; value > 0 {
		return value
	}
	return 0.5
}

func (c Config) CategoryRepairEnabled() bool {
	return c.Scanner.Artifact.CategoryRepairEnabled == nil || *c.Scanner.Artifact.CategoryRepairEnabled
}

func (c Config) CategoryRepairResource() string {
	if value := strings.TrimSpace(c.Scanner.Artifact.CategoryRepairResource); value != "" {
		return value
	}
	return c.SchemaRepairResource()
}

func (c Config) CategoryRepairTimeoutSeconds() int {
	if value := c.Scanner.Artifact.CategoryRepairTimeoutSeconds; value > 0 {
		return value
	}
	return 60
}

func (c Config) CategoryRepairMaxCandidates() int {
	if value := c.Scanner.Artifact.CategoryRepairMaxCandidates; value > 0 {
		return value
	}
	return 20
}

// GetChunkConcurrency 返回单个扫描任务内的语义分片并发数。
func (c Config) GetChunkConcurrency() int {
	if c.Scanner.ChunkConcurrency > 0 {
		return c.Scanner.ChunkConcurrency
	}
	return 6
}

// NormalizeDefaults 填充历史动态配置缺失的调度与阶梯超时字段。
func (s *ScannerConfig) NormalizeDefaults() {
	if s.ChunkConcurrency <= 0 {
		s.ChunkConcurrency = 6
	}
	for key, tier := range map[string]*TierBindingConfig{
		"tier1_hunter":     &s.Debate.Tiers.Tier1Hunter,
		"tier2_challenger": &s.Debate.Tiers.Tier2Challenger,
		"tier3_judge":      &s.Debate.Tiers.Tier3Judge,
		"tier4_synthesis":  &s.Debate.Tiers.Tier4Synthesis,
	} {
		if tier.TimeoutSeconds <= 0 {
			tier.TimeoutSeconds = 1800
		}
		if tier.AttemptTimeoutSeconds <= 0 {
			tier.AttemptTimeoutSeconds = 900
		}
		if tier.Recovery == nil {
			tier.Recovery = &TierRecoveryConfig{
				MaxTotalAttempts:       2,
				MaxAttemptsPerResource: 2,
				MaxCandidateFailovers:  0,
			}
		}
		if key == "tier1_hunter" && tier.IdleTimeoutSeconds <= 0 {
			tier.IdleTimeoutSeconds = 600
		}
		if key == "tier2_challenger" || key == "tier4_synthesis" {
			if tier.FirstByteTimeoutSeconds <= 0 {
				tier.FirstByteTimeoutSeconds = 180
			}
			if tier.IdleTimeoutSeconds <= 0 {
				tier.IdleTimeoutSeconds = 300
			}
		}
	}
	if s.Analysis.MaxRetries <= 0 {
		s.Analysis.MaxRetries = 3
	}
	if s.Analysis.RetryBackoffMs <= 0 {
		s.Analysis.RetryBackoffMs = 2000
	}
	if s.Analysis.MaxBackoffSeconds <= 0 {
		s.Analysis.MaxBackoffSeconds = 30
	}
	if len(s.Analysis.RetryableErrors) == 0 {
		s.Analysis.RetryableErrors = []string{"rate_limited", "network_transient", "unknown"}
	}
	if s.Artifact.MaxSchemaRepairAttempts <= 0 {
		s.Artifact.MaxSchemaRepairAttempts = 2
	}
	if s.Artifact.SchemaRepairTimeoutSeconds <= 0 {
		s.Artifact.SchemaRepairTimeoutSeconds = 600
	}
	if s.Artifact.SchemaRepairResource == "" {
		s.Artifact.SchemaRepairResource = "native"
	}
	if s.Artifact.CategoryRepairResource == "" {
		s.Artifact.CategoryRepairResource = s.Artifact.SchemaRepairResource
	}
	if s.Artifact.MaxQuarantinedCandidateRatio <= 0 {
		s.Artifact.MaxQuarantinedCandidateRatio = 0.5
	}
	if s.Artifact.CategoryRepairTimeoutSeconds <= 0 {
		s.Artifact.CategoryRepairTimeoutSeconds = 60
	}
	if s.Artifact.CategoryRepairMaxCandidates <= 0 {
		s.Artifact.CategoryRepairMaxCandidates = 20
	}
	if s.Resume.LegacyV2Policy == "" {
		s.Resume.LegacyV2Policy = "reject"
	}
}

// ValidateTierBindings 校验各阶梯绑定与流水线架构强制的引擎类型一致。
func ValidateTierBindings(llm *LLMConfig, scanner *ScannerConfig) error {
	tiers := map[string]struct {
		Binding      *TierBindingConfig
		AllowedThick bool
		RoleTitle    string
	}{
		"tier1_hunter":     {Binding: &scanner.Debate.Tiers.Tier1Hunter, AllowedThick: true, RoleTitle: "Tier 1 Hunter"},
		"tier2_challenger": {Binding: &scanner.Debate.Tiers.Tier2Challenger, AllowedThick: false, RoleTitle: "Tier 2 Challenger"},
		"tier3_judge":      {Binding: &scanner.Debate.Tiers.Tier3Judge, AllowedThick: true, RoleTitle: "Tier 3 Judge"},
		"tier4_synthesis":  {Binding: &scanner.Debate.Tiers.Tier4Synthesis, AllowedThick: false, RoleTitle: "Tier 4 Synthesis"},
	}

	for _, tier := range tiers {
		resources := tier.Binding.GetResources()
		if len(resources) == 0 {
			continue
		}
		for _, resourceID := range resources {
			resource := llm.FindResource(resourceID)
			if resource == nil {
				return fmt.Errorf("%s 绑定的算力节点 %q 不存在", tier.RoleTitle, resourceID)
			}
			isNative := resource.Driver == "native"
			if isNative == tier.AllowedThick {
				engineType := "Thick Agent"
				if isNative {
					engineType = "Native LLM"
				}
				return fmt.Errorf("%s 仅允许 %s，禁止绑定 %q", tier.RoleTitle, engineType, resourceID)
			}
		}
		if tier.Binding.AttemptTimeoutSeconds > tier.Binding.TimeoutSeconds {
			return fmt.Errorf("%s 的单次 AI 调用超时不能大于阶段总预算", tier.RoleTitle)
		}
	}
	return nil
}

// OpenCodeConfig contains driver-specific thick-agent recovery policy.
type OpenCodeConfig struct {
	Continuation OpenCodeContinuationConfig `yaml:"continuation" json:"continuation"`
}

// OpenCodeContinuationConfig controls one in-session recovery attempt.
type OpenCodeContinuationConfig struct {
	// Enabled defaults to true. A pointer is used so an explicit false can
	// disable the feature without accidentally enabling it during config load.
	Enabled    *bool `yaml:"enabled" json:"enabled"`
	MaxSeconds int   `yaml:"max_seconds" json:"max_seconds"`
}

// OpenCodeContinuationEnabled reports whether session continuation is enabled.
func (c Config) OpenCodeContinuationEnabled() bool {
	return c.Scanner.OpenCode.Continuation.Enabled == nil || *c.Scanner.OpenCode.Continuation.Enabled
}

// OpenCodeContinuationMaxSeconds returns the bounded retry budget.
func (c Config) OpenCodeContinuationMaxSeconds() int {
	if c.Scanner.OpenCode.Continuation.MaxSeconds <= 0 {
		return 600
	}
	return c.Scanner.OpenCode.Continuation.MaxSeconds
}

// GetAnalysisRetryConfig returns effective retry settings. The default base
// produces 2s/4s/8s exponential backoff and preserves three business retries.
func (c Config) GetAnalysisRetryConfig() AnalysisRetryConfig {
	config := c.Scanner.Analysis
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.RetryBackoffMs <= 0 {
		config.RetryBackoffMs = 2000
	}
	if config.MaxBackoffSeconds <= 0 {
		config.MaxBackoffSeconds = 30
	}
	if len(config.RetryableErrors) == 0 {
		config.RetryableErrors = []string{"rate_limited", "network_transient", "unknown"}
	}
	return config
}

type DeterminismConfig struct {
	Enabled          bool    `yaml:"enabled" json:"enabled"`
	Temperature      float64 `yaml:"temperature" json:"temperature"`
	BindModelPerTask bool    `yaml:"bind_model_per_task" json:"bind_model_per_task"`
	PreferSeed       bool    `yaml:"prefer_seed" json:"prefer_seed"`
	SeedPolicy       string  `yaml:"seed_policy" json:"seed_policy"`
}

// DeterministicTemperature returns an optional request-level override.
func (c Config) DeterministicTemperature() *float64 {
	if !c.Scanner.Determinism.Enabled {
		return nil
	}
	value := c.Scanner.Determinism.Temperature
	return &value
}

// ==============================================================================
// 3. 企业治理与通知配置 (Governance & Notification)
// ==============================================================================

// GovernancePolicyConfig 企业治理策略定义 (升级自原 GovernanceSystemConfig)
type GovernancePolicyConfig struct {
	Identity                      IdentityConfig        `yaml:"identity" json:"identity"`
	Arbitration                   ArbitrationConfig     `yaml:"arbitration" json:"arbitration"`
	Lifecycle                     LifecyclePolicyConfig `yaml:"lifecycle" json:"lifecycle"`
	CategoryRegressionBatchSize   int                   `yaml:"category_regression_batch_size" json:"category_regression_batch_size"`
	CategoryRegressionMaxFailures int                   `yaml:"category_regression_max_failures" json:"category_regression_max_failures"`
}

type IdentityConfig struct {
	AlgorithmVersion            string  `yaml:"algorithm_version" json:"algorithm_version"`
	MaxCandidatesPerObservation int     `yaml:"max_candidates_per_observation" json:"max_candidates_per_observation"`
	MaxCandidateEdgesPerReport  int     `yaml:"max_candidate_edges_per_report" json:"max_candidate_edges_per_report"`
	MaxAIAssignmentWidth        int     `yaml:"max_assignment_width" json:"max_assignment_width"`
	StrongSameThreshold         float64 `yaml:"strong_same_threshold" json:"strong_same_threshold"`
	AssignBand                  float64 `yaml:"assign_band" json:"assign_band"`
	RejectBelow                 float64 `yaml:"reject_below" json:"reject_below"`
	AutoResolveGrayZone         *bool   `yaml:"auto_resolve_gray_zone" json:"auto_resolve_gray_zone"`
	AIArbitrationConfidence     float64 `yaml:"ai_arbitration_confidence" json:"ai_arbitration_confidence"`
	GrayZoneFallbackMergeScore  float64 `yaml:"gray_zone_fallback_merge_score" json:"gray_zone_fallback_merge_score"`
}

type ArbitrationConfig struct {
	Enabled           *bool `yaml:"enabled" json:"enabled"`
	MaxCallsPerReport int   `yaml:"max_calls_per_report" json:"max_calls_per_report"`
	ContextLines      int   `yaml:"context_lines" json:"context_lines"`
	TimeoutSeconds    int   `yaml:"timeout_seconds" json:"timeout_seconds"`
}

func (c Config) ArbitrationEnabled() bool {
	return c.Arbitration.Enabled == nil || *c.Arbitration.Enabled
}

func (c Config) GrayZoneAutoResolveEnabled() bool {
	return c.Identity.AutoResolveGrayZone == nil || *c.Identity.AutoResolveGrayZone
}

type LifecyclePolicyConfig struct {
	HighRiskSeverities   []string `yaml:"high_risk_severities" json:"high_risk_severities"`
	ResolvedRounds       int      `yaml:"resolved_rounds" json:"resolved_rounds"`
	DormantThreshold     int      `yaml:"dormant_rounds" json:"dormant_rounds"`
	ObsoleteAfterDormant int      `yaml:"obsolete_after_dormant_rounds" json:"obsolete_after_dormant_rounds"`
	RequireCoverage      bool     `yaml:"require_coverage" json:"require_coverage"`
	RequireChange        bool     `yaml:"require_change" json:"require_change"`
}

// NotificationConfig 通知配置
type NotificationConfig struct {
	Webhook string `yaml:"webhook" json:"webhook"`
}

// RetentionConfig 台账快照生命周期与稀疏存储配置（08号设计 §6.1）
type RetentionConfig struct {
	// 缺陷台账历史快照保留天数（默认 30 天）
	LedgerRetentionDays int `yaml:"ledger_retention_days" json:"ledger_retention_days"`
	// 同一代码仓保留的最新完整扫描快照数（默认 10 轮，防止低频扫描仓基线丢失）
	MaxRetainedScansPerRepo int `yaml:"max_retained_scans_per_repo" json:"max_retained_scans_per_repo"`
	// 覆盖明细是否启用稀疏存储（默认 true，推荐开启）
	EnableSparseScope bool `yaml:"enable_sparse_scope" json:"enable_sparse_scope"`
	// 覆盖清单全量压缩产物存储目录（为空时跟随 report artifacts）
	ScopeManifestDir string `yaml:"scope_manifest_dir" json:"scope_manifest_dir"`
}

// NormalizeRetentionDefaults 填充保留策略默认值
func (c *RetentionConfig) NormalizeRetentionDefaults() {
	if c.LedgerRetentionDays <= 0 {
		c.LedgerRetentionDays = 30
	}
	if c.MaxRetainedScansPerRepo <= 0 {
		c.MaxRetainedScansPerRepo = 10
	}
	if !c.EnableSparseScope {
		// 若 YAML 中未显式设置，默认开启稀疏存储
		c.EnableSparseScope = true
	}
}

// AIFixConfig contains the cross-system integration with Shield-Fix.
type AIFixConfig struct {
	FixURL        string `yaml:"fix_url" json:"-"`
	ContextToken  string `yaml:"context_token" json:"-"`
	ReportBaseURL string `yaml:"report_base_url" json:"-"`
}

// Validate checks only operator-facing URL configuration; the token is not
// included in validation errors.
func (c *AIFixConfig) Validate() error {
	if strings.TrimSpace(c.FixURL) == "" {
		return errors.New("fix_url is required")
	}
	parsed, err := url.Parse(c.FixURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Fragment != "" {
		return errors.New("fix_url must be an absolute HTTPS URL")
	}
	if strings.Count(c.FixURL, "{defect_id}") != 1 {
		return errors.New("fix_url must contain exactly one {defect_id} placeholder")
	}
	return nil
}

// ServerConfig HTTP 服务配置
type ServerConfig struct {
	Port              string        `yaml:"port" json:"port"`
	DataDir           string        `yaml:"data_dir" json:"data_dir"`
	GinLog            bool          `yaml:"gin_log" json:"gin_log"`
	ReadTimeout       time.Duration `yaml:"read_timeout" json:"read_timeout"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout" json:"read_header_timeout"`
	WriteTimeout      time.Duration `yaml:"write_timeout" json:"write_timeout"`
	IdleTimeout       time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	MaxHeaderBytes    int           `yaml:"max_header_bytes" json:"max_header_bytes"`
	WorkerCount       int           `yaml:"worker_count" json:"worker_count"`
	MaxQueueSize      int           `yaml:"max_queue_size" json:"max_queue_size"`
	ExternalURL       string        `yaml:"external_url" json:"external_url"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	StandaloneMode       bool         `yaml:"standalone_mode" json:"standalone_mode"`
	JWTSecret            string       `yaml:"jwt_secret" json:"jwt_secret"`
	PasswordLoginEnabled bool         `yaml:"password_login_enabled" json:"password_login_enabled"`
	OAuth2               OAuth2Config `yaml:"oauth2" json:"oauth2"`
}

// ==============================================================================
// 4. 旧版结构体兼容别名 (Backward-Compatibility Types)
// ==============================================================================

type ModelConfig struct {
	OpenCode   string `yaml:"opencode"`
	Claude     string `yaml:"claude"`
	Codex      string `yaml:"codex"`
	Agy        string `yaml:"agy"`
	Native     string `yaml:"native"`
	Concurrent int    `yaml:"concurrent"`
}

type WorkHoursThrottleConfig = WorkHoursConfig

type TierConfig struct {
	Backend                 string             `yaml:"backend" json:"backend"`
	Model                   string             `yaml:"model" json:"model"`
	Temperature             float64            `yaml:"temperature" json:"temperature"`
	Concurrent              int                `yaml:"concurrent" json:"concurrent"`
	TimeoutSeconds          int                `yaml:"timeout_seconds" json:"timeout_seconds"`
	AttemptTimeoutSeconds   int                `yaml:"attempt_timeout_seconds" json:"attempt_timeout_seconds"`
	FirstByteTimeoutSeconds int                `yaml:"first_byte_timeout_seconds" json:"first_byte_timeout_seconds"`
	IdleTimeoutSeconds      int                `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
	MaxOutputBytes          int                `yaml:"max_output_bytes" json:"max_output_bytes"`
	TimeoutRecovery         string             `yaml:"timeout_recovery" json:"timeout_recovery"`
	MaxSplitDepth           int                `yaml:"max_split_depth" json:"max_split_depth"`
	Recovery                TierRecoveryConfig `yaml:"recovery" json:"recovery"`
	ResourceID              string             `yaml:"resource_id" json:"resource_id"`
}

type DebateFlowConfig = DebateConfig

type GovernanceSystemConfig struct {
	ScopeGuardEnabled  bool `yaml:"scope_guard_enabled" json:"scope_guard_enabled"`
	AutoResolveMissing bool `yaml:"auto_resolve_missing" json:"auto_resolve_missing"`
	FeedbackInjection  bool `yaml:"feedback_injection" json:"feedback_injection"`
	DiffGateStrict     bool `yaml:"diff_gate_strict" json:"diff_gate_strict"`
}

type NativeEndpointConfig = ResourceEndpointConfig

// NativeLLMConfig is deprecated; endpoints are managed on native compute resources.
type NativeLLMConfig struct {
	MaxTokens               int                    `yaml:"max_tokens" json:"max_tokens"`
	ResponseFormatJSON      bool                   `yaml:"response_format_json" json:"response_format_json"`
	EnableThinking          bool                   `yaml:"enable_thinking" json:"enable_thinking"`
	MaxRetries              int                    `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMs          int                    `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
	AttemptTimeoutSeconds   int                    `yaml:"attempt_timeout_seconds" json:"attempt_timeout_seconds"`
	FirstByteTimeoutSeconds int                    `yaml:"first_byte_timeout_seconds" json:"first_byte_timeout_seconds"`
	IdleTimeoutSeconds      int                    `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
	MaxOutputBytes          int                    `yaml:"max_output_bytes" json:"max_output_bytes"`
	Endpoints               []NativeEndpointConfig `yaml:"endpoints" json:"endpoints"`

	legacyEndpoint   string
	legacyBaseURL    string
	legacyAPIKey     string
	legacyModelField string
}

func (n *NativeLLMConfig) UnmarshalYAML(value *yaml.Node) error {
	type nativeLLMConfig NativeLLMConfig
	var raw nativeLLMConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*n = NativeLLMConfig(raw)
	for i := 0; i+1 < len(value.Content); i += 2 {
		switch strings.ToLower(strings.TrimSpace(value.Content[i].Value)) {
		case "endpoint":
			n.legacyEndpoint = value.Content[i+1].Value
		case "base_url":
			n.legacyBaseURL = value.Content[i+1].Value
		case "api_key":
			n.legacyAPIKey = value.Content[i+1].Value
		case "default_model":
			n.legacyModelField = value.Content[i+1].Value
		}
	}
	return nil
}

func (n *NativeLLMConfig) UnmarshalJSON(data []byte) error {
	type nativeLLMConfig NativeLLMConfig
	var raw nativeLLMConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*n = NativeLLMConfig(raw)
	var legacy struct {
		Endpoint     string `json:"endpoint"`
		BaseURL      string `json:"base_url"`
		APIKey       string `json:"api_key"`
		DefaultModel string `json:"default_model"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	n.legacyEndpoint = legacy.Endpoint
	n.legacyBaseURL = legacy.BaseURL
	n.legacyAPIKey = legacy.APIKey
	n.legacyModelField = legacy.DefaultModel
	return nil
}

type ToolBackendsConfig struct {
	RepairJSON         string `yaml:"repair_json" json:"repair_json"`
	FindingMatch       string `yaml:"finding_match" json:"finding_match"`
	FeedbackExtraction string `yaml:"feedback_extraction" json:"feedback_extraction"`
}

// ==============================================================================
// 5. 全局配置聚合单例 (Config)
// ==============================================================================

type Config struct {
	Server  ServerConfig `yaml:"server" json:"server"`
	Storage struct {
		Root string `yaml:"root" json:"root"`
	} `yaml:"storage" json:"storage"`
	Database     DatabaseConfig         `yaml:"database" json:"database"`
	Auth         AuthConfig             `yaml:"auth" json:"auth"`
	LLM          LLMConfig              `yaml:"llm" json:"llm"`
	Scanner      ScannerConfig          `yaml:"scanner" json:"scanner"`
	Governance   GovernancePolicyConfig `yaml:"governance" json:"governance"`
	Identity     IdentityConfig         `yaml:"identity" json:"identity"`
	Arbitration  ArbitrationConfig      `yaml:"arbitration" json:"arbitration"`
	Lifecycle    LifecyclePolicyConfig  `yaml:"lifecycle" json:"lifecycle"`
	Notification NotificationConfig     `yaml:"notification" json:"notification"`
	AIFix        AIFixConfig            `yaml:"ai_fix" json:"-"`
	Retention    RetentionConfig        `yaml:"retention" json:"retention"`

	// 兼容旧版 config.yaml 的 AI 块与影子镜像
	AI struct {
		Backend           string                  `yaml:"backend"`
		DebugLogs         bool                    `yaml:"debug_logs"`
		OutputFormat      string                  `yaml:"output_format"`
		MockOnMissingCLI  *bool                   `yaml:"mock_on_missing_cli"`
		WorkHoursThrottle WorkHoursThrottleConfig `yaml:"work_hours_throttle"`
		Models            []ModelConfig           `yaml:"models"`
		Native            NativeLLMConfig         `yaml:"native" json:"native"`
		ToolBackends      ToolBackendsConfig      `yaml:"tool_backends" json:"tool_backends"`
		Debate            DebateFlowConfig        `yaml:"debate" json:"debate"`
	} `yaml:"ai"`
}

var AppConfig Config

// SyncLegacy 保持新顶层结构与旧 AI 影子镜像的双向同步
func (c *Config) SyncLegacy() {
	// 1. 若新版 LLM 为空但旧版 AI 有配置，从旧版生成新版
	if len(c.LLM.Resources) == 0 && (c.AI.Backend != "" || len(c.AI.Models) > 0 || hasLegacyNativeConnection(&c.AI.Native) || len(c.AI.Native.Endpoints) > 0) {
		c.LLM.DefaultResource = "native"
		c.LLM.DebugLogs = c.AI.DebugLogs

		// 填充 native 资源节点
		nativeRes := ComputeResourceConfig{
			ID:                      "native",
			Driver:                  "native",
			ResponseFormatJSON:      c.AI.Native.ResponseFormatJSON,
			EnableThinking:          c.AI.Native.EnableThinking,
			MaxRetries:              c.AI.Native.MaxRetries,
			RetryBackoffMs:          c.AI.Native.RetryBackoffMs,
			MaxTokens:               c.AI.Native.MaxTokens,
			AttemptTimeoutSeconds:   c.AI.Native.AttemptTimeoutSeconds,
			FirstByteTimeoutSeconds: c.AI.Native.FirstByteTimeoutSeconds,
			IdleTimeoutSeconds:      c.AI.Native.IdleTimeoutSeconds,
			MaxOutputBytes:          c.AI.Native.MaxOutputBytes,
		}
		if len(c.AI.Native.Endpoints) > 0 {
			for _, ep := range c.AI.Native.Endpoints {
				nativeRes.Endpoints = append(nativeRes.Endpoints, ResourceEndpointConfig{
					Name:       ep.Name,
					BaseURL:    ep.BaseURL,
					APIKey:     ep.APIKey,
					Model:      ep.Model,
					Concurrent: ep.Concurrent,
				})
			}
		}
		c.LLM.Resources = append(c.LLM.Resources, nativeRes)

		// 填充 models 资源
		for i, m := range c.AI.Models {
			id := "model-" + string(rune('1'+i))
			driver := "opencode"
			modelName := m.OpenCode
			if m.Claude != "" {
				driver = "claude"
				modelName = m.Claude
			} else if m.Agy != "" {
				driver = "agy"
				modelName = m.Agy
			} else if m.Codex != "" {
				driver = "codex"
				modelName = m.Codex
			}
			c.LLM.Resources = append(c.LLM.Resources, ComputeResourceConfig{
				ID:     id,
				Driver: driver,
			})
			c.LLM.Resources[len(c.LLM.Resources)-1].legacyModel = modelName
			c.LLM.Resources[len(c.LLM.Resources)-1].legacyConcurrent = m.Concurrent
		}
	}

	// 2. 若 Scanner 为空，从 Server 与 AI 填充
	if c.Scanner.WorkerCount == 0 && c.Server.WorkerCount > 0 {
		c.Scanner.WorkerCount = c.Server.WorkerCount
	}
	if c.Scanner.MaxQueueSize == 0 && c.Server.MaxQueueSize > 0 {
		c.Scanner.MaxQueueSize = c.Server.MaxQueueSize
	}
	if c.Scanner.MockOnMissingCLI == nil && c.AI.MockOnMissingCLI != nil {
		c.Scanner.MockOnMissingCLI = c.AI.MockOnMissingCLI
	}
	if !c.Scanner.Throttling.WorkHours.Enabled && c.AI.WorkHoursThrottle.Enabled {
		c.Scanner.Throttling.WorkHours = c.AI.WorkHoursThrottle
	}
	if !c.Scanner.Debate.Enabled && c.AI.Debate.Enabled {
		c.Scanner.Debate = c.AI.Debate
	}
	c.Scanner.NormalizeDefaults()
	if c.Scanner.Tools.DefaultResource == "" {
		c.Scanner.Tools.DefaultResource = "native"
		c.Scanner.Tools.Overrides = map[string]string{
			"repair_json":         c.AI.ToolBackends.RepairJSON,
			"finding_match":       c.AI.ToolBackends.FindingMatch,
			"feedback_extraction": c.AI.ToolBackends.FeedbackExtraction,
		}
	}

	// 3. 将新版状态同步回旧版字段（保证旧业务逻辑不报错）
	if c.Scanner.WorkerCount > 0 {
		c.Server.WorkerCount = c.Scanner.WorkerCount
	}
	if c.Scanner.MaxQueueSize > 0 {
		c.Server.MaxQueueSize = c.Scanner.MaxQueueSize
	}
	c.AI.MockOnMissingCLI = c.Scanner.MockOnMissingCLI
	c.AI.WorkHoursThrottle = c.Scanner.Throttling.WorkHours
	c.AI.DebugLogs = c.LLM.DebugLogs
	c.AI.Debate = c.Scanner.Debate

	c.NormalizeNativeEndpoints()

	// 寻找 native 节点同步给 AI.Native
	for _, res := range c.LLM.Resources {
		if res.ID == "native" || res.Driver == "native" {
			c.AI.Native.legacyBaseURL = ""
			c.AI.Native.legacyEndpoint = ""
			c.AI.Native.legacyAPIKey = ""
			c.AI.Native.legacyModelField = ""
			c.AI.Native.ResponseFormatJSON = res.ResponseFormatJSON
			c.AI.Native.EnableThinking = res.EnableThinking
			c.AI.Native.MaxTokens = res.MaxTokens
			c.AI.Native.MaxRetries = res.MaxRetries
			c.AI.Native.RetryBackoffMs = res.RetryBackoffMs
			c.AI.Native.AttemptTimeoutSeconds = res.AttemptTimeoutSeconds
			c.AI.Native.FirstByteTimeoutSeconds = res.FirstByteTimeoutSeconds
			c.AI.Native.IdleTimeoutSeconds = res.IdleTimeoutSeconds
			c.AI.Native.MaxOutputBytes = res.MaxOutputBytes
			c.AI.Native.Endpoints = nil
			for _, ep := range res.Endpoints {
				c.AI.Native.Endpoints = append(c.AI.Native.Endpoints, ep)
			}
			break
		}
	}
	if c.AI.Backend == "" {
		if c.LLM.DefaultResource != "" {
			c.AI.Backend = c.LLM.DefaultResource
		} else {
			c.AI.Backend = "claude"
		}
	}
}

// FindResource 根据 ID 或 Driver 查找匹配的算力资源配置
func (c *Config) FindResource(id string) *ComputeResourceConfig {
	return c.LLM.FindResource(id)
}

// NormalizeNativeEndpoints expands legacy native shorthand into endpoint pools.
func (c *Config) NormalizeNativeEndpoints() {
	for i := range c.LLM.Resources {
		resource := &c.LLM.Resources[i]
		if resource.Driver != "native" || len(resource.Endpoints) > 0 {
			continue
		}

		baseURL := resource.legacyBaseURL
		apiKey := resource.legacyAPIKey
		model := resource.legacyModel
		concurrent := resource.legacyConcurrent
		if baseURL == "" && apiKey == "" && model == "" && !hasLegacyNativeConnection(&c.AI.Native) {
			continue
		}
		if baseURL == "" {
			baseURL = c.AI.Native.legacyBaseURL
		}
		if baseURL == "" {
			baseURL = c.AI.Native.legacyEndpoint
		}
		if apiKey == "" {
			apiKey = c.AI.Native.legacyAPIKey
		}
		if model == "" {
			model = c.AI.Native.legacyModelField
		}
		if model == "" {
			model = "glm-4-flash"
		}
		if concurrent <= 0 {
			concurrent = 20
		}
		resource.Endpoints = []ResourceEndpointConfig{{
			Name:       "default",
			BaseURL:    baseURL,
			APIKey:     apiKey,
			Model:      model,
			Concurrent: concurrent,
		}}
	}
}

func hasLegacyNativeConnection(cfg *NativeLLMConfig) bool {
	return cfg.legacyEndpoint != "" || cfg.legacyBaseURL != "" || cfg.legacyAPIKey != "" || cfg.legacyModelField != ""
}

func (c ComputeResourceConfig) ResourceModel() string {
	if len(c.Endpoints) == 1 {
		return c.Endpoints[0].Model
	}
	if len(c.Endpoints) > 1 {
		return ""
	}
	if c.legacyModel != "" {
		return c.legacyModel
	}
	return ""
}

func (c ComputeResourceConfig) ResourceConcurrent() int {
	if c.Driver == "native" {
		concurrent := 0
		for _, endpoint := range c.Endpoints {
			concurrent += endpoint.Concurrent
		}
		return concurrent
	}
	if c.legacyConcurrent > 0 {
		return c.legacyConcurrent
	}
	if len(c.Endpoints) > 0 {
		concurrent := 0
		for _, endpoint := range c.Endpoints {
			concurrent += endpoint.Concurrent
		}
		if concurrent > 0 {
			return concurrent
		}
	}
	return 5
}

// FindResource 根据 ID 或 Driver 查找匹配的算力资源配置。
func (l *LLMConfig) FindResource(id string) *ComputeResourceConfig {
	for i := range l.Resources {
		if l.Resources[i].ID == id || l.Resources[i].Driver == id {
			return &l.Resources[i]
		}
	}
	return nil
}

// GetTierResources 获取指定阶段阶梯配置的候选资源列表
func (c *Config) GetTierResources(tier string) []string {
	var binding TierBindingConfig
	switch tier {
	case "tier1_hunter":
		binding = c.Scanner.Debate.Tiers.Tier1Hunter
	case "tier2_challenger":
		binding = c.Scanner.Debate.Tiers.Tier2Challenger
	case "tier3_judge":
		binding = c.Scanner.Debate.Tiers.Tier3Judge
	case "tier4_synthesis":
		binding = c.Scanner.Debate.Tiers.Tier4Synthesis
	}
	return binding.GetResources()
}

// GetTierConfig 获取指定 Tier 的配置
func (c *Config) GetTierConfig(tier string) TierConfig {
	var binding TierBindingConfig
	switch tier {
	case "tier1_hunter":
		binding = c.Scanner.Debate.Tiers.Tier1Hunter
	case "tier2_challenger":
		binding = c.Scanner.Debate.Tiers.Tier2Challenger
	case "tier3_judge":
		binding = c.Scanner.Debate.Tiers.Tier3Judge
	case "tier4_synthesis":
		binding = c.Scanner.Debate.Tiers.Tier4Synthesis
	}

	resources := binding.GetResources()
	stageTimeout := c.Scanner.Debate.StageTimeoutSeconds
	if len(resources) > 0 {
		primaryResID := resources[0]
		// 在 LLM.Resources 中定位首选资源
		if res := c.FindResource(primaryResID); res != nil {
			recovery := binding.Recovery.normalized()
			timeout := binding.TimeoutSeconds
			if timeout <= 0 {
				if stageTimeout > 0 {
					timeout = stageTimeout
				} else {
					timeout = 600
				}
			}
			attemptTimeout := binding.AttemptTimeoutSeconds
			if attemptTimeout <= 0 {
				attemptTimeout = res.AttemptTimeoutSeconds
			}
			firstByteTimeout := binding.FirstByteTimeoutSeconds
			if firstByteTimeout <= 0 {
				firstByteTimeout = res.FirstByteTimeoutSeconds
			}
			if firstByteTimeout <= 0 {
				firstByteTimeout = 180
			}
			idleTimeout := binding.IdleTimeoutSeconds
			if idleTimeout <= 0 {
				idleTimeout = res.IdleTimeoutSeconds
			}
			if idleTimeout <= 0 {
				idleTimeout = 300
			}
			maxOutputBytes := binding.MaxOutputBytes
			if maxOutputBytes <= 0 {
				maxOutputBytes = res.MaxOutputBytes
			}
			model := res.ResourceModel()
			concurrent := res.ResourceConcurrent()
			return TierConfig{
				Backend:                 res.Driver,
				Model:                   model,
				Concurrent:              concurrent,
				TimeoutSeconds:          timeout,
				AttemptTimeoutSeconds:   attemptTimeout,
				FirstByteTimeoutSeconds: firstByteTimeout,
				IdleTimeoutSeconds:      idleTimeout,
				MaxOutputBytes:          maxOutputBytes,
				TimeoutRecovery:         binding.TimeoutRecovery,
				MaxSplitDepth:           binding.MaxSplitDepth,
				Recovery:                recovery,
				ResourceID:              primaryResID,
			}
		}
		// 若 Resource ID 为 driver 名称（如 "agy", "native", "opencode"）
		return TierConfig{
			Backend:         primaryResID,
			Concurrent:      5,
			TimeoutSeconds:  binding.TimeoutSeconds,
			TimeoutRecovery: binding.TimeoutRecovery,
			MaxSplitDepth:   binding.MaxSplitDepth,
			Recovery:        binding.Recovery.normalized(),
			ResourceID:      primaryResID,
		}
	}

	// 全局默认兜底
	concurrent := c.Server.WorkerCount
	if concurrent <= 0 {
		concurrent = 5
	}
	timeout := stageTimeout
	if timeout <= 0 {
		timeout = 600
	}
	return TierConfig{
		Backend:        c.AI.Backend,
		Concurrent:     concurrent,
		TimeoutSeconds: timeout,
	}
}

// normalized applies conservative recovery defaults. It never enables
// candidate failover: the zero value is intentional and means same-resource
// recovery only.
func (rc *TierRecoveryConfig) normalized() TierRecoveryConfig {
	if rc == nil {
		return TierRecoveryConfig{}
	}
	config := *rc
	if config.MaxTotalAttempts < 1 {
		config.MaxTotalAttempts = 2
	}
	if config.MaxAttemptsPerResource < 1 {
		config.MaxAttemptsPerResource = config.MaxTotalAttempts
	}
	if config.MaxAttemptsPerResource > config.MaxTotalAttempts {
		config.MaxAttemptsPerResource = config.MaxTotalAttempts
	}
	if config.MaxCandidateFailovers < 0 {
		config.MaxCandidateFailovers = 0
	}
	if len(config.ContractRepairOn) == 0 {
		config.ContractRepairOn = []string{
			"contract_mismatch",
			"json_invalid",
		}
	}
	return config
}

// GetAppBaseDir 获取应用程序基准目录（用于定位 tasks/ 等内置资产模版）
func GetAppBaseDir() string {
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if fi, err := os.Stat(filepath.Join(exeDir, "tasks")); err == nil && fi.IsDir() {
			return exeDir
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if fi, err := os.Stat(filepath.Join(wd, "tasks")); err == nil && fi.IsDir() {
			return wd
		}
		parentDir := filepath.Dir(wd)
		if fi, err := os.Stat(filepath.Join(parentDir, "tasks")); err == nil && fi.IsDir() {
			return parentDir
		}
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

// GetDataDir 获取运行时数据根目录（用于存放 codes/ 缓存和 reports/ 分析报告）
func (c *Config) GetDataDir() string {
	if c.Server.DataDir != "" {
		return c.Server.DataDir
	}
	if c.Storage.Root != "" {
		return c.Storage.Root
	}
	return "./data"
}

// GetCodesDir 获取代码仓缓存根目录
func (c *Config) GetCodesDir() string {
	return filepath.Join(c.GetDataDir(), "codes")
}

// GetReportsDir 获取分析报告输出根目录
func (c *Config) GetReportsDir() string {
	return filepath.Join(c.GetDataDir(), "reports")
}

// GetTaskAbsPath 返回 tasks 目录下模版或脚本的绝对路径
func (c *Config) GetTaskAbsPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(GetAppBaseDir(), path)
}

// GetAbsPath 综合路径解析
func (c *Config) GetAbsPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	cleanPath := filepath.Clean(path)
	if strings.HasPrefix(cleanPath, "tasks") || cleanPath == "tasks" {
		return c.GetTaskAbsPath(path)
	}
	return filepath.Join(c.GetDataDir(), path)
}

// LoadConfig reads configuration from specified YAML file
func LoadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	cfg.AIFix.ContextToken = os.ExpandEnv(cfg.AIFix.ContextToken)

	// 基础数据目录
	if cfg.Server.DataDir == "" {
		if cfg.Storage.Root != "" {
			cfg.Server.DataDir = cfg.Storage.Root
		} else {
			cfg.Server.DataDir = "./data"
		}
	}
	absDataDir, err := filepath.Abs(cfg.Server.DataDir)
	if err == nil {
		cfg.Server.DataDir = absDataDir
	}
	cfg.Storage.Root = cfg.Server.DataDir

	if cfg.Identity.AlgorithmVersion == "" {
		cfg.Identity.AlgorithmVersion = "v1"
	}
	if cfg.Identity.MaxCandidatesPerObservation <= 0 {
		cfg.Identity.MaxCandidatesPerObservation = 64
	}
	if cfg.Identity.MaxCandidateEdgesPerReport <= 0 {
		cfg.Identity.MaxCandidateEdgesPerReport = 20000
	}
	if cfg.Identity.MaxAIAssignmentWidth <= 0 {
		cfg.Identity.MaxAIAssignmentWidth = 16
	}
	if cfg.Identity.StrongSameThreshold <= 0 {
		cfg.Identity.StrongSameThreshold = 0.90
	}
	if cfg.Identity.AssignBand <= 0 {
		cfg.Identity.AssignBand = 0.65
	}
	if cfg.Identity.RejectBelow <= 0 {
		cfg.Identity.RejectBelow = 0.45
	}
	if cfg.Identity.AIArbitrationConfidence <= 0 {
		cfg.Identity.AIArbitrationConfidence = 0.70
	}
	if cfg.Identity.GrayZoneFallbackMergeScore <= 0 {
		cfg.Identity.GrayZoneFallbackMergeScore = 0.60
	}
	if cfg.Arbitration.MaxCallsPerReport <= 0 {
		cfg.Arbitration.MaxCallsPerReport = 20
	}
	if cfg.Arbitration.ContextLines <= 0 {
		cfg.Arbitration.ContextLines = 8
	}
	if cfg.Arbitration.TimeoutSeconds <= 0 {
		cfg.Arbitration.TimeoutSeconds = 30
	}
	if len(cfg.Lifecycle.HighRiskSeverities) == 0 {
		cfg.Lifecycle.HighRiskSeverities = []string{"致命", "严重"}
	}
	if cfg.Lifecycle.ResolvedRounds <= 0 {
		cfg.Lifecycle.ResolvedRounds = 2
	}
	if cfg.Lifecycle.DormantThreshold <= 0 {
		cfg.Lifecycle.DormantThreshold = 2
	}
	if cfg.Lifecycle.ObsoleteAfterDormant <= 0 {
		cfg.Lifecycle.ObsoleteAfterDormant = 8
	}
	if cfg.Governance.CategoryRegressionBatchSize <= 0 {
		cfg.Governance.CategoryRegressionBatchSize = 50
	}
	if cfg.Governance.CategoryRegressionMaxFailures <= 0 {
		cfg.Governance.CategoryRegressionMaxFailures = 5
	}
	if !cfg.Lifecycle.RequireCoverage {
		cfg.Lifecycle.RequireCoverage = true
	}
	if !cfg.Lifecycle.RequireChange {
		cfg.Lifecycle.RequireChange = true
	}
	cfg.Governance.Identity = cfg.Identity
	cfg.Governance.Arbitration = cfg.Arbitration
	cfg.Governance.Lifecycle = cfg.Lifecycle

	// 默认输出格式
	if cfg.AI.OutputFormat == "" {
		cfg.AI.OutputFormat = "text"
	}
	if cfg.AI.MockOnMissingCLI == nil {
		enabled := true
		cfg.AI.MockOnMissingCLI = &enabled
	}
	if cfg.Scanner.OpenCode.Continuation.MaxSeconds <= 0 {
		cfg.Scanner.OpenCode.Continuation.MaxSeconds = 300
	}
	if cfg.Scanner.Artifact.MaxSchemaRepairAttempts <= 0 {
		cfg.Scanner.Artifact.MaxSchemaRepairAttempts = 2
	}
	if cfg.Scanner.Artifact.SchemaRepairTimeoutSeconds <= 0 {
		cfg.Scanner.Artifact.SchemaRepairTimeoutSeconds = 600
	}
	if cfg.Scanner.Artifact.SchemaRepairResource == "" {
		cfg.Scanner.Artifact.SchemaRepairResource = cfg.AI.ToolBackends.RepairJSON
	}
	if cfg.Scanner.Artifact.SchemaRepairResource == "" {
		cfg.Scanner.Artifact.SchemaRepairResource = "native"
	}
	if cfg.Scanner.Artifact.MaxQuarantinedCandidateRatio <= 0 {
		cfg.Scanner.Artifact.MaxQuarantinedCandidateRatio = 0.5
	}
	if cfg.Scanner.Analysis.MaxRetries <= 0 {
		cfg.Scanner.Analysis.MaxRetries = 3
	}
	if cfg.Scanner.Analysis.RetryBackoffMs <= 0 {
		cfg.Scanner.Analysis.RetryBackoffMs = 2000
	}
	if cfg.Scanner.Analysis.MaxBackoffSeconds <= 0 {
		cfg.Scanner.Analysis.MaxBackoffSeconds = 30
	}
	if len(cfg.Scanner.Analysis.RetryableErrors) == 0 {
		cfg.Scanner.Analysis.RetryableErrors = []string{
			"rate_limited",
			"network_transient",
			"unknown",
		}
	}
	cfg.Scanner.NormalizeDefaults()

	// 同步新旧配置
	cfg.SyncLegacy()

	// 算力总并发推导 WorkerCount
	sumConcurrent := 0
	for _, r := range cfg.LLM.Resources {
		sumConcurrent += r.ResourceConcurrent()
	}
	for i := range cfg.AI.Models {
		if cfg.AI.Models[i].Concurrent <= 0 {
			cfg.AI.Models[i].Concurrent = 1
		}
		sumConcurrent += cfg.AI.Models[i].Concurrent
	}

	if cfg.Server.WorkerCount <= 0 {
		if sumConcurrent > 0 {
			calculated := (sumConcurrent + 1) / 2
			if calculated < 1 {
				calculated = 1
			}
			cfg.Server.WorkerCount = calculated
			cfg.Scanner.WorkerCount = calculated
			log.Printf("[Config] Dynamic worker_count set to %d (calculated from sum of LLM concurrencies %d)\n", calculated, sumConcurrent)
		} else {
			cfg.Server.WorkerCount = 5
			cfg.Scanner.WorkerCount = 5
		}
	}
	if cfg.Server.MaxQueueSize == 0 {
		cfg.Server.MaxQueueSize = 2000
		cfg.Scanner.MaxQueueSize = 2000
	}

	// Server timeout defaults
	serverCfg := configutil.ServerConfig{
		Port:              cfg.Server.Port,
		GinLog:            cfg.Server.GinLog,
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
		ExternalURL:       cfg.Server.ExternalURL,
	}
	configutil.ApplyServerDefaults(&serverCfg, ":8082")
	cfg.Server.Port = serverCfg.Port
	cfg.Server.ExternalURL = serverCfg.ExternalURL
	cfg.Server.ReadTimeout = serverCfg.ReadTimeout
	cfg.Server.ReadHeaderTimeout = serverCfg.ReadHeaderTimeout
	cfg.Server.WriteTimeout = serverCfg.WriteTimeout
	cfg.Server.IdleTimeout = serverCfg.IdleTimeout
	cfg.Server.MaxHeaderBytes = serverCfg.MaxHeaderBytes

	// Auth defaults
	configutil.EnsureJWTSecret(&cfg.Auth.JWTSecret, "Shield-Auth")
	if !cfg.Auth.OAuth2.Enabled && !cfg.Auth.PasswordLoginEnabled {
		cfg.Auth.PasswordLoginEnabled = true
	}
	if cfg.Auth.OAuth2.Enabled {
		if len(cfg.Auth.OAuth2.Scopes) == 0 {
			cfg.Auth.OAuth2.Scopes = []string{"openid", "profile", "email"}
		}
		if cfg.Auth.OAuth2.FieldMapping.Username == "" {
			cfg.Auth.OAuth2.FieldMapping.Username = "preferred_username"
		}
		if cfg.Auth.OAuth2.FieldMapping.Email == "" {
			cfg.Auth.OAuth2.FieldMapping.Email = "email"
		}
		if cfg.Auth.OAuth2.FieldMapping.Name == "" {
			cfg.Auth.OAuth2.FieldMapping.Name = "name"
		}
		if cfg.Auth.OAuth2.FieldMapping.EmployeeID == "" {
			cfg.Auth.OAuth2.FieldMapping.EmployeeID = "employee_id"
		}
		if cfg.Auth.OAuth2.FieldMapping.UniqueID == "" {
			cfg.Auth.OAuth2.FieldMapping.UniqueID = "unique_id"
		}
		if cfg.Auth.OAuth2.FieldMapping.EmployeeType == "" {
			cfg.Auth.OAuth2.FieldMapping.EmployeeType = "employee_type"
		}
		if cfg.Auth.OAuth2.RedirectURL == "" {
			cfg.Auth.OAuth2.RedirectURL = strings.TrimRight(cfg.Server.ExternalURL, "/") + "/api/oauth2/callback"
		}
	}

	cfg.Retention.NormalizeRetentionDefaults()

	if err := cfg.AIFix.Validate(); err != nil {
		return fmt.Errorf("invalid ai_fix config: %w", err)
	}

	AppConfig = cfg
	return nil
}

// MockOnMissingCLIEnabled 返回 CLI 未安装时是否启用模拟降级
func (c *Config) MockOnMissingCLIEnabled() bool {
	if c.Scanner.MockOnMissingCLI != nil {
		return *c.Scanner.MockOnMissingCLI
	}
	return c.AI.MockOnMissingCLI == nil || *c.AI.MockOnMissingCLI
}
