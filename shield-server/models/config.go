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
	} `yaml:"server"`
	Notifier struct {
		URL string `yaml:"url"`
	} `yaml:"notifier"`
	Review struct {
		NotifyThreshold int `yaml:"notify_threshold"`
	} `yaml:"review"`
	Workspace struct {
		Home string `yaml:"home"`
	} `yaml:"workspace"`
}

var AppConfig Config

// GetAbsPath returns the absolute path relative to the workspace home if the path is relative.
func (c *Config) GetAbsPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.Workspace.Home, path)
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
	if AppConfig.Workspace.Home == "" {
		AppConfig.Workspace.Home = "."
	}

	// Server timeout defaults
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

	// Convert home to absolute path
	absHome, err := filepath.Abs(AppConfig.Workspace.Home)
	if err == nil {
		AppConfig.Workspace.Home = absHome
	}

	return nil
}
