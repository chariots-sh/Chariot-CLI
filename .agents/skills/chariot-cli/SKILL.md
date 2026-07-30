---
name: chariot-cli
description: Deploy and manage Chariot agents, images, and fleet packs with the Chariot CLI. Use when Codex needs to install or update Chariot, authenticate an account, inspect credits or agents, deploy and lifecycle agents, connect over SSH, push custom images, publish or deploy fleet packs, or wire production messaging through Chariot's HTTP API.
---

# Chariot CLI

Use the CLI for account, fleet, image, and lifecycle operations. Use Chariot's
HTTP API for application messaging.

## Start Safely

1. Run `chariot version` and `chariot --help` before relying on remembered
   syntax. Use `chariot update` when the CLI reports a newer release.
2. Install with `brew install chariots-sh/tap/chariot` when Chariot is missing.
3. Never print `~/.chariot/config.json`, token seeds, bot tokens, API keys, or
   deployment handoff files.
4. Treat purchases, permanent deletion, publication, and public endpoints as
   user-confirmed external actions.

For a second account, isolate all CLI state rather than replacing the user's
main login:

```bash
mkdir -p ~/.chariot-test-account
HOME=~/.chariot-test-account chariot login
HOME=~/.chariot-test-account chariot account
```

Keep using the same `HOME` override for every command on that account.

## Authenticate And Inspect

Run `chariot login`. It opens a browser and waits for approval. Pause when the
user must enter an email code, approve the device, or buy credits. After login,
verify the intended account before changing anything:

```bash
chariot account
chariot list
chariot images
chariot models
```

Do not infer agent state from a previous conversation; query it again.

## Deploy Agents

Choose the count, image, model, and optional webhook endpoint with the user.
Omit `--endpoint` for inbox polling.

```bash
chariot deploy --count <N> \
  [--image <image>] \
  [--model <provider/model>] \
  [--endpoint <https-url>]
```

Agents start deactivated and wake on first use. Deployment prints a token seed
once. Capture output in a private directory, immediately store the seed as
`CHARIOT_TOKEN_SEED` in a mode-`0600` secret file or secret manager, and ensure
it cannot enter source control. Do not paste it into chat or terminal summaries.

After deployment, verify:

```bash
chariot list
chariot account
```

Use the long UUID from `chariot list` when an API or destructive command
requires an agent ID. Use the slug where the command explicitly accepts a slug.

## Message An Agent

Message an agent with the logged-in session; no token seed is required:

```bash
chariot message <agent> "hello"        # waits up to --wait (default 3m) for the reply
chariot inbox --follow                 # every reply as it arrives
```

`<agent>` is an id, slug, or name. A hibernating agent is woken by the message
and the send is retried while its pod starts, regardless of `--wait`. `--wait`
covers only the reply; `--wait 0` returns once the agent has the message, and
the reply still lands in `chariot inbox`.

For production integrations, run `chariot api` and call the HTTP API directly:

- `POST /v1/agents/{agent-id}/messages` to send.
- Receive replies through the configured webhook or poll `GET /v1/replies`.
- Authenticate with the deployment token seed as documented by `chariot api`.

Never build an application by spawning CLI processes.

## Work In A Workspace

A workspace groups agents around a shared chat thread, shared documents, and
agent-to-agent messaging. Address a workspace by name.

```bash
chariot workspace list
chariot workspace create <name>
chariot workspace add <workspace> <agent> [<agent>...]
chariot workspace chat <workspace> "message"              # broadcast to all members
chariot workspace chat <workspace> "message" --agent <agent>
chariot workspace docs <workspace>                        # shared documents
chariot workspace crosstalk <workspace>                   # agent-to-agent messages
```

Adding an agent grants it the workspace tools. `chariot workspace skills
<workspace>` shows coverage; `skills add`/`remove` apply to every member.
Deleting a workspace does not delete its agents.

## Drive Standing Goals

A goal is a standing objective one workspace member pursues on its own until
it completes, blocks, or is canceled. Every `chariot goal` command requires
`--workspace <name-or-id>`.

```bash
chariot goal set <agent> "objective" --workspace <workspace>
chariot goal status <agent> --workspace <workspace>
chariot goal pause <agent> --workspace <workspace>
chariot goal resume <agent> --workspace <workspace>
chariot goal cancel <agent> --workspace <workspace>
chariot goal history <agent> --workspace <workspace> [--limit N]
```

An agent holds at most one open goal. `goal set` on an agent that already has
one fails with a 409; pass `--replace` to supersede it. Replacing and
canceling stop the standing work, so both prompt — pass `-y` only after the
user confirms. Check `goal status` before pausing, resuming, or canceling;
the CLI sends the version it just read so a concurrent change fails loudly
instead of being overwritten.

## Manage Lifecycle

Query state before acting:

```bash
chariot list
```

- `chariot hibernate <agent-slug>` is reversible. It stops active compute while
  preserving persistent state; a message or SSH session wakes the agent.
- `chariot hibernate-after` manages automatic idle hibernation. Its duration is
  `dd:hh:mm`.
- `chariot delete <agent-uuid> --yes` is permanent. Confirm intent and the UUID
  immediately before deletion.

Do not delete an agent merely to stop charges; hibernate it unless the user
explicitly wants the persistent state destroyed.

## Connect Over SSH

Use Chariot-issued short-lived certificates:

```bash
chariot ssh <agent-slug>
chariot ssh <agent-slug> -- <command> [args...]
chariot ssh --config <agent-slug>
```

SSH can wake a hibernating agent. Prefer non-interactive remote commands for
repeatable checks. Use `--config` only when the user needs plain `ssh`, `scp`,
or VS Code Remote access.

## Push A Custom Image

1. Run `chariot image guidelines` and read the complete container contract.
2. Ensure Docker is running and build for `linux/amd64`.
3. Verify locally before upload.
4. Push with a stable name and explicit pod size:

```bash
chariot image push <image:tag> --name <name> --pod-size <small|medium|large>
chariot image status
```

Wait for `ready`. A failed push does not alter the running fleet. Push and
verify the intended image before sending the first message to newly deployed
agents, because first wake selects their runtime.

## Work With Fleet Packs

Fleet packs bundle image/count recipes and may carry a setup skill. The setup
skill guides the recipient's coding agent; it is not injected into agent pods.

```bash
chariot fleet create <pack> --image <name>:<count> \
  --description "<description>"
chariot fleet skill set <pack> <file.md>
chariot fleet publish <pack>

chariot fleet browse
chariot deploy-fleet <pack> --from <owner-email>
```

Before publishing, verify the pack composition and skill. Before deploying
another account's pack, report its images, agent count, and daily fee. Secure
the fleet token seed exactly like a normal deployment.

Use `chariot fleet unpublish <pack>` to stop discovery and new deployments;
existing deployed fleets continue running. Delete a pack only when the user
intends to remove the recipe itself.

## Finish

Report the account, created or affected agent slugs, image/model, final states,
active daily fees, verification performed, and any remaining setup owned by the
user. Never include secret values. Hibernate disposable test agents and remove
temporary watchers or test resources before handing off.
