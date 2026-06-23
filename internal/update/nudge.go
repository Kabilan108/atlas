package update

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const NudgeTTL = 24 * time.Hour

type NudgeChecker struct {
	Client      HTTPClient
	MetadataURL string
	CachePath   string
	Now         func() time.Time
}

type Nudge struct {
	Latest   string
	Minimum  string
	Message  string
	Required bool
}

type cachedMetadata struct {
	CheckedAt time.Time `json:"checked_at"`
	Metadata  Metadata  `json:"metadata"`
}

func (checker NudgeChecker) Check(currentVersion string) (Nudge, error) {
	if os.Getenv("ATLAS_UPDATE_CHECK") == "0" {
		return Nudge{}, nil
	}
	if _, err := ParseVersion(currentVersion); err != nil {
		return Nudge{}, nil
	}

	now := time.Now
	if checker.Now != nil {
		now = checker.Now
	}

	metadata, err := checker.cached(now())
	if err != nil {
		metadata, err = checker.fetch()
		if err != nil {
			return Nudge{}, err
		}
		_ = checker.save(now(), metadata)
	}

	cmpLatest, err := CompareVersions(currentVersion, metadata.Latest)
	if err != nil {
		return Nudge{}, err
	}
	if cmpLatest >= 0 {
		return Nudge{}, nil
	}

	cmpMinimum, err := CompareVersions(currentVersion, metadata.Minimum)
	if err != nil {
		return Nudge{}, err
	}

	return Nudge{
		Latest:   metadata.Latest,
		Minimum:  metadata.Minimum,
		Message:  metadata.Message,
		Required: cmpMinimum < 0,
	}, nil
}

func (checker NudgeChecker) cached(now time.Time) (Metadata, error) {
	cachePath, err := checker.cachePath()
	if err != nil {
		return Metadata{}, err
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return Metadata{}, err
	}

	var cached cachedMetadata
	if err := json.Unmarshal(data, &cached); err != nil {
		return Metadata{}, err
	}
	if now.Sub(cached.CheckedAt) > NudgeTTL {
		return Metadata{}, fmt.Errorf("cached metadata is stale")
	}
	if _, err := ParseVersion(cached.Metadata.Latest); err != nil {
		return Metadata{}, err
	}
	if _, err := ParseVersion(cached.Metadata.Minimum); err != nil {
		return Metadata{}, err
	}
	return cached.Metadata, nil
}

func (checker NudgeChecker) fetch() (Metadata, error) {
	url := checker.MetadataURL
	if url == "" {
		url = DefaultMetadataURL
	}
	data, err := Download(checker.Client, url)
	if err != nil {
		return Metadata{}, err
	}
	return DecodeMetadata(bytes.NewReader(data))
}

func (checker NudgeChecker) save(now time.Time, metadata Metadata) error {
	cachePath, err := checker.cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(cachedMetadata{CheckedAt: now, Metadata: metadata})
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath, data, 0600)
}

func (checker NudgeChecker) cachePath() (string, error) {
	if checker.CachePath != "" {
		return checker.CachePath, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "atlas", "update-check.json"), nil
}

func NewHTTPClient(timeout time.Duration) HTTPClient {
	return &http.Client{Timeout: timeout}
}
