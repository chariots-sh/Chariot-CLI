package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var (
	imagePushTarball string
	imagePushReplace bool
	imagePushPodSize string
	imagePushName    string
)

// The pod-size tiers the backend accepts (kept in sync with the backend's
// PodSize enum; the backend re-validates).
var podSizes = []string{"small", "medium", "large"}

var imagePushCmd = &cobra.Command{
	Use:   "push [image-ref]",
	Short: "Upload a custom agent image and verify it works with Chariot",
	Long: `Upload a custom agent image and verify it works with Chariot.

Exports the image from your local docker daemon (or takes --tarball from
` + "`docker save`" + `), uploads it, then Chariot verifies it end-to-end: the image is
spun up as one ephemeral test agent in your namespace, sent a message, and must
reply through the Chariot integration. Only a verified image is adopted by
your fleet. Requirements: ` + "`chariot image guidelines`" + `.

--pod-size picks the CPU/memory tier your agents run at (small 1cpu/512MiB,
medium 2cpu/2GiB, large 4cpu/4GiB). The stock agent fits small; heavier
runtimes like an OpenClaw gateway need medium. The verification agent runs at
the chosen size, so a verified image is proven at the size your fleet will get.

--name registers the image under a name of your own (default: "default"), so
you can hold several verified images and deploy different agents onto
different ones: ` + "`chariot deploy --image <name>`" + `. Pushing an existing name
upgrades that image; pushing a new name adds one alongside.

Verification costs a flat $0.01 plus normal metered model usage.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if imagePushTarball == "" && len(args) == 0 {
			return fmt.Errorf("pass an image ref (e.g. `chariot image push my-agent:latest`) or --tarball")
		}
		if !slices.Contains(podSizes, imagePushPodSize) {
			return fmt.Errorf("--pod-size must be one of %s", strings.Join(podSizes, ", "))
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		ctx := cmd.Context()

		tarballPath, cleanup, err := resolveTarball(ctx, cmd, args, imagePushTarball)
		if err != nil {
			return err
		}
		defer cleanup()

		imageID, err := uploadTarball(ctx, cmd, client, tarballPath)
		if err != nil {
			return err
		}
		return verifyWithProgress(ctx, cmd, client, imageID)
	},
}

// resolveTarball returns a docker-save tarball path: --tarball verbatim, or a
// temp file exported from the local docker daemon.
func resolveTarball(ctx context.Context, cmd *cobra.Command, args []string, tarball string) (string, func(), error) {
	if tarball != "" {
		if _, err := os.Stat(tarball); err != nil {
			return "", nil, fmt.Errorf("tarball %s: %w", tarball, err)
		}
		return tarball, func() {}, nil
	}
	ref := args[0]
	if _, err := exec.LookPath("docker"); err != nil {
		return "", nil, fmt.Errorf("docker not found — install docker, or pass --tarball with a `docker save` archive")
	}
	tmp, err := os.CreateTemp("", "chariot-image-*.tar")
	if err != nil {
		return "", nil, err
	}
	tmp.Close()
	cleanup := func() { os.Remove(tmp.Name()) }
	fmt.Fprintf(cmd.ErrOrStderr(), "Exporting %s from the docker daemon...\n", ref)
	save := exec.CommandContext(ctx, "docker", "save", "-o", tmp.Name(), ref)
	if out, err := save.CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("docker save %s: %s", ref, strings.TrimSpace(string(out)))
	}
	return tmp.Name(), cleanup, nil
}

// uploadTarball runs the chunked upload (create → chunks → finalize) with a
// byte-accurate progress bar, resuming from the backend's committed count.
func uploadTarball(ctx context.Context, cmd *cobra.Command, client *api.Client, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	digest := hex.EncodeToString(hasher.Sum(nil))

	created, err := client.CreateImage(ctx, size, digest, imagePushPodSize, imagePushName, imagePushReplace)
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 409 {
			return "", fmt.Errorf("%s (use --replace to abandon an unfinished upload)", apiErr.Detail)
		}
		return "", err
	}

	bar := progressbar.DefaultBytes(size, "Uploading")
	offset := int64(0)
	buf := make([]byte, created.ChunkSizeBytes)
	for offset < size {
		n, err := f.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return "", err
		}
		ack, err := client.PutImageChunk(ctx, created.ImageID, offset, buf[:n])
		if err != nil {
			return "", fmt.Errorf("uploading chunk at %d: %w", offset, err)
		}
		// The backend's committed count is authoritative (replays fast-forward).
		offset = ack.CommittedBytes
		_ = bar.Set64(offset)
	}
	_ = bar.Finish()
	fmt.Fprintln(cmd.ErrOrStderr())

	if _, err := client.FinalizeImage(ctx, created.ImageID); err != nil {
		return "", err
	}
	return created.ImageID, nil
}

// The backend's pipeline statuses, in order, with the labels the user sees.
var verifyPhases = []struct{ status, label string }{
	{"pushing", "Processing image"},
	{"starting", "Spinning up test agent"},
	{"verifying", "Verifying (waiting for the agent's reply)"},
	{"tearing_down", "Tearing down"},
}

func phaseRank(status string) int {
	for i, p := range verifyPhases {
		if p.status == status {
			return i
		}
	}
	switch status {
	case "uploaded", "uploading":
		return -1
	default: // ready / failed / superseded — terminal
		return len(verifyPhases)
	}
}

// verifyWithProgress fires the (long) verify request in the background and
// renders one spinner line per pipeline phase from concurrent status polls.
func verifyWithProgress(ctx context.Context, cmd *cobra.Command, client *api.Client, imageID string) error {
	out := cmd.ErrOrStderr()
	verifyDone := make(chan error, 1)
	go func() {
		_, err := client.VerifyImage(ctx, imageID)
		verifyDone <- err
	}()

	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	current := -1
	phaseStart := time.Now()
	var latest *api.Image
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	tick := 0
	verifyErr := error(nil)
	verifyReturned := false

	advanceTo := func(rank int) {
		for current < rank {
			if current >= 0 {
				fmt.Fprintf(out, "\r\033[K✓ %s (%.0fs)\n", verifyPhases[current].label, time.Since(phaseStart).Seconds())
				phaseStart = time.Now()
			}
			current++
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-verifyDone:
			verifyReturned = true
			verifyErr = err
		case <-ticker.C:
		}
		tick++
		if verifyReturned || tick%5 == 0 { // poll ~every 1.5s
			img, err := client.GetImage(ctx, imageID)
			if err == nil {
				latest = img
			}
		}
		if latest != nil {
			rank := phaseRank(latest.Status)
			if rank > current {
				advanceTo(min(rank, len(verifyPhases)))
			}
			if rank >= len(verifyPhases) { // terminal
				return printVerdict(cmd, latest)
			}
		}
		if verifyReturned && verifyErr != nil {
			// The verify request failed outright (402, 409, network) before
			// the row went terminal — surface that error.
			fmt.Fprintf(out, "\r\033[K")
			return verifyErr
		}
		if current >= 0 && current < len(verifyPhases) {
			fmt.Fprintf(out, "\r\033[K%s %s... %.0fs", spinner[tick%len(spinner)],
				verifyPhases[current].label, time.Since(phaseStart).Seconds())
		}
	}
}

func printVerdict(cmd *cobra.Command, img *api.Image) error {
	out := cmd.OutOrStdout()
	if img.Status == "ready" {
		ref := ""
		if img.ImageRef != nil {
			ref = *img.ImageRef
		}
		fmt.Fprintf(out, "\n✅ Image verified and ready!\n\n")
		fmt.Fprintf(out, "  image  : %s\n", ref)
		if img.PodSize != "" {
			fmt.Fprintf(out, "  pod    : %s\n", img.PodSize)
		}
		if img.NonceMatched != nil && !*img.NonceMatched {
			fmt.Fprintln(out, "  note   : the test reply didn't echo the probe code — your agent")
			fmt.Fprintln(out, "           replied, but may not be reading message content.")
		}
		fmt.Fprintln(out, "\n  New agent activations use it immediately; running agents adopt it")
		fmt.Fprintln(out, "  the next time they wake from hibernation.")
		return nil
	}
	phase := "unknown"
	if img.FailedPhase != nil {
		phase = *img.FailedPhase
	}
	detail := "verification failed"
	if img.Error != nil {
		detail = *img.Error
	}
	fmt.Fprintf(out, "\n❌ Verification failed during %s\n\n", phase)
	fmt.Fprintf(out, "  %s\n\n", detail)
	fmt.Fprintln(out, "  Your fleet keeps running its previous image. Fix the issue and")
	fmt.Fprintln(out, "  re-run `chariot image push` — see `chariot image guidelines`.")
	return fmt.Errorf("image verification failed in %s", phase)
}

func init() {
	imagePushCmd.Flags().StringVar(&imagePushTarball, "tarball", "", "path to a `docker save` archive (skips the local docker daemon)")
	imagePushCmd.Flags().BoolVar(&imagePushReplace, "replace", false, "abandon an unfinished previous upload")
	imagePushCmd.Flags().StringVar(&imagePushPodSize, "pod-size", "small", "CPU/memory tier for this image's agents: small, medium, or large")
	imagePushCmd.Flags().StringVar(&imagePushName, "name", "default", "name agents reference this image by (`chariot deploy --image <name>`)")
	imageCmd.AddCommand(imagePushCmd)
}
