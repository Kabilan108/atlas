package update

import (
	"fmt"
)

type Updater struct {
	Client HTTPClient
	Owner  string
	Repo   string
	GOOS   string
	GOARCH string
}

type Result struct {
	CurrentVersion string
	LatestVersion  string
	Updated        bool
}

func (u Updater) Update(currentVersion, executablePath string) (Result, error) {
	if _, err := ParseVersion(currentVersion); err != nil {
		return Result{}, fmt.Errorf("cannot update atlas because current version %q is not a release version", currentVersion)
	}

	owner := u.Owner
	if owner == "" {
		owner = DefaultRepoOwner
	}
	repo := u.Repo
	if repo == "" {
		repo = DefaultRepoName
	}
	goos := u.GOOS
	goarch := u.GOARCH
	var assetName string
	var err error
	if goos == "" && goarch == "" {
		assetName, err = CurrentAssetName()
	} else {
		assetName, err = AssetName(goos, goarch)
	}
	if err != nil {
		return Result{}, err
	}

	release, err := FetchLatestRelease(u.Client, owner, repo)
	if err != nil {
		return Result{}, err
	}
	if _, err := ParseVersion(release.TagName); err != nil {
		return Result{}, fmt.Errorf("latest release has invalid tag %q: %w", release.TagName, err)
	}

	result := Result{CurrentVersion: currentVersion, LatestVersion: release.TagName}
	cmp, err := CompareVersions(currentVersion, release.TagName)
	if err != nil {
		return Result{}, err
	}
	if cmp >= 0 {
		return result, nil
	}

	if executablePath == "" {
		executablePath, err = CurrentExecutablePath()
		if err != nil {
			return Result{}, err
		}
	}
	if err := (InstallCheck{Path: executablePath}).Validate(); err != nil {
		return Result{}, err
	}

	binaryURL, err := AssetURL(release, assetName)
	if err != nil {
		return Result{}, err
	}
	checksumsURL, err := AssetURL(release, "checksums.txt")
	if err != nil {
		return Result{}, err
	}

	binary, err := Download(u.Client, binaryURL)
	if err != nil {
		return Result{}, err
	}
	checksums, err := Download(u.Client, checksumsURL)
	if err != nil {
		return Result{}, err
	}
	if err := VerifyChecksum(assetName, binary, checksums); err != nil {
		return Result{}, err
	}
	if err := ReplaceExecutable(executablePath, binary); err != nil {
		return Result{}, err
	}

	result.Updated = true
	return result, nil
}
