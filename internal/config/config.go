package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config captures user configurable defaults loaded from disk.
type Config struct {
	Workspace      string `mapstructure:"workspace"`
	ConfluenceSite string `mapstructure:"confluence_site"`
	Space          string `mapstructure:"space"`
}

// Credentials holds the Atlassian credentials sourced from environment variables.
type Credentials struct {
	Email string
	Token string
}

const (
	emailEnvVar = "ATLASSIAN_EMAIL"
	tokenEnvVar = "ATLASSIAN_TOKEN"
)

// LoadConfig looks up configuration files in standard locations and merges them when present.
func LoadConfig() (Config, error) {
	candidateFiles := candidateConfigFiles()
	v := viper.New()
	v.SetConfigType("json")

	var cfg Config
	for _, path := range candidateFiles {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		v.SetConfigFile(path)
		if err := v.MergeInConfig(); err != nil {
			return cfg, fmt.Errorf("load config %s: %w", path, err)
		}
	}

	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal config: %w", err)
	}

	return cfg, nil
}

// CredentialsFromEnv fetches the Atlassian credentials from the environment.
func CredentialsFromEnv() (Credentials, error) {
	email, ok := os.LookupEnv(emailEnvVar)
	if !ok || email == "" {
		return Credentials{}, fmt.Errorf("environment variable %s is required", emailEnvVar)
	}

	token, ok := os.LookupEnv(tokenEnvVar)
	if !ok || token == "" {
		return Credentials{}, fmt.Errorf("environment variable %s is required", tokenEnvVar)
	}

	return Credentials{Email: email, Token: token}, nil
}

func candidateConfigFiles() []string {
	var files []string

	if xdgHome := os.Getenv("XDG_CONFIG_HOME"); xdgHome != "" {
		files = append(files, filepath.Join(xdgHome, "atlas", "config.json"))
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(homeDir, ".config", "atlas", "config.json"))
	}

	files = append(files, "atlas.json")
	return files
}
