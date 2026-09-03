package models

import (
	"code-common/backend/configutil"
	commonModels "code-common/backend/models"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	Name        string  `yaml:"name" json:"name"`
	BaseURL     string  `yaml:"base_url" json:"base_url"`
	APIKey      string  `yaml:"api_key" json:"api_key"`
	Model       string  `yaml:"model" json:"model"`
	Concurrent  int     `yaml:"concurrent" json:"concurrent"`
	Weight      int     `yaml:"weight" json:"weight"` // 相对权重比例，如 80 / 20
	Temperature float64 `yaml:"temperature" json:"temperature"`
}

// ComputeResourceConfig 算力节点实体定义
type ComputeResourceConfig struct {
	ID                 string                   `yaml:"id" json:"id"`
	Driver             string                   `yaml:"driver" json:"driver"` // agy / opencode / claude / codex / native
	Model              string                   `yaml:"model" json:"model"`
	Concurrent         int                      `yaml:"concurrent" json:"concurrent"`
	BaseURL            string                   `yaml:"base_url" json:"base_url"`
	APIKey             string                   `yaml:"api_key" json:"api_key"`
	ResponseFormatJSON bool                     `yaml:"response_format_json" json:"response_format_json"`
	MaxRetries         int                      `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMs     int                      `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
	Endpoints          []ResourceEndpointConfig `yaml:"endpoints" json:"endpoints"`
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

// TierBindingConfig 阶梯绑定配置
type TierBindingConfig struct {
	Resource       string `yaml:"resource" json:"resource"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// DebateTiersConfig 辩论阶梯流水线配置 (3 层组织映射 4 大角色)
type DebateTiersConfig struct {
	Tier1Hunter    TierBindingConfig `yaml:"tier1_hunter" json:"tier1_hunter"`       // Hunter 初筛角色
	Tier2Reasoning TierBindingConfig `yaml:"tier2_reasoning" json:"tier2_reasoning"` // Challenger & Judge 角色
	Tier3Synthesis TierBindingConfig `yaml:"tier3_synthesis" json:"tier3_synthesis"` // Synthesis 汇总角色
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

// ScannerConfig 智能扫描引擎与任务调度配置
type ScannerConfig struct {
	WorkerCount      int              `yaml:"worker_count" json:"worker_count"`
	MaxQueueSize     int              `yaml:"max_queue_size" json:"max_queue_size"`
	MockOnMissingCLI *bool            `yaml:"mock_on_missing_cli" json:"mock_on_missing_cli"`
	Throttling       ThrottlingConfig `yaml:"throttling" json:"throttling"`
	Debate           DebateConfig     `yaml:"debate" json:"debate"`
	Tools            ToolsConfig      `yaml:"tools" json:"tools"`
}

// ==============================================================================
// 3. 企业治理与通知配置 (Governance & Notification)
// ==============================================================================

// GovernancePolicyConfig 企业治理策略定义 (升级自原 GovernanceSystemConfig)
type GovernancePolicyConfig struct {
	Fingerprint struct {
		Enabled             bool    `yaml:"enabled" json:"enabled"`
		SimilarityThreshold float64 `yaml:"similarity_threshold" json:"similarity_threshold"`
	} `yaml:"fingerprint" json:"fingerprint"`

	Lifecycle struct {
		ScopeGuardEnabled  bool `yaml:"scope_guard_enabled" json:"scope_guard_enabled"`
		AutoResolveMissing bool `yaml:"auto_resolve_missing" json:"auto_resolve_missing"`
		DiffGateStrict     bool `yaml:"diff_gate_strict" json:"diff_gate_strict"`
	} `yaml:"lifecycle" json:"lifecycle"`

	FeedbackMemory struct {
		InjectionEnabled bool `yaml:"injection_enabled" json:"injection_enabled"`
		MaxRulesInjected int  `yaml:"max_rules_injected" json:"max_rules_injected"`
	} `yaml:"feedback_memory" json:"feedback_memory"`
}

// NotificationConfig 通知配置
type NotificationConfig struct {
	Webhook string `yaml:"webhook" json:"webhook"`
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
	Backend        string `yaml:"backend" json:"backend"`
	Model          string `yaml:"model" json:"model"`
	Concurrent     int    `yaml:"concurrent" json:"concurrent"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
}

type DebateFlowConfig = DebateConfig

type GovernanceSystemConfig struct {
	FingerprintEnabled bool `yaml:"fingerprint_enabled" json:"fingerprint_enabled"`
	ScopeGuardEnabled  bool `yaml:"scope_guard_enabled" json:"scope_guard_enabled"`
	AutoResolveMissing bool `yaml:"auto_resolve_missing" json:"auto_resolve_missing"`
	FeedbackInjection  bool `yaml:"feedback_injection" json:"feedback_injection"`
	DiffGateStrict     bool `yaml:"diff_gate_strict" json:"diff_gate_strict"`
}

type NativeEndpointConfig = ResourceEndpointConfig

type NativeLLMConfig struct {
	BaseURL            string                 `yaml:"base_url" json:"base_url"`
	Endpoint           string                 `yaml:"endpoint" json:"endpoint"`
	APIKey             string                 `yaml:"api_key" json:"api_key"`
	DefaultModel       string                 `yaml:"default_model" json:"default_model"`
	Temperature        float64                `yaml:"temperature" json:"temperature"`
	MaxTokens          int                    `yaml:"max_tokens" json:"max_tokens"`
	ResponseFormatJSON bool                   `yaml:"response_format_json" json:"response_format_json"`
	MaxRetries         int                    `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMs     int                    `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
	Endpoints          []NativeEndpointConfig `yaml:"endpoints" json:"endpoints"`
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
	Server       ServerConfig           `yaml:"server" json:"server"`
	Storage      struct {
		Root string `yaml:"root" json:"root"`
	} `yaml:"storage" json:"storage"`
	Database     DatabaseConfig         `yaml:"database" json:"database"`
	Auth         AuthConfig             `yaml:"auth" json:"auth"`
	LLM          LLMConfig              `yaml:"llm" json:"llm"`
	Scanner      ScannerConfig          `yaml:"scanner" json:"scanner"`
	Governance   GovernancePolicyConfig `yaml:"governance" json:"governance"`
	Notification NotificationConfig     `yaml:"notification" json:"notification"`

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
		Tiers             struct {
			Tier1Fast      TierConfig `yaml:"tier1_fast" json:"tier1_fast"`
			Tier2Reasoning TierConfig `yaml:"tier2_reasoning" json:"tier2_reasoning"`
			Tier3Synthesis TierConfig `yaml:"tier3_synthesis" json:"tier3_synthesis"`
		} `yaml:"tiers" json:"tiers"`
		Debate DebateFlowConfig `yaml:"debate" json:"debate"`
	} `yaml:"ai"`
}

var AppConfig Config

// SyncLegacy 保持新顶层结构与旧 AI 影子镜像的双向同步
func (c *Config) SyncLegacy() {
	// 1. 若新版 LLM 为空但旧版 AI 有配置，从旧版生成新版
	if len(c.LLM.Resources) == 0 && (c.AI.Backend != "" || len(c.AI.Models) > 0 || c.AI.Native.BaseURL != "" || len(c.AI.Native.Endpoints) > 0) {
		c.LLM.DefaultResource = "native"
		c.LLM.DebugLogs = c.AI.DebugLogs

		// 填充 native 资源节点
		nativeRes := ComputeResourceConfig{
			ID:                 "native",
			Driver:             "native",
			Model:              c.AI.Native.DefaultModel,
			Concurrent:         20,
			BaseURL:            c.AI.Native.BaseURL,
			APIKey:             c.AI.Native.APIKey,
			ResponseFormatJSON: c.AI.Native.ResponseFormatJSON,
			MaxRetries:         c.AI.Native.MaxRetries,
			RetryBackoffMs:     c.AI.Native.RetryBackoffMs,
		}
		if len(c.AI.Native.Endpoints) > 0 {
			for _, ep := range c.AI.Native.Endpoints {
				nativeRes.Endpoints = append(nativeRes.Endpoints, ResourceEndpointConfig{
					Name:        ep.Name,
					BaseURL:     ep.BaseURL,
					APIKey:      ep.APIKey,
					Model:       ep.Model,
					Concurrent:  ep.Concurrent,
					Weight:      ep.Weight,
					Temperature: ep.Temperature,
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
				ID:         id,
				Driver:     driver,
				Model:      modelName,
				Concurrent: m.Concurrent,
			})
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
		// 映射旧版 Tiers
		c.Scanner.Debate.Tiers.Tier1Hunter = TierBindingConfig{
			Resource:       c.AI.Tiers.Tier1Fast.Backend,
			TimeoutSeconds: c.AI.Tiers.Tier1Fast.TimeoutSeconds,
		}
		c.Scanner.Debate.Tiers.Tier2Reasoning = TierBindingConfig{
			Resource:       c.AI.Tiers.Tier2Reasoning.Backend,
			TimeoutSeconds: c.AI.Tiers.Tier2Reasoning.TimeoutSeconds,
		}
		c.Scanner.Debate.Tiers.Tier3Synthesis = TierBindingConfig{
			Resource:       c.AI.Tiers.Tier3Synthesis.Backend,
			TimeoutSeconds: c.AI.Tiers.Tier3Synthesis.TimeoutSeconds,
		}
	}
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

	// 寻找 native 节点同步给 AI.Native
	for _, res := range c.LLM.Resources {
		if res.ID == "native" || res.Driver == "native" {
			c.AI.Native.BaseURL = res.BaseURL
			c.AI.Native.APIKey = res.APIKey
			c.AI.Native.DefaultModel = res.Model
			c.AI.Native.ResponseFormatJSON = res.ResponseFormatJSON
			c.AI.Native.MaxRetries = res.MaxRetries
			c.AI.Native.RetryBackoffMs = res.RetryBackoffMs
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

// GetTierConfig 智能获取指定 Tier 的配置（支持新版绑定与旧版向后兼容兜底）
func (c *Config) GetTierConfig(tier string) TierConfig {
	// 1. 优先从新版 Scanner.Debate.Tiers 中查找对应 Resource
	var binding TierBindingConfig
	switch tier {
	case "tier1_fast", "tier1_hunter":
		binding = c.Scanner.Debate.Tiers.Tier1Hunter
	case "tier2_reasoning":
		binding = c.Scanner.Debate.Tiers.Tier2Reasoning
	case "tier3_synthesis":
		binding = c.Scanner.Debate.Tiers.Tier3Synthesis
	}

	if binding.Resource != "" {
		// 在 LLM.Resources 中定位对应资源
		for _, res := range c.LLM.Resources {
			if res.ID == binding.Resource {
				timeout := binding.TimeoutSeconds
				if timeout <= 0 {
					timeout = 600
				}
				return TierConfig{
					Backend:        res.Driver,
					Model:          res.Model,
					Concurrent:     res.Concurrent,
					TimeoutSeconds: timeout,
				}
			}
		}
		// 若 Resource ID 为 driver 名称（如 "agy", "native", "opencode"）
		return TierConfig{
			Backend:        binding.Resource,
			Concurrent:     5,
			TimeoutSeconds: binding.TimeoutSeconds,
		}
	}

	// 2. 回退到旧版 AI.Tiers
	switch tier {
	case "tier1_fast", "tier1_hunter":
		if c.AI.Tiers.Tier1Fast.Backend != "" {
			return c.AI.Tiers.Tier1Fast
		}
	case "tier2_reasoning":
		if c.AI.Tiers.Tier2Reasoning.Backend != "" {
			return c.AI.Tiers.Tier2Reasoning
		}
	case "tier3_synthesis":
		if c.AI.Tiers.Tier3Synthesis.Backend != "" {
			return c.AI.Tiers.Tier3Synthesis
		}
	}

	// 3. 全局默认兜底
	concurrent := c.Server.WorkerCount
	if concurrent <= 0 {
		concurrent = 5
	}
	return TierConfig{
		Backend:        c.AI.Backend,
		Concurrent:     concurrent,
		TimeoutSeconds: 60,
	}
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

	// 默认输出格式
	if cfg.AI.OutputFormat == "" {
		cfg.AI.OutputFormat = "text"
	}
	if cfg.AI.MockOnMissingCLI == nil {
		enabled := true
		cfg.AI.MockOnMissingCLI = &enabled
	}

	// 同步新旧配置
	cfg.SyncLegacy()

	// 算力总并发推导 WorkerCount
	sumConcurrent := 0
	for _, r := range cfg.LLM.Resources {
		if r.Concurrent <= 0 {
			r.Concurrent = 5
		}
		sumConcurrent += r.Concurrent
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
