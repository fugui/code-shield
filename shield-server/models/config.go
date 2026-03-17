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
}

var AppConfig Config

// LoadConfig reads the configuration from the specified YAML file
func LoadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &AppConfig)
}
