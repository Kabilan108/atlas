package update

import (
	"fmt"
	"runtime"
)

func AssetName(goos, goarch string) (string, error) {
	switch goos {
	case "linux", "darwin":
	default:
		return "", fmt.Errorf("unsupported operating system %q", goos)
	}

	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported architecture %q", goarch)
	}

	return fmt.Sprintf("atlas-%s-%s", goos, goarch), nil
}

func CurrentAssetName() (string, error) {
	return AssetName(runtime.GOOS, runtime.GOARCH)
}
