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

func TestBitbucketCredentialsPrefersAtlasAPIToken(t *testing.T) {
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

	creds, err := BitbucketCredentials()
	if err != nil {
		t.Fatalf("BitbucketCredentials() error = %v", err)
	}

	if creds.Username != "file@example.com" {
		t.Fatalf("Username = %q, want file@example.com", creds.Username)
	}
	if creds.Token != "env-token" {
		t.Fatalf("Token = %q, want env-token", creds.Token)
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

	creds, err := BitbucketCredentials()
	if err != nil {
		t.Fatalf("BitbucketCredentials() error = %v", err)
	}
	if creds.Username != "dev@example.com" {
		t.Fatalf("Username = %q, want dev@example.com", creds.Username)
	}
	if creds.Token != "saved-token" {
		t.Fatalf("Token = %q, want saved-token", creds.Token)
	}
}

func TestBitbucketCredentialsIgnoresLegacyAppPassword(t *testing.T) {
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

	creds, err := BitbucketCredentials()
	if err != nil {
		t.Fatalf("BitbucketCredentials() error = %v", err)
	}
	if creds.Username != "dev@example.com" {
		t.Fatalf("Username = %q, want dev@example.com", creds.Username)
	}
	if creds.Token != "" {
		t.Fatalf("Token = %q, want empty", creds.Token)
	}
}

func TestCredentialsReadJiraTokenAndCloudID(t *testing.T) {
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

	bb, err := BitbucketCredentials()
	if err != nil {
		t.Fatal(err)
	}
	jt, err := JiraCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if bb.Token != "bb" || jt.Token != "jt" {
		t.Fatalf("Bitbucket = %+v, Jira = %+v", bb, jt)
	}
	cfg, err := Load()
	if err != nil || cfg.JiraCloudID != "cloud-1" {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}

	t.Setenv("ATLAS_JIRA_TOKEN", "env-jt")
	t.Setenv("ATLAS_JIRA_CLOUD_ID", "env-cloud")
	if jt, _ := JiraCredentials(); jt.Token != "env-jt" {
		t.Fatalf("Jira env override: %+v", jt)
	}
	if bb, _ := BitbucketCredentials(); bb.Token != "bb" {
		t.Fatalf("Bitbucket with Jira env override: %+v", bb)
	}
	if cfg, _ := Load(); cfg.JiraCloudID != "env-cloud" {
		t.Fatalf("env cloud ID: %+v", cfg)
	}

	// Logging in again replaces the Bitbucket token but keeps Jira's.
	if err := SaveAPIToken("bb2"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATLAS_JIRA_TOKEN", "")
	bb, _ = BitbucketCredentials()
	jt, _ = JiraCredentials()
	if bb.Token != "bb2" || jt.Token != "jt" {
		t.Fatalf("after SaveAPIToken: Bitbucket = %+v, Jira = %+v", bb, jt)
	}
}

// Each product reads the credentials file only for its own token, so a
// broken file never blocks a product whose token comes from the environment.
func TestBrokenCredentialsFileBlocksOnlyProductsThatReadIt(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		envOnly  func() (*Credentials, error)
		fromFile func() (*Credentials, error)
	}{
		{"Bitbucket from env", "ATLAS_API_TOKEN", BitbucketCredentials, JiraCredentials},
		{"Jira from env", "ATLAS_JIRA_TOKEN", JiraCredentials, BitbucketCredentials},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("ATLAS_USERNAME", "dev@example.com")
			t.Setenv("ATLAS_WORKSPACE", "")
			t.Setenv("ATLAS_API_TOKEN", "")
			t.Setenv("ATLAS_JIRA_TOKEN", "")
			t.Setenv(tc.envVar, "env-token")
			path, err := CredentialsPath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("jira_token = [\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			creds, err := tc.envOnly()
			if err != nil {
				t.Fatalf("token from %s blocked by the credentials file: %v", tc.envVar, err)
			}
			if creds.Username != "dev@example.com" || creds.Token != "env-token" {
				t.Fatalf("creds = %+v", creds)
			}
			if _, err := tc.fromFile(); err == nil {
				t.Fatal("product reading the broken credentials file got no error")
			}
		})
	}
}

func TestCredentialsFileAndBitbucketTokenOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATLAS_API_TOKEN", "")
	t.Setenv("ATLAS_BITBUCKET_TOKEN", "")
	t.Setenv("ATLAS_JIRA_TOKEN", "")
	secret := filepath.Join(t.TempDir(), "atlas-credentials.toml")
	if err := os.WriteFile(secret, []byte("api_token = \"file-bb\"\njira_token = \"file-jira\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATLAS_CREDENTIALS_FILE", secret)

	if path, err := CredentialsPath(); err != nil || path != secret {
		t.Fatalf("CredentialsPath = %q, %v", path, err)
	}
	bb, err := BitbucketCredentials()
	if err != nil || bb.Token != "file-bb" {
		t.Fatalf("bitbucket from file = %+v, %v", bb, err)
	}
	jira, err := JiraCredentials()
	if err != nil || jira.Token != "file-jira" {
		t.Fatalf("jira from file = %+v, %v", jira, err)
	}

	t.Setenv("ATLAS_API_TOKEN", "legacy-env")
	if bb, _ := BitbucketCredentials(); bb.Token != "legacy-env" {
		t.Fatalf("ATLAS_API_TOKEN: token = %q", bb.Token)
	}
	t.Setenv("ATLAS_BITBUCKET_TOKEN", "new-env")
	if bb, _ := BitbucketCredentials(); bb.Token != "new-env" {
		t.Fatalf("ATLAS_BITBUCKET_TOKEN should win: token = %q", bb.Token)
	}
}
