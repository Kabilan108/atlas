package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Workspace      string `mapstructure:"workspace"`
	ConfluenceSite string `mapstructure:"confluence_site"`
	Space          string `mapstructure:"space"`
}

func LoadConfig() (*Config, error) {
	v := viper.New()

	// Set config name and type
	v.SetConfigName("config")
	v.SetConfigType("json")

	// Add config paths in order of preference
	configPaths := []string{}

	// 1. $XDG_CONFIG_HOME/atlas/config.json
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		configPaths = append(configPaths, filepath.Join(xdgConfigHome, "atlas"))
	}

	// 2. ~/.config/atlas/config.json
	if homeDir, err := os.UserHomeDir(); err == nil {
		configPaths = append(configPaths, filepath.Join(homeDir, ".config", "atlas"))
	}

	// 3. ./atlas.json (current directory, different name)
	configPaths = append(configPaths, ".")

	// Add all paths to viper
	for _, path := range configPaths {
		v.AddConfigPath(path)
	}

	// Also check for atlas.json in current directory
	v.SetConfigName("atlas")
	v.AddConfigPath(".")

	// Read environment variables
	v.SetEnvPrefix("ATLAS")
	v.AutomaticEnv()

	// Attempt to read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found is okay, we'll use defaults and env vars
	}

	config := &Config{}
	if err := v.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return config, nil
}

func GetAtlassianCredentials() (email, token string, err error) {
	email = os.Getenv("ATLASSIAN_EMAIL")
	token = os.Getenv("ATLASSIAN_TOKEN")

	if email == "" {
		return "", "", fmt.Errorf("ATLASSIAN_EMAIL environment variable is required")
	}

	if token == "" {
		return "", "", fmt.Errorf("ATLASSIAN_TOKEN environment variable is required")
	}

	return email, token, nil
}
