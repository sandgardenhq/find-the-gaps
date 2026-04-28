# `ftg serve` — Implementation Plan

## Goal

Add a `serve` subcommand that boots a local HTTP server against the static Hugo site produced by `ftg analyze`. Zero external runtime dependencies — the site is already pre-rendered HTML on disk.

## Path Resolution (Source of Truth)

`analyze` writes to `<cacheDir>/<base(repo)>/site/`, where:

- `cacheDir` defaults to `.find-the-gaps` (see `internal/cli/analyze.go:433`)
- `projectName = filepath.Base(filepath.Clean(repoPath))` (see `internal/cli/analyze.go:109`)
- final dir: `<cacheDir>/<projectName>/site/`

`serve` MUST resolve the same way so the same flags point at the same place.

## Public Surface

```
ftg serve [--repo PATH] [--cache-dir PATH] [--addr HOST:PORT] [--open]
```

| Flag          | Default            | Notes                                                                  |
|---------------|--------------------|------------------------------------------------------------------------|
| `--repo`      | `.`                | Mirror of `analyze`. Used only to derive `projectName`.                |
| `--cache-dir` | `.find-the-gaps`   | Mirror of `analyze`.                                                   |
| `--addr`      | `127.0.0.1:8080`   | Bind address. Pass `:0` to pick a free port; the chosen URL is always printed. |
| `--open`      | `false`            | If true, open the URL in the default browser after the server is up.   |

Behavior:

1. Compute `siteDir = <cacheDir>/<base(repo)>/site`.
2. If `siteDir` does not exist or is empty, exit non-zero with a hint to run `ftg analyze` first.
3. Bind a `net.Listener` on `--addr`. Resolve the actual `host:port` (so `:0` works).
4. Print one line to stdout: `serving <siteDir> at http://<host>:<port>/`.
5. Serve with `http.FileServer(http.Dir(siteDir))` until SIGINT/SIGTERM.
6. On signal: graceful `http.Server.Shutdown` with a 5-second deadline, then exit 0.

Out of scope (YAGNI):

- Live reload / Hugo dev-server mode — defer until someone asks.
- HTTPS, auth, multi-host — local-only tool.
- Watching `siteDir` for changes — `analyze` rewrites the whole tree on each run; restart `serve` if you re-run.

## Files

| File                                    | Action  | Purpose                                                              |
|-----------------------------------------|---------|----------------------------------------------------------------------|
| `internal/cli/serve.go`                 | CREATE  | Cobra command, flag parsing, listener+server orchestration.          |
| `internal/cli/serve_test.go`            | CREATE  | Package-level tests for path resolution and server behavior.         |
| `internal/cli/root.go`                  | EDIT    | Register `newServeCmd()` in `NewRootCmd().AddCommand(...)`.          |
| `internal/cli/root_test.go`             | EDIT    | Add `"serve": false` to the expected-subcommands map at line 96.     |
| `README.md`                             | EDIT    | One-paragraph usage section under the existing `analyze` docs.       |
| `PROGRESS.md`                           | EDIT    | Append a Task entry per CLAUDE.md §8.                                |

## Implementation Tasks (TDD, in order)

Every task = RED test → run, watch fail → minimal GREEN → run, watch pass → commit.

### Task 1 — Register `serve` subcommand in root

**RED**: In `internal/cli/root_test.go`, add `"serve": false` to the `want` map at the existing `TestNewRootCmd_Structure` test (line ~96). Run `go test ./internal/cli/ -run TestNewRootCmd_Structure` — expect failure: `missing subcommand "serve"`.

**GREEN**: Create `internal/cli/serve.go` with a minimal stub:

```go
package cli

import "github.com/spf13/cobra"

func newServeCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "serve",
        Short: "Serve the find-the-gaps report site over HTTP.",
        RunE: func(cc *cobra.Command, _ []string) error {
            return nil
        },
    }
}
```

In `internal/cli/root.go:74`, change:

```go
cmd.AddCommand(newDoctorCmd(), newAnalyzeCmd(), newInstallDepsCmd())
```

to include `newServeCmd()`.

Run `go test ./internal/cli/...` — green.

**Commit**: `feat(cli): register serve subcommand stub`

---

### Task 2 — Resolve site dir from `--repo` and `--cache-dir`

**RED**: In `internal/cli/serve_test.go`, add a test `TestServe_resolvesSiteDir_fromRepoAndCacheDir` that:

1. Creates `cacheBase := t.TempDir()`.
2. Creates `repoDir := filepath.Join(t.TempDir(), "myproject")` and `os.MkdirAll`s it.
3. Creates `siteDir := filepath.Join(cacheBase, "myproject", "site")` and writes `index.html` with body `"hello from serve"`.
4. Runs `run(&stdout, &stderr, []string{"serve", "--repo", repoDir, "--cache-dir", cacheBase, "--addr", "127.0.0.1:0"})` in a goroutine.
5. Polls `stdout` for the `serving ... at http://...` line, parses the URL.
6. Issues `http.Get(url)`, asserts body contains `hello from serve`.
7. Sends SIGINT to terminate (or cancels via a context the cmd respects — see Task 4).

Run — fails (RunE is empty).

**GREEN**: Flesh out `newServeCmd`:

- Add flags: `repoPath` (default `.`), `cacheDir` (default `.find-the-gaps`), `addr` (default `127.0.0.1:0`).
- In `RunE`:
  1. `projectName := filepath.Base(filepath.Clean(repoPath))`
  2. `siteDir := filepath.Join(cacheDir, projectName, "site")`
  3. `info, err := os.Stat(siteDir); if err != nil || !info.IsDir() { return error }` (full error handling lands in Task 3).
  4. `ln, err := net.Listen("tcp", addr)` — propagate error.
  5. Print `serving <siteDir> at http://<ln.Addr()>/` to `cc.OutOrStdout()`.
  6. `srv := &http.Server{Handler: http.FileServer(http.Dir(siteDir)), ReadHeaderTimeout: 5 * time.Second}`
  7. `go srv.Serve(ln)`; block on `cc.Context().Done()`; then `srv.Shutdown(ctx5s)`.

Run — green.

**Commit**: `feat(cli): serve static site from <cacheDir>/<repo>/site`

---

### Task 3 — Helpful error when site dir is missing

**RED**: Add `TestServe_missingSiteDir_returnsErrorWithHint`:

1. Use a fresh `cacheBase := t.TempDir()` with no `<repo>/site` inside.
2. Run `serve` against it.
3. Assert exit code is non-zero.
4. Assert stderr contains both the missing path AND the substring `ftg analyze` (so the user knows the fix).

Run — fails (current code returns a generic stat error).

**GREEN**: In `RunE`, when `os.Stat` fails or the path isn't a directory, return:

```go
return fmt.Errorf("no rendered site at %s — run `ftg analyze` first to generate it", siteDir)
```

Run — green.

**Commit**: `feat(cli): explain how to populate the site dir when serve has nothing to serve`

---

### Task 4 — Graceful shutdown on context cancel

**RED**: Add `TestServe_shutdownOnContextCancel`:

1. Set up the same fixture as Task 2.
2. Build the root cmd manually so the test owns the context: `root := NewRootCmd(); ctx, cancel := context.WithCancel(context.Background()); root.SetContext(ctx); root.SetArgs(...)`.
3. Run `root.Execute()` in a goroutine, capturing the returned error.
4. Wait for the `serving ...` line on stdout.
5. Issue an `http.Get` to confirm liveness.
6. `cancel()`.
7. Assert the goroutine returns within 6 seconds with a nil error.
8. Assert a follow-up `http.Get` to the same URL fails (server is down).

Run — fails (server is started in a goroutine but RunE doesn't currently block on `cc.Context()`).

**GREEN**: Block on `<-cc.Context().Done()`, then call `srv.Shutdown(shutdownCtx)` with a 5-second deadline. Return any non-`http.ErrServerClosed` error from `Serve` via a channel. Confirm both code paths return nil on a clean shutdown.

Run — green.

**Commit**: `feat(cli): graceful shutdown on SIGINT for serve`

---

### Task 5 — `--open` opens the browser after bind

**RED**: Add `TestServe_open_invokesOpener`. Don't shell out to a real browser — inject the opener.

Approach: define a package-level variable `var openInBrowser = openURLInBrowser` (where `openURLInBrowser` is the real implementation). The test swaps it for a fake that records its arg into a channel. Test asserts the fake receives the same URL printed to stdout.

Run — fails (no `--open` flag, no opener call).

**GREEN**: Add the `open` flag. Add `openURLInBrowser(url string) error` that delegates to `open` (darwin) / `xdg-open` (linux) / `rundll32 url.dll,FileProtocolHandler` (windows) using `exec.Command(...).Start()`. Wire it: when `--open` is set and the listener is up, call `openInBrowser(url)`; log a warning on error but do not fail the command.

Run — green.

**Commit**: `feat(cli): --open flag for serve to launch the report in the default browser`

---

### Task 6 — README + PROGRESS

**RED**: This task is documentation; no test. (CLAUDE.md §8 still requires PROGRESS.md updates.)

**GREEN**:

1. In `README.md`, after the `analyze` usage block, add:

   ```
   ### Browse the report

       ftg serve --repo ./myrepo

   Opens a local web server against the rendered Hextra site at
   `.find-the-gaps/<repo>/site/`. Pass `--addr` to bind a specific port
   or `--open` to launch your browser automatically.
   ```

2. Append a new task block to `PROGRESS.md` per the template in CLAUDE.md §8.

**Commit**: `docs: document ftg serve`

---

## Verification

After all tasks:

```bash
go test ./...                                              # all green
go test -coverprofile=coverage.out ./internal/cli/... \
  && go tool cover -func=coverage.out | grep serve         # ≥90% on serve.go
golangci-lint run
go build ./...
```

Manual smoke test:

```bash
go build -o /tmp/ftg ./cmd/find-the-gaps
cd <a-repo-with-a-prior-analyze-run>
/tmp/ftg serve --addr 127.0.0.1:8765
# in another terminal:
curl -sS http://127.0.0.1:8765/ | head
# Ctrl-C — server exits cleanly with code 0
```

## Open Questions for the Author

1. ~~**Default address**~~ — Resolved 2026-04-28: `127.0.0.1:8080`.
2. **`--open` default** — currently `false`. Most local-doc-server CLIs (e.g., `pkgsite`, `godoc -http`) leave it off. Confirm.
3. **Multi-project layout** — `.find-the-gaps/` may contain multiple `<projectName>/site/` directories. Should `serve` accept a positional `--project` flag instead of (or in addition to) `--repo`? Not in this plan — `--repo` is sufficient and matches `analyze`. Flag if you want a different ergonomic.
