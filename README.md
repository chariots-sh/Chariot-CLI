# Chariot CLI

Deploy and manage enterprise agent fleets from your terminal.

The CLI's job is fleet management: login, deploy, list — plus talking to your
agents yourself. Driving agents at scale in production is your service's job,
via the [HTTP API](https://app.chariots.sh/docs) — `chariot api` prints the full
reference.

```
chariot login                                        # authenticate (opens browser)
chariot deploy --count 10000 --endpoint https://…    # spin up a fleet
chariot list                                         # agents + their ids
chariot message researcher "status?"                 # message an agent, print its reply
chariot inbox --follow                               # watch replies as they arrive
chariot workspace chat research "who has capacity?"  # ask a group of agents at once
chariot rename agent-000003 researcher               # name an agent; the name works anywhere an id/slug does
chariot hibernate my-agent-3                         # pause one agent's compute; keep its session state
chariot account                                      # credits + status
chariot api                                          # HTTP API reference for your service
chariot images                                       # deployable images (built-in + yours)
chariot image push my-agent:latest --pod-size medium # run your OWN agent image (verified first)
chariot hibernate-after set 00:04:00                 # idle 4h → agents hibernate
```

## Install

```bash
brew install chariots-sh/tap/chariot
# or via Go:
go install github.com/chariots-sh/Chariot-CLI@latest
# or build locally:
go build -o chariot .
```

## Agent skill

Codex discovers the repository's Chariot workflow at
`.agents/skills/chariot-cli/SKILL.md`. Invoke it explicitly with
`$chariot-cli`, or let Codex select it when a task involves Chariot account,
agent, image, fleet, SSH, or API operations.

## The one user journey

1. `chariot login` — opens your browser to the Chariot site. Sign in (email code)
   and buy credits, then approve the CLI. The CLI stores a session token in
   `~/.chariot/config.json`. No email or card? `chariot login --wallet` signs in
   with a Base wallet and `chariot fund` tops up with USDC — see
   [Wallet sign-in + USDC funding](#wallet-sign-in--usdc-funding).
2. `chariot deploy --count N --endpoint URL` — creates `N` agents (they start
   deactivated — not yet woken by a message — and cost nothing until messaged)
   and prints a **token-seed** (shown once). `URL` is where your agents POST
   their replies.
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

   To talk to an agent yourself instead of through a service, use
   `chariot message <agent> "…"` — same endpoint, authenticated with your login
   rather than the token-seed.

## Wallet sign-in + USDC funding

An account needs neither an email nor a card. The CLI holds a Base wallet
(a secp256k1 key in `~/.chariot/wallet.json`, 0600) that signs Chariot's
sign-in challenge — the first sign-in creates the account — and pays by
sending USDC on Base to Chariot's treasury.

```bash
chariot login --wallet          # create (or reuse) the local wallet and sign in
chariot wallet                  # its address + USDC/ETH balance on Base
# send USDC (and a little ETH for gas) on Base to that address, then:
chariot fund 25                 # transfer 25 USDC to the treasury and credit it
chariot account                 # credits: $25.00
```

Prefer your own wallet app? `chariot login --wallet-address 0x…` prints the
message to sign; paste the signature back (MetaMask, Rabby, `cast wallet
sign`, …). To fund from it, send USDC to the treasury shown by `chariot fund`
and then `chariot fund --tx <hash>`.

Deposits are credited by **sender**: only USDC sent from the wallet that owns
(or is linked to) your account counts — never from an exchange. An account
created by email links a wallet with `chariot wallet link`. Set
`CHARIOT_WALLET_PRIVATE_KEY` to sign headlessly with a key that never touches
disk (CI, agents); `chariot wallet import` reads a key from stdin.

## Talking to your agents

Your `chariot login` session is enough — the token-seed is for services, not
for you:

```bash
chariot message researcher "summarize today's filings"   # waits for the reply
chariot message researcher "long job" --wait 0           # don't wait for the reply
chariot inbox --follow                                   # every reply, as it lands
```

`message` wakes a hibernating agent and re-sends while its pod starts (up to
3m) — that happens whatever `--wait` says, since a message that never arrived
has no reply coming. `--wait` (default 3m) governs only how long to stay for
the answer, which it matches by a correlation id it sends along; `--wait 0`
returns as soon as the agent has the message.

The reply is stored either way, so a slow one is still there in `chariot inbox`
later — and still goes to your fleet's `--endpoint` webhook.

## Workspaces

A workspace is a group of your agents with a shared chat thread, shared
documents, and the ability to hand work to each other. They are the same
workspaces the web app shows.

```bash
chariot workspace create research
chariot workspace add research agent-000001 agent-000002
chariot workspace chat research "who has capacity?"          # every member answers
chariot workspace chat research "start part 2" --agent scout # just that member
chariot workspace chat research --follow                     # read the thread live
chariot workspace docs research                              # what they wrote down
chariot workspace crosstalk research                         # what they told each other
```

Joining a workspace equips an agent with the shared-documents and
agent-messaging tools; `chariot workspace skills research` shows who holds
what, and `skills add`/`remove` change it for every member at once. Documents
are addressed by title: `docs read`, `docs write … --file`, `docs delete`.

You can also invite **guests** — people, not builders — to chat with chosen
member agents. A guest signs in at the web app's `/g` page with just their
email (their first invite emails them the link), shares the same 1:1 thread
you see in the web chat, and their messages bill your account.

```bash
chariot workspace guests research                                # who has access
chariot workspace guests add research alice@example.com scout    # invite / extend
chariot workspace guests remove research alice@example.com scout # revoke one agent
chariot workspace guests remove research alice@example.com       # revoke everything
```

Every workspace command takes the workspace's name (or its id), and any member
can be addressed by id, slug, or name.

## Goals

A goal is a standing objective one workspace member keeps working toward on
its own schedule — across turns, without you prompting each step — until it
completes, blocks, or you cancel it.

```bash
chariot goal set scout "ship the Q3 report" --workspace research
chariot goal status scout --workspace research   # plan, progress, recent events
chariot goal pause scout --workspace research    # stop the autonomous turns
chariot goal resume scout --workspace research
chariot goal cancel scout --workspace research   # end it for good
chariot goal history scout --workspace research  # every goal, newest first
```

An agent holds at most one open goal; `goal set --replace` supersedes it (the
standing work on the old goal stops, so it prompts first). The objective can
also be piped in on stdin: `cat objective.md | chariot goal set scout
--workspace research`.

## Agent lifecycle

- `deactivated`: deployed but never messaged. There is no running pod yet, so
  there is no active compute fee.
- `active`: the agent has been messaged and its pod is running. Active agents
  accrue the daily fee for their image's pod size.
- `hibernating`: the pod is scaled to 0 after the idle window, or immediately
  with `chariot hibernate <agent>`. Session state is kept, compute billing
  stops, and the next message wakes the agent.
- `deleted`: `chariot delete <agent>` permanently tears down the agent's
  pod, PVC, and session state. Use `hibernate` when you only want to stop
  compute while preserving state.

Anywhere a command takes an agent, its id, slug, or owner-chosen name are
interchangeable. `chariot rename <agent> <name>` sets the name (`--clear`
removes it); it shows in the NAME column of `chariot list`. Names are 1-63
lowercase letters, digits, or hyphens, unique within your fleet — the slug and
id never change, so renaming is safe at any time.

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
`chariot deploy` (pass `--token` or set `CHARIOT_TOKEN_SEED`) — deliberately,
because they stand in for a service holding that credential. To just talk to an
agent, use `chariot message` / `chariot inbox`, which use your login.

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
everything deployable). Swap an existing agent onto a different image with
`chariot images set <name> --agent <agent>` — a running agent is re-imaged in
place (its pod restarts on the new image; its workspace is kept), a
hibernating or never-activated one picks the image up when it next starts,
and the daily active fee follows the new image's pod size. `chariot images
set default --agent <agent>` clears the override. Agents deployed without
`--image` run your account default — change it with `chariot images
set-default <name>`. Re-pushing a
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
the small storage fee. The idle window is yours to choose, in `dd:hh:mm`
(days:hours:minutes), so `02:00:00` means two days and `00:02:00` means two
hours:

```bash
chariot hibernate-after                              # show the fleet window (default 48h)
chariot hibernate-after set 00:04:00                 # fleet: hibernate after 4 idle hours
chariot hibernate-after set default                  # fleet: back to 48h
chariot hibernate-after set 00:06:00 --agent <id>    # one agent: 6h (overrides the fleet window)
chariot hibernate-after set default --agent <id>     # one agent: back to the fleet window
```

The fleet window is the default for every agent; `--agent <id>` (find ids with
`chariot list`) overrides just that one. Per-agent overrides show in the
`HIBERNATE` column of `chariot list`. Minimum 10 minutes, maximum 90 days;
changes apply from the next sweep (every ~15 minutes). To hibernate one agent
right now, use `chariot hibernate <agent-slug>`.

## Configuration

| What | How |
|---|---|
| API base URL | `CHARIOT_API_URL` env, or `api_url` in `~/.chariot/config.json` (defaults to the hosted backend) |
| Session token | written by `chariot login` |
| Wallet key | `~/.chariot/wallet.json` (0600), or `CHARIOT_WALLET_PRIVATE_KEY` |
| Base RPC | `CHARIOT_BASE_RPC_URL` env or `--rpc` (defaults to `https://mainnet.base.org`) |

## Development

```bash
go build ./...
go vet ./...
go test ./...
```

Layout: `cmd/` (Cobra commands), `internal/api` (backend client), `internal/config`
(local config), `internal/wallet` (Base wallet: key, EIP-191 signing, EIP-1559 USDC transfer, RPC). CI runs build + vet + test on every push (`.github/workflows/ci.yml`).

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
   [chariots-sh/homebrew-tap](https://github.com/chariots-sh/homebrew-tap),
   so users get the new version with `brew upgrade chariot`.

The formula push authenticates with the `HOMEBREW_TAP_TOKEN` repo secret — a
fine-grained PAT (resource owner `chariots-sh`) with read/write Contents
access to `homebrew-tap`. If a release fails with a 403 on the formula step,
the token has likely expired: create a new one and run
`gh secret set HOMEBREW_TAP_TOKEN -R chariots-sh/Chariot-CLI`, then
delete the partial GitHub Release and re-run the workflow.
