package cmd

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// The dedicated-GPU hosted-model surface: catalog / push / host / hosted /
// drop, all under the existing `chariot models` group. A hosted model is
// addressable as the model id `self/<name>` through the SAME `chariot models
// set` everyone already uses — hosting only manages the serving deployment.

var modelsCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Open-weight models Chariot can host on a dedicated GPU for you",
	Long: `Open-weight models Chariot can host on a dedicated GPU in your account.

Dedicated means YOUR GPU in your account's namespace: prompts never leave
Chariot's cluster, and you're billed per GPU-hour from your credits instead of
per token. Models scale to zero when idle ($0/hr) and wake on the first
request (~1-5 min of weight loading).

Host one with ` + "`chariot models host <model>`" + `. Bring your own model —
a Hugging Face repo, a safetensors checkpoint, or a LoRA adapter — with
` + "`chariot models push`" + `.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		catalog, err := client.ModelCatalog(cmd.Context())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "  %-24s %-10s %-14s %s\n", "MODEL", "GPU", "DEDICATED $/HR", "NOTES")
		for _, m := range catalog.Models {
			fmt.Fprintf(
				out,
				"  %-24s %-10s $%-13.2f %s\n",
				m.CatalogID,
				m.GpuTier,
				float64(m.GpuHourMicros)/1_000_000,
				m.Description,
			)
		}
		fmt.Fprintln(out, "\nBilled per GPU-hour while running; scale-to-zero when idle ($0/hr).")
		fmt.Fprintln(out, "Host one with `chariot models host <model>`.")
		fmt.Fprintln(out, "Custom weights: `chariot models push --help`.")
		return nil
	},
}

var (
	modelsPushHF      string
	modelsPushHFToken string
	modelsPushBase    string
	modelsPushGPU     string
	modelsPushReplace bool
)

// GPU tiers the backend accepts (kept in sync with its GpuTier enum; the
// backend re-validates).
var gpuTiers = []string{"l4", "a100-80", "h100", "h200"}

var modelsPushCmd = &cobra.Command{
	Use:   "push <name> [checkpoint-dir]",
	Short: "Register a custom model (HF repo, checkpoint, or LoRA) and verify it serves",
	Long: `Register a custom model under a name of your own and prove it serves.

Three sources:

  chariot models push my-model --hf org/repo --gpu a100-80
      A Hugging Face repo (add --hf-token for gated/private repos; the token
      is stored as a Secret in your namespace, never in Chariot's database).

  chariot models push my-model ./checkpoint-dir --gpu a100-80
      A local safetensors checkpoint directory, uploaded (resumable). Uploads
      are SAFETENSORS-ONLY: pickle checkpoints (.bin/.pt/.pth/.ckpt) are
      rejected — they execute arbitrary code at load time. Convert first.

  chariot models push my-lora ./adapter-dir --base qwen3.6-35b-a3b
      A LoRA adapter served on a catalog base model (uploads in seconds; the
      GPU tier follows the base).

Every push then runs verification: the real serving deployment spins up on
your GPU tier, vLLM must load the weights, and a smoke completion must
answer. Verification GPU-seconds are billed at the tier's hourly rate
(pass or fail — the GPU ran either way). A verified model is addressable as
` + "`self/<name>`" + ` after ` + "`chariot models host <name>`" + `.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if modelsPushHF != "" && len(args) > 1 {
			return errors.New("pass either --hf or a checkpoint directory, not both")
		}
		if modelsPushHF == "" && len(args) < 2 {
			return errors.New("pass a checkpoint directory, or --hf <org/repo>")
		}
		if modelsPushGPU != "" && !contains(gpuTiers, modelsPushGPU) {
			return fmt.Errorf("--gpu must be one of %s", strings.Join(gpuTiers, ", "))
		}
		if modelsPushBase == "" && modelsPushHF == "" && modelsPushGPU == "" {
			return errors.New("--gpu is required for a checkpoint upload (l4, a100-80, h100, h200)")
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		ctx := cmd.Context()

		var modelID string
		if modelsPushHF != "" {
			created, err := client.RegisterModel(ctx, api.RegisterModelParams{
				Name:    name,
				Source:  "hf",
				HFRepo:  modelsPushHF,
				HFToken: modelsPushHFToken,
				GpuTier: modelsPushGPU,
				Replace: modelsPushReplace,
			})
			if err != nil {
				return pushConflictHint(err)
			}
			modelID = created.ModelID
		} else {
			modelID, err = uploadModelDir(ctx, cmd, client, name, args[1])
			if err != nil {
				return err
			}
		}
		return verifyModelWithProgress(ctx, cmd, client, modelID, name)
	},
}

// pushConflictHint decorates a 409 with the --replace hint.
func pushConflictHint(err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 409 {
		return fmt.Errorf("%s (use --replace to abandon an unfinished push)", apiErr.Detail)
	}
	return err
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// uploadModelDir tars the checkpoint directory (plain tar — weights don't
// compress), registers the push with the file manifest, and runs the chunked
// upload with a byte-accurate progress bar.
func uploadModelDir(ctx context.Context, cmd *cobra.Command, client *api.Client, name, dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("checkpoint dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory (pass the model directory, not a file)", dir)
	}

	tmp, err := os.CreateTemp("", "chariot-model-*.tar")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	manifest, err := tarDirectory(tmp, dir)
	if err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	f, err := os.Open(tmp.Name())
	if err != nil {
		return "", err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := stat.Size()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	digest := hex.EncodeToString(hasher.Sum(nil))

	source := "upload"
	if modelsPushBase != "" {
		source = "adapter"
	}
	created, err := client.RegisterModel(ctx, api.RegisterModelParams{
		Name:      name,
		Source:    source,
		CatalogID: modelsPushBase,
		GpuTier:   modelsPushGPU,
		SizeBytes: size,
		SHA256:    digest,
		Manifest:  manifest,
		Replace:   modelsPushReplace,
	})
	if err != nil {
		return "", pushConflictHint(err)
	}

	bar := progressbar.DefaultBytes(size, "Uploading")
	offset := int64(0)
	buf := make([]byte, created.ChunkSizeBytes)
	for offset < size {
		n, err := f.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return "", err
		}
		ack, err := client.PutModelChunk(ctx, created.ModelID, offset, buf[:n])
		if err != nil {
			return "", fmt.Errorf("uploading chunk at %d: %w", offset, err)
		}
		offset = ack.CommittedBytes // authoritative (replays fast-forward)
		_ = bar.Set64(offset)
	}
	_ = bar.Finish()
	fmt.Fprintln(cmd.ErrOrStderr())

	if _, err := client.FinalizeModel(ctx, created.ModelID); err != nil {
		return "", err
	}
	return created.ModelID, nil
}

// tarDirectory writes a plain tar of dir's regular files (relative paths) to
// w and returns the manifest of member names.
func tarDirectory(w io.Writer, dir string) ([]string, error) {
	tw := tar.NewWriter(w)
	var manifest []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil // no symlinks/devices in a checkpoint tar
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		manifest = append(manifest, rel)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(manifest)
	return manifest, tw.Close()
}

// verifyModelWithProgress fires the (long) verify request and renders a
// spinner while it runs, mirroring image push's ergonomics. (Model verify has
// a single pipeline phase, so one spinner line — no phase table needed.)
func verifyModelWithProgress(ctx context.Context, cmd *cobra.Command, client *api.Client, modelID, name string) error {
	out := cmd.ErrOrStderr()
	verifyDone := make(chan error, 1)
	go func() {
		_, err := client.VerifyModel(ctx, modelID)
		verifyDone <- err
	}()

	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	start := time.Now()
	tick := 0
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-verifyDone:
			fmt.Fprint(out, "\r\033[K")
			if err != nil {
				return err
			}
			final, ferr := client.GetModel(ctx, modelID)
			if ferr != nil {
				return ferr
			}
			return renderModelVerdict(cmd, final, name)
		case <-ticker.C:
			tick++
			fmt.Fprintf(
				out,
				"\r\033[K%s Verifying %s (%.0fs — big models can take 15+ min)",
				spinner[tick%len(spinner)],
				name,
				time.Since(start).Seconds(),
			)
		}
	}
}

func renderModelVerdict(cmd *cobra.Command, model *api.HostedModel, name string) error {
	out := cmd.OutOrStdout()
	if model.Status == "verified" {
		seconds := int64(0)
		if model.VerifyGpuSeconds != nil {
			seconds = *model.VerifyGpuSeconds
		}
		cost := float64(seconds) * float64(model.GpuHourMicros) / 3600 / 1_000_000
		fmt.Fprintf(out, "✓ verified on %s (%ds, ~$%.2f)\n", model.GpuTier, seconds, cost)
		fmt.Fprintf(out, "\nModel id `self/%s` is ready to host:\n", name)
		fmt.Fprintf(out, "  chariot models host %s\n", name)
		return nil
	}
	phase := ""
	if model.FailedPhase != nil {
		phase = *model.FailedPhase
	}
	detail := ""
	if model.Error != nil {
		detail = *model.Error
	}
	return fmt.Errorf("verification failed in %s: %s", phase, detail)
}

var (
	modelsHostAlwaysOn  bool
	modelsHostIdleAfter time.Duration
	modelsHostYes       bool
)

var modelsHostCmd = &cobra.Command{
	Use:   "host <model>",
	Short: "Serve a model on a dedicated GPU in your account (billed per GPU-hour)",
	Long: `Serve a model on a dedicated GPU in your account's namespace.

<model> is a catalog entry (` + "`chariot models catalog`" + `) or one of your
verified custom models (` + "`chariot models push`" + `). Once warm it is
addressable as the model id ` + "`self/<name>`" + `:

  chariot models set self/<name>              # whole fleet
  chariot models set self/<name> --agent <id> # one agent

Billing is per GPU-hour from your credits while the model is starting or
warm. By default it scales to ZERO after 15m without a request ($0/hr idle;
the next request wakes it, ~1-5 min). --always-on keeps it warm around the
clock — mind the burn rate. --idle-after tunes the scale-to-zero window.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		target, err := resolveHostTarget(ctx, client, args[0])
		if err != nil {
			return err
		}
		name := target.name

		hourly := float64(target.hourMicros) / 1_000_000
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Hosting %s on a dedicated %s in your namespace.\n\n", name, target.gpuTier)
		fmt.Fprintf(out, "  billing     $%.2f/GPU-hr while running, from your credits\n", hourly)
		if modelsHostAlwaysOn {
			fmt.Fprintf(out, "  mode        always-on (~$%.0f/day until dropped)\n", hourly*24)
		} else {
			idle := modelsHostIdleAfter
			if idle == 0 {
				idle = 15 * time.Minute
			}
			fmt.Fprintf(out, "  idle        scales to zero after %s idle (no charge while stopped)\n", idle)
			fmt.Fprintln(out, "  cold start  ~1-5 min on first request after idle")
		}
		fmt.Fprintf(out, "  model id    self/%s\n\n", name)
		if !modelsHostYes {
			fmt.Fprint(out, "Proceed? [y/N] ")
			reader := bufio.NewReader(cmd.InOrStdin())
			answer, _ := reader.ReadString('\n')
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				fmt.Fprintln(out, "aborted")
				return nil
			}
		}

		// Registration happens ONLY after consent — declining the prompt must
		// leave no state behind, and a catalog entry needs a hosted-model row
		// before it can be hosted.
		if target.registerCatalogID != "" {
			if _, err := client.RegisterModel(ctx, api.RegisterModelParams{
				Name:      name,
				Source:    "catalog",
				CatalogID: target.registerCatalogID,
			}); err != nil {
				return err
			}
		}
		mode := "scale_to_zero"
		if modelsHostAlwaysOn {
			mode = "always_on"
		}
		hosted, err := client.HostModel(ctx, name, mode, int64(modelsHostIdleAfter.Seconds()))
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ %s is %s (weights loading — a few minutes for big models)\n", name, hosted.ServingState)
		fmt.Fprintf(out, "\nModel id `self/%s` is live once warm (watch `chariot models hosted`).\n", name)
		fmt.Fprintf(out, "Point your fleet at it:  chariot models set self/%s\n", name)
		return nil
	},
}

// hostTarget is what `models host` resolved its argument to. registerCatalogID
// is non-empty when the target is a catalog entry that still needs a
// hosted-model row — registration is deferred until AFTER the billing prompt,
// so declining leaves no state behind.
type hostTarget struct {
	name              string
	gpuTier           string
	hourMicros        int64
	registerCatalogID string
}

// resolveHostTarget maps the argument to a hostable model: one of the
// account's verified models first, else a catalog entry (to be registered
// post-consent under its Service-name-safe id, dots → hyphens). READ-ONLY —
// it must not mutate account state before the user consents.
func resolveHostTarget(ctx context.Context, client *api.Client, raw string) (hostTarget, error) {
	name := strings.TrimPrefix(raw, "self/")
	models, err := client.ListModels(ctx)
	if err != nil {
		return hostTarget{}, err
	}
	for _, m := range models {
		if m.Name == name && m.Status == "verified" {
			return hostTarget{name: m.Name, gpuTier: m.GpuTier, hourMicros: m.GpuHourMicros}, nil
		}
	}
	catalog, err := client.ModelCatalog(ctx)
	if err != nil {
		return hostTarget{}, err
	}
	for _, entry := range catalog.Models {
		if entry.CatalogID != raw {
			continue
		}
		return hostTarget{
			name:              strings.ReplaceAll(entry.CatalogID, ".", "-"),
			gpuTier:           entry.GpuTier,
			hourMicros:        entry.GpuHourMicros,
			registerCatalogID: entry.CatalogID,
		}, nil
	}
	return hostTarget{}, fmt.Errorf(
		"unknown model %q: not one of your verified models (`chariot models hosted`) "+
			"and not a catalog entry (`chariot models catalog`)", raw,
	)
}

var modelsHostedCmd = &cobra.Command{
	Use:   "hosted",
	Short: "Your hosted models: state, GPU, and what they cost per hour",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		models, err := client.ListModels(cmd.Context())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(models) == 0 {
			fmt.Fprintln(out, "No hosted models. Browse `chariot models catalog` or push your own.")
			return nil
		}
		fmt.Fprintf(out, "  %-24s %-10s %-10s %-9s %s\n", "MODEL ID", "GPU", "$/HR", "STATUS", "STATE")
		for _, m := range models {
			state := m.ServingState
			if m.Status != "verified" {
				state = "-"
			}
			fmt.Fprintf(
				out,
				"  %-24s %-10s $%-9.2f %-9s %s\n",
				"self/"+m.Name,
				m.GpuTier,
				float64(m.GpuHourMicros)/1_000_000,
				m.Status,
				state,
			)
		}
		fmt.Fprintln(out, "\nidle = scaled to zero ($0/hr), wakes on first request (~1-5 min).")
		fmt.Fprintln(out, "warm/starting time bills per GPU-hour. Stop one with `chariot models drop <name>`.")
		return nil
	},
}

var modelsDropFallback string

var modelsDropCmd = &cobra.Command{
	Use:   "drop <name>",
	Short: "Stop serving a hosted model (billing stops immediately)",
	Long: `Stop serving a hosted model and stop its GPU-hour billing.

Refuses while your fleet default or any agent still points at self/<name> —
re-point them first (` + "`chariot models set`" + `), or pass --fallback
<model-id> to switch them in the same step. The verified model itself stays
pushed; ` + "`chariot models host`" + ` brings it back any time.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(args[0], "self/")
		result, err := client.DropModel(cmd.Context(), name, modelsDropFallback)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if modelsDropFallback != "" && result.AgentsRepointed >= 0 {
			fmt.Fprintf(out, "✓ fleet model: %s\n", modelsDropFallback)
		}
		fmt.Fprintf(out, "✓ dropped self/%s — billing stopped\n", name)
		return nil
	},
}

func init() {
	modelsPushCmd.Flags().StringVar(&modelsPushHF, "hf", "", "Hugging Face repo (`org/name`) to serve — no upload needed")
	modelsPushCmd.Flags().StringVar(&modelsPushHFToken, "hf-token", "", "access token for a gated/private HF repo (stored as a Secret in your namespace)")
	modelsPushCmd.Flags().StringVar(&modelsPushBase, "base", "", "catalog base model for a LoRA adapter upload")
	modelsPushCmd.Flags().StringVar(&modelsPushGPU, "gpu", "", "GPU tier to verify + serve on: l4, a100-80, h100, or h200")
	modelsPushCmd.Flags().BoolVar(&modelsPushReplace, "replace", false, "abandon an unfinished previous push")
	modelsHostCmd.Flags().BoolVar(&modelsHostAlwaysOn, "always-on", false, "never scale to zero (billed around the clock until dropped)")
	modelsHostCmd.Flags().DurationVar(&modelsHostIdleAfter, "idle-after", 0, "scale-to-zero window (default 15m)")
	modelsHostCmd.Flags().BoolVarP(&modelsHostYes, "yes", "y", false, "skip the confirmation prompt")
	modelsDropCmd.Flags().StringVar(&modelsDropFallback, "fallback", "", "model id to re-point the fleet/agents that still use this model")
	modelsCmd.AddCommand(modelsCatalogCmd)
	modelsCmd.AddCommand(modelsPushCmd)
	modelsCmd.AddCommand(modelsHostCmd)
	modelsCmd.AddCommand(modelsHostedCmd)
	modelsCmd.AddCommand(modelsDropCmd)
}
