package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Attribution bool   `mapstructure:"attribution"`
}

type Credentials struct {
	Username string
	APIToken string
}

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

func CredentialsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.toml"), nil
}

func Load() (*Config, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(configDir)
	v.SetDefault("attribution", true)

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return configWithEnv(&Config{Attribution: true}), nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return configWithEnv(&cfg), nil
}

func configWithEnv(cfg *Config) *Config {
	if workspace := os.Getenv("ATLAS_WORKSPACE"); workspace != "" {
		cfg.Workspace = workspace
	}
	if username := os.Getenv("ATLAS_USERNAME"); username != "" {
		cfg.Username = username
	}
	return cfg
}

func Get(key string) (string, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return "", err
	}

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(configDir)

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	return v.GetString(key), nil
}

func GetRaw(key string) (string, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return "", err
	}

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(configDir)

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	return v.GetString(key), nil
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

	if key == "attribution" {
		boolValue, err := parseBoolConfigValue(value)
		if err != nil {
			return err
		}
		v.Set(key, boolValue)
	} else {
		v.Set(key, value)
	}
	if err := v.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func LoadCredentials() (*Credentials, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}

	creds := &Credentials{Username: cfg.Username}
	if token := os.Getenv("ATLAS_API_TOKEN"); token != "" {
		creds.APIToken = token
		return creds, nil
	}

	credentialsPath, err := CredentialsPath()
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigFile(credentialsPath)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) || os.IsNotExist(err) {
			return creds, nil
		}
		return nil, fmt.Errorf("failed to read credentials: %w", err)
	}

	creds.APIToken = v.GetString("api_token")
	return creds, nil
}

func SaveAPIToken(token string) error {
	credentialsPath, err := CredentialsPath()
	if err != nil {
		return err
	}

	configDir := filepath.Dir(credentialsPath)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.Chmod(configDir, 0700); err != nil {
		return fmt.Errorf("failed to set config directory permissions: %w", err)
	}

	data := []byte(fmt.Sprintf("api_token = %q\n", token))
	if err := os.WriteFile(credentialsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	if err := os.Chmod(credentialsPath, 0600); err != nil {
		return fmt.Errorf("failed to set credentials permissions: %w", err)
	}

	return nil
}

func ValidKeys() []string {
	return []string{"workspace", "username", "attribution"}
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

func parseBoolConfigValue(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid attribution value %q: expected true or false", value)
	}
}
