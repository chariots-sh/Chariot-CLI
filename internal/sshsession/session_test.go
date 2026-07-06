package sshsession

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
)

// fakeIssuer signs certs with an in-test CA, mirroring the backend.
type fakeIssuer struct {
	caSigner ssh.Signer
	caPub    string
	validFor time.Duration
	issued   int
}

func newFakeIssuer(t *testing.T, validFor time.Duration) *fakeIssuer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeIssuer{caSigner: s, caPub: string(ssh.MarshalAuthorizedKey(s.PublicKey())), validFor: validFor}
}

func (f *fakeIssuer) IssueSSHCert(_ context.Context, publicKey, _ string) (*api.SSHCertResponse, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	cert := &ssh.Certificate{
		Key:             pub,
		Serial:          7,
		CertType:        ssh.UserCert,
		KeyId:           "test",
		ValidPrincipals: []string{"acct:acc-1"},
		ValidAfter:      uint64(now.Add(-time.Minute).Unix()),
		ValidBefore:     uint64(now.Add(f.validFor).Unix()),
	}
	if err := cert.SignCert(rand.Reader, f.caSigner); err != nil {
		return nil, err
	}
	f.issued++
	return &api.SSHCertResponse{
		Certificate: string(ssh.MarshalAuthorizedKey(cert)),
		ExpiresAt:   now.Add(f.validFor),
	}, nil
}

func (f *fakeIssuer) SSHCA(_ context.Context) (*api.SSHCAResponse, error) {
	return &api.SSHCAResponse{UserCAPublicKey: "ssh-ed25519 AAAAuser", HostCAPublicKey: f.caPub}, nil
}

func TestEnsureMintsThenCaches(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, "ssh.chariots.sh")
	iss := newFakeIssuer(t, time.Hour)

	creds, err := mgr.Ensure(context.Background(), iss)
	if err != nil {
		t.Fatal(err)
	}
	// All three files written, key is 0600.
	for _, p := range []string{creds.KeyPath, creds.CertPath, creds.KnownHostsPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s: %v", p, err)
		}
	}
	info, _ := os.Stat(creds.KeyPath)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key perms = %o, want 600", info.Mode().Perm())
	}
	// The private key parses as an OpenSSH key.
	keyData, _ := os.ReadFile(creds.KeyPath)
	if _, err := ssh.ParsePrivateKey(keyData); err != nil {
		t.Fatalf("private key not parseable: %v", err)
	}
	// known_hosts pins the host CA for both port forms.
	kh, _ := os.ReadFile(creds.KnownHostsPath)
	if !strings.HasPrefix(string(kh), "@cert-authority ssh.chariots.sh,[ssh.chariots.sh]:443 ") {
		t.Fatalf("unexpected known_hosts: %q", kh)
	}
	if !strings.Contains(string(kh), strings.TrimSpace(iss.caPub)) {
		t.Fatal("known_hosts does not contain the host CA key")
	}

	// A second Ensure with a still-fresh cert must NOT re-mint.
	if _, err := mgr.Ensure(context.Background(), iss); err != nil {
		t.Fatal(err)
	}
	if iss.issued != 1 {
		t.Fatalf("expected 1 issuance (cached), got %d", iss.issued)
	}
}

func TestEnsureRefreshesNearExpiry(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, "ssh.chariots.sh")
	// A cert valid for only 2 minutes is already within the renewBefore window.
	iss := newFakeIssuer(t, 2*time.Minute)

	if _, err := mgr.Ensure(context.Background(), iss); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Ensure(context.Background(), iss); err != nil {
		t.Fatal(err)
	}
	if iss.issued != 2 {
		t.Fatalf("expected re-mint near expiry, got %d issuances", iss.issued)
	}
}

func TestEnsureReMintsWhenKeyMissing(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, "ssh.chariots.sh")
	iss := newFakeIssuer(t, time.Hour)

	creds, err := mgr.Ensure(context.Background(), iss)
	if err != nil {
		t.Fatal(err)
	}
	// Cert is fresh, but a lost private key must force a re-mint (else ssh has
	// a cert it can't use).
	if err := os.Remove(creds.KeyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Ensure(context.Background(), iss); err != nil {
		t.Fatal(err)
	}
	if iss.issued != 2 {
		t.Fatalf("expected re-mint when key missing, got %d", iss.issued)
	}
}

func TestSSHArgs(t *testing.T) {
	creds := Creds{
		KeyPath:        "/home/u/.chariot/ssh/id_ed25519",
		CertPath:       "/home/u/.chariot/ssh/id_ed25519-cert.pub",
		KnownHostsPath: "/home/u/.chariot/ssh/known_hosts",
	}
	args := SSHArgs(creds, "ssh.chariots.sh", "my-agent-3", 443, []string{"cat", "/etc/hostname"})
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-i /home/u/.chariot/ssh/id_ed25519",
		"CertificateFile=/home/u/.chariot/ssh/id_ed25519-cert.pub",
		"IdentitiesOnly=yes",
		"UserKnownHostsFile=/home/u/.chariot/ssh/known_hosts",
		"StrictHostKeyChecking=yes",
		"-p 443",
		"my-agent-3@ssh.chariots.sh",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q\n  got: %s", want, joined)
		}
	}
	// The login user is the slug, and the remote command is appended verbatim
	// after the destination.
	if args[len(args)-3] != "my-agent-3@ssh.chariots.sh" || args[len(args)-2] != "cat" || args[len(args)-1] != "/etc/hostname" {
		t.Fatalf("destination/command tail wrong: %v", args[len(args)-3:])
	}
}

func TestSSHArgsNoCommand(t *testing.T) {
	creds := Creds{KeyPath: "k", CertPath: "c", KnownHostsPath: "kh"}
	args := SSHArgs(creds, "ssh.chariots.sh", "a1", 22, nil)
	if got := args[len(args)-1]; got != "a1@ssh.chariots.sh" {
		t.Fatalf("last arg = %q, want destination", got)
	}
	_ = filepath.Separator
}
