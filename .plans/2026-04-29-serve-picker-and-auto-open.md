# Serve Picker + Analyze Auto-Open — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Two related UX upgrades for the local report viewer:
1. When `ftg serve` finds multiple analyzed projects under `.find-the-gaps/`, present a picker (via `huh`) so the user chooses one.
2. When `ftg analyze` finishes, automatically start the server, print a message + URL, and (by default) open the browser — unless the user opts out.

**Architecture:** Extract the server-boot logic out of `newServeCmd`'s `RunE` into a reusable internal function (`runHTTPServer`) so `analyze` can call it directly without going through cobra. Add a tiny `cachescan` helper that lists project directories with a rendered `site/` under a given cache dir. Add `huh` for interactive selection, gated by a TTY check so non-interactive runs (CI) fall back to a clear error listing the available projects. The analyze auto-open piggybacks on the same `runHTTPServer` plus the existing `openInBrowser` indirection.

**Tech Stack:** Go 1.26+, Cobra, `github.com/charmbracelet/huh` (new dep), `golang.org/x/term` (new indirect via huh; we use it directly for TTY detection), stdlib `net/http`, `net`.

---

## Background — what's there now

- `internal/cli/serve.go` — current `serve`. Computes `siteDir = <cacheDir>/<filepath.Base(absRepo)>/site`, errors if missing, otherwise listens + serves + waits for context cancel.
- `internal/cli/analyze.go` — current `analyze`. After it builds the Hugo site (around `internal/cli/analyze.go:391-415`), it prints a `reports:` block and returns.
- `internal/cli/serve_test.go` — exercises path resolution, missing-site error, shutdown-on-cancel, `--open` opener, addr-in-use.
- `go.mod` — already pulls in `charmbracelet/log`, `lipgloss`, `bubbles`, etc., as transitive deps. `huh` is **not** yet a direct dep; this plan adds it.
- The package-level `var openInBrowser = openURLInBrowser` indirection is already in `serve.go` so tests can swap it. Reuse this for analyze auto-open.

---

## New behavior — summary

### `ftg serve`
- If `--repo` is **explicitly set** by the user → behave exactly as today.
- If `--repo` is **not set** (default `.`) → scan `<cacheDir>/` for subdirectories that contain a `site/` directory.
  - 0 matches → error: "no analyzed projects found in <cacheDir>; run `ftg analyze` first".
  - 1 match → use it (no prompt). Print which one.
  - 2+ matches → if stdout is a TTY: open a `huh.NewSelect` picker; serve the chosen project. If not a TTY: error listing the names so the user can re-run with `--repo`.
- A new flag `--project NAME` (mutually exclusive with `--repo`) lets users pick a project by its directory name in `<cacheDir>/` without needing the original repo path. Skips the picker.

### `ftg analyze`
- After the run succeeds and a site was built (i.e. `--no-site` not set):
  - Print: `report ready — opening http://127.0.0.1:8080/`.
  - Start the same HTTP server `serve` would start, on `127.0.0.1:8080` by default (configurable via `--serve-addr`, mirrors `--addr`).
  - Open the URL in the default browser unless `--no-open` is passed.
  - Block until SIGINT/SIGTERM (graceful shutdown, just like `serve`).
- New flags on `analyze`:
  - `--no-serve` (default `false`) → skip auto-server entirely; preserves current behavior.
  - `--no-open` (default `false`) → start the server but don't launch the browser.
  - `--serve-addr` (default `127.0.0.1:8080`) → mirrors `serve`'s `--addr`.
- `--no-site` already implies nothing to serve → if `--no-site` is set, auto-serve is a no-op (don't error).

### Out of scope (YAGNI)
- Watching files / live reload.
- Choosing a default project via config file.
- Killing/restarting an already-running server on the same port.

---

## Files to touch

| File | Action | Purpose |
|---|---|---|
| `go.mod`, `go.sum` | EDIT | `go get github.com/charmbracelet/huh@latest` |
| `internal/cli/cachescan.go` | CREATE | `ListAnalyzedProjects(cacheDir) ([]Project, error)`; pure, no I/O beyond stat. |
| `internal/cli/cachescan_test.go` | CREATE | Tests for empty / one / many / non-existent / mixed (some without `site/`). |
| `internal/cli/server_runner.go` | CREATE | Extract `runHTTPServer(ctx, w, siteDir, addr, openBrowser bool) error` from current `serve.go`. |
| `internal/cli/server_runner_test.go` | CREATE | Smoke tests covering listen-error, shutdown-on-cancel (mirrors current serve_test). |
| `internal/cli/serve.go` | EDIT | Add `--project` flag; add picker logic gated on `--repo` being default; delegate to `runHTTPServer`. |
| `internal/cli/serve_test.go` | EDIT | Add tests: picker invoked when N>1, auto-pick when N=1, error when N=0, `--project` short-circuit, non-TTY error path. |
| `internal/cli/picker.go` | CREATE | Thin wrapper `pickProject(in io.Reader, out io.Writer, projects []Project) (Project, error)` so tests can inject a fake without driving huh's tea program directly. Real impl uses `huh.NewSelect`. |
| `internal/cli/picker_test.go` | CREATE | Verify the wrapper passes through huh selection (use a stub; do not drive a real terminal). |
| `internal/cli/analyze.go` | EDIT | After successful site build, run `runHTTPServer` unless `--no-serve` or `--no-site`. New flags. |
| `internal/cli/analyze_test.go` | EDIT | Add: auto-serve happens when site built, skipped on `--no-serve`, skipped on `--no-site`, browser opener invoked when not `--no-open`. |
| `README.md` | EDIT | Update `serve` and `analyze` sections to describe the picker and the auto-open behavior. |
| `PROGRESS.md` | EDIT | One block per task (per CLAUDE.md §8). |

---

## Design decisions worth noting

1. **TTY detection:** use `golang.org/x/term.IsTerminal(int(os.Stdout.Fd()))`. `huh` requires a TTY; without one it deadlocks. Treat non-TTY as "can't pick — list options and bail".
2. **Picker abstraction:** `pickProject` is a small wrapper, NOT a fully mockable interface. Tests for `picker.go` use a `huhSelectFn` package-level var (same pattern as `openInBrowser`) so we never spin up a real `huh.Form` in tests.
3. **`--repo` vs `--project`:** `--project` is the picker's natural output (a directory name under `<cacheDir>/`). `--repo` continues to mean "filesystem path to the source repo" and we derive `projectName` from it. If both are set, error out; pick one.
4. **Why not auto-pick in CI?** A non-interactive environment with multiple projects has no good default. Surfacing the names lets the user (or a script) re-run with `--project`.
5. **Why default `127.0.0.1:8080`?** Same as `serve`. Consistency wins. If the port is in use, the analyze run fails with the same listen error `serve` would produce — the user re-runs with `--serve-addr` or `--no-serve`.
6. **Browser-open default differs between commands.** `serve` defaults `--open=false` (preserved). `analyze` defaults to opening the browser because the user just produced fresh artifacts and almost certainly wants to see them. Both can be overridden.
7. **Don't shadow flag defaults.** Use `cmd.Flags().Changed("repo")` to detect explicit `--repo` so that the picker only fires when the user truly didn't specify a repo.

---

## TDD discipline reminder

Per `CLAUDE.md`: every production line follows RED → verify red → minimal GREEN → verify green → commit. No "I'll add tests later". Each task below is one feature behind one test. **Run** the test before and after each step. Commit at the end of each task.

---

## Task 0 — Add `huh` dependency

**Files:**
- Modify: `go.mod`, `go.sum`

**Step 1: Add the dep**

Run: `go get github.com/charmbracelet/huh@latest`

**Step 2: Verify build still passes**

Run: `go build ./...`
Expected: success, no compile errors.

**Step 3: Verify nothing breaks**

Run: `go test ./...`
Expected: PASS (no behavior change yet).

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add huh for interactive selects"
```

---

## Task 1 — `ListAnalyzedProjects` helper (RED)

**Files:**
- Create: `internal/cli/cachescan_test.go`

**Step 1: Write the failing test**

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAnalyzedProjects_emptyDir_returnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := ListAnalyzedProjects(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d projects, want 0", len(got))
	}
}

func TestListAnalyzedProjects_missingDir_returnsEmpty(t *testing.T) {
	got, err := ListAnalyzedProjects(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should be a soft empty result, got error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d projects, want 0", len(got))
	}
}

func TestListAnalyzedProjects_onlyDirsWithSiteCount(t *testing.T) {
	cache := t.TempDir()
	// project A — has site/
	if err := os.MkdirAll(filepath.Join(cache, "alpha", "site"), 0o755); err != nil {
		t.Fatal(err)
	}
	// project B — has scan/ but no site/, must be ignored
	if err := os.MkdirAll(filepath.Join(cache, "beta", "scan"), 0o755); err != nil {
		t.Fatal(err)
	}
	// project C — has site/
	if err := os.MkdirAll(filepath.Join(cache, "gamma", "site"), 0o755); err != nil {
		t.Fatal(err)
	}
	// loose file
	if err := os.WriteFile(filepath.Join(cache, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListAnalyzedProjects(cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d projects, want 2 (alpha, gamma)", len(got))
	}
	// Result must be deterministic (sorted by name).
	if got[0].Name != "alpha" || got[1].Name != "gamma" {
		t.Errorf("got %+v, want [alpha gamma] in order", got)
	}
	// SiteDir is correctly resolved.
	if got[0].SiteDir != filepath.Join(cache, "alpha", "site") {
		t.Errorf("alpha SiteDir = %q", got[0].SiteDir)
	}
}
```

**Step 2: Run — must fail**

Run: `go test ./internal/cli/ -run TestListAnalyzedProjects -v`
Expected: FAIL with `undefined: ListAnalyzedProjects`.

---

## Task 2 — `ListAnalyzedProjects` helper (GREEN)

**Files:**
- Create: `internal/cli/cachescan.go`

**Step 1: Implement minimal code**

```go
package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Project is one analyzed repo whose Hugo site lives under cacheDir/<Name>/site.
type Project struct {
	Name    string
	SiteDir string
}

// ListAnalyzedProjects returns every immediate subdirectory of cacheDir that
// contains a `site` subdirectory. A non-existent cacheDir is treated as "no
// analyzed projects" (not an error) so the caller can produce one helpful
// message.
func ListAnalyzedProjects(cacheDir string) ([]Project, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		siteDir := filepath.Join(cacheDir, e.Name(), "site")
		info, err := os.Stat(siteDir)
		if err != nil || !info.IsDir() {
			continue
		}
		out = append(out, Project{Name: e.Name(), SiteDir: siteDir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
```

**Step 2: Run — must pass**

Run: `go test ./internal/cli/ -run TestListAnalyzedProjects -v`
Expected: PASS (3/3).

**Step 3: Commit**

```bash
git add internal/cli/cachescan.go internal/cli/cachescan_test.go
git commit -m "feat(cli): list analyzed projects in cache dir"
```

---

## Task 3 — Extract `runHTTPServer` (RED)

**Files:**
- Create: `internal/cli/server_runner_test.go`

**Step 1: Write a failing test**

This test runs the extracted helper directly (no cobra), verifies it serves a temp dir and shuts down on context cancel.

```go
package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunHTTPServer_servesAndShutsDown(t *testing.T) {
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout safeBuffer // already exists in serve_test.go
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = runHTTPServer(ctx, &stdout, siteDir, "127.0.0.1:0", false)
	}()

	// Wait for the URL to be printed.
	deadline := time.Now().Add(3 * time.Second)
	var url string
	for time.Now().Before(deadline) {
		if m := servingURLRe.FindString(stdout.String()); m != "" {
			url = strings.TrimRight(m, "/")
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if url == "" {
		t.Fatalf("never saw serving URL; out=%q", stdout.String())
	}

	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Contains(body, []byte("hi")) {
		t.Errorf("body = %q", body)
	}

	cancel()
	wg.Wait()
	if runErr != nil {
		t.Errorf("runHTTPServer returned %v on graceful shutdown", runErr)
	}
}
```

**Step 2: Run — must fail**

Run: `go test ./internal/cli/ -run TestRunHTTPServer -v`
Expected: FAIL with `undefined: runHTTPServer`.

---

## Task 4 — Extract `runHTTPServer` (GREEN)

**Files:**
- Create: `internal/cli/server_runner.go`
- Modify: `internal/cli/serve.go` — delegate to the new helper

**Step 1: Create `server_runner.go`**

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
)

// runHTTPServer binds a listener on addr, serves siteDir, prints the resolved
// URL to out, optionally opens the URL in the default browser, and blocks
// until ctx is canceled. It is shared by `serve` and the post-`analyze`
// auto-open path.
func runHTTPServer(ctx context.Context, out io.Writer, siteDir, addr string, openBrowser bool) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           http.FileServer(http.Dir(siteDir)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	url := fmt.Sprintf("http://%s/", ln.Addr().String())
	_, _ = fmt.Fprintf(out, "serving %s at %s\n", siteDir, url)

	if openBrowser {
		if err := openInBrowser(url); err != nil {
			log.Warnf("could not open browser: %v", err)
		}
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	}
}
```

**Step 2: Update `serve.go`'s `RunE` to delegate**

Replace the body of `RunE` (everything after `siteDir` is computed and the missing-site check passes) with:

```go
return runHTTPServer(cc.Context(), cc.OutOrStdout(), siteDir, addr, openFlag)
```

Remove the now-unused imports (`context`, `errors`, `net`, `net/http`, `time`) from `serve.go` if they're no longer referenced after the delegation. Keep `log` only if `serve.go` still references it.

**Step 3: Run all serve tests**

Run: `go test ./internal/cli/ -run 'TestServe|TestRunHTTPServer' -v`
Expected: PASS — old serve tests remain green; new helper test passes.

**Step 4: Commit**

```bash
git add internal/cli/server_runner.go internal/cli/server_runner_test.go internal/cli/serve.go
git commit -m "refactor(cli): extract runHTTPServer for reuse by analyze"
```

---

## Task 5 — Picker wrapper (RED)

**Files:**
- Create: `internal/cli/picker_test.go`

**Step 1: Write the failing test**

The wrapper exists so tests can swap the actual `huh.Form.Run` call. We assert the wrapper:
- Returns the project at the index produced by the injected fake.
- Errors out when the user cancels (fake returns the sentinel).

```go
package cli

import (
	"errors"
	"testing"
)

func TestPickProject_returnsSelected(t *testing.T) {
	projects := []Project{
		{Name: "alpha", SiteDir: "/x/alpha/site"},
		{Name: "beta", SiteDir: "/x/beta/site"},
	}
	original := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		return opts[1], nil // pick "beta"
	}
	t.Cleanup(func() { huhSelectFn = original })

	got, err := pickProject(projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "beta" {
		t.Errorf("got %q, want beta", got.Name)
	}
}

func TestPickProject_propagatesCancel(t *testing.T) {
	projects := []Project{{Name: "alpha"}, {Name: "beta"}}
	original := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		return Project{}, errors.New("user cancelled")
	}
	t.Cleanup(func() { huhSelectFn = original })

	if _, err := pickProject(projects); err == nil {
		t.Error("expected error when underlying select cancels, got nil")
	}
}
```

**Step 2: Run — must fail**

Run: `go test ./internal/cli/ -run TestPickProject -v`
Expected: FAIL with `undefined: pickProject` and/or `huhSelectFn`.

---

## Task 6 — Picker wrapper (GREEN)

**Files:**
- Create: `internal/cli/picker.go`

**Step 1: Implement**

```go
package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// huhSelectFn is the real interactive picker, swappable in tests.
var huhSelectFn = func(opts []Project) (Project, error) {
	options := make([]huh.Option[string], len(opts))
	for i, p := range opts {
		options[i] = huh.NewOption(p.Name, p.Name)
	}
	var chosen string
	form := huh.NewForm(
		huh.NewGroup(
			// PROMPT: not an LLM prompt — UI-only, no PROMPT comment needed.
			huh.NewSelect[string]().
				Title("Multiple analyzed projects found. Pick one to serve:").
				Options(options...).
				Value(&chosen),
		),
	)
	if err := form.Run(); err != nil {
		return Project{}, err
	}
	for _, p := range opts {
		if p.Name == chosen {
			return p, nil
		}
	}
	return Project{}, fmt.Errorf("internal: huh returned unknown project %q", chosen)
}

// pickProject prompts the user to choose one project from opts. opts must be
// non-empty.
func pickProject(opts []Project) (Project, error) {
	if len(opts) == 0 {
		return Project{}, fmt.Errorf("pickProject: no options to choose from")
	}
	return huhSelectFn(opts)
}
```

**Step 2: Run — must pass**

Run: `go test ./internal/cli/ -run TestPickProject -v`
Expected: PASS (2/2).

**Step 3: Commit**

```bash
git add internal/cli/picker.go internal/cli/picker_test.go
git commit -m "feat(cli): add huh-backed project picker"
```

---

## Task 7 — Wire picker into `serve` when N>1 (RED)

**Files:**
- Modify: `internal/cli/serve_test.go`

**Step 1: Write the failing test**

```go
func TestServe_multipleProjects_invokesPickerAndServesChoice(t *testing.T) {
	cacheBase := t.TempDir()
	// Two analyzed projects.
	for _, name := range []string{"alpha", "beta"} {
		siteDir := filepath.Join(cacheBase, name, "site")
		if err := os.MkdirAll(siteDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("hello "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Inject picker — choose beta.
	originalPicker := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		for _, p := range opts {
			if p.Name == "beta" {
				return p, nil
			}
		}
		return Project{}, errors.New("beta not in options")
	}
	t.Cleanup(func() { huhSelectFn = originalPicker })

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
		// no --repo on purpose — triggers picker
	})
	t.Cleanup(cancel)

	url := waitForServingURL(t, stdout)
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "hello beta") {
		t.Errorf("served wrong project: body=%q", body)
	}

	cancel()
	<-done
}

func TestServe_singleProject_skipsPicker(t *testing.T) {
	cacheBase := t.TempDir()
	siteDir := filepath.Join(cacheBase, "solo", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("solo"), 0o644); err != nil {
		t.Fatal(err)
	}

	called := false
	originalPicker := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		called = true
		return opts[0], nil
	}
	t.Cleanup(func() { huhSelectFn = originalPicker })

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	t.Cleanup(cancel)

	_ = waitForServingURL(t, stdout)
	if called {
		t.Error("picker was called even though only one project exists")
	}

	cancel()
	<-done
}

func TestServe_noProjects_errorsWithHint(t *testing.T) {
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	if code == 0 {
		t.Fatalf("exit 0, want non-zero; stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "ftg analyze") {
		t.Errorf("stderr should hint at `ftg analyze`, got %q", stderr.String())
	}
}

func TestServe_projectFlag_shortCircuitsPicker(t *testing.T) {
	cacheBase := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(cacheBase, name, "site"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cacheBase, name, "site", "index.html"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	called := false
	originalPicker := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		called = true
		return opts[0], nil
	}
	t.Cleanup(func() { huhSelectFn = originalPicker })

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--project", "beta",
		"--addr", "127.0.0.1:0",
	})
	t.Cleanup(cancel)

	url := waitForServingURL(t, stdout)
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	_ = body
	if called {
		t.Error("picker called despite --project being set")
	}

	cancel()
	<-done
}
```

**Step 2: Run — must fail**

Run: `go test ./internal/cli/ -run 'TestServe_multipleProjects|TestServe_singleProject|TestServe_noProjects|TestServe_projectFlag' -v`
Expected: FAIL — `serve` doesn't yet honor `--project`, doesn't pick when N>1, etc.

---

## Task 8 — Wire picker into `serve` when N>1 (GREEN)

**Files:**
- Modify: `internal/cli/serve.go`

**Step 1: Update flags + RunE**

Add a `projectFlag` string. Make `--repo` "explicit-aware". The new `RunE` flow:

```go
func newServeCmd() *cobra.Command {
	var (
		repoPath    string
		cacheDir    string
		addr        string
		openFlag    bool
		projectFlag string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the find-the-gaps report site over HTTP.",
		RunE: func(cc *cobra.Command, _ []string) error {
			if projectFlag != "" && cc.Flags().Changed("repo") {
				return fmt.Errorf("--project and --repo are mutually exclusive")
			}

			siteDir, err := resolveServeSiteDir(cc, cacheDir, repoPath, projectFlag)
			if err != nil {
				return err
			}

			return runHTTPServer(cc.Context(), cc.OutOrStdout(), siteDir, addr, openFlag)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository whose report should be served")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory containing analyze output")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "bind address (host:port; use 127.0.0.1:0 to pick a free port)")
	cmd.Flags().BoolVar(&openFlag, "open", false, "open the served URL in the default browser after the server is up")
	cmd.Flags().StringVar(&projectFlag, "project", "", "name of an analyzed project under <cache-dir>/; bypasses the picker")

	return cmd
}

// resolveServeSiteDir picks the site to serve based on flags + cache contents.
// Order of precedence:
//  1. --project NAME            → use <cacheDir>/NAME/site
//  2. --repo PATH (explicit)    → use <cacheDir>/base(PATH)/site
//  3. --repo not set            → scan, then auto-pick / prompt / error
func resolveServeSiteDir(cc *cobra.Command, cacheDir, repoPath, projectFlag string) (string, error) {
	if projectFlag != "" {
		siteDir := filepath.Join(cacheDir, projectFlag, "site")
		if info, err := os.Stat(siteDir); err != nil || !info.IsDir() {
			return "", fmt.Errorf("no rendered site at %s — check --project or run `ftg analyze` first", siteDir)
		}
		return siteDir, nil
	}

	if cc.Flags().Changed("repo") {
		absRepo, err := filepath.Abs(repoPath)
		if err != nil {
			return "", fmt.Errorf("resolve repo path: %w", err)
		}
		siteDir := filepath.Join(cacheDir, filepath.Base(absRepo), "site")
		if info, err := os.Stat(siteDir); err != nil || !info.IsDir() {
			return "", fmt.Errorf("no rendered site at %s — run `ftg analyze` first to generate it", siteDir)
		}
		return siteDir, nil
	}

	projects, err := ListAnalyzedProjects(cacheDir)
	if err != nil {
		return "", fmt.Errorf("scan cache dir: %w", err)
	}
	switch len(projects) {
	case 0:
		return "", fmt.Errorf("no analyzed projects found in %s — run `ftg analyze` first", cacheDir)
	case 1:
		_, _ = fmt.Fprintf(cc.OutOrStdout(), "found one project: %s\n", projects[0].Name)
		return projects[0].SiteDir, nil
	default:
		if !isInteractive() {
			names := make([]string, len(projects))
			for i, p := range projects {
				names[i] = p.Name
			}
			return "", fmt.Errorf("multiple analyzed projects found in %s; re-run with --project NAME (one of: %s)",
				cacheDir, strings.Join(names, ", "))
		}
		chosen, err := pickProject(projects)
		if err != nil {
			return "", err
		}
		return chosen.SiteDir, nil
	}
}

// isInteractive reports whether stdin/stdout look like a TTY. Tests force this
// to true via the testInteractiveOverride hook.
var testInteractiveOverride *bool

func isInteractive() bool {
	if testInteractiveOverride != nil {
		return *testInteractiveOverride
	}
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}
```

Add the import for `golang.org/x/term` and `strings`.

**Step 2: Make tests force-interactive**

In the multi-project test, set `testInteractiveOverride` to a `true` pointer in a `t.Cleanup`-restored override. (Add a tiny helper if you want — or just `tr := true; testInteractiveOverride = &tr`.)

Add this setup to `TestServe_multipleProjects_invokesPickerAndServesChoice` and `TestServe_projectFlag_shortCircuitsPicker` so the picker code path runs under `go test` (which is non-TTY).

**Step 3: Add a non-interactive test**

```go
func TestServe_multipleProjects_nonInteractive_errorsWithList(t *testing.T) {
	cacheBase := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(cacheBase, name, "site"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cacheBase, name, "site", "index.html"), []byte("ok"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Force non-interactive (default for `go test`, but be explicit).
	noTTY := false
	prev := testInteractiveOverride
	testInteractiveOverride = &noTTY
	t.Cleanup(func() { testInteractiveOverride = prev })

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr.String())
	}
	for _, want := range []string{"--project", "alpha", "beta"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %q", want, stderr.String())
		}
	}
}
```

**Step 4: Run — must pass**

Run: `go test ./internal/cli/ -run TestServe -v`
Expected: PASS — all old + new tests.

**Step 5: Commit**

```bash
git add internal/cli/serve.go internal/cli/serve_test.go
git commit -m "feat(cli): pick a project when serve finds multiple in cache dir"
```

---

## Task 9 — Auto-serve after `analyze` (RED)

**Files:**
- Modify: `internal/cli/analyze_test.go`

**Step 1: Write the failing test**

There is already a real-mapping test setup in this file. Use the smallest possible analyze run by passing no `--docs-url` so the path that builds the site is short-circuited — except we still need a built site for auto-serve to make sense. The cleanest approach is to test the post-build hook in isolation: factor the auto-serve invocation through a small `maybeServeAfterAnalyze(ctx, w, cfg)` function and unit-test that.

```go
func TestMaybeServeAfterAnalyze_skipsWhenNoServeFlag(t *testing.T) {
	cfg := autoServeConfig{NoServe: true, NoSite: false, SiteDir: t.TempDir(), Addr: "127.0.0.1:0", Open: false}
	// Should return immediately, no error, no goroutine.
	if err := maybeServeAfterAnalyze(context.Background(), io.Discard, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMaybeServeAfterAnalyze_skipsWhenNoSite(t *testing.T) {
	cfg := autoServeConfig{NoServe: false, NoSite: true, SiteDir: t.TempDir(), Addr: "127.0.0.1:0", Open: false}
	if err := maybeServeAfterAnalyze(context.Background(), io.Discard, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMaybeServeAfterAnalyze_servesAndOpens(t *testing.T) {
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	openedURL := make(chan string, 1)
	original := openInBrowser
	openInBrowser = func(url string) error {
		openedURL <- url
		return nil
	}
	t.Cleanup(func() { openInBrowser = original })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var out safeBuffer
	doneCh := make(chan error, 1)
	cfg := autoServeConfig{NoServe: false, NoSite: false, SiteDir: siteDir, Addr: "127.0.0.1:0", Open: true}
	go func() {
		doneCh <- maybeServeAfterAnalyze(ctx, &out, cfg)
	}()

	select {
	case got := <-openedURL:
		if !strings.HasPrefix(got, "http://127.0.0.1:") {
			t.Errorf("opener got %q, want http://127.0.0.1:*", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("opener never invoked")
	}

	if !strings.Contains(out.String(), "report ready") {
		t.Errorf("expected `report ready` in output, got %q", out.String())
	}

	cancel()
	if err := <-doneCh; err != nil {
		t.Errorf("clean shutdown returned %v", err)
	}
}

func TestMaybeServeAfterAnalyze_respectsNoOpen(t *testing.T) {
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := make(chan struct{}, 1)
	original := openInBrowser
	openInBrowser = func(url string) error {
		called <- struct{}{}
		return nil
	}
	t.Cleanup(func() { openInBrowser = original })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var out safeBuffer
	doneCh := make(chan error, 1)
	cfg := autoServeConfig{NoServe: false, NoSite: false, SiteDir: siteDir, Addr: "127.0.0.1:0", Open: false}
	go func() {
		doneCh <- maybeServeAfterAnalyze(ctx, &out, cfg)
	}()

	// Wait for the URL line, then ensure opener is NOT invoked.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "report ready") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-called:
		t.Error("opener invoked despite --no-open")
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	<-doneCh
}
```

**Step 2: Run — must fail**

Run: `go test ./internal/cli/ -run TestMaybeServeAfterAnalyze -v`
Expected: FAIL — `undefined: maybeServeAfterAnalyze`, `autoServeConfig`.

---

## Task 10 — Auto-serve after `analyze` (GREEN)

**Files:**
- Modify: `internal/cli/analyze.go`

**Step 1: Add config + helper**

```go
type autoServeConfig struct {
	NoServe bool
	NoSite  bool
	SiteDir string
	Addr    string
	Open    bool
}

func maybeServeAfterAnalyze(ctx context.Context, out io.Writer, cfg autoServeConfig) error {
	if cfg.NoServe || cfg.NoSite {
		return nil
	}
	_, _ = fmt.Fprintf(out, "report ready — serving %s\n", cfg.SiteDir)
	return runHTTPServer(ctx, out, cfg.SiteDir, cfg.Addr, cfg.Open)
}
```

**Step 2: Wire flags**

Inside `newAnalyzeCmd`:

```go
var (
    // existing vars...
    noServe   bool
    noOpen    bool
    serveAddr string
)
```

After the existing flags block:

```go
cmd.Flags().BoolVar(&noServe, "no-serve", false, "do not auto-start the local server after analyze")
cmd.Flags().BoolVar(&noOpen, "no-open", false, "skip opening the report in the default browser")
cmd.Flags().StringVar(&serveAddr, "serve-addr", "127.0.0.1:8080", "bind address for the auto-started server")
```

**Step 3: Call helper at the end of `RunE`**

Right before the existing `return nil` at the bottom of `RunE`, add:

```go
return maybeServeAfterAnalyze(cmd.Context(), cmd.OutOrStdout(), autoServeConfig{
    NoServe: noServe,
    NoSite:  noSite,
    SiteDir: filepath.Join(projectDir, "site"),
    Addr:    serveAddr,
    Open:    !noOpen,
})
```

(Replace the existing `return nil`.)

**Step 4: Run**

Run: `go test ./internal/cli/ -run TestMaybeServeAfterAnalyze -v`
Expected: PASS (4/4).

Run the broader analyze tests to confirm no regression:

Run: `go test ./internal/cli/ -run TestAnalyze -v`
Expected: PASS — existing analyze tests must still pass. If any current test runs analyze to completion without `--no-serve` it will hang (the new server blocks on context); update those tests to pass `--no-serve` to preserve old behavior.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "feat(cli): auto-serve report when analyze completes"
```

---

## Task 11 — README + PROGRESS

**Files:**
- Modify: `README.md`
- Modify: `PROGRESS.md`

**Step 1: README — `serve` section**

Replace the existing `serve` paragraph with:

```
### Browse the report

    ftg serve

If `.find-the-gaps/` contains analyzed projects, `serve` either:
- serves the only project it finds, or
- prompts you to pick one when there are several (interactive terminals only).

You can skip the picker with `--project NAME` (the directory name under
`.find-the-gaps/`) or `--repo PATH` (matches the analyze invocation). Use
`--addr` to bind a specific port and `--open` to launch the browser.
```

**Step 2: README — `analyze` section**

Add after the existing `analyze` block:

```
When `analyze` finishes it automatically launches the report locally and
opens it in your browser. Pass `--no-serve` to skip the server, or
`--no-open` to keep the server running headless.
```

**Step 3: PROGRESS.md**

Append the standard CLAUDE.md §8 task block summarizing this feature.

**Step 4: Commit**

```bash
git add README.md PROGRESS.md
git commit -m "docs: document serve picker and analyze auto-open"
```

---

## Final verification

```bash
go test ./...                                                       # all green
go test -coverprofile=coverage.out ./internal/cli/... && \
  go tool cover -func=coverage.out | grep -E 'serve|cachescan|picker|server_runner'
                                                                    # ≥90% per file
golangci-lint run                                                   # clean
go build ./...                                                      # builds
```

Manual smoke test:

```bash
go build -o /tmp/ftg ./cmd/find-the-gaps
mkdir -p .find-the-gaps/foo/site .find-the-gaps/bar/site
echo foo > .find-the-gaps/foo/site/index.html
echo bar > .find-the-gaps/bar/site/index.html
/tmp/ftg serve --cache-dir .find-the-gaps     # picker appears
/tmp/ftg serve --project foo --cache-dir .find-the-gaps --addr 127.0.0.1:0
                                              # serves foo, no picker
/tmp/ftg analyze --repo . --no-serve          # analyze still works headless
```

---

## Open questions

1. **`--no-open` default for analyze.** Plan ships with auto-open ON. If the user runs analyze in CI by accident, this would block until killed. The `--no-serve` flag is the escape hatch, but should we instead default `--no-serve` when `!isInteractive()`? Suggested rule: if not a TTY, skip both server and open. This is one extra branch in `maybeServeAfterAnalyze`. Confirm before implementing.
2. **Picker keybindings.** `huh.NewSelect` ships with sensible defaults. No customization needed for v1.
3. **Port conflicts during analyze auto-serve.** If `127.0.0.1:8080` is busy, the analyze run will fail at the very end. Acceptable — the user re-runs with `--serve-addr 127.0.0.1:0` and gets a random free port. Document in the README's troubleshooting section if/when one exists.
