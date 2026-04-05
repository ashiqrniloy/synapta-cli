package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	repoOwner      = "synapta"
	repoName       = "synapta-cli"
	checkTimeout   = 1500 * time.Millisecond
	cacheTTL       = 6 * time.Hour
	updateCheckURL = "https://api.github.com/repos/%s/%s/releases/latest"
)

type cacheState struct {
	CheckedAt time.Time `json:"checkedAt"`
	LatestTag string    `json:"latestTag"`
}

type latestReleaseResponse struct {
	TagName string `json:"tag_name"`
}

func NotifyIfAvailable(currentVersion string, w io.Writer) {
	currentVersion = normalizeVersion(currentVersion)
	if currentVersion == "" || currentVersion == "dev" {
		return
	}

	latest, err := latestVersionWithCache()
	if err != nil {
		return
	}
	latest = normalizeVersion(latest)
	if latest == "" || latest == currentVersion {
		return
	}

	fmt.Fprintf(w, "\n⚠ Update available: %s → %s\n", currentVersion, latest)
	fmt.Fprintln(w, "  Update with:")
	fmt.Fprintf(w, "  curl -fsSL https://raw.githubusercontent.com/%s/%s/main/scripts/install.sh | sh\n\n", repoOwner, repoName)
}

func latestVersionWithCache() (string, error) {
	cachePath, err := cacheFilePath()
	if err != nil {
		return "", err
	}

	state, _ := readCache(cachePath)
	if !state.CheckedAt.IsZero() && time.Since(state.CheckedAt) < cacheTTL && state.LatestTag != "" {
		return state.LatestTag, nil
	}

	latest, err := fetchLatestReleaseTag()
	if err != nil {
		if state.LatestTag != "" {
			return state.LatestTag, nil
		}
		return "", err
	}

	_ = writeCache(cachePath, cacheState{
		CheckedAt: time.Now().UTC(),
		LatestTag: latest,
	})

	return latest, nil
}

func fetchLatestReleaseTag() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(updateCheckURL, repoOwner, repoName), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "synapta-update-checker")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github releases api returned status %d", resp.StatusCode)
	}

	var release latestReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("empty tag_name in GitHub response")
	}
	return release.TagName, nil
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func cacheFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".synapta", "update-cache.json"), nil
}

func readCache(path string) (cacheState, error) {
	var state cacheState
	f, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		return cacheState{}, err
	}
	return state, nil
}

func writeCache(path string, state cacheState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
