package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"1.2.3", "1.2.4", true},
		{"v1.2.3", "v1.2.4", true},
		{"1.2.3", "1.3.0", true},
		{"1.2.3", "2.0.0", true},
		{"1.2.4", "1.2.3", false},
		{"1.2.3", "1.2.3", false},
		{"1.2", "1.2.1", true},
		{"dev", "1.0.0", false},
		{"1.0.0", "not-a-version", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestInstallMethod(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/opt/homebrew/Cellar/chariot/1.2.3/bin/chariot", "brew"},
		{"/home/linuxbrew/.linuxbrew/bin/chariot", "brew"},
		{"/usr/local/Cellar/chariot/1.2.3/bin/chariot", "brew"},
		{"/home/user/go/bin/chariot", "binary"},
		{"/usr/local/bin/chariot", "binary"},
	}
	for _, c := range cases {
		if got := InstallMethod(c.path); got != c.want {
			t.Errorf("InstallMethod(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestFetchLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Immortal-Protocols/Chariot-CLI/releases/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Release{
			TagName: "v9.9.9",
			Assets:  []Asset{{Name: "chariot_9.9.9_linux_amd64.tar.gz", BrowserDownloadURL: "http://example.com/a"}},
		})
	}))
	defer srv.Close()
	t.Setenv("CHARIOT_UPDATE_API_URL", srv.URL)

	rel, err := FetchLatest(context.Background(), srv.Client())
	if err != nil {
		t.Fatalf("FetchLatest: %v", err)
	}
	if rel.TagName != "v9.9.9" {
		t.Fatalf("TagName = %q", rel.TagName)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Name != "chariot_9.9.9_linux_amd64.tar.gz" {
		t.Fatalf("Assets = %+v", rel.Assets)
	}
}

func TestFetchLatestNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()
	t.Setenv("CHARIOT_UPDATE_API_URL", srv.URL)

	if _, err := FetchLatest(context.Background(), srv.Client()); err == nil {
		t.Fatal("want an error")
	}
}

// buildArchive makes a valid gzipped tar containing a single "chariot" file
// with the given contents, mirroring what goreleaser publishes.
func buildArchive(t *testing.T, contents []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "chariot", Mode: 0o755, Size: int64(len(contents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestApplyDownloadsVerifiesAndInstalls(t *testing.T) {
	binContents := []byte("new-chariot-binary-contents")
	archive := buildArchive(t, binContents)
	assetName := "chariot_9.9.9_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	checksums := sha256Hex(archive) + "  " + assetName + "\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(checksums)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel := &Release{
		TagName: "v9.9.9",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/archive"},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums"},
		},
	}

	dir := t.TempDir()
	exePath := filepath.Join(dir, "chariot")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Apply(context.Background(), srv.Client(), rel, exePath); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binContents) {
		t.Fatalf("installed contents = %q, want %q", got, binContents)
	}
	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}

	// No leftover temp file next to the installed binary.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1 (just chariot): %+v", len(entries), entries)
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	archive := buildArchive(t, []byte("payload"))
	assetName := "chariot_9.9.9_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"

	mux := http.NewServeMux()
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("deadbeef  " + assetName + "\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel := &Release{
		TagName: "v9.9.9",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/archive"},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums"},
		},
	}
	exePath := filepath.Join(t.TempDir(), "chariot")
	if err := os.WriteFile(exePath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Apply(context.Background(), srv.Client(), rel, exePath)
	if err == nil {
		t.Fatal("want a checksum mismatch error")
	}

	got, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old" {
		t.Fatalf("exePath was modified despite the checksum failure: %q", got)
	}
}

func TestApplyMissingAssetForPlatform(t *testing.T) {
	rel := &Release{
		TagName: "v9.9.9",
		Assets: []Asset{
			{Name: "chariot_9.9.9_plan9_amd64.tar.gz", BrowserDownloadURL: "http://example.com/a"},
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/c"},
		},
	}
	err := Apply(context.Background(), http.DefaultClient, rel, filepath.Join(t.TempDir(), "chariot"))
	if err == nil {
		t.Fatal("want an error for a missing platform asset")
	}
}

func TestCheckInBackgroundUsesFreshCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveCache(&Cache{LastChecked: time.Now(), Latest: "v5.0.0"}); err != nil {
		t.Fatal(err)
	}
	// No server configured: a network call here would fail/hang, proving the
	// fresh cache short-circuits it.
	t.Setenv("CHARIOT_UPDATE_API_URL", "http://127.0.0.1:1")

	got := CheckInBackground("1.0.0", 50*time.Millisecond)
	if got != "v5.0.0" {
		t.Fatalf("got %q, want v5.0.0", got)
	}
}

func TestCheckInBackgroundNoNoticeWhenUpToDate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveCache(&Cache{LastChecked: time.Now(), Latest: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHARIOT_UPDATE_API_URL", "http://127.0.0.1:1")

	got := CheckInBackground("1.0.0", 50*time.Millisecond)
	if got != "" {
		t.Fatalf("got %q, want empty (already up to date)", got)
	}
}

func TestCheckInBackgroundRefreshesStaleCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveCache(&Cache{LastChecked: time.Now().Add(-48 * time.Hour), Latest: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Release{TagName: "v2.0.0"})
	}))
	defer srv.Close()
	t.Setenv("CHARIOT_UPDATE_API_URL", srv.URL)

	got := CheckInBackground("1.0.0", 2*time.Second)
	if got != "v2.0.0" {
		t.Fatalf("got %q, want v2.0.0", got)
	}

	c, err := loadCache()
	if err != nil {
		t.Fatal(err)
	}
	if c.Latest != "v2.0.0" {
		t.Fatalf("cache not refreshed: %+v", c)
	}
}
