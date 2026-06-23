package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultRepoOwner   = "kabilan108"
	DefaultRepoName    = "atlas"
	DefaultMetadataURL = "https://raw.githubusercontent.com/kabilan108/atlas/master/update.json"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Release struct {
	TagName string
	Assets  []ReleaseAsset
}

type ReleaseAsset struct {
	Name string
	URL  string
}

func FetchLatestRelease(client HTTPClient, owner, repo string) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "atlas-update")

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return Release{}, fmt.Errorf("failed to fetch latest release: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var githubRelease struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&githubRelease); err != nil {
		return Release{}, fmt.Errorf("failed to decode latest release: %w", err)
	}

	release := Release{TagName: strings.TrimPrefix(githubRelease.TagName, "v")}
	for _, asset := range githubRelease.Assets {
		release.Assets = append(release.Assets, ReleaseAsset{Name: asset.Name, URL: asset.BrowserDownloadURL})
	}
	return release, nil
}

func Download(client HTTPClient, url string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "atlas-update")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("failed to download %s: %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read download response: %w", err)
	}
	return data, nil
}

func AssetURL(release Release, assetName string) (string, error) {
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return asset.URL, nil
		}
	}
	return "", fmt.Errorf("release v%s does not include asset %s", release.TagName, assetName)
}
