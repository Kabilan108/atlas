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
	JiraCloudID string `mapstructure:"jira_cloud_id"`
}

// Credentials are the Atlassian email (the configured username) and the API
// tokens for each product. APIToken is the Bitbucket token.
type Credentials struct {
	Username  string
	APIToken  string
	JiraToken string
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

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return configWithEnv(&Config{}), nil
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
	if cloudID := os.Getenv("ATLAS_JIRA_CLOUD_ID"); cloudID != "" {
		cfg.JiraCloudID = cloudID
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

	v.Set(key, value)
	if err := v.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// LoadCredentials reads the tokens from credentials.toml (api_token,
// jira_token); ATLAS_API_TOKEN and ATLAS_JIRA_TOKEN override them.
func LoadCredentials() (*Credentials, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}

	creds := &Credentials{
		Username:  cfg.Username,
		APIToken:  os.Getenv("ATLAS_API_TOKEN"),
		JiraToken: os.Getenv("ATLAS_JIRA_TOKEN"),
	}
	if creds.APIToken != "" && creds.JiraToken != "" {
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

	if creds.APIToken == "" {
		creds.APIToken = v.GetString("api_token")
	}
	if creds.JiraToken == "" {
		creds.JiraToken = v.GetString("jira_token")
	}
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

	data := fmt.Sprintf("api_token = %q\n", token)
	if jiraToken, err := storedCredential(credentialsPath, "jira_token"); err != nil {
		return err
	} else if jiraToken != "" {
		data += fmt.Sprintf("jira_token = %q\n", jiraToken)
	}
	if err := os.WriteFile(credentialsPath, []byte(data), 0600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	if err := os.Chmod(credentialsPath, 0600); err != nil {
		return fmt.Errorf("failed to set credentials permissions: %w", err)
	}

	return nil
}

// storedCredential reads key from the credentials file, or "" when the file
// does not exist.
func storedCredential(path, key string) (string, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read credentials: %w", err)
	}
	return v.GetString(key), nil
}

func ValidKeys() []string {
	return []string{"workspace", "username", "jira_cloud_id"}
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
