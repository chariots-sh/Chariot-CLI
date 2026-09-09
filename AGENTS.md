# Working on the Chariot CLI

Go CLI built on Cobra. The product's core split, which the code and all docs
enforce: the **CLI manages fleets** (login, deploy, list, images, hibernation);
**production messaging goes through the HTTP API** (`chariot api` prints it);
`chariot demo` is a one-off terminal smoke test, never a production interface.
Don't blur these lines in help text, docs, or new commands.

## Layout

- `main.go` — entrypoint; calls `cmd.Execute()`.
- `cmd/` — one file per command (`deploy.go`, `list.go`, …) plus its test
  (`deploy_test.go`). All commands hang off the global `rootCmd` in `root.go`;
  flags bind to package-level vars registered in each file's `init()`.
- `internal/api` — typed HTTP client for the backend. One method per endpoint,
  all going through `Client.do`/`doHeaders`; non-2xx becomes `*APIError`
  (`Detail` + `Status`), which commands can return directly — the detail text
  is user-readable.
- `internal/config` — `~/.chariot/config.json` (session token, optional
  `api_url`). `CHARIOT_API_URL` env overrides.
- `internal/update` — self-update + the background version check
  (`update.CheckInBackground`) that `PersistentPostRun` uses to print upgrade
  hints.
- `internal/demo` — local webhook receiver behind `chariot demo serve`.
- `internal/sshsession` — terminal session plumbing for `chariot ssh`.
- `.agents/skills/chariot-cli/SKILL.md` — documents *using* the CLI, for end
  users' coding agents. Update it when user-visible behavior changes; it is
  not about developing this repo.

## Build, test, verify

```bash
go build ./...
go vet ./...
go test ./...
```

All three must pass before pushing; CI (`ci.yml`) runs them on every push.

## Adding or changing a command

1. New file in `cmd/` with the `cobra.Command`, flags as package-level vars,
   registration in `init()` (`rootCmd.AddCommand(...)` or a parent command).
2. Get the API client via `authedClient()` — it returns the friendly
   "not logged in — run `chariot login` first" error for you.
3. New backend calls get a typed method in `internal/api`, not inline HTTP.
4. Update the command's Use/Short/Long text and the README when user-visible.
5. Add the new flags to `resetFlags` in `cmd/main_test.go` (see below), and
   write the test alongside.

## Testing patterns (cmd/main_test.go)

- `runCLI(t, stdin, args...)` runs the real root command and captures both
  streams; assert with `mustContain` / `mustNotContain`.
- `login(t, handler)` spins up an `httptest.Server`, points `CHARIOT_API_URL`
  at it, and seeds a config with a session token in an isolated `HOME`.
  `logout(t)` gives an empty HOME.
- **Global-flag leak gotcha**: flags bind to package vars on a single global
  `rootCmd`, and Cobra does not reset them between runs. Every new flag MUST
  be added to `resetFlags`, or one test's flag value silently leaks into the
  next test.
- `TestMain` sets `disableAutoUpdateCheck = true` for the whole test binary so
  `PersistentPostRun` never hits GitHub. Tests exercising the update notice
  flip it back with `t.Cleanup`; `resetFlags` deliberately leaves it alone.
- **No network in tests**, ever: no real backend, GitHub, or Docker.

## Conventions

- **Output through the command**: write to `cmd.OutOrStdout()` /
  `cmd.ErrOrStderr()`, never `fmt.Print*` directly — tests capture output
  through the command.
- **Errors that teach**: user-facing errors name the fixing command, e.g.
  "not logged in — run `chariot login` first". Use `RunE` and return errors;
  don't `os.Exit` mid-flow (`Execute()` handles the exit code).
- **Secret hygiene**: token-seeds and session tokens never go into logs or
  world-readable files; files containing secrets are written `0600` (see
  `writeFleetHandoffFile`). The token-seed is shown once at deploy — treat
  any output path that touches it accordingly.
- **Error wrapping**: `fmt.Errorf("...: %w", err)`; pass `cmd.Context()` into
  API calls.
- **Prompting**: interactive flows share ONE `bufio.Reader` per command (a
  second reader would swallow read-ahead input — see `deploy_fleet.go`), and
  every prompt needs a `--yes`/flag path so the command stays scriptable.

## CI and workflows

- `ci.yml` — build + vet + test on every push.
- `codex-review.yml` — automated Codex review on every PR. Label a PR
  `ai:codex` to skip it (used when Codex authored the PR).
- `release.yml` — GoReleaser on tag push; also announces the release to
  Discord (`DISCORD_*` secrets).
- `repo-backup.yml` — scheduled mirror.

## Releasing

Tag main and push the tag (`git tag vX.Y.Z && git push origin vX.Y.Z`).
GoReleaser builds macOS/Linux binaries (arm64 + amd64), stamps the version
into `chariot version` via ldflags, publishes the GitHub Release, and updates
the Homebrew formula in chariots-sh/homebrew-tap. A 403 on the formula step
means `HOMEBREW_TAP_TOKEN` expired: create a new fine-grained PAT (owner
`chariots-sh`, read/write Contents on `homebrew-tap`), run
`gh secret set HOMEBREW_TAP_TOKEN -R chariots-sh/Chariot-CLI`, delete the
partial Release, and re-run the workflow.
