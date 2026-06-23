package update

import (
	"bytes"
	"os"
	"regexp"
	"testing"
)

func TestDecodeMetadata(t *testing.T) {
	t.Parallel()

	data := []byte(`{"latest":"0.0.10","minimum":"0.0.9","message":"Run atlas update"}`)
	metadata, err := DecodeMetadata(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeMetadata() error = %v", err)
	}
	if metadata.Latest != "0.0.10" || metadata.Minimum != "0.0.9" {
		t.Fatalf("DecodeMetadata() = %#v", metadata)
	}
}

func TestDecodeMetadataRejectsMinimumNewerThanLatest(t *testing.T) {
	t.Parallel()

	data := []byte(`{"latest":"0.0.10","minimum":"0.0.11","message":"Run atlas update"}`)
	if _, err := DecodeMetadata(bytes.NewReader(data)); err == nil {
		t.Fatal("DecodeMetadata() error = nil, want error")
	}
}

func TestUpdateMetadataMatchesFlakeVersion(t *testing.T) {
	t.Parallel()

	metadataData, err := os.ReadFile("../../update.json")
	if err != nil {
		t.Fatalf("read update.json: %v", err)
	}
	metadata, err := DecodeMetadata(bytes.NewReader(metadataData))
	if err != nil {
		t.Fatalf("DecodeMetadata(update.json) error = %v", err)
	}

	flakeData, err := os.ReadFile("../../flake.nix")
	if err != nil {
		t.Fatalf("read flake.nix: %v", err)
	}
	re := regexp.MustCompile(`version = "([^"]+)"`)
	matches := re.FindSubmatch(flakeData)
	if len(matches) != 2 {
		t.Fatal("flake.nix version not found")
	}

	flakeVersion := string(matches[1])
	if metadata.Latest != flakeVersion {
		t.Fatalf("update.json latest = %s, want flake version %s", metadata.Latest, flakeVersion)
	}
}
