# Plan: Fix the `go install` Path

## Problem

`go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest` produces two surprises:

1. **Wrong binary name.** The binary lands at `$GOPATH/bin/find-the-gaps`, not `ftg`. Every doc, command, and example uses `ftg`. The README currently papers over this with a `ln -s` hack (`README.md:62-66`). That's a bad first impression — the tool's own docs don't work until the user runs an extra command.
2. **No external dependencies.** `go install` only installs the Go binary — it cannot pull in `mdfetch` or `hugo`, which Find the Gaps shells out to. A fresh `go install` user runs `ftg analyze` and the tool fails on the first `mdfetch` invocation.

Issue #1 is fixable. Issue #2 is fundamental to `go install` — the realistic fix is to make sure the runtime experience tells go-install users exactly what to do.

## Fix #1 — Rename `cmd/find-the-gaps/` → `cmd/ftg/`

Go's `go install` names the binary after the last segment of the package path. Rename the cmd dir and the binary becomes `ftg` automatically — no aliases, no symlinks, no install-time scripts.

The module path (`github.com/sandgardenhq/find-the-gaps`), the GitHub repo, and the Homebrew formula name (`find-the-gaps`) all stay unchanged. Only the cmd subdirectory moves.

### Files to change

| File | Line | Change |
|---|---|---|
| `cmd/find-the-gaps/` (entire dir) | — | `git mv cmd/find-the-gaps cmd/ftg` (preserves history; moves `main.go`, `main_test.go`, and the `testdata/script/` testscript fixtures together) |
| `Makefile` | 4 | `PKG := ./cmd/ftg` |
| `.goreleaser.yaml` | 20 | `main: ./cmd/ftg` |
| `.github/workflows/release.yml` | 60 | `./cmd/ftg` (the `go build` invocation that produces `dist/ftg`) |
| `internal/reporter/reporter_test.go` | 372 | `Files: []string{"cmd/ftg/main.go"}` (test fixture string, unrelated to the binary path but currently mentions the old cmd dir) |
| `internal/updatecheck/notice.go` | 12 | `goLine = "  go install github.com/sandgardenhq/find-the-gaps/cmd/ftg@latest"` |
| `internal/updatecheck/notice_test.go` | 24, 34, 44, 53 | Update the four asserted substrings to match the new path |
| `README.md` | 59 | Replace the `go install ...cmd/find-the-gaps@latest` line with `...cmd/ftg@latest`, **delete** the symlink workaround on lines 62–66 (no longer needed) |
| `CLAUDE.md` | 297 | Update the testscript path comment to `cmd/ftg/testdata/*.txtar` |
| `.plans/UPDATE_CHECK_ON_STARTUP.md` | 18 | Reference-only update; non-load-bearing |

Search command to confirm nothing was missed:

```sh
rg -nF 'cmd/find-the-gaps' --hidden
```

Expected result after the change: the only matches should be in historical plans (`.plans/2026-04-30-screenshots-opt-in-plan.md`, `.plans/MISSING_SCREENSHOTS_IMPLEMENTATION_PLAN.md`, `.plans/2026-04-29-drift-cache.md`) and `PROGRESS.md`. Those describe past work and shouldn't be rewritten — they document the world as it was.

### Verification

```sh
make build                         # produces ./ftg as before
make test                          # all unit tests + testscripts green
go test ./internal/updatecheck/... # the upgrade-notice substring assertions pass
go install ./cmd/ftg               # ~/go/bin/ftg exists; no `find-the-gaps` binary
~/go/bin/ftg --version             # version flag works
```

The release workflow already produces `dist/ftg` (it overrides `-o`), so the artifact name and tarball name are unaffected — only the source path changes.

### Risk: a stale `cmd/find-the-gaps/` directory after rebase

If anyone has in-flight work that adds files under `cmd/find-the-gaps/`, a merge will resurrect the old dir while the new code paths point at `cmd/ftg/`. Mitigation: do this rename on a quiet day, or coordinate by checking open PRs/branches for paths under `cmd/find-the-gaps/` before merging. There are no destructive `rm` operations here — `git mv` keeps history.

## Fix #2 — Upfront Dependency Checks on `analyze` and `render`

`go install` cannot install non-Go binaries. CLAUDE.md already accepts this:

> `go install` works as a fallback for Go users, but users are then responsible for installing `mdfetch` themselves. The CLI should detect the missing binary on startup and print a clear install hint.

The fix is two-pronged: tell users in the README, and have the commands that need `mdfetch`/`hugo` check upfront and refuse to start with a clear, formatted install message. Today both `analyze` and `render` only fail *during* execution — `analyze` after spinning up workers and partially populating the cache, `render` after walking the whole spider index. That's wasteful and the error message is a one-liner buried under a stack of `fmt.Errorf` wrapping.

### Which commands need which dep

| Command | `mdfetch` | `hugo` | Notes |
|---|---|---|---|
| `analyze` | required | required unless `--no-site` | Spider always shells out to `mdfetch`; site build is the default and uses `hugo` |
| `render`  | not used | required | Pure re-render from cached data; no docs ingestion |
| `serve`   | not used | not used | Serves static files from `<projectDir>/site/`; no shell-out |
| `doctor`  | n/a (the check itself) | n/a | Already checks both |

`serve` does not need a dependency check. Adding one would be wrong — a user who built the site on machine A and copied `<projectDir>/` to machine B (or installed via `go install` and only ever wants to serve a pre-built report) does not need `mdfetch` or `hugo` to be present.

### Implementation shape

Add a small helper in `internal/doctor/` that `analyze` and `render` can call from their `RunE` before any other work. The helper reuses the existing `RequiredTools` data so install hints stay in one place.

```go
// internal/doctor/precheck.go (new file)

// RequireTools verifies the named tools are on $PATH. On success returns nil.
// On failure returns an error whose message is a multi-line formatted install
// guide that the caller can return directly from cobra's RunE.
//
// Names must match Tool.Name in RequiredTools (currently "mdfetch", "hugo").
func RequireTools(ctx context.Context, names ...string) error
```

Failure message shape (printed to stderr by cobra when RunE returns the error):

```
ftg analyze needs the following external tool(s):

  mdfetch — not found on $PATH
    install: brew install sandgardenhq/tap/mdfetch
             (or: npm install -g @sandgarden/mdfetch@latest)

  hugo — not found on $PATH
    install: brew install hugo
             (or: https://github.com/gohugoio/hugo/releases)

Run `ftg doctor` to verify after installing. Pass --no-site to skip Hugo.
```

The "Pass --no-site to skip Hugo" tail is conditional — only included when the missing tool is `hugo` and the calling command supports `--no-site` (i.e. `analyze`). Wire that as a small `Hint` field per call site rather than baking it into the helper.

### Call sites

1. **`internal/cli/analyze.go`** — at the top of `RunE`, after flag validation, before any cache reads:
   ```go
   required := []string{"mdfetch"}
   if !noSite {
       required = append(required, "hugo")
   }
   if err := doctor.RequireTools(cc.Context(), required...); err != nil {
       return err
   }
   ```
   Then **delete** the late-stage hugo error at `analyze.go:607` (the `errors.Is(err, site.ErrHugoMissing)` branch) — the upfront check makes it unreachable. Leave the `site.Build` call site clean.

2. **`internal/cli/render.go`** — same pattern, hugo-only:
   ```go
   if err := doctor.RequireTools(cc.Context(), "hugo"); err != nil {
       return err
   }
   ```
   Delete the late-stage hugo error at `render.go:149`.

3. **`internal/doctor/doctor.go:29`** — update the `mdfetch` install hint to lead with the brew formula:
   ```go
   InstallHint: "brew install sandgardenhq/tap/mdfetch (or: npm install -g @sandgarden/mdfetch@latest)",
   ```
   `RequireTools`'s formatted error reuses this string, so doctor and the precheck stay in lockstep.

4. **`internal/cli/serve.go`** — no change. Serve does not need either binary.

### Tests

- **`internal/doctor/precheck_test.go`** (new) — table tests with hermetic `t.Setenv("PATH", t.TempDir())` and shell-script fakes (mirrors the existing `doctor_test.go` pattern):
  - Both tools present → returns nil.
  - One missing → returns error containing the binary's name and install hint.
  - Both missing → returns one error listing both, in the order requested.
- **`cmd/ftg/testdata/script/analyze_missing_mdfetch.txtar`** (new) — testscript that scrubs `mdfetch` from `$PATH` and asserts: non-zero exit, stderr contains `mdfetch — not found`, and `<projectDir>/` is **not** created (proving the check runs before any work).
- **`cmd/ftg/testdata/script/render_missing_hugo.txtar`** (new) — same shape for `render`.
- **`internal/cli/analyze_test.go`** — update any test that scrubbed `hugo` from `$PATH` to expect the new precheck error message rather than the old `errors.Is(err, site.ErrHugoMissing)` message.

### README updates

After removing the symlink workaround, the "Other platforms" section becomes:

```sh
go install github.com/sandgardenhq/find-the-gaps/cmd/ftg@latest
```

Followed by a tight callout. The Homebrew-first install path is the wrong default here — anyone on the `go install` track is, by selection, not on the Homebrew track. Tell them to use `npm` for `mdfetch` and the upstream Hugo releases page for `hugo`:

> `go install` only installs the `ftg` binary. `analyze` and `render` also need `mdfetch` and `hugo` on your `$PATH`:
>
> ```sh
> npm install -g @sandgarden/mdfetch@latest
> ```
>
> For `hugo`, grab a binary from [github.com/gohugoio/hugo/releases](https://github.com/gohugoio/hugo/releases) (or use your distro's package manager).
>
> Run `ftg doctor` to confirm. If `mdfetch` or `hugo` is missing, the offending command will refuse to run and tell you exactly what to install.

This makes the contract explicit, and the install commands match the audience: a user on the `go install` path is given a non-Homebrew install path. (The Homebrew users have their own section above; the formula's `depends_on` already wires `mdfetch` and `hugo` in for them.)

### Things explicitly NOT being done

- **No check on `serve`.** It doesn't need either binary. Adding one would block legitimate use cases.
- **No `ftg install-deps` subcommand.** Auto-installing third-party binaries is invasive, brittle, and out of spec.
- **No first-run banner.** CLAUDE.md describes one but the upfront precheck on `analyze`/`render` covers the same need at the right moment (when the user actually invokes a command that needs the deps), without firing for `--version`, `--help`, `serve`, or `doctor`.
- **No keeping `cmd/find-the-gaps/` as a wrapper.** No reason for two cmd dirs once Fix #1 lands.
- **No renaming of the GitHub repo or module path.**

## Suggested commit shape

One PR, three commits:

1. `refactor(cmd): rename cmd/find-the-gaps to cmd/ftg`
   - The `git mv` + every code/docs/yaml reference update.
   - Tests stay green.

2. `feat(cli): precheck mdfetch/hugo on analyze and render`
   - New `internal/doctor/precheck.go` + tests.
   - Wire into `analyze` and `render` `RunE`. Delete the now-unreachable late-stage hugo error branches.
   - New testscripts under `cmd/ftg/testdata/script/`.

3. `docs(install): clarify go install path requires mdfetch + hugo`
   - README callout under "Other platforms" (delete the symlink workaround).
   - `internal/doctor/doctor.go` mdfetch install-hint update + matching `doctor_test.go` fixture update if it asserts on the string.

Branch: `refactor/cmd-ftg-rename` (per CLAUDE.md branch-naming conventions).

## Acceptance

- [ ] `go install github.com/sandgardenhq/find-the-gaps/cmd/ftg@latest` produces a binary named `ftg`.
- [ ] `make build`, `make test`, `make lint` all green.
- [ ] `rg -nF 'cmd/find-the-gaps' --hidden` returns matches only in historical plan files and `PROGRESS.md`.
- [ ] README's go-install path no longer requires a symlink workaround and tells the user about `mdfetch`/`hugo` upfront.
- [ ] With `mdfetch` removed from `$PATH`, `ftg analyze ...` exits non-zero before any work and prints an install message naming `mdfetch`.
- [ ] With `hugo` removed from `$PATH`, `ftg analyze ...` exits non-zero before any work; the install message names `hugo` and mentions `--no-site`.
- [ ] With `hugo` removed from `$PATH`, `ftg analyze --no-site ...` runs cleanly to completion.
- [ ] With `hugo` removed from `$PATH`, `ftg render ...` exits non-zero before any work and prints an install message naming `hugo`.
- [ ] With both binaries removed from `$PATH`, `ftg serve ...` still serves a pre-built site.
- [ ] `ftg doctor` prints a Homebrew-first install hint for `mdfetch`.
- [ ] Release workflow still produces `dist/ftg` and the `find-the-gaps_<tag>_<os>-<arch>.tar.gz` archives unchanged.
