package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pdfreader "github.com/ledongthuc/pdf"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
	"github.com/sandgardenhq/find-the-gaps/internal/site"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
)

// renderProjectFixture writes a minimal but realistic set of cache files into
// <cacheDir>/<name>/ so the render command has something to consume. Returns
// the project directory.
func renderProjectFixture(t *testing.T, cacheDir, name string) string {
	t.Helper()
	dir := filepath.Join(cacheDir, name)
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := spider.LoadIndex(filepath.Join(dir, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.SetProductSummary("A small CLI demo.", []string{"search"}); err != nil {
		t.Fatal(err)
	}

	featureMapJSON := `{
  "features": [{"name": "search", "description": "", "layer": "cli", "user_facing": true}],
  "entries": [
    {
      "feature": {"name": "search", "description": "", "layer": "cli", "user_facing": true},
      "files": ["search.go"],
      "symbols": ["Search"]
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "featuremap.json"), []byte(featureMapJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	docsMapJSON := `{
  "features": ["search"],
  "entries": [{"feature": "search", "pages": ["https://docs.example.com/search"]}]
}`
	if err := os.WriteFile(filepath.Join(dir, "docsfeaturemap.json"), []byte(docsMapJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	driftJSON := `{
  "entries": [
    {
      "feature": "search",
      "files": ["search.go"],
      "filtered_pages": ["https://docs.example.com/search"],
      "pages": ["https://docs.example.com/search"],
      "issues": [
        {"page": "https://docs.example.com/search", "issue": "old signature", "priority": "large", "priority_reason": "user-impact"}
      ]
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "drift.json"), []byte(driftJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

// installFakeHugo installs a hugo stub on the bin directory and points
// site.HugoBin at it. The stub creates `--destination` so site.Build is
// satisfied. Returns a cleanup func.
func installFakeHugo(t *testing.T) func() {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake hugo shim is shell-script based; skipping on windows")
	}
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "hugo-fake")
	script := `#!/bin/sh
# Walk argv looking for --destination DEST and create it.
prev=""
for arg in "$@"; do
  if [ "$prev" = "--destination" ]; then
    mkdir -p "$arg"
    : > "$arg/index.html"
  fi
  prev="$arg"
done
exit 0
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := site.HugoBin
	site.HugoBin = fake
	return func() { site.HugoBin = orig }
}

// TestRenderCmd_RegeneratesGapsAndBuildsSite is the happy-path acceptance
// test for the render command: with all caches in place, gaps.md is
// rewritten with the latest reporter output (regression: pre-render-command,
// re-running analyze on a cached project skipped WriteGaps and the user got
// the old gaps.md), and site.Build is invoked (verified via fake hugo).
func TestRenderCmd_RegeneratesGapsAndBuildsSite(t *testing.T) {
	cleanup := installFakeHugo(t)
	defer cleanup()

	cacheDir := t.TempDir()
	projectDir := renderProjectFixture(t, cacheDir, "demo")

	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{
		"--cache-dir", cacheDir,
		"--project", "demo",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render failed: %v\nstderr: %s", err, stderr.String())
	}

	// gaps.md must be regenerated as plain markdown containing the drift
	// finding pulled from drift.json.
	gapsBytes, err := os.ReadFile(filepath.Join(projectDir, "gaps.md"))
	if err != nil {
		t.Fatalf("gaps.md not written: %v", err)
	}
	gaps := string(gapsBytes)
	for _, want := range []string{
		"## Stale Documentation",
		"### Large",
		"- **search** — [https://docs.example.com/search](https://docs.example.com/search) — old signature",
		"  - _Why:_ user-impact",
	} {
		if !strings.Contains(gaps, want) {
			t.Errorf("missing %q in regenerated gaps.md:\n%s", want, gaps)
		}
	}
	if strings.Contains(gaps, "<div") || strings.Contains(gaps, "<span") {
		t.Errorf("regenerated gaps.md must be plain markdown; got:\n%s", gaps)
	}

	// mapping.md is unconditionally regenerated.
	if _, err := os.Stat(filepath.Join(projectDir, "mapping.md")); err != nil {
		t.Errorf("mapping.md not written: %v", err)
	}
	// site/ exists thanks to the fake hugo stub.
	if _, err := os.Stat(filepath.Join(projectDir, "site", "index.html")); err != nil {
		t.Errorf("site/index.html not built: %v", err)
	}
	// stdout reports the rendered path.
	if !strings.Contains(stdout.String(), filepath.Join(projectDir, "site/")) {
		t.Errorf("stdout should report rendered site path; got: %q", stdout.String())
	}
}

// TestRenderCmd_RewritesScreenshotsWhenCached pins that a cached
// screenshots.json triggers regeneration of screenshots.md (as plain
// markdown) and that the file is also re-emitted as JSON so future reads
// stay self-consistent.
func TestRenderCmd_RewritesScreenshotsWhenCached(t *testing.T) {
	cleanup := installFakeHugo(t)
	defer cleanup()

	cacheDir := t.TempDir()
	projectDir := renderProjectFixture(t, cacheDir, "demo")

	// Seed screenshots.json via the writer so the on-disk shape stays in
	// sync with whatever WriteScreenshotsJSON produces.
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{{
			PageURL:        "https://docs.example.com/start",
			QuotedPassage:  "open the dashboard",
			ShouldShow:     "the dashboard view",
			SuggestedAlt:   "dashboard",
			InsertionHint:  "after first paragraph",
			Priority:       analyzer.PriorityMedium,
			PriorityReason: "user-impact",
		}},
	}
	if err := reporter.WriteScreenshotsJSON(projectDir, res); err != nil {
		t.Fatal(err)
	}

	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cache-dir", cacheDir, "--project", "demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render failed: %v\nstderr: %s", err, stderr.String())
	}

	shotsBytes, err := os.ReadFile(filepath.Join(projectDir, "screenshots.md"))
	if err != nil {
		t.Fatalf("screenshots.md not written: %v", err)
	}
	shots := string(shotsBytes)
	for _, want := range []string{
		"# Missing Screenshots",
		"### Medium",
		"- **Page:** [https://docs.example.com/start](https://docs.example.com/start)",
		"- **Should show:** the dashboard view",
		"- **Alt text:** `dashboard`",
	} {
		if !strings.Contains(shots, want) {
			t.Errorf("missing %q in regenerated screenshots.md:\n%s", want, shots)
		}
	}
	if strings.Contains(shots, "<div") || strings.Contains(shots, "<span") {
		t.Errorf("regenerated screenshots.md must be plain markdown; got:\n%s", shots)
	}
	// JSON is re-emitted on disk.
	if _, err := os.Stat(filepath.Join(projectDir, "screenshots.json")); err != nil {
		t.Errorf("screenshots.json not re-emitted: %v", err)
	}
}

// TestRenderCmd_NoScreenshotsWhenJSONAbsent pins that screenshots.md is NOT
// written when no screenshots.json is cached — render should not synthesize
// an empty screenshots page. A user who never ran the experimental pass
// should not see a screenshots/ section appear in their site.
func TestRenderCmd_NoScreenshotsWhenJSONAbsent(t *testing.T) {
	cleanup := installFakeHugo(t)
	defer cleanup()

	cacheDir := t.TempDir()
	projectDir := renderProjectFixture(t, cacheDir, "demo")

	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cache-dir", cacheDir, "--project", "demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render failed: %v\nstderr: %s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(projectDir, "screenshots.md")); !os.IsNotExist(err) {
		t.Errorf("screenshots.md must not be written when screenshots.json is absent; got err=%v", err)
	}
}

// TestRenderCmd_EmitsPDFByDefault pins that `ftg render` writes
// <projectDir>/report.pdf next to the existing markdown reports unless
// --no-pdf is passed. Verifies the integration of internal/pdf with the
// render command.
func TestRenderCmd_EmitsPDFByDefault(t *testing.T) {
	cleanup := installFakeHugo(t)
	defer cleanup()

	cacheDir := t.TempDir()
	projectDir := renderProjectFixture(t, cacheDir, "demo")

	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cache-dir", cacheDir, "--project", "demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render failed: %v\nstderr: %s", err, stderr.String())
	}

	info, err := os.Stat(filepath.Join(projectDir, "report.pdf"))
	if err != nil {
		t.Fatalf("report.pdf must be emitted by default: %v", err)
	}
	if info.Size() == 0 {
		t.Error("report.pdf must be non-empty")
	}
}

// TestRenderCmd_IncludesDeadLinksInPDFWhenLinksJSONPresent pins that render
// loads links.json from the project cache and threads it through to
// pdf.Inputs so the regenerated report.pdf carries the Dead Links section.
// Without this wiring, ftg render silently drops the section that ftg
// analyze produced.
func TestRenderCmd_IncludesDeadLinksInPDFWhenLinksJSONPresent(t *testing.T) {
	cleanup := installFakeHugo(t)
	defer cleanup()

	cacheDir := t.TempDir()
	projectDir := renderProjectFixture(t, cacheDir, "demo")

	const seededURL = "https://very-unique-broken-host.example/path"
	linksJSON := `{
  "broken": [{"url": "` + seededURL + `", "error_type": "http_404", "detail": "HTTP 404 Not Found", "pages": ["https://docs.example.com/x"]}],
  "auth_required": []
}`
	if err := os.WriteFile(filepath.Join(projectDir, "links.json"), []byte(linksJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cache-dir", cacheDir, "--project", "demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render failed: %v\nstderr: %s", err, stderr.String())
	}

	pdfPath := filepath.Join(projectDir, "report.pdf")
	f, r, err := pdfreader.Open(pdfPath)
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer f.Close()
	rd, err := r.GetPlainText()
	if err != nil {
		t.Fatalf("plaintext: %v", err)
	}
	b, err := io.ReadAll(rd)
	if err != nil {
		t.Fatalf("read pdf text: %v", err)
	}
	text := string(b)
	if !strings.Contains(text, "Dead Links") {
		t.Errorf("expected 'Dead Links' heading in PDF text; got:\n%s", text)
	}
	if !strings.Contains(text, seededURL) {
		t.Errorf("expected seeded URL %q in PDF text", seededURL)
	}
}

// TestRenderCmd_SkipsPDFWithNoPDFFlag pins that --no-pdf suppresses
// the report.pdf artifact while leaving every other output in place.
func TestRenderCmd_SkipsPDFWithNoPDFFlag(t *testing.T) {
	cleanup := installFakeHugo(t)
	defer cleanup()

	cacheDir := t.TempDir()
	projectDir := renderProjectFixture(t, cacheDir, "demo")

	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cache-dir", cacheDir, "--project", "demo", "--no-pdf"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render failed: %v\nstderr: %s", err, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(projectDir, "report.pdf")); !os.IsNotExist(err) {
		t.Errorf("report.pdf must NOT be written when --no-pdf is set; got err=%v", err)
	}
	// Markdown reports still emitted.
	if _, err := os.Stat(filepath.Join(projectDir, "mapping.md")); err != nil {
		t.Errorf("mapping.md must still be written with --no-pdf: %v", err)
	}
}

// TestRenderCmd_FailsWithoutFeatureMap pins that render exits non-zero with
// a clear "run analyze first" message when the required cache files are
// missing — the user gets a real signal, not a partial run.
func TestRenderCmd_FailsWithoutFeatureMap(t *testing.T) {
	cleanup := installFakeHugo(t)
	defer cleanup()

	cacheDir := t.TempDir()
	dir := filepath.Join(cacheDir, "demo")
	// Just the docs/ subdir + product summary; no featuremap.json.
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := spider.LoadIndex(filepath.Join(dir, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.SetProductSummary("x", []string{"y"}); err != nil {
		t.Fatal(err)
	}

	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cache-dir", cacheDir, "--project", "demo"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error when featuremap.json is missing")
	}
	if !strings.Contains(err.Error(), "featuremap.json not found") {
		t.Errorf("error should name the missing file; got: %v", err)
	}
	if !strings.Contains(err.Error(), "ftg analyze") {
		t.Errorf("error should point user at `ftg analyze`; got: %v", err)
	}
}

// TestRenderCmd_ProjectFlag_UnknownProject pins the error path when the
// caller passes --project for a non-existent project.
func TestRenderCmd_ProjectFlag_UnknownProject(t *testing.T) {
	cacheDir := t.TempDir()
	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cache-dir", cacheDir, "--project", "ghost"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
	if !strings.Contains(err.Error(), "no analyzed project") {
		t.Errorf("expected helpful 'no analyzed project' error; got: %v", err)
	}
}

// TestRenderCmd_RejectsConflictingFlags pins that --project and --repo
// can't be combined (mirrors `ftg serve`).
func TestRenderCmd_RejectsConflictingFlags(t *testing.T) {
	cmd := newRenderCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--project", "x", "--repo", "/tmp/y"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --project and --repo combined")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' error; got: %v", err)
	}
}

// TestRenderCmd_InHelp pins that the new subcommand surfaces in `ftg --help`
// — a regression here means the user sees no command.
func TestRenderCmd_InHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--help"})
	if code != 0 {
		t.Fatalf("--help failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "render") {
		t.Errorf("`render` not in help output:\n%s", stdout.String())
	}
}

// stayCloseToWriter verifies the JSON shape expected by loadCachedFeatureMap
// matches what saveFeatureMapCache produces — i.e., a forward-compat lock on
// the on-disk format. Checks both ways round-trip cleanly.
func TestLoadCachedFeatureMap_ShapeMatchesWriter(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "featuremap.json")
	features := []analyzer.CodeFeature{{Name: "x", UserFacing: true}}
	fm := analyzer.FeatureMap{{
		Feature: analyzer.CodeFeature{Name: "x", UserFacing: true},
		Files:   []string{"a.go"},
		Symbols: []string{"Sym"},
	}}
	if err := saveFeatureMapCache(path, features, fm); err != nil {
		t.Fatal(err)
	}
	got, err := loadCachedFeatureMap(path)
	if err != nil {
		t.Fatalf("loadCachedFeatureMap: %v", err)
	}
	if len(got) != 1 || got[0].Feature.Name != "x" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	// Sanity: the file is parseable as the writer's expected shape too.
	data, _ := os.ReadFile(path)
	var raw featureMapCacheFile
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("writer shape unparseable: %v", err)
	}
}
