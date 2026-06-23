package update

import (
	"encoding/json"
	"fmt"
	"io"
)

type Metadata struct {
	Latest  string `json:"latest"`
	Minimum string `json:"minimum"`
	Message string `json:"message"`
}

func DecodeMetadata(r io.Reader) (Metadata, error) {
	var metadata Metadata
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return Metadata{}, fmt.Errorf("failed to decode update metadata: %w", err)
	}
	if metadata.Latest == "" {
		return Metadata{}, fmt.Errorf("update metadata latest version is required")
	}
	if metadata.Minimum == "" {
		return Metadata{}, fmt.Errorf("update metadata minimum version is required")
	}
	if _, err := ParseVersion(metadata.Latest); err != nil {
		return Metadata{}, fmt.Errorf("invalid latest version: %w", err)
	}
	if _, err := ParseVersion(metadata.Minimum); err != nil {
		return Metadata{}, fmt.Errorf("invalid minimum version: %w", err)
	}
	cmp, err := CompareVersions(metadata.Minimum, metadata.Latest)
	if err != nil {
		return Metadata{}, err
	}
	if cmp > 0 {
		return Metadata{}, fmt.Errorf("minimum version %s is newer than latest version %s", metadata.Minimum, metadata.Latest)
	}

	return metadata, nil
}
