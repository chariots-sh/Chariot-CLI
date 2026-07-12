// Package update checks GitHub for newer chariot releases and, for
// non-Homebrew installs, downloads and installs them in place.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	RepoOwner = "chariots-sh"
	RepoName  = "Chariot-CLI"

	// checkInterval is how often the background check refreshes the cache.
	checkInterval = 24 * time.Hour

	maxDownloadSize = 200 << 20 // sanity bound on a release asset's size
)

// apiBaseURL is the GitHub API root, overridable so tests don't hit the real
// GitHub API.
func apiBaseURL() string {
	if v := os.Getenv("CHARIOT_UPDATE_API_URL"); v != "" {
		return v
	}
	return "https://api.github.com"
}

// Release is the subset of the GitHub releases API this package uses.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is one file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchLatest fetches the latest published release from GitHub.
func FetchLatest(ctx context.Context, client *http.Client) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBaseURL(), RepoOwner, RepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parsing release response: %w", err)
	}
	return &rel, nil
}

// IsNewer reports whether latest is a newer version than current ("v" prefix
// optional on either). A version that fails to parse compares as not-newer,
// so a malformed tag never falsely triggers an update prompt.
func IsNewer(current, latest string) bool {
	c, ok := parseVersion(current)
	if !ok {
		return false
	}
	l, ok := parseVersion(latest)
	if !ok {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// InstallMethod classifies how the running binary was likely installed, based
// on its resolved path. Homebrew-installed binaries must not be self-replaced:
// brew keeps its own checksums for files in the Cellar, and a self-update
// would just get clobbered (or flagged by `brew doctor`) on the next `brew
// upgrade`.
func InstallMethod(exePath string) string {
	lower := strings.ToLower(exePath)
	if strings.Contains(lower, "/cellar/") || strings.Contains(lower, "/homebrew/") || strings.Contains(lower, "linuxbrew") {
		return "brew"
	}
	return "binary"
}

// Cache is the on-disk state backing the background update check, stored at
// ~/.chariot/update-check.json.
type Cache struct {
	LastChecked time.Time `json:"last_checked"`
	Latest      string    `json:"latest_version"`
}

func cachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".chariot", "update-check.json"), nil
}

func loadCache() (*Cache, error) {
	p, err := cachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &Cache{}, nil
	}
	if err != nil {
		return nil, err
	}
	c := &Cache{}
	if err := json.Unmarshal(data, c); err != nil {
		// A corrupt cache shouldn't break every command that follows.
		return &Cache{}, nil
	}
	return c, nil
}

func saveCache(c *Cache) error {
	p, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// CheckInBackground returns the latest released version if it's newer than
// current, or "" if up to date. It never blocks a caller for longer than
// wait: a cached result (refreshed at most once per checkInterval) is
// returned immediately, and a stale cache triggers a refresh that this call
// waits for only up to wait before falling back to the cached value — the
// refresh itself keeps running and updates the cache for next time either way.
func CheckInBackground(current string, wait time.Duration) string {
	cache, err := loadCache()
	if err != nil {
		cache = &Cache{}
	}
	fallback := ""
	if cache.Latest != "" && IsNewer(current, cache.Latest) {
		fallback = cache.Latest
	}
	if !cache.LastChecked.IsZero() && time.Since(cache.LastChecked) < checkInterval {
		return fallback
	}

	done := make(chan string, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rel, err := FetchLatest(ctx, &http.Client{Timeout: 10 * time.Second})
		if err != nil {
			done <- ""
			return
		}
		_ = saveCache(&Cache{LastChecked: time.Now(), Latest: rel.TagName})
		done <- rel.TagName
	}()

	select {
	case latest := <-done:
		if latest != "" && IsNewer(current, latest) {
			return latest
		}
		return fallback
	case <-time.After(wait):
		return fallback
	}
}

// assetFor finds the release asset matching the archive name template in
// .goreleaser.yaml: <project>_<version>_<os>_<arch>.tar.gz.
func assetFor(rel *Release, goos, goarch string) (*Asset, error) {
	suffix := fmt.Sprintf("_%s_%s.tar.gz", goos, goarch)
	for i := range rel.Assets {
		if strings.HasSuffix(rel.Assets[i].Name, suffix) {
			return &rel.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("release %s has no build for %s/%s", rel.TagName, goos, goarch)
}

func checksumsAsset(rel *Release) (*Asset, error) {
	for i := range rel.Assets {
		if rel.Assets[i].Name == "checksums.txt" {
			return &rel.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("release %s is missing checksums.txt", rel.TagName)
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize))
}

// verifyChecksum checks data's sha256 against name's entry in checksums.txt
// (goreleaser's default "<hex>  <filename>" format, one per line).
func verifyChecksum(checksums []byte, name string, data []byte) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			if fields[0] != got {
				return fmt.Errorf("checksum mismatch for %s: got %s, want %s", name, got, fields[0])
			}
			return nil
		}
	}
	return fmt.Errorf("no checksum entry for %s in checksums.txt", name)
}

// extractBinary pulls the "chariot" file out of a downloaded tar.gz archive.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == "chariot" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("archive did not contain a chariot binary")
}

// Apply downloads the release build for the current OS/arch, verifies its
// checksum, and installs it over exePath. The replace is atomic: the new
// binary is written to a temp file in exePath's directory (so the rename
// below is same-filesystem) and swapped in with os.Rename, so exePath is
// never observably a half-written file.
func Apply(ctx context.Context, client *http.Client, rel *Release, exePath string) error {
	asset, err := assetFor(rel, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	sumsAsset, err := checksumsAsset(rel)
	if err != nil {
		return err
	}
	archive, err := download(ctx, client, asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", asset.Name, err)
	}
	sums, err := download(ctx, client, sumsAsset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	if err := verifyChecksum(sums, asset.Name, archive); err != nil {
		return err
	}
	bin, err := extractBinary(archive)
	if err != nil {
		return err
	}

	mode := os.FileMode(0o755)
	if info, err := os.Stat(exePath); err == nil {
		mode = info.Mode()
	}

	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".chariot-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file next to %s: %w", exePath, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		return fmt.Errorf("installing new binary over %s: %w", exePath, err)
	}
	return nil
}
