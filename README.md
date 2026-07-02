# Chariot CLI

Deploy and manage enterprise agent fleets from your terminal.

```
chariot login                                        # authenticate (opens browser)
chariot deploy --count 10000 --endpoint https://…    # spin up a fleet
chariot list                                         # agents + their ids
chariot account                                      # credits + status
chariot demo serve                                   # receive agent replies locally
chariot demo send <agent-id> "hello"                 # message an agent
```

## Install

```bash
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

`chariot demo` stands in for your backend on both sides of the loop:

1. `chariot demo serve` — a local webhook receiver that prints every reply
   POSTed to it (`{"agent_id", "message", "reply_to"}`). The hosted backend can
   only reach a public URL, so expose the port with a tunnel (ngrok,
   cloudflared) and use the tunnel URL as your deploy `--endpoint`.
2. `chariot demo send <agent-id> "hello"` — sends a message to an agent using
   the token-seed from `chariot deploy` (pass `--token` or set
   `CHARIOT_TOKEN_SEED`). The reply arrives at the `--endpoint`, i.e. in the
   `demo serve` terminal.

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
