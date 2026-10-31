package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type AccountConfig struct {
	ID                 string `yaml:"id"`
	Cookies            string `yaml:"cookies"`
	AuthUser           string `yaml:"auth_user"`
	RefreshIntervalMin int    `yaml:"refresh_interval_min"`
	Proxy              string `yaml:"proxy"`
}

type Config struct {
	Port     int             `yaml:"port"`
	Host     string          `yaml:"host"`
	AdminKey string          `yaml:"admin_key"`
	Accounts []AccountConfig `yaml:"accounts"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{
		Port: 8787,
		Host: "0.0.0.0",
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse yaml config: %w", err)
	}

	return cfg, nil
}
