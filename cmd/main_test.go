package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/config"
)

// The cmd package hangs every command off a single global rootCmd whose flags
// bind to package-level vars. Cobra does not restore those vars to their
// defaults between runs, so a flag set by one test would leak into the next.
// resetFlags puts them back; runCLI calls it before every invocation.
func resetFlags() {
	deployCount, deployEndpoint, deployModel, deployImage = 0, "", "", ""
	listLimit, listAll = 50, false
	deleteYes = false
	imagePushTarball, imagePushReplace, imagePushPodSize = "", false, "small"
	imageShareWith, imageUnshareWith = "", ""
	imageAcceptAlias, imageAcceptFrom = "", ""
	demoSendToken = ""
	demoWatchToken, demoWatchInterval, demoWatchFromNow = "", 2*time.Second, false
	sshHost, sshPort, sshConfig = defaultSSHHost, 22, false
}

// result is the observable outcome of one CLI invocation.
type result struct {
	stdout string
	stderr string
	err    error
}

// runCLI executes the real root command with args, capturing both streams.
// stdin feeds commands that prompt (pass "" when none is expected).
func runCLI(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	resetFlags()

	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetIn(strings.NewReader(stdin))
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetIn(nil)
		rootCmd.SetArgs(nil)
	})

	err := rootCmd.ExecuteContext(context.Background())
	return result{stdout: out.String(), stderr: errOut.String(), err: err}
}

// login points the CLI at a fake backend: an isolated HOME holding a config
// with a session token, and CHARIOT_API_URL aimed at srv. Commands that call
// authedClient() then talk to the handler instead of the real backend.
func login(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("CHARIOT_API_URL", srv.URL)
	if err := config.Save(&config.Config{Token: "session-jwt"}); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	return srv
}

// logout gives the CLI an empty HOME, so authedClient() sees no session token.
func logout(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func mustContain(t *testing.T, got, want, label string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: missing %q in:\n%s", label, want, got)
	}
}

func mustNotContain(t *testing.T, got, unwanted, label string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Errorf("%s: unexpected %q in:\n%s", label, unwanted, got)
	}
}
