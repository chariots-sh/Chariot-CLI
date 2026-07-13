# Working on the Chariot CLI

Go CLI built on Cobra. `cmd/` holds one file per command plus its tests,
`internal/api` the backend HTTP client, `internal/config` local config
(`~/.chariot/config.json`), `internal/update` self-update, `internal/demo`
the demo webhook receiver, `internal/sshsession` the `chariot ssh` session.

Note: `.agents/skills/chariot-cli/SKILL.md` documents *using* the CLI (for
end users' coding agents), not developing it. Keep it updated when you change
user-visible behavior.

## Build, test, verify

```bash
go build ./...
go vet ./...
go test ./...
```

All three must pass before pushing; CI runs them on every push.

## Conventions

- **Output through the command**: write to `cmd.OutOrStdout()` /
  `cmd.ErrOrStderr()`, never `fmt.Print*` directly — tests capture output
  through the command.
- **Errors that teach**: user-facing errors name the fixing command, e.g.
  "not logged in — run `chariot login` first". Use `RunE` and return errors;
  don't `os.Exit` mid-flow (`Execute()` handles the exit code).
- **No network in tests**: set `disableAutoUpdateCheck = true` and point the
  API client at an `httptest` server. Never hit the real backend, GitHub, or
  Docker.
- **Secret hygiene**: token-seeds and session tokens never go into logs or
  world-readable files; files containing secrets are written `0600`.
- **Error wrapping**: `fmt.Errorf("...: %w", err)`; pass `cmd.Context()` into
  API calls.
- **Help/docs drift**: changing a command or flag means updating its
  Use/Short/Long text, and the README when user-visible.

## Releasing

Tag main and push the tag (`git tag vX.Y.Z && git push origin vX.Y.Z`).
GoReleaser builds macOS/Linux binaries, publishes the GitHub Release, and
updates the Homebrew formula in chariots-sh/homebrew-tap. A 403 on the
formula step means `HOMEBREW_TAP_TOKEN` expired: create a new fine-grained
PAT (owner `chariots-sh`, read/write Contents on `homebrew-tap`), run
`gh secret set HOMEBREW_TAP_TOKEN -R chariots-sh/Chariot-CLI`, delete the
partial Release, and re-run the workflow.

## PR reviews

Every PR gets automated reviews from Claude (`claude-code-review.yml`) and
Codex (`codex-review.yml`). Label a PR `ai:claude` or `ai:codex` to skip the
matching reviewer (used when that model authored the PR).
