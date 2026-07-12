package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

func TestImagePushRequiresRefOrTarball(t *testing.T) {
	logout(t)
	got := runCLI(t, "", "image", "push")
	if got.err == nil {
		t.Fatal("want an error with neither an image ref nor --tarball")
	}
	mustContain(t, got.err.Error(), "--tarball", "error")
}

// --pod-size is validated locally before anything is uploaded.
func TestImagePushValidatesPodSize(t *testing.T) {
	logout(t)
	got := runCLI(t, "", "image", "push", "my-agent:latest", "--pod-size", "enormous")
	if got.err == nil {
		t.Fatal("want an error for an unknown pod size")
	}
	mustContain(t, got.err.Error(), "small, medium, large", "error")
}

func TestImagePushAcceptsEveryDeclaredPodSize(t *testing.T) {
	// A pod size in podSizes must get past validation — it should fail later,
	// on the missing login, not on the size itself.
	for _, size := range podSizes {
		t.Run(size, func(t *testing.T) {
			logout(t)
			got := runCLI(t, "", "image", "push", "my-agent:latest", "--pod-size", size)
			if got.err == nil {
				t.Fatal("want the not-logged-in error")
			}
			mustContain(t, got.err.Error(), "chariot login", "error")
		})
	}
}

// --- resolveTarball --------------------------------------------------------

func TestResolveTarballUsesExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(path, []byte("tar bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, cleanup, err := resolveTarball(context.Background(), &cobra.Command{}, nil, path)
	if err != nil {
		t.Fatalf("resolveTarball: %v", err)
	}
	defer cleanup()
	if got != path {
		t.Errorf("path = %q, want %q", got, path)
	}
	// An explicit tarball is the user's file — cleanup must not delete it.
	cleanup()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("cleanup removed the user's tarball: %v", err)
	}
}

func TestResolveTarballRejectsMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.tar")
	_, _, err := resolveTarball(context.Background(), &cobra.Command{}, nil, missing)
	if err == nil {
		t.Fatal("want an error for a missing tarball")
	}
	mustContain(t, err.Error(), "nope.tar", "error")
}

// Without --tarball the image is exported from the local docker daemon; with
// no docker on PATH the user gets an actionable message, not an exec error.
func TestResolveTarballRequiresDockerWhenExporting(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // a PATH with no docker in it

	_, _, err := resolveTarball(context.Background(), &cobra.Command{}, []string{"my-agent:latest"}, "")
	if err == nil {
		t.Fatal("want an error when docker is absent")
	}
	mustContain(t, err.Error(), "docker not found", "error")
	mustContain(t, err.Error(), "--tarball", "error")
}

// --- phaseRank -------------------------------------------------------------

// phaseRank drives the progress display: pre-pipeline statuses sort before
// every phase, the pipeline phases sort in their declared order, and terminal
// statuses sort after them all.
func TestPhaseRankOrdersPipeline(t *testing.T) {
	for _, status := range []string{"uploading", "uploaded"} {
		if got := phaseRank(status); got != -1 {
			t.Errorf("phaseRank(%q) = %d, want -1", status, got)
		}
	}

	for i, p := range verifyPhases {
		if got := phaseRank(p.status); got != i {
			t.Errorf("phaseRank(%q) = %d, want %d", p.status, got, i)
		}
	}

	// Anything terminal (or unrecognised) ranks past the last phase, which is
	// what stops the spinner loop.
	for _, status := range []string{"ready", "failed", "superseded", "who-knows"} {
		if got := phaseRank(status); got != len(verifyPhases) {
			t.Errorf("phaseRank(%q) = %d, want %d", status, got, len(verifyPhases))
		}
	}
}

func TestPhaseRankIsStrictlyIncreasing(t *testing.T) {
	for i := 1; i < len(verifyPhases); i++ {
		if phaseRank(verifyPhases[i].status) <= phaseRank(verifyPhases[i-1].status) {
			t.Fatalf("phases %q and %q are out of order",
				verifyPhases[i-1].status, verifyPhases[i].status)
		}
	}
}

// --- printVerdict ----------------------------------------------------------

func newVerdictCmd() (*cobra.Command, *strings.Builder) {
	cmd := &cobra.Command{}
	buf := &strings.Builder{}
	cmd.SetOut(buf)
	return cmd, buf
}

func TestPrintVerdictReady(t *testing.T) {
	cmd, buf := newVerdictCmd()
	ref := "registry.chariots.sh/cust-1/agent@sha256:abc"
	matched := true

	err := printVerdict(cmd, &api.Image{
		Status: "ready", ImageRef: &ref, PodSize: "medium", NonceMatched: &matched,
	})
	if err != nil {
		t.Fatalf("a ready image must not error: %v", err)
	}
	out := buf.String()
	mustContain(t, out, "Image verified and ready!", "stdout")
	mustContain(t, out, ref, "stdout")
	mustContain(t, out, "pod    : medium", "stdout")
	mustNotContain(t, out, "didn't echo the probe code", "stdout")
}

// A verified image whose reply ignored the probe code still passes, but the
// user is warned their agent may not be reading message content.
func TestPrintVerdictReadyWarnsOnNonceMismatch(t *testing.T) {
	cmd, buf := newVerdictCmd()
	ref := "img-ref"
	matched := false

	if err := printVerdict(cmd, &api.Image{
		Status: "ready", ImageRef: &ref, NonceMatched: &matched,
	}); err != nil {
		t.Fatalf("a ready image must not error: %v", err)
	}
	mustContain(t, buf.String(), "didn't echo the probe code", "stdout")
}

func TestPrintVerdictFailedErrorsWithPhase(t *testing.T) {
	cmd, buf := newVerdictCmd()
	phase, detail := "verifying", "agent never replied within 2m"

	err := printVerdict(cmd, &api.Image{Status: "failed", FailedPhase: &phase, Error: &detail})
	if err == nil {
		t.Fatal("a failed verification must return an error (non-zero exit)")
	}
	mustContain(t, err.Error(), "verifying", "error")

	out := buf.String()
	mustContain(t, out, "Verification failed during verifying", "stdout")
	mustContain(t, out, detail, "stdout")
	// Reassure the user their running fleet was not downgraded.
	mustContain(t, out, "keeps running its previous image", "stdout")
}

// The backend may report a failure with no phase or detail; the command must
// still fail cleanly rather than dereference a nil pointer.
func TestPrintVerdictFailedHandlesMissingFields(t *testing.T) {
	cmd, buf := newVerdictCmd()

	err := printVerdict(cmd, &api.Image{Status: "failed"})
	if err == nil {
		t.Fatal("want an error")
	}
	mustContain(t, err.Error(), "unknown", "error")
	mustContain(t, buf.String(), "verification failed", "stdout")
}

// A ready image with no image_ref must not panic on the nil pointer.
func TestPrintVerdictReadyHandlesMissingRef(t *testing.T) {
	cmd, _ := newVerdictCmd()
	if err := printVerdict(cmd, &api.Image{Status: "ready"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
