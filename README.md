# Chariot CLI

Deploy and manage enterprise agent fleets from your terminal.

```
chariot login                                        # authenticate (opens browser)
chariot deploy --count 10000 --endpoint https://…    # spin up a fleet
chariot list                                         # agents + their ids
chariot account                                      # credits + status
chariot demo send <agent-id> "hello"                 # message an agent
chariot demo watch                                   # poll the reply inbox
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
4. From your own backend, message an agent:
   ```
   POST {chariot-base}/v1/agents/{agent-id}/messages
   header  X-Chariot-Token: <token-seed>
   body    {"message": "…"}
   ```
   The agent replies to your `--endpoint`.

## Demo: the round-trip without a backend

`chariot demo` stands in for your backend on both sides of the loop. The
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
