# Verbose Logging Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `charmbracelet/log` structured logging and a `--verbose` / `-v` persistent flag that enables debug-level output across all subcommands.

**Architecture:** A single global `charmbracelet/log` logger is configured in the root command's `PersistentPreRunE`, which runs before every subcommand. Without `--verbose` the level is `InfoLevel` (Info + Warn + Error visible); with `--verbose` it drops to `DebugLevel`. All log output goes to stderr; existing stdout program output (`scanned N files…`, report paths) is unchanged.

**Tech Stack:** `github.com/charmbracelet/log` — leveled, colorful, structured logging for Go CLIs.

---

### Task 1: Add charmbracelet/log dependency

**Files:**
- Modify: `go.mod`, `go.sum` (via `go get`)

**Step 1: Add the dependency**

```bash
go get github.com/charmbracelet/log
```

**Step 2: Verify it appears as a direct dep in go.mod**

```bash
grep 'charmbracelet/log' go.mod
```

Expected: a line like `github.com/charmbracelet/log v0.4.x`

**Step 3: Build to confirm no issues**

```bash
go build ./...
```

Expected: no output, exit 0.

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): add charmbracelet/log"
```

---

### Task 2: Add `--verbose` persistent flag and configure the logger

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

**Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestRootCmd_verboseFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--help"})
	if code != 0 {
		t.Fatalf("--help failed: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "--verbose") {
		t.Errorf("--verbose flag not in help output:\n%s", stdout.String())
	}
}

func TestRootCmd_verboseShorthand_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(&stdout, &stderr, []string{"--help"})
	if !strings.Contains(stdout.String(), "-v") {
		t.Errorf("-v shorthand not in help output:\n%s", stdout.String())
	}
}

func TestRootCmd_verbose_acceptedWithoutError(t *testing.T) {
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--verbose", "analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Fatalf("--verbose flag rejected (code=%d): stderr=%q", code, stderr.String())
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/cli/ -run 'TestRootCmd_verbose' -v
```

Expected: FAIL — `--verbose` not in help output.

**Step 3: Implement in `internal/cli/root.go`**

Add the import `"github.com/charmbracelet/log"` and update `NewRootCmd`:

```go
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var version = "dev"

// ExitCodeError signals to Execute that the CLI should exit with the given
// non-zero code without printing any additional error text. The subcommand
// that returns this error is responsible for having already written any
// user-facing output.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string { return fmt.Sprintf("exit code %d", e.Code) }

func NewRootCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "find-the-gaps",
		Short: "Find outdated or missing documentation in a codebase.",
		Long: "find-the-gaps analyzes a codebase alongside its documentation site to " +
			"identify outdated or missing documentation.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			log.SetOutput(cmd.ErrOrStderr())
			if verbose {
				log.SetLevel(log.DebugLevel)
			} else {
				log.SetLevel(log.InfoLevel)
			}
			return nil
		},
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "show debug logs")
	cmd.AddCommand(newDoctorCmd(), newAnalyzeCmd())
	return cmd
}

func Execute() int {
	return run(os.Stdout, os.Stderr, os.Args[1:])
}

func run(stdout, stderr io.Writer, args []string) int {
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return errorToExitCode(root.Execute(), stderr)
}

func errorToExitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code
	}
	_, _ = fmt.Fprintln(stderr, "Error:", err)
	return 1
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/cli/ -run 'TestRootCmd_verbose' -v
```

Expected: all three tests PASS.

**Step 5: Run full suite to confirm no regressions**

```bash
go test ./...
```

Expected: all packages pass.

**Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add --verbose/-v persistent flag to configure log level"
```

---

### Task 3: Add log calls to the analyze pipeline

**Files:**
- Modify: `internal/cli/analyze.go`
- Test: `internal/cli/root_test.go`

The analyze pipeline has several distinct phases. Replace the one existing `fmt.Fprintf` warning with `log.Warn`, add `log.Info` at each phase boundary, and `log.Debug` for granular detail. All use structured key-value pairs.

**Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestRun_verbose_showsDebugOutput(t *testing.T) {
	// Runs analyze over an empty repo (no docs-url) with --verbose.
	// Expects at least one DEBUG line in stderr.
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--verbose", "analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "DEBU") {
		t.Errorf("expected DEBU lines in stderr with --verbose; got: %q", stderr.String())
	}
}

func TestRun_noVerbose_noDebugOutput(t *testing.T) {
	// Same analyze call without --verbose must produce no DEBUG lines.
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "DEBU") {
		t.Errorf("expected no DEBU lines in stderr without --verbose; got: %q", stderr.String())
	}
}

func TestRun_verbose_warnVisibleWithoutVerbose(t *testing.T) {
	// Warn-level messages from the analyze pipeline must appear even without --verbose.
	// (Nothing currently triggers a Warn in the no-docs-url path, so just confirm
	// Info lines appear — the phase-start logs are Info.)
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	// The "scanning repository" Info log must appear at default level.
	if !strings.Contains(stderr.String(), "scanning repository") {
		t.Errorf("expected 'scanning repository' info log in stderr; got: %q", stderr.String())
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/cli/ -run 'TestRun_verbose|TestRun_noVerbose' -v
```

Expected: FAIL — no DEBU lines and no "scanning repository" in stderr.

**Step 3: Add log calls to `internal/cli/analyze.go`**

Add `"github.com/charmbracelet/log"` to the imports. Then add structured log calls at each pipeline stage. Here is the full updated `RunE` body with log calls in place:

```go
RunE: func(cmd *cobra.Command, _ []string) error {
    ctx := context.Background()

    projectName := filepath.Base(filepath.Clean(repoPath))
    projectDir := filepath.Join(cacheDir, projectName)

    log.Info("scanning repository", "path", repoPath)
    scanOpts := scanner.Options{
        CacheDir: filepath.Join(projectDir, "scan"),
        NoCache:  noCache,
    }
    scan, err := scanner.Scan(repoPath, scanOpts)
    if err != nil {
        return fmt.Errorf("scan failed: %w", err)
    }
    log.Debug("scan complete", "files", len(scan.Files))

    if docsURL == "" {
        _, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files\n", len(scan.Files))
        return nil
    }

    llmClient, err := newLLMClient(cfg)  // cfg is the *LLMConfig built from flags (unchanged)
    if err != nil {
        return fmt.Errorf("LLM client: %w", err)
    }

    log.Info("crawling docs site", "url", docsURL)
    docsDir := filepath.Join(projectDir, "docs")
    spiderOpts := spider.Options{
        CacheDir: docsDir,
        Workers:  workers,
    }
    pages, err := spider.Crawl(docsURL, spiderOpts, spider.MdfetchFetcher(spiderOpts))
    if err != nil {
        return fmt.Errorf("crawl failed: %w", err)
    }
    log.Debug("crawl complete", "pages", len(pages))

    // ... (rest of the function continues with log calls below)
```

For the per-page analysis loop, replace the existing warning and add debug logging:

```go
    log.Info("analyzing pages", "count", len(pages))
    var analyses []analyzer.PageAnalysis
    freshCount := 0
    for url, filePath := range pages {
        if summary, features, ok := idx.Analysis(url); ok {
            log.Debug("page cache hit", "url", url)
            analyses = append(analyses, analyzer.PageAnalysis{
                URL:      url,
                Summary:  summary,
                Features: features,
            })
            continue
        }
        content, readErr := os.ReadFile(filePath)
        if readErr != nil {
            continue
        }
        log.Debug("analyzing page", "url", url)
        pa, analyzeErr := analyzer.AnalyzePage(ctx, llmClient, url, string(content))
        if analyzeErr != nil {
            log.Warn("AnalyzePage failed", "url", url, "err", analyzeErr)  // replaces fmt.Fprintf warning
            continue
        }
        if recErr := idx.RecordAnalysis(url, pa.Summary, pa.Features); recErr != nil {
            return fmt.Errorf("record analysis: %w", recErr)
        }
        analyses = append(analyses, pa)
        freshCount++
    }
```

After the page loop:

```go
    log.Info("synthesizing product summary", "fresh_pages", freshCount)
    // ... synthesis logic unchanged ...

    log.Info("mapping features to code", "features", len(productSummary.Features))
    // ... token counter setup + MapFeaturesToCode call unchanged ...

    log.Debug("feature mapping complete", "mapped", len(featureMap))
```

The `fmt.Fprintf(cmd.ErrOrStderr(), "warning: AnalyzePage %s: %v\n", ...)` line is the only existing warn-level output. It is replaced by `log.Warn(...)` in the step above. Remove the `"os"` import only if it becomes unused after this change (it is still used for `os.ReadFile` and `os.Getenv`, so leave it).

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/cli/ -run 'TestRun_verbose|TestRun_noVerbose' -v
```

Expected: all three tests PASS.

**Step 5: Run full suite**

```bash
go test ./...
```

Expected: all packages pass.

**Step 6: Commit**

```bash
git add internal/cli/analyze.go internal/cli/root_test.go
git commit -m "feat(cli): add structured log calls to analyze pipeline"
```

---

### Task 4: Add log.Debug calls to the doctor package

**Files:**
- Modify: `internal/doctor/doctor.go`
- Test: `internal/cli/root_test.go`

Doctor currently uses `fmt.Fprintf` for all output. The user-facing OK/error lines stay as `fmt.Fprintf` (they are structured program output, not diagnostic logs). Add `log.Debug` for the internal probe step.

**Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestRun_verbose_doctorShowsDebugOutput(t *testing.T) {
	// Running doctor --verbose must produce DEBU lines in stderr
	// regardless of whether rg/mdfetch are installed.
	var stdout, stderr bytes.Buffer
	run(&stdout, &stderr, []string{"--verbose", "doctor"})
	// Do not assert exit code — tools may or may not be present in CI.
	if !strings.Contains(stderr.String(), "DEBU") {
		t.Errorf("expected DEBU lines in stderr with --verbose doctor; got: %q", stderr.String())
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/ -run 'TestRun_verbose_doctorShowsDebugOutput' -v
```

Expected: FAIL — no DEBU lines in stderr.

**Step 3: Add debug log calls to `internal/doctor/doctor.go`**

Add `"github.com/charmbracelet/log"` to the imports and add one `log.Debug` call per tool check:

```go
import (
    "context"
    "errors"
    "fmt"
    "io"
    "os/exec"
    "strings"

    "github.com/charmbracelet/log"
)
```

In the `check` function, add debug logging:

```go
func check(ctx context.Context, t Tool) result {
    log.Debug("checking tool", "name", t.Name, "binary", t.Binary)
    path, err := exec.LookPath(t.Binary)
    if err != nil {
        log.Debug("tool not found", "binary", t.Binary, "err", err)
        return result{tool: t, err: err}
    }
    out, err := exec.CommandContext(ctx, path, t.VersionArg).Output()
    if err != nil {
        log.Debug("tool version check failed", "binary", t.Binary, "path", path, "err", err)
        return result{tool: t, path: path, err: err}
    }
    version := firstLine(string(out))
    log.Debug("tool found", "binary", t.Binary, "path", path, "version", version)
    return result{tool: t, path: path, version: version}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/cli/ -run 'TestRun_verbose_doctorShowsDebugOutput' -v
```

Expected: PASS.

**Step 5: Run full suite and check coverage**

```bash
go test ./...
go test -cover ./internal/cli/ ./internal/doctor/
```

Expected: all pass, coverage at or above prior thresholds.

**Step 6: Commit**

```bash
git add internal/doctor/doctor.go internal/cli/root_test.go
git commit -m "feat(doctor): add debug log calls for tool probe steps"
```

---

## Done

After all four tasks pass with green tests, use `superpowers:finishing-a-development-branch` to complete the branch.
