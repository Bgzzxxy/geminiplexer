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

	// Path is the filesystem path this config was loaded from (not serialized).
	Path string `yaml:"-"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{
		Port: 8787,
		Host: "0.0.0.0",
		Path: path,
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse yaml config: %w", err)
	}

	return cfg, nil
}

// SaveConfig writes the configuration back to its source path as YAML.
func SaveConfig(cfg *Config) error {
	if cfg.Path == "" {
		return fmt.Errorf("config has no source path")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(cfg.Path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// UpdateAccountCookies updates (or adds) a single account's cookies + auth_user
// in the YAML file at path, then re-writes it. Never touches other fields.
func UpdateAccountCookies(path, id, cookies, authUser string) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	found := false
	for i := range cfg.Accounts {
		if cfg.Accounts[i].ID == id {
			cfg.Accounts[i].Cookies = cookies
			if authUser != "" {
				cfg.Accounts[i].AuthUser = authUser
			}
			found = true
			break
		}
	}
	if !found {
		cfg.Accounts = append(cfg.Accounts, AccountConfig{
			ID:       id,
			Cookies:  cookies,
			AuthUser: authUser,
		})
	}

	return SaveConfig(cfg)
}
