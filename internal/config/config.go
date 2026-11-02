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
	Group              string `yaml:"group"` // group name for key isolation
}

type APIKeyConfig struct {
	Key   string `yaml:"key"`
	Group string `yaml:"group"` // maps to account group; empty = access all
}

type Config struct {
	Port     int             `yaml:"port"`
	Host     string          `yaml:"host"`
	AdminKey string          `yaml:"admin_key"`
	Accounts []AccountConfig `yaml:"accounts"`
	APIKeys  []APIKeyConfig  `yaml:"api_keys"` // optional key-group mappings
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
