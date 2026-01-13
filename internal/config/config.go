package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

var (
	ErrMissingEnvVar = errors.New("missing environment variable")
	ErrInvalidConfig = errors.New("invalid configuration")
)

type Config struct {
	Workspace   string `mapstructure:"workspace"`
	Username    string `mapstructure:"username"`
	AppPassword string `mapstructure:"app_password"`
}

var envVarPattern = regexp.MustCompile(`\$\{env:([^}]+)\}`)

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".config", "atlas"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func Load() (*Config, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}

	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(configDir)

	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	cfg.AppPassword, err = expandEnvVar(cfg.AppPassword)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func expandEnvVar(value string) (string, error) {
	matches := envVarPattern.FindStringSubmatch(value)
	if matches == nil {
		return value, nil
	}

	varName := matches[1]
	envValue := os.Getenv(varName)
	if envValue == "" {
		return "", fmt.Errorf("%w: %s", ErrMissingEnvVar, varName)
	}

	return envVarPattern.ReplaceAllString(value, envValue), nil
}

func Get(key string) (string, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return "", err
	}

	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(configDir)

	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	return viper.GetString(key), nil
}

func GetRaw(key string) (string, bool, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return "", false, err
	}

	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(configDir)

	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read config: %w", err)
	}

	value := viper.GetString(key)
	hasEnvRef := envVarPattern.MatchString(value)
	return value, hasEnvRef, nil
}

func Set(key, value string) error {
	configPath, err := ConfigPath()
	if err != nil {
		return err
	}

	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")

	if _, err := os.Stat(configPath); err == nil {
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("failed to read config: %w", err)
		}
	}

	v.Set(key, value)
	if err := v.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func IsEnvReference(value string) bool {
	return envVarPattern.MatchString(value)
}

func ValidKeys() []string {
	return []string{"workspace", "username", "app_password"}
}

func IsValidKey(key string) bool {
	key = strings.ToLower(key)
	for _, valid := range ValidKeys() {
		if key == valid {
			return true
		}
	}
	return false
}
