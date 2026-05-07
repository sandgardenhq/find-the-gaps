# Update Check on Startup

## Goal

When `ftg` runs, check whether a newer release is available on GitHub. If one is, print a short, platform-aware upgrade notice on stderr **after** the command finishes. Stay quiet, fast, and out of the user's way the rest of the time.

## User experience

When the running version is behind the latest GitHub Release tag, the user sees something like this on stderr after their command completes:

```
A new version of ftg is available: v1.4.2 (you have v1.3.0)

To upgrade on macOS or Linux with Homebrew:
  brew upgrade sandgardenhq/tap/find-the-gaps

Or with Go:
  go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest

Release notes: https://github.com/sandgardenhq/find-the-gaps/releases/tag/v1.4.2
```

The notice never replaces, prefixes, or interleaves with command output. It is appended once, on stderr, after the command's normal output and any first-run banner.

## When the check runs

The check runs at most **once per 24h per machine**, gated by a cache file under the existing config dir (`~/.find-the-gaps/update-check.json`).

It runs for: `analyze`, `render`, `serve`, `doctor`.

It is skipped for:

- `ftg --version`, `ftg help`, `ftg completion ...` (trivially fast, often scripted).
- Dev builds — `currentVersion() == "dev"`.
- CI environments — `CI` env var set to a truthy value.
- Non-TTY stderr — when the user has piped or redirected stderr away.
- `FIND_THE_GAPS_QUIET=1` (already established for the first-run banner).
- `FIND_THE_GAPS_NO_UPDATE_CHECK=1` (dedicated kill switch, in case the user wants the first-run banner but not update checks).
- The cache file says we already checked within the last 24h.

If any of these gate conditions is true, the check is a no-op and produces no output, no network call, no cache write.

## How the check works

1. **Resolve current version** — already implemented in `internal/cli/root.go:currentVersion()`.
2. **Read the cache.** If `last_checked_at` is within 24h, use the cached `latest_version`. Skip the network entirely.
3. **Otherwise hit GitHub.** `GET https://api.github.com/repos/sandgardenhq/find-the-gaps/releases/latest`, with:
   - 2-second total timeout (connect + read).
   - `User-Agent: find-the-gaps/<version>` header (GitHub requires a UA).
   - `Accept: application/vnd.github+json`.
   - No auth — public endpoint, well within unauthenticated rate limits for once-per-day-per-user traffic.
4. **Parse `tag_name`.** Compare to current version using semver (`golang.org/x/mod/semver` is already in the indirect deps via Cobra/Viper; otherwise add it explicitly).
5. **Write the cache** with the latest tag and the current timestamp, even when the user is up to date — so we don't re-hit the API tomorrow morning until the 24h window elapses.
6. **If newer**, render the notice (see below). If older or equal, render nothing.

Any error (timeout, non-2xx, malformed JSON, comparison failure) is silently swallowed. The check is best-effort. We do **not** want a flaky network or a GitHub outage to print scary text or change exit codes.

### Cache file format

`~/.find-the-gaps/update-check.json`:

```json
{
  "last_checked_at": "2026-05-06T14:32:11Z",
  "latest_version": "v1.4.2",
  "current_version_at_check": "v1.3.0"
}
```

`current_version_at_check` is recorded so that if the user upgrades mid-day, the next invocation can detect that the cached comparison is stale (current ≥ cached latest) and either skip the notice or refresh early.

## Notice rendering — platform-aware

`runtime.GOOS` drives which install paths to show:

| GOOS      | Lines shown (in order)                                             |
| --------- | ------------------------------------------------------------------ |
| `darwin`  | Homebrew first, `go install` second.                               |
| `linux`   | If `brew` is on `$PATH`, Homebrew first, `go install` second. Otherwise `go install` first, Homebrew second. |
| anything else | `go install` only.                                             |

Both Homebrew and `go install` lines are taken verbatim from `README.md` "Install" section, so a single edit point keeps them in sync. We will *not* duplicate the symlink/aliasing instructions — the upgrade notice points to the release notes URL for the rest.

The release notes URL is constructed deterministically from the latest tag: `https://github.com/sandgardenhq/find-the-gaps/releases/tag/<tag>`.

## Implementation outline

A new internal package keeps this isolated and easy to test.

**New package: `internal/updatecheck/`**

| File | Purpose |
| --- | --- |
| `updatecheck.go` | Public `Run(ctx, currentVersion, opts) (notice string, err error)`. Orchestrates gate → cache read → fetch → cache write → render. Returns the rendered notice (empty string when nothing to show) so the caller controls when/where to write it. |
| `gate.go`        | Pure function `shouldSkip(env Env, version string, stderrIsTTY bool, command string) (skip bool, reason string)`. Encodes every skip rule from "When the check runs" above. `Env` is a small interface so tests don't touch real `os.Getenv`. |
| `cache.go`       | Read/write `update-check.json` with file locking-free, last-write-wins semantics (single user per machine; no concurrency to worry about). |
| `github.go`      | The HTTP call. Constructor takes a base URL so tests can point at `httptest.Server`. |
| `notice.go`      | Pure function `renderNotice(currentVersion, latestVersion, goos string, brewOnPath bool) string`. |
| `*_test.go`      | One test file per source file. White-box tests for unit logic, black-box `updatecheck_test` for `Run` against an `httptest.Server`. |

**Integration points (no behavior change to existing code paths):**

1. `internal/cli/root.go` — register a new `PersistentPostRunE` that calls `updatecheck.Run` and writes the returned notice to `cmd.ErrOrStderr()`. Skip registration on the commands listed above (use Cobra's per-command annotations or check `cmd.Name()`).
2. `cmd/find-the-gaps/main.go` — no change. The post-run hook handles everything.

**Config dir resolution:** reuse whatever helper already exists for the first-run banner / `.find-the-gaps/` config dir. If there isn't a single helper, this work introduces one in `internal/configdir/` so the update-check cache and the first-run state file share it. (Decide during implementation, not in this plan.)

## Testing strategy

Per CLAUDE.md (TDD is mandatory, ≥90% statement coverage), each piece is built test-first:

- **`gate_test.go`** — table-driven across every skip combination (env vars, GOOS, TTY, version="dev", command name).
- **`cache_test.go`** — round-trip read/write in a temp dir; corrupt JSON tolerated; missing file tolerated; stale-after-upgrade detection.
- **`notice_test.go`** — golden-string assertions per platform matrix (darwin, linux+brew, linux-no-brew, windows).
- **`github_test.go`** — `httptest.Server` returning real GitHub-shaped JSON; timeouts; 5xx; 404; malformed body; rate-limit response.
- **`updatecheck_test.go`** — orchestration: cache hit short-circuits the network; cache miss writes a fresh entry; network failure produces empty notice and no panic.
- **`internal/cli/root_test.go`** — extend with a test that runs a fake command, asserts the notice ends up on stderr after stdout, and asserts gating on `--version`/`help`/`completion`.
- **`cmd/find-the-gaps/testdata/*.txtar`** — at least one testscript scenario that runs `ftg doctor` with a stub GitHub server (via env-var-injected base URL) and asserts the notice appears on stderr exactly once, after the doctor output.

Network calls in tests use `httptest.Server` — never the real GitHub API, never a mock library. This is consistent with the project's "no mocks" verification policy and avoids flaky tests.

## Verification (additions to `.plans/VERIFICATION_PLAN.md`)

A new scenario covers real end-to-end behavior:

- **Scenario 15: Update Check on Startup**
  1. With cache cleared and a current version older than the latest GitHub release, run `ftg doctor`. Assert the notice appears on stderr after the doctor output.
  2. Run `ftg doctor` a second time. Assert the cache is honored and the network is not hit (verifiable by setting an unreachable `FTG_UPDATE_CHECK_URL` for the second run; if behavior is unchanged, the cache short-circuited).
  3. Run `ftg --version`. Assert no notice is printed.
  4. Run with `FIND_THE_GAPS_NO_UPDATE_CHECK=1`. Assert no notice and no cache write.
  5. Run with `CI=true`. Assert no notice.
  6. Run on a build whose ldflags version equals the latest release tag. Assert no notice.

Wired in to existing testdata and verification policy. No mocks; the test uses a real `httptest.Server` substitute reachable over loopback, which qualifies as "a fully running copy of the system being integrated with."

## Non-goals

- **Auto-upgrade.** We do not run `brew upgrade` or `go install` on the user's behalf. Print, exit, let the user decide.
- **Pre-release / RC awareness.** We compare against `releases/latest`, which GitHub already filters to non-pre-release tags. RC users get no nag, which is correct.
- **Plugin / sub-tool version checks.** We do not check `mdfetch` or `hugo` here; `ftg doctor` already owns that.
- **Telemetry.** No phoning home beyond the single GitHub API call. No usage data, no install ID.

## Open questions

1. **Should `serve` skip the check?** `ftg serve` is long-lived; the post-run hook fires only on shutdown, so the notice would only print after the user kills the server. Probably fine, but worth confirming during implementation. Fallback: print on `serve` startup instead.
2. **Cache location when `XDG_CONFIG_HOME` is set on Linux.** Defer to whatever the existing first-run banner code does. If that code uses `~/.find-the-gaps/` literally, we follow suit; if it respects XDG, we follow suit there too.
