package cmd

import (
	"net/http"
	"strings"
	"testing"
)

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
