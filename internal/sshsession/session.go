// Package sshsession manages the local SSH credentials `chariot ssh` needs: an
// ephemeral keypair, a short-lived Chariot-signed certificate, and a pinned
// known_hosts entry — then builds the argv to hand off to the system `ssh`.
//
// Nothing here touches GCP or Kubernetes: the client only ever sees a hostname,
// a certificate the backend signs, and the public host CA it pins.
package sshsession

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
)

// Refresh a cert once it has less than this remaining, so a session never dies
// mid-connect on an expiry we could have seen coming.
const renewBefore = 5 * time.Minute

// certIssuer is the slice of the API client this package needs (satisfied by
// *api.Client); an interface keeps the manager unit-testable without a server.
type certIssuer interface {
	IssueSSHCert(ctx context.Context, publicKey, agentSlug string) (*api.SSHCertResponse, error)
	SSHCA(ctx context.Context) (*api.SSHCAResponse, error)
}

// Manager owns the ~/.chariot/ssh credential directory for one gateway host.
type Manager struct {
	dir  string
	host string
}

// NewManager roots credentials under dir (typically ~/.chariot/ssh) for the
// given gateway hostname.
func NewManager(dir, host string) *Manager {
	return &Manager{dir: dir, host: host}
}

// Creds are the on-disk file paths the system ssh is pointed at.
type Creds struct {
	KeyPath        string
	CertPath       string
	KnownHostsPath string
}

func (m *Manager) keyPath() string        { return filepath.Join(m.dir, "id_ed25519") }
func (m *Manager) certPath() string       { return filepath.Join(m.dir, "id_ed25519-cert.pub") }
func (m *Manager) knownHostsPath() string { return filepath.Join(m.dir, "known_hosts") }

// Ensure returns valid credentials, minting a fresh keypair + certificate (and
// refreshing the pinned host CA) whenever the cached cert is missing or within
// renewBefore of expiry. The cert is account-wide (agentSlug ""), so one cached
// credential opens any agent the account owns; the gateway enforces per-agent
// ownership at resolve time.
func (m *Manager) Ensure(ctx context.Context, client certIssuer) (Creds, error) {
	creds := Creds{
		KeyPath:        m.keyPath(),
		CertPath:       m.certPath(),
		KnownHostsPath: m.knownHostsPath(),
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return creds, err
	}
	if m.certFresh() && fileExists(creds.KeyPath) && fileExists(creds.KnownHostsPath) {
		return creds, nil
	}
	if err := m.mint(ctx, client, creds); err != nil {
		return creds, err
	}
	return creds, nil
}

// certFresh reports whether the cached cert exists and is valid beyond the
// renewBefore window.
func (m *Manager) certFresh() bool {
	exp, err := m.cachedCertExpiry()
	if err != nil {
		return false
	}
	return time.Now().Add(renewBefore).Before(exp)
}

func (m *Manager) cachedCertExpiry() (time.Time, error) {
	data, err := os.ReadFile(m.certPath())
	if err != nil {
		return time.Time{}, err
	}
	cert, err := parseCertificate(data)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(cert.ValidBefore), 0), nil
}

// mint generates a keypair, has the backend sign a certificate, pins the host
// CA, and writes all three files atomically enough for a CLI (0600 key).
func (m *Manager) mint(ctx context.Context, client certIssuer, creds Creds) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return err
	}
	authorizedPub := string(ssh.MarshalAuthorizedKey(sshPub))

	resp, err := client.IssueSSHCert(ctx, authorizedPub, "")
	if err != nil {
		return err
	}
	if _, err := parseCertificate([]byte(resp.Certificate)); err != nil {
		return fmt.Errorf("backend returned an unparseable certificate: %w", err)
	}

	ca, err := client.SSHCA(ctx)
	if err != nil {
		return err
	}

	pemBlock, err := ssh.MarshalPrivateKey(priv, "chariot")
	if err != nil {
		return err
	}
	if err := writeFile(creds.KeyPath, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		return err
	}
	if err := writeFile(creds.CertPath, ensureTrailingNewline(resp.Certificate), 0o644); err != nil {
		return err
	}
	if err := writeFile(creds.KnownHostsPath, m.knownHostsLine(ca.HostCAPublicKey), 0o644); err != nil {
		return err
	}
	return nil
}

// knownHostsLine pins the host CA for both the default port and the :443 form,
// so `--port 443` still validates without a TOFU prompt.
func (m *Manager) knownHostsLine(hostCAPublicKey string) []byte {
	patterns := m.host + ",[" + m.host + "]:443"
	line := "@cert-authority " + patterns + " " + strings.TrimSpace(hostCAPublicKey) + "\n"
	return []byte(line)
}

// SSHArgs builds the argv for the system ssh binary (pure — unit-tested). The
// login user is the agent slug; a non-empty remoteCmd runs non-interactively.
func SSHArgs(creds Creds, host, slug string, port int, remoteCmd []string) []string {
	args := []string{
		"-i", creds.KeyPath,
		"-o", "CertificateFile=" + creds.CertPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "UserKnownHostsFile=" + creds.KnownHostsPath,
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "PasswordAuthentication=no",
		"-p", strconv.Itoa(port),
		slug + "@" + host,
	}
	return append(args, remoteCmd...)
}

func parseCertificate(data []byte) (*ssh.Certificate, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, err
	}
	cert, ok := pub.(*ssh.Certificate)
	if !ok {
		return nil, fmt.Errorf("not an SSH certificate")
	}
	return cert, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func ensureTrailingNewline(s string) []byte {
	if strings.HasSuffix(s, "\n") {
		return []byte(s)
	}
	return []byte(s + "\n")
}
