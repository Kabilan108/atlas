package update

import "testing"

func TestInstallCheckRejectsManagedPaths(t *testing.T) {
	t.Parallel()

	tests := []string{
		"/nix/store/abc-atlas/bin/atlas",
		"/opt/homebrew/bin/atlas",
		"/usr/local/Cellar/atlas/0.0.10/bin/atlas",
	}

	for _, path := range tests {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			if err := (InstallCheck{Path: path}).Validate(); err == nil {
				t.Fatal("InstallCheck.Validate() error = nil, want error")
			}
		})
	}
}
