# Chariot CLI

Deploy and manage enterprise agent fleets from your terminal.

```
chariot login                                        # authenticate (opens browser)
chariot deploy --count 10000 --endpoint https://…    # spin up a fleet
chariot list                                         # agents + their ids
chariot account                                      # credits + status
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
