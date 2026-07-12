package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chariots-sh/Chariot-CLI/internal/update"
)

// updateServer serves a fake GitHub "latest release" API response pointing
// at a self-contained fake tar.gz + checksums.txt for the current platform.
func updateServer(t *testing.T, tagName string, binContents []byte) *httptest.Server {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "chariot", Mode: 0o755, Size: int64(len(binContents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binContents); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	assetName := "chariot_" + tagName + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	checksums := hex.EncodeToString(sum[:]) + "  " + assetName + "\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+update.RepoOwner+"/"+update.RepoName+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(update.Release{
			TagName: tagName,
			Assets: []update.Asset{
				{Name: assetName, BrowserDownloadURL: base + "/archive"},
				{Name: "checksums.txt", BrowserDownloadURL: base + "/checksums"},
			},
		})
	})
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(checksums)) })

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("CHARIOT_UPDATE_API_URL", srv.URL)
	return srv
}

func TestUpdateDevBuildSkipsCheck(t *testing.T) {
	Version = "dev"
	t.Cleanup(func() { Version = "dev" })

	got := runCLI(t, "", "update")
	if got.err != nil {
		t.Fatalf("update: %v", got.err)
	}
	mustContain(t, got.stdout, "skipping the update check", "stdout")
}

func TestUpdateAlreadyUpToDate(t *testing.T) {
	Version = "v1.0.0"
	t.Cleanup(func() { Version = "dev" })
	updateServer(t, "v1.0.0", nil)

	got := runCLI(t, "", "update")
	if got.err != nil {
		t.Fatalf("update: %v", got.err)
	}
	mustContain(t, got.stdout, "up to date", "stdout")
}

func TestUpdateCheckOnlyReportsNewVersion(t *testing.T) {
	Version = "v1.0.0"
	t.Cleanup(func() { Version = "dev" })
	updateServer(t, "v2.0.0", []byte("irrelevant"))

	got := runCLI(t, "", "update", "--check")
	if got.err != nil {
		t.Fatalf("update --check: %v", got.err)
	}
	mustContain(t, got.stdout, "v2.0.0", "stdout")
	mustContain(t, got.stdout, "chariot update", "stdout")
	mustNotContain(t, got.stdout, "updating chariot", "stdout")
}

func TestUpdateInstallsNewerRelease(t *testing.T) {
	Version = "v1.0.0"
	t.Cleanup(func() { Version = "dev" })
	updateServer(t, "v2.0.0", []byte("brand-new-binary"))

	exePath := filepath.Join(t.TempDir(), "chariot")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	executablePath = func() (string, error) { return exePath, nil }
	t.Cleanup(func() { executablePath = func() (string, error) { return os.Executable() } })

	got := runCLI(t, "", "update")
	if got.err != nil {
		t.Fatalf("update: %v", got.err)
	}
	mustContain(t, got.stdout, "updated to v2.0.0", "stdout")

	installed, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "brand-new-binary" {
		t.Fatalf("exePath contents = %q, want the new binary", installed)
	}
}

func TestUpdateDefersToBrewWithoutHittingNetwork(t *testing.T) {
	Version = "v1.0.0"
	t.Cleanup(func() { Version = "dev" })
	// Point at an address nothing is listening on: if the brew check didn't
	// short-circuit before the network call, this would fail/hang instead of
	// producing the brew message.
	t.Setenv("CHARIOT_UPDATE_API_URL", "http://127.0.0.1:1")

	brewPath := "/opt/homebrew/Cellar/chariot/1.0.0/bin/chariot"
	executablePath = func() (string, error) { return brewPath, nil }
	t.Cleanup(func() { executablePath = func() (string, error) { return os.Executable() } })

	got := runCLI(t, "", "update")
	if got.err != nil {
		t.Fatalf("update: %v", got.err)
	}
	mustContain(t, got.stdout, "brew upgrade chariot", "stdout")
}
