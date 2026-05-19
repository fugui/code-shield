package models

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

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
	} `yaml:"server"`
	Storage struct {
		Root string `yaml:"root"` // 数据根目录，下设 codes/ 和 reports/
	} `yaml:"storage"`
	AI struct {
		Backend      string `yaml:"backend"`       // CLI 后端：claude 或 opencode，默认 claude
		DebugLogs    bool   `yaml:"debug_logs"`    // 是否输出 AI 引擎底层的 debug 级别日志
		OutputFormat string `yaml:"output_format"` // 输出格式：text 或 json，默认 text
	} `yaml:"ai"`
	Notification struct {
		Webhook string `yaml:"webhook"` // 通知回调地址
	} `yaml:"notification"`
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
	if AppConfig.Server.WorkerCount <= 0 {
		AppConfig.Server.WorkerCount = 5
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

	return nil
}
