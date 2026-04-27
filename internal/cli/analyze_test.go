package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeCmd_AcceptsTierFlags(t *testing.T) {
	cmd := newAnalyzeCmd()
	err := cmd.Flags().Parse([]string{
		"--llm-small=anthropic/claude-haiku-4-5",
		"--llm-typical=anthropic/claude-sonnet-4-6",
		"--llm-large=anthropic/claude-opus-4-7",
	})
	if err != nil {
		t.Fatalf("tier flags should parse: %v", err)
	}
}

func TestAnalyzeCmd_RejectsRemovedFlags(t *testing.T) {
	cmd := newAnalyzeCmd()
	err := cmd.Flags().Parse([]string{"--llm-provider=anthropic"})
	if err == nil {
		t.Fatal("--llm-provider should be removed and rejected")
	}
}

func TestAnalyze_repoFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--help"})
	if code != 0 {
		t.Fatalf("--help failed: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "--repo") {
		t.Errorf("--repo flag not in help output:\n%s", stdout.String())
	}
}

func TestAnalyze_noCacheFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(&stdout, &stderr, []string{"analyze", "--help"})
	if !strings.Contains(stdout.String(), "--no-cache") {
		t.Errorf("--no-cache flag not in help output:\n%s", stdout.String())
	}
}

func TestAnalyze_cacheDirFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(&stdout, &stderr, []string{"analyze", "--help"})
	if !strings.Contains(stdout.String(), "--cache-dir") {
		t.Errorf("--cache-dir flag not in help output:\n%s", stdout.String())
	}
}

func TestAnalyze_cacheUsesProjectSubdir(t *testing.T) {
	dir := t.TempDir()
	cacheBase := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", dir,
		"--cache-dir", cacheBase,
	})
	if code != 0 {
		t.Fatalf("analyze failed (code=%d): stderr=%q", code, stderr.String())
	}

	projectName := filepath.Base(dir)
	scanDir := filepath.Join(cacheBase, projectName, "scan")
	if _, err := os.Stat(filepath.Join(scanDir, "scan.json")); err != nil {
		t.Errorf("expected scan.json under %s: %v", scanDir, err)
	}
}

func TestAnalyze_docsURLFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--docs-url", "https://docs.example.com", "--help"})
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, "unknown flag") {
		t.Errorf("--docs-url flag not registered; got: %s", combined)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0; output=%q", code, combined)
	}
}

func TestAnalyze_repoFlag_scansDirectory(t *testing.T) {
	dir := t.TempDir()
	cacheBase := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", dir,
		"--cache-dir", cacheBase,
	})
	if code != 0 {
		t.Fatalf("analyze failed (code=%d): stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "scanned") {
		t.Errorf("expected 'scanned' in output, got:\n%s", stdout.String())
	}
}

func TestAnalyze_noCache_flagAccepted(t *testing.T) {
	dir := t.TempDir()
	cacheBase := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", dir,
		"--cache-dir", cacheBase,
		"--no-cache",
	})
	if code != 0 {
		t.Fatalf("analyze --no-cache failed (code=%d): stderr=%q", code, stderr.String())
	}
}

func TestAnalyze_crawlFails_returnsError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-dir")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// Eager LLM construction happens before Crawl, so we need a valid tiering.
	// fake-key is enough to satisfy the env-var check (no real API call is made
	// until after crawl, which errors first).
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--docs-url", "https://docs.example.com",
		"--cache-dir", f.Name(),
		"--workers", "1",
		"--llm-small", "ollama/llama3",
		"--llm-large", "anthropic/claude-test",
	})
	if code == 0 {
		t.Error("expected non-zero exit when crawl fails")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "crawl failed") {
		t.Errorf("expected 'crawl failed' in output; got: %s", combined)
	}
}

// prepareDocsCache pre-populates the spider index for docsURL inside cacheBase/projectName/docs
// so that spider.Crawl skips mdfetch and returns immediately with the cached page.
// pageContent is written to the cached markdown file.
func prepareDocsCache(t *testing.T, cacheBase, projectName, docsURL, pageContent string) {
	t.Helper()
	docsDir := filepath.Join(cacheBase, projectName, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := spider.LoadIndex(docsDir)
	if err != nil {
		t.Fatal(err)
	}
	filename := spider.URLToFilename(docsURL)
	mdPath := filepath.Join(docsDir, filename)
	if err := os.WriteFile(mdPath, []byte(pageContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.Record(docsURL, filename); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyze_llmClientError_returnsError(t *testing.T) {
	// Ensure Anthropic key is absent so newLLMTiering fails (all default tiers
	// resolve to anthropic/* and require ANTHROPIC_API_KEY).
	t.Setenv("ANTHROPIC_API_KEY", "")

	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	prepareDocsCache(t, cacheBase, projectName, docsURL, "# Doc page\nSome content.")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
	})
	if code == 0 {
		t.Error("expected non-zero exit when LLM client init fails")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "LLM client") {
		t.Errorf("expected 'LLM client' in output; got: %s", combined)
	}
}

func TestAnalyze_screenshotCheck_exercisesPath(t *testing.T) {
	// Pre-cache every other LLM-triggering step; the screenshot detector has no
	// cache, so it always fires when not explicitly skipped. Stub returns an
	// empty gap array so parsing succeeds and analyze completes.
	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(projectDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := spider.LoadIndex(docsDir)
	if err != nil {
		t.Fatal(err)
	}
	filename := spider.URLToFilename(docsURL)
	mdPath := filepath.Join(docsDir, filename)
	if err := os.WriteFile(mdPath, []byte("# Doc page\n\nClick the Save button.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.Record(docsURL, filename); err != nil {
		t.Fatal(err)
	}
	if err := idx.RecordAnalysis(docsURL, "Covers doc page.", []string{"feature-one"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.SetProductSummary("A test product.", []string{"feature-one"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codeFeatures := []analyzer.CodeFeature{
		{Name: "feature-one", Description: "Does feature one.", Layer: "cli", UserFacing: true},
	}
	scanForCache := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{{Path: "main.go"}},
	}
	if err := saveCodeFeaturesCache(filepath.Join(projectDir, "codefeatures.json"), scanForCache, codeFeatures); err != nil {
		t.Fatal(err)
	}
	fmCache := analyzer.FeatureMap{
		{Feature: codeFeatures[0], Files: []string{"main.go"}, Symbols: []string{"Run"}},
	}
	if err := saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"), codeFeatures, fmCache); err != nil {
		t.Fatal(err)
	}
	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: nil}}
	if err := saveDocsFeatureMapCache(filepath.Join(projectDir, "docsfeaturemap.json"), []string{"feature-one"}, docsFM); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"gaps":[]}`}},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-test",
		"--llm-large", "anthropic/claude-test",
		"--no-site",
	})
	if code != 0 {
		t.Fatalf("analyze failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "scanned") {
		t.Errorf("expected 'scanned' in output; got: %s", combined)
	}
	// screenshots.md must exist because the screenshot pass ran.
	if _, err := os.Stat(filepath.Join(projectDir, "screenshots.md")); err != nil {
		t.Errorf("expected screenshots.md to exist; got: %v", err)
	}
	// Stdout must list it in the reports block.
	if !strings.Contains(combined, "screenshots.md") {
		t.Errorf("expected 'screenshots.md' in output; got: %s", combined)
	}
	// It must NOT be annotated as skipped on the happy path.
	if strings.Contains(combined, "screenshots.md (skipped)") {
		t.Errorf("unexpected 'skipped' annotation on happy path; got: %s", combined)
	}
}

func TestAnalyze_allCached_noLLMCalls(t *testing.T) {
	// Every LLM-triggering step is pre-cached so analyze exits cleanly without
	// making any LLM calls. A fake server stands by to fail loudly if an
	// unexpected request arrives.
	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	// Write a simple Go file so scanner finds a symbol.
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	docsDir := filepath.Join(projectDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-populate the spider index so Crawl skips mdfetch.
	idx, err := spider.LoadIndex(docsDir)
	if err != nil {
		t.Fatal(err)
	}
	filename := spider.URLToFilename(docsURL)
	mdPath := filepath.Join(docsDir, filename)
	if err := os.WriteFile(mdPath, []byte("# Doc page\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.Record(docsURL, filename); err != nil {
		t.Fatal(err)
	}
	// Pre-record the analysis in the index so the page is skipped in the analyze loop.
	if err := idx.RecordAnalysis(docsURL, "Covers doc page.", []string{"feature-one"}); err != nil {
		t.Fatal(err)
	}
	// Pre-cache the product summary so SynthesizeProduct is skipped.
	if err := idx.SetProductSummary("A test product.", []string{"feature-one"}); err != nil {
		t.Fatal(err)
	}

	// Pre-cache code features, feature map, and docs feature map. This skips
	// ExtractFeaturesFromCode, MapFeaturesToCode, and MapFeaturesToDocs — all of
	// which would otherwise make LLM calls through tiering.Large().
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codeFeatures := []analyzer.CodeFeature{
		{Name: "feature-one", Description: "Does feature one.", Layer: "cli", UserFacing: true},
	}
	scanForCache := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{{Path: "main.go"}},
	}
	if err := saveCodeFeaturesCache(filepath.Join(projectDir, "codefeatures.json"), scanForCache, codeFeatures); err != nil {
		t.Fatal(err)
	}
	fmCache := analyzer.FeatureMap{
		{Feature: codeFeatures[0], Files: []string{"main.go"}, Symbols: []string{"Run"}},
	}
	if err := saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"), codeFeatures, fmCache); err != nil {
		t.Fatal(err)
	}
	// docsFeatureMap with no pages → DetectDrift skips the feature (no LLM call).
	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: nil}}
	if err := saveDocsFeatureMapCache(filepath.Join(projectDir, "docsfeaturemap.json"), []string{"feature-one"}, docsFM); err != nil {
		t.Fatal(err)
	}

	// Fake server that fails loudly if any LLM request arrives — none should.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("unexpected LLM request: all steps should hit cache")
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-test",
		"--llm-large", "anthropic/claude-test",
		"--skip-screenshot-check",
		"--no-site",
	})
	if code != 0 {
		t.Fatalf("analyze failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "scanned") {
		t.Errorf("expected 'scanned' in output; got: %s", combined)
	}
	// Screenshot pass was skipped — screenshots.md must NOT exist.
	if _, err := os.Stat(filepath.Join(projectDir, "screenshots.md")); !os.IsNotExist(err) {
		t.Errorf("expected screenshots.md to NOT exist when skipped; Stat err=%v", err)
	}
	// Stdout lists it as (skipped).
	if !strings.Contains(combined, "screenshots.md (skipped)") {
		t.Errorf("expected '(skipped)' annotation in output; got: %s", combined)
	}
}

func TestAnalyze_writesSiteAfterReports(t *testing.T) {
	// End-to-end: with hugo on PATH and no --no-site, the analyze pipeline must
	// produce <projectDir>/site/index.html alongside the markdown reports. This
	// covers the gap that the existing analyze tests dodge by passing --no-site.
	if _, err := exec.LookPath("hugo"); err != nil {
		t.Skip("hugo not on PATH; skipping site integration test")
	}

	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	docsDir := filepath.Join(projectDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := spider.LoadIndex(docsDir)
	if err != nil {
		t.Fatal(err)
	}
	filename := spider.URLToFilename(docsURL)
	if err := os.WriteFile(filepath.Join(docsDir, filename), []byte("# Doc page\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.Record(docsURL, filename); err != nil {
		t.Fatal(err)
	}
	if err := idx.RecordAnalysis(docsURL, "Covers doc page.", []string{"feature-one"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.SetProductSummary("A test product.", []string{"feature-one"}); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codeFeatures := []analyzer.CodeFeature{
		{Name: "feature-one", Description: "Does feature one.", Layer: "cli", UserFacing: true},
	}
	scanForCache := &scanner.ProjectScan{Files: []scanner.ScannedFile{{Path: "main.go"}}}
	if err := saveCodeFeaturesCache(filepath.Join(projectDir, "codefeatures.json"), scanForCache, codeFeatures); err != nil {
		t.Fatal(err)
	}
	fmCache := analyzer.FeatureMap{
		{Feature: codeFeatures[0], Files: []string{"main.go"}, Symbols: []string{"Run"}},
	}
	if err := saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"), codeFeatures, fmCache); err != nil {
		t.Fatal(err)
	}
	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: nil}}
	if err := saveDocsFeatureMapCache(filepath.Join(projectDir, "docsfeaturemap.json"), []string{"feature-one"}, docsFM); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("unexpected LLM request: all steps should hit cache")
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-test",
		"--llm-large", "anthropic/claude-test",
		"--skip-screenshot-check",
	})
	if code != 0 {
		t.Fatalf("analyze failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	siteIndex := filepath.Join(projectDir, "site", "index.html")
	if _, err := os.Stat(siteIndex); err != nil {
		t.Fatalf("expected rendered site at %s; Stat err=%v\nstdout=%s\nstderr=%s",
			siteIndex, err, stdout.String(), stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "/site/") {
		t.Errorf("expected stdout reports block to mention /site/; got: %s", combined)
	}
}

func TestAnalyze_anthropicProvider_usesAnthropicTokenCounter(t *testing.T) {
	// Verify the anthropic-backed LargeCounter is wired up without error.
	// To avoid real Anthropic API calls, we pre-cache all spider and product-summary
	// data so MapFeaturesToCode is called with zero sym lines (no files with symbols),
	// causing it to return immediately without invoking the counter.
	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))

	// Write an empty Go file — scanner includes it but finds no exported symbols.
	if err := os.WriteFile(filepath.Join(repoDir, "empty.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate the spider index with a page, its analysis, and a product summary.
	docsDir := filepath.Join(cacheBase, projectName, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := spider.LoadIndex(docsDir)
	if err != nil {
		t.Fatal(err)
	}
	filename := spider.URLToFilename(docsURL)
	if err := os.WriteFile(filepath.Join(docsDir, filename), []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.Record(docsURL, filename); err != nil {
		t.Fatal(err)
	}
	if err := idx.RecordAnalysis(docsURL, "A product.", []string{"feature-one"}); err != nil {
		t.Fatal(err)
	}
	// Pre-cache the product summary so synthesis (Bifrost LLM call) is skipped.
	if err := idx.SetProductSummary("A product.", []string{"feature-one"}); err != nil {
		t.Fatal(err)
	}

	// Set a fake API key so newLLMTiering succeeds without real credentials.
	t.Setenv("ANTHROPIC_API_KEY", "fake-key-for-test")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--llm-large", "anthropic/claude-test",
		"--skip-screenshot-check",
		"--no-site",
	})
	if code != 0 {
		t.Fatalf("analyze with anthropic provider failed (code=%d): stdout=%q stderr=%q",
			code, stdout.String(), stderr.String())
	}
}

func TestAnalyze_llmAnalyzeError_continuesWithWarning(t *testing.T) {
	// AnalyzePage routes through tiering.Small(). Point Small at a local ollama-
	// compatible server that returns 500 for every request; the analyze loop must
	// log a warning and continue rather than abort.
	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate the spider index but do NOT record an analysis, so the
	// analyze loop invokes AnalyzePage for this page.
	docsDir := filepath.Join(projectDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := spider.LoadIndex(docsDir)
	if err != nil {
		t.Fatal(err)
	}
	filename := spider.URLToFilename(docsURL)
	if err := os.WriteFile(filepath.Join(docsDir, filename), []byte("# Doc page\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.Record(docsURL, filename); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-test",
		"--llm-large", "anthropic/claude-test",
	})
	if code != 0 {
		t.Fatalf("analyze should exit 0 after skipping failed page (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "0 pages analyzed") {
		t.Errorf("expected '0 pages analyzed' in output; got: %s", combined)
	}
}

func TestAnalyzeCmd_HasSkipScreenshotCheckFlag(t *testing.T) {
	cmd := newAnalyzeCmd()
	f := cmd.Flags().Lookup("skip-screenshot-check")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
	assert.Contains(t, f.Usage, "screenshot")
}
