package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const runMainEnv = "ATLAS_TEST_RUN_MAIN"

// TestMain lets the tests run this binary as atlas itself, so they see the
// real process exit code.
func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		os.Args = append([]string{"atlas"}, os.Args[1:]...)
		main()
		return
	}
	os.Exit(m.Run())
}

// runAtlas runs atlas with args in an empty home and no ATLAS_* settings,
// so no real credentials or config are read and nothing reaches a network.
func runAtlas(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = []string{runMainEnv + "=1", "ATLAS_UPDATE_CHECK=0", "HOME=" + home, "XDG_CONFIG_HOME=" + home, "XDG_CACHE_HOME=" + home, "PATH=" + os.Getenv("PATH")}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, stdout.String(), stderr.String()
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("running atlas %v: %v", args, err)
	return 0, "", ""
}

func TestProcessExitCodes(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		want   int
		output string
	}{
		{[]string{"--help"}, 0, "Exit codes:"},
		{[]string{"bogus"}, 2, `unknown command "bogus"`},
		{[]string{"pr", "bogus"}, 2, `unknown command "bogus" for "atlas pr"`},
		{[]string{"branch", "list", "-R", "acme/widgets", "--nope"}, 2, "unknown flag"},
		{[]string{"branch", "list", "-R", "acme/widgets", "-L", "5000"}, 2, "over the maximum of 1000"},
		{[]string{"whoami"}, 3, "credentials"},
		{[]string{"jira", "whoami"}, 3, "Jira"},
	} {
		code, stdout, stderr := runAtlas(t, tc.args...)
		if code != tc.want || !strings.Contains(stdout+stderr, tc.output) {
			t.Errorf("atlas %v: exit %d, want %d; output lacks %q:\n%s%s", tc.args, code, tc.want, tc.output, stdout, stderr)
		}
	}
}
