package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesAtlasEnvOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATLAS_WORKSPACE", "env-workspace")
	t.Setenv("ATLAS_USERNAME", "env@example.com")

	if err := Set("workspace", "file-workspace"); err != nil {
		t.Fatalf("Set(workspace) error = %v", err)
	}
	if err := Set("username", "file@example.com"); err != nil {
		t.Fatalf("Set(username) error = %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Workspace != "env-workspace" {
		t.Fatalf("Workspace = %q, want env-workspace", cfg.Workspace)
	}
	if cfg.Username != "env@example.com" {
		t.Fatalf("Username = %q, want env@example.com", cfg.Username)
	}
}

func TestLoadCredentialsPrefersAtlasAPIToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATLAS_API_TOKEN", "env-token")
	t.Setenv("ATLAS_USERNAME", "")
	t.Setenv("ATLAS_WORKSPACE", "")

	if err := Set("username", "file@example.com"); err != nil {
		t.Fatalf("Set(username) error = %v", err)
	}
	if err := SaveAPIToken("file-token"); err != nil {
		t.Fatalf("SaveAPIToken() error = %v", err)
	}

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}

	if creds.Username != "file@example.com" {
		t.Fatalf("Username = %q, want file@example.com", creds.Username)
	}
	if creds.APIToken != "env-token" {
		t.Fatalf("APIToken = %q, want env-token", creds.APIToken)
	}
}

func TestSaveAPITokenWritesCredentialsFileWithPrivatePermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATLAS_API_TOKEN", "")
	t.Setenv("ATLAS_USERNAME", "")
	t.Setenv("ATLAS_WORKSPACE", "")

	if err := Set("username", "dev@example.com"); err != nil {
		t.Fatalf("Set(username) error = %v", err)
	}

	configDir := filepath.Join(home, ".config", "atlas")
	if err := os.Chmod(configDir, 0755); err != nil {
		t.Fatalf("Chmod(configDir) error = %v", err)
	}
	if err := SaveAPIToken("saved-token"); err != nil {
		t.Fatalf("SaveAPIToken() error = %v", err)
	}

	dirInfo, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0700 {
		t.Fatalf("config dir mode = %o, want 0700", got)
	}

	credentialsPath := filepath.Join(configDir, "credentials.toml")
	info, err := os.Stat(credentialsPath)
	if err != nil {
		t.Fatalf("stat credentials file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("credentials mode = %o, want 0600", got)
	}

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if creds.Username != "dev@example.com" {
		t.Fatalf("Username = %q, want dev@example.com", creds.Username)
	}
	if creds.APIToken != "saved-token" {
		t.Fatalf("APIToken = %q, want saved-token", creds.APIToken)
	}
}

func TestLoadCredentialsIgnoresLegacyAppPassword(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATLAS_API_TOKEN", "")
	t.Setenv("ATLAS_USERNAME", "")
	t.Setenv("ATLAS_WORKSPACE", "")

	configDir := filepath.Join(home, ".config", "atlas")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	data := []byte("username = \"dev@example.com\"\napp_password = \"legacy-token\"\n")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if creds.Username != "dev@example.com" {
		t.Fatalf("Username = %q, want dev@example.com", creds.Username)
	}
	if creds.APIToken != "" {
		t.Fatalf("APIToken = %q, want empty", creds.APIToken)
	}
}

func TestLoadCredentialsReadsJiraTokenAndCloudID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ATLAS_API_TOKEN", "")
	t.Setenv("ATLAS_JIRA_TOKEN", "")
	t.Setenv("ATLAS_USERNAME", "")
	t.Setenv("ATLAS_WORKSPACE", "")
	t.Setenv("ATLAS_JIRA_CLOUD_ID", "")

	path, err := CredentialsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("api_token = \"bb\"\njira_token = \"jt\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Set("jira_cloud_id", "cloud-1"); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if creds.APIToken != "bb" || creds.JiraToken != "jt" {
		t.Fatalf("creds = %+v", creds)
	}
	cfg, err := Load()
	if err != nil || cfg.JiraCloudID != "cloud-1" {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}

	t.Setenv("ATLAS_JIRA_TOKEN", "env-jt")
	t.Setenv("ATLAS_JIRA_CLOUD_ID", "env-cloud")
	if creds, _ := LoadCredentials(); creds.JiraToken != "env-jt" || creds.APIToken != "bb" {
		t.Fatalf("env override: %+v", creds)
	}
	if cfg, _ := Load(); cfg.JiraCloudID != "env-cloud" {
		t.Fatalf("env cloud ID: %+v", cfg)
	}

	// Logging in again replaces the Bitbucket token but keeps Jira's.
	if err := SaveAPIToken("bb2"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATLAS_JIRA_TOKEN", "")
	if creds, _ := LoadCredentials(); creds.APIToken != "bb2" || creds.JiraToken != "jt" {
		t.Fatalf("after SaveAPIToken: %+v", creds)
	}
}
