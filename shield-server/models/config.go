package models

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port string `yaml:"port"`
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
	
	// Convert home to absolute path
	absHome, err := filepath.Abs(AppConfig.Workspace.Home)
	if err == nil {
		AppConfig.Workspace.Home = absHome
	}
	
	return nil
}
