package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type InstallCheck struct {
	Path string
}

func CurrentExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve current executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve executable symlinks: %w", err)
	}
	return resolved, nil
}

func (check InstallCheck) Validate() error {
	path := filepath.Clean(check.Path)
	if path == "." || path == string(filepath.Separator) {
		return fmt.Errorf("invalid executable path %q", check.Path)
	}
	if strings.HasPrefix(path, "/nix/store/") {
		return errors.New("atlas is installed in /nix/store and cannot update itself")
	}
	if strings.HasPrefix(path, "/opt/homebrew/") || strings.Contains(path, "/Cellar/") {
		return errors.New("atlas appears to be managed by Homebrew and cannot update itself")
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat current executable: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("current executable path %s is a directory", path)
	}
	if info.Mode().Perm()&0200 == 0 {
		return fmt.Errorf("current executable %s is not writable", path)
	}

	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".atlas-update-check-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to close temporary file: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return fmt.Errorf("failed to remove temporary file: %w", err)
	}

	return nil
}

func ReplaceExecutable(path string, data []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".atlas-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file in %s: %w", dir, err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to write updated binary: %w", err)
	}
	if err := temp.Chmod(0755); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to mark updated binary executable: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to close updated binary: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("failed to replace current binary: %w", err)
	}

	return nil
}
