package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/update"
)

// seedUpdateCache writes ~/.chariot/update-check.json directly, standing in
// for a previous run's background check.
func seedUpdateCache(t *testing.T, latest string, checkedAt time.Time) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".chariot")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(update.Cache{LastChecked: checkedAt, Latest: latest})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "update-check.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVersionPrintsVersion(t *testing.T) {
	Version = "1.2.3"
	t.Cleanup(func() { Version = "dev" })

	got := runCLI(t, "", "version")
	if got.err != nil {
		t.Fatalf("version: %v", got.err)
	}
	if strings.TrimSpace(got.stdout) != "chariot 1.2.3" {
		t.Fatalf("stdout = %q", got.stdout)
	}
}

// Every authed command must fail with an actionable hint rather than a bare
// 401 from the backend when there is no session token on disk.
func TestAuthedCommandsRequireLogin(t *testing.T) {
	for _, args := range [][]string{
		{"account"},
		{"list"},
		{"images"},
		{"models"},
		{"deploy", "--count", "1"},
		{"delete", "agent-1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			logout(t)
			got := runCLI(t, "", args...)
			if got.err == nil {
				t.Fatalf("want a not-logged-in error, got none (stdout=%q)", got.stdout)
			}
			mustContain(t, got.err.Error(), "chariot login", "error")
		})
	}
}

// authedClient reads the session token off disk and hands it to the API client
// as a bearer token.
func TestAuthedClientSendsBearerToken(t *testing.T) {
	var gotAuth string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"email":"a@b.c","status":"active","credit_dollars":12.5,"model":"m"}`))
	})

	got := runCLI(t, "", "account")
	if got.err != nil {
		t.Fatalf("account: %v", got.err)
	}
	if gotAuth != "Bearer session-jwt" {
		t.Errorf("Authorization = %q, want Bearer session-jwt", gotAuth)
	}
	mustContain(t, got.stdout, "credits   : $12.50", "stdout")
}

// A non-2xx from the backend surfaces the backend's detail message.
func TestBackendErrorSurfacesDetail(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"detail":"out of credits"}`))
	})

	got := runCLI(t, "", "account")
	if got.err == nil {
		t.Fatal("want an error")
	}
	mustContain(t, got.err.Error(), "out of credits", "error")
	mustContain(t, got.err.Error(), "402", "error")
}

// notifyTestSetup points the background update check at an unreachable
// address (the cache is always fresh in these tests, so it must never be
// dialed) and re-enables the check for the duration of the test.
func notifyTestSetup(t *testing.T, current string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CHARIOT_UPDATE_API_URL", "http://127.0.0.1:1")
	Version = current
	t.Cleanup(func() { Version = "dev" })
	disableAutoUpdateCheck = false
	t.Cleanup(func() { disableAutoUpdateCheck = true })
}

func TestUpdateNoticePrintedWhenCachedVersionIsNewer(t *testing.T) {
	notifyTestSetup(t, "v1.0.0")
	seedUpdateCache(t, "v2.0.0", time.Now())

	got := runCLI(t, "", "api")
	if got.err != nil {
		t.Fatalf("api: %v", got.err)
	}
	mustContain(t, got.stderr, "v2.0.0", "stderr")
	mustContain(t, got.stderr, "chariot update", "stderr")
}

func TestUpdateNoticeSilentWhenUpToDate(t *testing.T) {
	notifyTestSetup(t, "v1.0.0")
	seedUpdateCache(t, "v1.0.0", time.Now())

	got := runCLI(t, "", "api")
	if got.err != nil {
		t.Fatalf("api: %v", got.err)
	}
	mustNotContain(t, got.stderr, "new version", "stderr")
}

// version's own output already talks about the version, and it has no reason
// to hit the network (unlike `update`, whose skip-check is exercised by
// TestUpdateDefersToBrewWithoutHittingNetwork instead), so it's the cleanest
// case to prove updateNoticeSkip works.
func TestUpdateNoticeSkippedForVersionCommand(t *testing.T) {
	notifyTestSetup(t, "v1.0.0")
	seedUpdateCache(t, "v2.0.0", time.Now())

	got := runCLI(t, "", "version")
	if got.err != nil {
		t.Fatalf("version: %v", got.err)
	}
	mustNotContain(t, got.stderr, "new version", "stderr")
}
