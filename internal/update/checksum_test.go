package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	t.Parallel()

	data := []byte("binary")
	sum := sha256.Sum256(data)
	checksums := []byte(fmt.Sprintf("%s  atlas-linux-amd64\n", hex.EncodeToString(sum[:])))

	if err := VerifyChecksum("atlas-linux-amd64", data, checksums); err != nil {
		t.Fatalf("VerifyChecksum() error = %v", err)
	}
}

func TestVerifyChecksumRejectsMismatch(t *testing.T) {
	t.Parallel()

	checksums := []byte("0000000000000000000000000000000000000000000000000000000000000000  atlas-linux-amd64\n")
	if err := VerifyChecksum("atlas-linux-amd64", []byte("binary"), checksums); err == nil {
		t.Fatal("VerifyChecksum() error = nil, want mismatch")
	}
}
