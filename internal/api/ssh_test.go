package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestIssueSSHCertScopedToAgent(t *testing.T) {
	var body SSHCertRequest
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ssh/certs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("cert issuance must be authenticated")
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"certificate":"ssh-cert-data","key_id":"k1","serial":42,
			"principals":["agent-3"],"expires_at":"2030-01-01T00:00:00Z",
			"user_ca_public_key":"user-ca"}`))
	})
	defer srv.Close()

	got, err := c.IssueSSHCert(context.Background(), "ssh-ed25519 AAAA", "agent-3")
	if err != nil {
		t.Fatal(err)
	}
	if body.PublicKey != "ssh-ed25519 AAAA" || body.AgentSlug != "agent-3" {
		t.Errorf("unexpected request body: %+v", body)
	}
	if got.Certificate != "ssh-cert-data" || got.Serial != 42 {
		t.Errorf("unexpected cert: %+v", got)
	}
	if len(got.Principals) != 1 || got.Principals[0] != "agent-3" {
		t.Errorf("principals = %v", got.Principals)
	}
	if !got.ExpiresAt.Equal(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expires_at = %v", got.ExpiresAt)
	}
}

// An empty agent slug means an account-wide cert; the field is omitempty, so
// it must not appear on the wire at all.
func TestIssueSSHCertOmitsEmptyAgentSlug(t *testing.T) {
	var raw map[string]any
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(`{"certificate":"c"}`))
	})
	defer srv.Close()

	if _, err := c.IssueSSHCert(context.Background(), "pk", ""); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["agent_slug"]; present {
		t.Errorf("agent_slug should be omitted, got %v", raw["agent_slug"])
	}
}

func TestSSHCAReturnsBothKeys(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/ssh/ca" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"user_ca_public_key":"user-ca","host_ca_public_key":"host-ca"}`))
	})
	defer srv.Close()

	got, err := c.SSHCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.UserCAPublicKey != "user-ca" || got.HostCAPublicKey != "host-ca" {
		t.Errorf("unexpected CA response: %+v", got)
	}
}

func TestSSHCertErrorSurfaced(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":"agent not in your namespace"}`))
	})
	defer srv.Close()

	_, err := c.IssueSSHCert(context.Background(), "pk", "someone-elses-agent")
	if err == nil {
		t.Fatal("want an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("want a 403 APIError, got %v", err)
	}
	if apiErr.Detail != "agent not in your namespace" {
		t.Errorf("detail = %q", apiErr.Detail)
	}
}
