package api

import (
	"context"
	"net/http"
	"time"
)

// SSHCertRequest asks the backend to sign an ephemeral public key. AgentSlug is
// optional: when set, the returned certificate is scoped to that one agent.
type SSHCertRequest struct {
	PublicKey string `json:"public_key"`
	AgentSlug string `json:"agent_slug,omitempty"`
}

// SSHCertResponse is a signed OpenSSH user certificate plus its metadata.
type SSHCertResponse struct {
	Certificate     string    `json:"certificate"`
	KeyID           string    `json:"key_id"`
	Serial          uint64    `json:"serial"`
	Principals      []string  `json:"principals"`
	ExpiresAt       time.Time `json:"expires_at"`
	UserCAPublicKey string    `json:"user_ca_public_key"`
}

// SSHCAResponse is the pair of CA public keys the client needs: the host CA to
// pin (so there is no fingerprint prompt) and the user CA for reference.
type SSHCAResponse struct {
	UserCAPublicKey string `json:"user_ca_public_key"`
	HostCAPublicKey string `json:"host_ca_public_key"`
}

// IssueSSHCert signs publicKey with the Chariot user CA. Pass agentSlug to scope
// the cert to a single agent, or "" for an account-wide cert.
func (c *Client) IssueSSHCert(ctx context.Context, publicKey, agentSlug string) (*SSHCertResponse, error) {
	out := &SSHCertResponse{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/ssh/certs",
		SSHCertRequest{PublicKey: publicKey, AgentSlug: agentSlug}, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SSHCA fetches the CA public keys (unauthenticated: they are public).
func (c *Client) SSHCA(ctx context.Context) (*SSHCAResponse, error) {
	out := &SSHCAResponse{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/ssh/ca", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}
