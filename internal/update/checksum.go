package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func VerifyChecksum(assetName string, data []byte, checksums []byte) error {
	want, err := checksumForAsset(assetName, string(checksums))
	if err != nil {
		return err
	}

	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}

	return nil
}

func checksumForAsset(assetName, checksums string) (string, error) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[len(fields)-1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}
