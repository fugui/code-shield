package models

import (
	"os"

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
	return nil
}
