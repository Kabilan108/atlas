package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Workspace      string `mapstructure:"workspace"`
	ConfluenceSite string `mapstructure:"confluence_site"`
	Space          string `mapstructure:"space"`
	AtlassianEmail string `mapstructure:"atlassian_email"`
	AtlassianToken string `mapstructure:"atlassian_token"`
}

func LoadConfig() (*Config, error) {
	v := viper.New()

	// We only support these two locations, in this priority:
	// 1) ~/.config/atlas/config.json
	// 2) $XDG_CONFIG_HOME/atlas/config.json

	var candidates []string

	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		candidates = append(candidates, filepath.Join(homeDir, ".config", "atlas", "config.json"))
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "atlas", "config.json"))
	}

	var chosen string
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			chosen = p
			break
		}
	}

	if chosen == "" {
		// Exit early if config cannot be found in supported locations
		return nil, errors.New("config file not found; expected at ~/.config/atlas/config.json or $XDG_CONFIG_HOME/atlas/config.json")
	}

	v.SetConfigFile(chosen)
	v.SetConfigType("json")

	// Environment variables can override values in the file
	v.SetEnvPrefix("ATLAS")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file %s: %w", chosen, err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return cfg, nil
}

func GetAtlassianCredentials() (email, token string, err error) {
	// Prefer credentials from config file
	if cfg, cfgErr := LoadConfig(); cfgErr == nil && cfg != nil {
		if cfg.AtlassianEmail != "" && cfg.AtlassianToken != "" {
			return cfg.AtlassianEmail, cfg.AtlassianToken, nil
		}
	}

	// Fallback to environment variables
	email = os.Getenv("ATLASSIAN_EMAIL")
	token = os.Getenv("ATLASSIAN_TOKEN")

	if email != "" && token != "" {
		return email, token, nil
	}

	return "", "", fmt.Errorf("missing credentials: set atlassian_email and atlassian_token in config file or ATLASSIAN_EMAIL and ATLASSIAN_TOKEN environment variables")
}
