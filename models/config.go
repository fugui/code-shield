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

type ModelConfig struct {
	OpenCode   string `yaml:"opencode"`   // OpenCode 引擎对应的具体模型名
	Claude     string `yaml:"claude"`     // Claude 引擎对应的具体模型名
	Concurrent int    `yaml:"concurrent"` // 该 LLM 服务器允许的最大并发数
}

type WorkHoursThrottleConfig struct {
	Enabled   bool    `yaml:"enabled" json:"enabled"`
	Workdays  []int   `yaml:"workdays" json:"workdays"`     // 生效星期: 1=周一, 2=周二, ..., 5=周五, 6=周六, 7=周日 (0也代表周日)
	StartTime string  `yaml:"start_time" json:"start_time"` // 工作时间开始，如 "09:00"
	EndTime   string  `yaml:"end_time" json:"end_time"`     // 工作时间结束，如 "22:00"
	Scale     float64 `yaml:"scale" json:"scale"`           // 工作时间内的并发比例 (0.0~1.0, 如 0.1 代表 10%)
}

type Config struct {
	Server struct {
		Port              string        `yaml:"port"`
		GinLog            bool          `yaml:"gin_log"`             // 是否打印 GIN 请求日志，默认 false
		ReadTimeout       time.Duration `yaml:"read_timeout"`        // 读取请求超时，默认 15s
		ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"` // 读取 header 超时，默认 10s
		WriteTimeout      time.Duration `yaml:"write_timeout"`       // 写入响应超时，默认 15s
		IdleTimeout       time.Duration `yaml:"idle_timeout"`        // keep-alive 空闲超时，默认 60s
		MaxHeaderBytes    int           `yaml:"max_header_bytes"`    // 最大 header 字节数，默认 1MB
		WorkerCount       int           `yaml:"worker_count"`        // 全局任务并发数，默认 5
		MaxQueueSize      int           `yaml:"max_queue_size"`      // 任务排队最大上限，默认 2000，-1 表示不限制
		ExternalURL       string        `yaml:"external_url"`        // 外部访问基准 URL，用于通知和邮件跳转，如 http://127.0.0.1:8080
	} `yaml:"server"`
	Storage struct {
		Root string `yaml:"root"` // 数据根目录，下设 codes/ 和 reports/
	} `yaml:"storage"`
	Database DatabaseConfig `yaml:"database"`
	AI       struct {
		Backend           string                  `yaml:"backend"`             // CLI 后端：claude 或 opencode，默认 claude
		DebugLogs         bool                    `yaml:"debug_logs"`          // 是否输出 AI 引擎底层的 debug 级别日志
		OutputFormat      string                  `yaml:"output_format"`       // 输出格式：text 或 json，默认 text
		WorkHoursThrottle WorkHoursThrottleConfig `yaml:"work_hours_throttle"` // 工作时间自动限流配置
		Models            []ModelConfig           `yaml:"models"`              // 多 LLM 服务器并发配置
	} `yaml:"ai"`
	Notification struct {
		Webhook string `yaml:"webhook"` // 通知回调地址
	} `yaml:"notification"`
	Auth struct {
		StandaloneMode       bool         `yaml:"standalone_mode"`        // 是否以独立系统模式运行（默认 false 为微前端模式）
		JWTSecret            string       `yaml:"jwt_secret"`             // JWT 签名密钥（替代硬编码，留空则启动时随机生成临时密钥）
		PasswordLoginEnabled bool         `yaml:"password_login_enabled"` // 是否启用密码登录，默认 false
		OAuth2               OAuth2Config `yaml:"oauth2"`
	} `yaml:"auth"`
}

var AppConfig Config

// GetAbsPath returns the absolute path relative to the storage root if the path is relative.
func (c *Config) GetAbsPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.Storage.Root, path)
}

// LoadConfig reads the configuration from the specified YAML file
func LoadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, &AppConfig); err != nil {
		return err
	}
	// Default values
	if AppConfig.Storage.Root == "" {
		AppConfig.Storage.Root = "."
	}
	if AppConfig.AI.Backend == "" {
		AppConfig.AI.Backend = "claude"
	}
	if AppConfig.AI.OutputFormat == "" {
		AppConfig.AI.OutputFormat = "text"
	}

	// Server timeout defaults
	if AppConfig.Server.ExternalURL == "" {
		port := AppConfig.Server.Port
		if strings.HasPrefix(port, ":") {
			AppConfig.Server.ExternalURL = "http://127.0.0.1" + port
		} else {
			AppConfig.Server.ExternalURL = "http://127.0.0.1:8080"
		}
	}
	// 校验工作时间限流配置
	if AppConfig.AI.WorkHoursThrottle.Enabled {
		wt := &AppConfig.AI.WorkHoursThrottle
		if len(wt.Workdays) == 0 {
			wt.Workdays = []int{1, 2, 3, 4, 5}
		}
		if wt.StartTime == "" {
			wt.StartTime = "09:00"
		}
		if wt.EndTime == "" {
			wt.EndTime = "22:00"
		}
		if wt.Scale < 0 {
			wt.Scale = 0
		}
		if wt.Scale > 1.0 {
			wt.Scale = 1.0
		}
		log.Printf("[Config] Work hours auto-throttle enabled: workdays=%v, %s-%s, scale=%.2f\n",
			wt.Workdays, wt.StartTime, wt.EndTime, wt.Scale)
	}

	// 校验并补充 Models 默认并发数，并计算所有模型并发之和
	sumConcurrent := 0
	for i := range AppConfig.AI.Models {
		if AppConfig.AI.Models[i].Concurrent <= 0 {
			AppConfig.AI.Models[i].Concurrent = 1
		}
		sumConcurrent += AppConfig.AI.Models[i].Concurrent
	}

	// 确定全局任务并发数（WorkerCount）
	// 如果用户在 config.yaml 中配置了 worker_count 且其值 > 0，则直接使用它；
	// 否则，根据大模型节点的并发限制动态计算折中任务并发，防止多任务交替争夺槽位引起效率损耗。
	if AppConfig.Server.WorkerCount <= 0 {
		if sumConcurrent > 0 {
			// 因为 chunked 任务每个会并发 4 个请求，若直接将 WorkerCount 设为 sumConcurrent 会引发交替抢槽导致的“磨洋工”。
			// 采用 (sumConcurrent + 1) / 2 作为折中，既能给单个分片任务留有合理的子并发，又不会造成严重的槽位争夺。
			calculated := (sumConcurrent + 1) / 2
			if calculated < 1 {
				calculated = 1
			}
			AppConfig.Server.WorkerCount = calculated
			log.Printf("[Config] Dynamic worker_count set to %d (calculated from sum of LLM concurrencies %d to prevent chunk interleaving)\n", calculated, sumConcurrent)
		} else {
			AppConfig.Server.WorkerCount = 5 // 默认兜底值
		}
	} else {
		log.Printf("[Config] Using explicitly configured worker_count: %d\n", AppConfig.Server.WorkerCount)
	}
	if AppConfig.Server.MaxQueueSize == 0 {
		AppConfig.Server.MaxQueueSize = 2000
	}
	if AppConfig.Server.MaxQueueSize > 0 {
		log.Printf("[Config] Max pending queue size limit set to %d\n", AppConfig.Server.MaxQueueSize)
	} else {
		log.Println("[Config] Max pending queue size limit is disabled (unlimited).")
	}
	// Server timeout defaults
	serverCfg := configutil.ServerConfig{
		Port:              AppConfig.Server.Port,
		GinLog:            AppConfig.Server.GinLog,
		ReadTimeout:       AppConfig.Server.ReadTimeout,
		ReadHeaderTimeout: AppConfig.Server.ReadHeaderTimeout,
		WriteTimeout:      AppConfig.Server.WriteTimeout,
		IdleTimeout:       AppConfig.Server.IdleTimeout,
		MaxHeaderBytes:    AppConfig.Server.MaxHeaderBytes,
		ExternalURL:       AppConfig.Server.ExternalURL,
	}
	configutil.ApplyServerDefaults(&serverCfg, ":8080")
	AppConfig.Server.Port = serverCfg.Port
	AppConfig.Server.ExternalURL = serverCfg.ExternalURL
	AppConfig.Server.ReadTimeout = serverCfg.ReadTimeout
	AppConfig.Server.ReadHeaderTimeout = serverCfg.ReadHeaderTimeout
	AppConfig.Server.WriteTimeout = serverCfg.WriteTimeout
	AppConfig.Server.IdleTimeout = serverCfg.IdleTimeout
	AppConfig.Server.MaxHeaderBytes = serverCfg.MaxHeaderBytes

	// Convert root to absolute path
	absRoot, err := filepath.Abs(AppConfig.Storage.Root)
	if err == nil {
		AppConfig.Storage.Root = absRoot
	}

	// Auth defaults
	configutil.EnsureJWTSecret(&AppConfig.Auth.JWTSecret, "Shield-Auth")
	// If neither OAuth2 nor password login is explicitly enabled, enable password login as fallback
	if !AppConfig.Auth.OAuth2.Enabled && !AppConfig.Auth.PasswordLoginEnabled {
		AppConfig.Auth.PasswordLoginEnabled = true
	}
	// OAuth2 defaults
	if AppConfig.Auth.OAuth2.Enabled {
		if len(AppConfig.Auth.OAuth2.Scopes) == 0 {
			AppConfig.Auth.OAuth2.Scopes = []string{"openid", "profile", "email"}
		}
		if AppConfig.Auth.OAuth2.FieldMapping.Username == "" {
			AppConfig.Auth.OAuth2.FieldMapping.Username = "preferred_username"
		}
		if AppConfig.Auth.OAuth2.FieldMapping.Email == "" {
			AppConfig.Auth.OAuth2.FieldMapping.Email = "email"
		}
		if AppConfig.Auth.OAuth2.FieldMapping.Name == "" {
			AppConfig.Auth.OAuth2.FieldMapping.Name = "name"
		}
		if AppConfig.Auth.OAuth2.FieldMapping.EmployeeID == "" {
			AppConfig.Auth.OAuth2.FieldMapping.EmployeeID = "employee_id"
		}
		if AppConfig.Auth.OAuth2.FieldMapping.UniqueID == "" {
			AppConfig.Auth.OAuth2.FieldMapping.UniqueID = "unique_id"
		}
		if AppConfig.Auth.OAuth2.FieldMapping.EmployeeType == "" {
			AppConfig.Auth.OAuth2.FieldMapping.EmployeeType = "employee_type"
		}
		// Default redirect URL based on external URL
		if AppConfig.Auth.OAuth2.RedirectURL == "" {
			AppConfig.Auth.OAuth2.RedirectURL = strings.TrimRight(AppConfig.Server.ExternalURL, "/") + "/api/oauth2/callback"
			log.Printf("[Auth] OAuth2 redirect_url auto-derived: %s", AppConfig.Auth.OAuth2.RedirectURL)
		}
	}

	return nil
}
