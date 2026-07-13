//go:build windows

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const githubReleaseAPI = "https://api.github.com/repos/TinoA/claude-desktop-swap/releases/latest"

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func latestGitHubRelease(ctx context.Context) (githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleaseAPI, nil)
	if err != nil {
		return githubRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", ProductName)
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return githubRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubRelease{}, errors.New("GitHub release check returned a non-success status")
	}
	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if release.Draft || release.Prerelease || release.TagName == "" {
		return githubRelease{}, errors.New("no stable GitHub release available")
	}
	return release, nil
}

func updateAvailable(current, latest string) bool {
	if strings.EqualFold(strings.TrimSpace(current), "dev") {
		return latest != ""
	}
	local, localOK := parseVersion(current)
	remote, remoteOK := parseVersion(latest)
	if !localOK || !remoteOK {
		return false
	}
	for i := range local {
		if remote[i] != local[i] {
			return remote[i] > local[i]
		}
	}
	return false
}

func parseVersion(value string) ([3]int, bool) {
	var version [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "-", 2)[0]
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return version, false
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return version, false
		}
		version[i] = number
	}
	return version, true
}
