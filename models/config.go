package models

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type FieldMappingConfig struct {
	Username     string `yaml:"username"`      // IdP 用户名字段，默认 "preferred_username"
	Email        string `yaml:"email"`         // IdP 邮箱字段，默认 "email"
	Name         string `yaml:"name"`          // IdP 姓名字段，默认 "name"
	EmployeeID   string `yaml:"employee_id"`   // IdP 工号字段，默认 "employee_id"
	UniqueID     string `yaml:"unique_id"`     // IdP 唯一ID字段，默认 "unique_id"
	EmployeeType string `yaml:"employee_type"` // IdP 员工类型字段，默认 "employee_type"
}

type OAuth2Config struct {
	Enabled      bool               `yaml:"enabled"`       // 是否启用 OAuth2 SSO
	ClientID     string             `yaml:"client_id"`     // OAuth2 Client ID
	ClientSecret string             `yaml:"client_secret"` // OAuth2 Client Secret
	AuthURL      string             `yaml:"auth_url"`      // Authorization Endpoint URL
	TokenURL     string             `yaml:"token_url"`     // Token Endpoint URL
	UserInfoURL  string             `yaml:"userinfo_url"`  // UserInfo Endpoint URL
	RedirectURL  string             `yaml:"redirect_url"`  // 回调地址 (如 https://shield.company.com/api/oauth2/callback)
	Scopes       []string           `yaml:"scopes"`        // 请求的 Scopes，默认 ["openid", "profile", "email"]
	AdminList    []string           `yaml:"admin_list"`    // 自动提权为管理员的邮箱/用户名列表
	FieldMapping FieldMappingConfig `yaml:"field_mapping"` // 用户属性字段映射
	DeptAPIURL   string             `yaml:"dept_api_url"`  // 从 SSO 获取部门信息的外部 API 地址
}

type ModelConfig struct {
	OpenCode   string `yaml:"opencode"`   // OpenCode 引擎对应的具体模型名
	Claude     string `yaml:"claude"`     // Claude 引擎对应的具体模型名
	Concurrent int    `yaml:"concurrent"` // 该 LLM 服务器允许的最大并发数
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
		ExternalURL       string        `yaml:"external_url"`        // 外部访问基准 URL，用于通知和邮件跳转，如 http://127.0.0.1:8080
	} `yaml:"server"`
	Storage struct {
		Root string `yaml:"root"` // 数据根目录，下设 codes/ 和 reports/
	} `yaml:"storage"`
	AI struct {
		Backend      string        `yaml:"backend"`       // CLI 后端：claude 或 opencode，默认 claude
		DebugLogs    bool          `yaml:"debug_logs"`    // 是否输出 AI 引擎底层的 debug 级别日志
		OutputFormat string        `yaml:"output_format"` // 输出格式：text 或 json，默认 text
		Models       []ModelConfig `yaml:"models"`        // 多 LLM 服务器并发配置
	} `yaml:"ai"`
	Notification struct {
		Webhook string `yaml:"webhook"` // 通知回调地址
	} `yaml:"notification"`
	Auth struct {
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
	if AppConfig.Server.ReadTimeout == 0 {
		AppConfig.Server.ReadTimeout = 15 * time.Second
	}
	if AppConfig.Server.ReadHeaderTimeout == 0 {
		AppConfig.Server.ReadHeaderTimeout = 10 * time.Second
	}
	if AppConfig.Server.WriteTimeout == 0 {
		AppConfig.Server.WriteTimeout = 15 * time.Second
	}
	if AppConfig.Server.IdleTimeout == 0 {
		AppConfig.Server.IdleTimeout = 60 * time.Second
	}
	if AppConfig.Server.MaxHeaderBytes == 0 {
		AppConfig.Server.MaxHeaderBytes = 1 << 20 // 1MB
	}

	// Convert root to absolute path
	absRoot, err := filepath.Abs(AppConfig.Storage.Root)
	if err == nil {
		AppConfig.Storage.Root = absRoot
	}

	// Auth defaults
	if AppConfig.Auth.JWTSecret == "" {
		// Generate ephemeral random secret. Instance-isolated, sessions lost on restart.
		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			log.Fatalf("Failed to generate random JWT secret: %v", err)
		}
		AppConfig.Auth.JWTSecret = hex.EncodeToString(randomBytes)
		log.Println("[Auth] WARNING: jwt_secret not configured. Using ephemeral random secret. Sessions will be lost on restart. Set auth.jwt_secret in config.yaml for production use.")
	}
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
