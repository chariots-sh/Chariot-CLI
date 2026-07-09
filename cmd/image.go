package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var imageCmd = &cobra.Command{
	Use:   "image",
	Short: "Upload and manage your fleet's custom agent image",
	Long: `Upload and manage your fleet's custom agent image.

By default your fleet runs the stock Chariot agent image. You can replace it
with your own container image — your runtime, your tools — as long as it
follows the Chariot agent contract (` + "`chariot image guidelines`" + `).

  chariot image push my-agent:latest   # upload + verify an image
  chariot image push my-agent --pod-size medium   # bigger CPU/memory tier
  chariot image status                 # what your fleet runs now
  chariot image guidelines             # the contract your image must satisfy`,
}

var imageStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show your account's current custom image",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		img, err := client.CurrentImage(cmd.Context())
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintf(w, "id\t%s\n", img.ID)
		if img.Name != "" {
			fmt.Fprintf(w, "name\t%s\n", img.Name)
		}
		fmt.Fprintf(w, "status\t%s\n", img.Status)
		if img.PodSize != "" {
			fmt.Fprintf(w, "pod size\t%s\n", img.PodSize)
		}
		if img.ImageRef != nil {
			fmt.Fprintf(w, "image\t%s\n", *img.ImageRef)
		}
		if img.ReadyAt != nil {
			fmt.Fprintf(w, "ready since\t%s\n", img.ReadyAt.Local().Format("2006-01-02 15:04:05"))
		}
		if img.FailedPhase != nil {
			fmt.Fprintf(w, "failed in\t%s\n", *img.FailedPhase)
		}
		if img.Error != nil {
			fmt.Fprintf(w, "error\t%s\n", *img.Error)
		}
		return w.Flush()
	},
}

const imageGuidelines = `CHARIOT CUSTOM AGENT IMAGE — THE CONTRACT

Your image runs in place of the stock agent, so it must satisfy the same
runtime shape. Verification implicitly checks 1-5.

1. PROCESS MODEL
   The container starts with args ["daemon"]. Your entrypoint must accept that
   argument and run a long-lived daemon.

2. USER & FILESYSTEM (enforced; build your image to work within them)
   - Runs as UID:GID 65534:65534, no privilege escalation, seccomp default.
   - Read-only root filesystem. The ONLY writable path is /zeroclaw-data
     (HOME; a persistent volume for fleet agents, scratch during verification).
   - Bake the data directory into your image so it also runs volume-less:
       RUN mkdir -p /zeroclaw-data && chown 65534:65534 /zeroclaw-data
   - Keep every writable path your runtime needs (including its temp dir —
     set TMPDIR) under /zeroclaw-data; /tmp is read-only.

3. HEALTH (a pod that never goes Ready fails verification at spin-up)
   Your daemon serves ONE HTTP port, :8088:
   - TCP :8088 must accept connections shortly after start (startup +
     liveness probes — the socket just has to be open);
   - GET /health on :8088 must return 200 once you can accept messages
     (readiness; return 503 while warming up).

4. RECEIVING MESSAGES
   Chariot delivers each message as an HTTP POST to your daemon, from inside
   your own pod (agent pods accept no network ingress):
     POST http://127.0.0.1:8088/message
     Header X-Gateway-Token: $AGENT_GATEWAY_TOKEN
     Body   {"message": "...", "message_id": "<opaque id>"}
   Respond with any 2xx once the message is safely accepted (non-2xx or a
   refused connection is retried with the SAME message_id — treat message_id
   as a dedupe key), reject bad X-Gateway-Token, and run the message
   asynchronously after the 2xx. No shell, HTTP client, zeroclaw binary, or
   session files are required of your image — this endpoint is the whole
   inbound contract; fully distroless images work.

5. REPLYING (what verification checks)
   Printing to stdout does NOT reach the user. Reply with:
     POST $CHARIOT_OUTBOUND_URL
     Header X-Chariot-Agent-Token: $CHARIOT_AGENT_TOKEN
     Body   {"message": "your reply text"}
   A helper doing exactly this is mounted at /zeroclaw-data/workspace/reply.sh
   (run it as: /chariot/bin/sh /zeroclaw-data/workspace/reply.sh "reply" —
   it needs nothing from your image). Verification passes when your test agent
   sends ANY reply within ~2 minutes of the probe message.

6. MODEL ACCESS
   OpenAI-compatible chat completions at $CHARIOT_PROXY_BASE_URL, API key =
   $CHARIOT_AGENT_TOKEN, model in $CHARIOT_MODEL. Metered against your credits.
   ($AGENT_GATEWAY_TOKEN carries the inbound delivery auth from §4; both are
   injected into every pod.)

7. POD SIZE
   Choose the CPU/memory tier at push time (--pod-size):
     small   1 cpu / 512 MiB   (default — fits the stock agent)
     medium  2 cpu / 2 GiB     (Node-based runtimes, e.g. OpenClaw)
     large   4 cpu / 4 GiB
   The verification agent runs at the chosen size, and your fleet adopts the
   size together with the image.

8. LIMITS & BILLING
   - Compressed tarball (docker save output): <= 2 GiB.
   - Images are NAMED (--name, default "default"); you can hold several
     verified images and deploy different agents onto different ones
     (chariot deploy --image <name>). Re-pushing a name replaces that image
     only AFTER the new one verifies — a failed verification never
     downgrades your fleet. One upload/verification runs at a time.
   - Each verification run: flat $0.01 + normal metered model usage.
   - The test agent is hard-killed after 10 minutes; keep images small and
     fast to start (pull time counts against the spin-up deadline).

ADOPTION
   New agent activations use a verified image immediately; agents already
   running pick it up the next time they wake from hibernation. Agents
   deployed without --image run your account default (chariot images
   set-default).

Full document — including a worked OpenClaw example image:
chariot/docs/custom-agent-images.md (in the Chariot repo).`

var imageGuidelinesCmd = &cobra.Command{
	Use:   "guidelines",
	Short: "Print the contract a custom agent image must satisfy",
	RunE: func(cmd *cobra.Command, args []string) error {
		// io.WriteString, not Fprintln: the text contains a literal printf
		// directive (the exec-delivery example) that trips go vet otherwise.
		_, err := io.WriteString(cmd.OutOrStdout(), imageGuidelines+"\n")
		return err
	},
}

func init() {
	imageCmd.AddCommand(imageStatusCmd)
	imageCmd.AddCommand(imageGuidelinesCmd)
	rootCmd.AddCommand(imageCmd)
}
