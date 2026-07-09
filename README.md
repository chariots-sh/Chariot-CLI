# Chariot CLI

Deploy and manage enterprise agent fleets from your terminal.

The CLI's job is fleet management: login, deploy, list. Messaging agents in
production is your service's job, via the [HTTP API](https://app.chariots.sh/docs) —
`chariot api` prints the full reference.

```
chariot login                                        # authenticate (opens browser)
chariot deploy --count 10000 --endpoint https://…    # spin up a fleet
chariot list                                         # agents + their ids
chariot hibernate my-agent-3                         # force-hibernate an agent now (skip the 48h idle wait)
chariot account                                      # credits + status
chariot api                                          # HTTP API reference for your service
chariot images                                       # deployable images (built-in + yours)
chariot image push my-agent:latest --pod-size medium # run your OWN agent image (verified first)
chariot hibernate-after set 00:04:00                 # idle 4h → agents hibernate
chariot demo send <agent-id> "hello"                 # one-off test message (demo only)
chariot demo watch                                   # print replies in the terminal (demo only)
```

## Install

```bash
brew install immortal-protocols/tap/chariot
# or via Go:
go install github.com/Immortal-Protocols/Chariot-CLI@latest
# or build locally:
go build -o chariot .
```

## The one user journey

1. `chariot login` — opens your browser to the Chariot site. Sign in (email code)
   and buy credits, then approve the CLI. The CLI stores a session token in
   `~/.chariot/config.json`.
2. `chariot deploy --count N --endpoint URL` — creates `N` agents (they start
   deactivated and cost nothing until messaged) and prints a **token-seed**
   (shown once). `URL` is where your agents POST their replies.
3. `chariot list` — shows each agent's id.
4. From your own service, message an agent:
   ```
   POST {chariot-base}/v1/agents/{agent-id}/messages
   header  X-Chariot-Token: <token-seed>
   body    {"message": "…"}
   ```
   The agent replies to your `--endpoint` (and to the reply inbox,
   `GET /v1/replies`). `chariot api` prints the full request/response
   reference — send, webhook payload, inbox polling, agent listing — and
   https://app.chariots.sh/docs has the complete docs.

## Demo: smoke-test the round-trip without a backend

`chariot demo` stands in for your service on both sides of the loop so you can
try the round-trip **once, from a terminal, before writing code**. It is not a
production interface: don't script or wrap these commands to build an
application — integrate against the HTTP API directly (`chariot api`). The
no-tunnel flow (replies are stored server-side in a reply inbox):

```bash
chariot deploy --count 1                 # no --endpoint → inbox-only
chariot demo send <agent-id> "hello" --token ts_…
chariot demo watch --token ts_…          # replies print as they arrive
```

`demo send` and `demo watch` authenticate with the token-seed from
`chariot deploy` (pass `--token` or set `CHARIOT_TOKEN_SEED`).

To exercise the real webhook path instead, run `chariot demo serve` (a local
receiver that prints every reply POSTed to it), expose the port with a tunnel
(ngrok, cloudflared), and deploy with the tunnel URL as `--endpoint`. Replies
land in the inbox either way; the webhook is an additional delivery.

## Custom agent images

By default your fleet runs the stock Chariot agent image. `chariot image push
my-agent:latest` uploads your own image (exported from your local docker
daemon, or pass `--tarball` with a `docker save` archive) and verifies it
end-to-end before your fleet adopts it: Chariot spins up one ephemeral test
agent on the image, sends it a message, and requires a reply through the
Chariot integration — you watch each phase progress in the terminal. A failed
verification never touches your running fleet.

Your image must satisfy the Chariot agent contract (entrypoint, health ports,
message delivery shim, reply endpoint) — `chariot image guidelines` prints it.
`chariot image status` shows your most recent push. Images are **named**
(`--name`, default `default`): push several and deploy different agents onto
different ones with `chariot deploy --image <name>` (`chariot images` lists
everything deployable). Agents deployed without `--image` run your account
default — change it with `chariot images set-default <name>`. Re-pushing a
name replaces that image only after the new one verifies; verification costs
a flat $0.01 plus normal metered model usage, and the test agent is
hard-capped at 10 minutes.

`--pod-size {small|medium|large}` picks the CPU/memory tier your agents run at
(default `small`, 1 cpu / 512 MiB — sized for the stock agent; `medium` is
2 cpu / 2 GiB, `large` is 4 cpu / 4 GiB). The verification agent runs at the
chosen size, and your fleet adopts the size together with the image. Heavier
runtimes — e.g. an [OpenClaw](https://openclaw.ai) gateway — need `medium`;
the Chariot repo's `chariot/docs/custom-agent-images.md` walks through a
complete, verified OpenClaw image alongside the full contract.

## Hibernation

Agents that sit idle hibernate (pod scaled to 0, session state kept; the next
message wakes them) — hibernated agents skip the daily active fee and pay only
the small storage fee. The idle window is yours to choose, in `dd:hh:mm`:

```bash
chariot hibernate-after                  # show the current window (default 48h)
chariot hibernate-after set 00:04:00     # hibernate after 4 idle hours
chariot hibernate-after set default      # back to 48h
```

Minimum 10 minutes, maximum 90 days; changes apply from the next sweep
(every ~15 minutes). To hibernate one agent right now, use the HTTP API:
`POST /v1/agents/{slug}/hibernate` (see `chariot api`).

## Configuration

| What | How |
|---|---|
| API base URL | `CHARIOT_API_URL` env, or `api_url` in `~/.chariot/config.json` (defaults to the hosted backend) |
| Session token | written by `chariot login` |

## Development

```bash
go build ./...
go vet ./...
go test ./...
```

Layout: `cmd/` (Cobra commands), `internal/api` (backend client), `internal/config`
(local config). CI runs build + vet + test on every push (`.github/workflows/ci.yml`).

## Releasing

Releases are fully automated. To ship a new version, tag main and push the tag:

```bash
git tag v0.2.0 && git push origin v0.2.0
```

The release workflow (`.github/workflows/release.yml`) runs GoReleaser, which:

1. builds `chariot` for macOS and Linux (arm64 + amd64), stamping the version
   into `chariot version` via ldflags,
2. publishes a GitHub Release with the binaries and checksums, and
3. updates the Homebrew formula in
   [Immortal-Protocols/homebrew-tap](https://github.com/Immortal-Protocols/homebrew-tap),
   so users get the new version with `brew upgrade chariot`.

The formula push authenticates with the `HOMEBREW_TAP_TOKEN` repo secret — a
fine-grained PAT (resource owner `Immortal-Protocols`) with read/write Contents
access to `homebrew-tap`. If a release fails with a 403 on the formula step,
the token has likely expired: create a new one and run
`gh secret set HOMEBREW_TAP_TOKEN -R Immortal-Protocols/Chariot-CLI`, then
delete the partial GitHub Release and re-run the workflow.
