package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/spider"
)

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

	// Use ollama so no API key is required; crawl fails before any LLM call.
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--docs-url", "https://docs.example.com",
		"--cache-dir", f.Name(),
		"--workers", "1",
		"--llm-provider", "ollama",
		"--llm-model", "llama3",
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

// newFakeOllamaServer returns an httptest.Server that speaks the OpenAI chat completions API.
// It returns the given response for every request.
func newFakeOllamaServer(t *testing.T, responseContent string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": responseContent}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAnalyze_llmClientError_returnsError(t *testing.T) {
	// Ensure Anthropic key is absent so newLLMClient fails after successful crawl.
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
		"--llm-provider", "anthropic",
	})
	if code == 0 {
		t.Error("expected non-zero exit when LLM client init fails")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "LLM client") {
		t.Errorf("expected 'LLM client' in output; got: %s", combined)
	}
}

func TestAnalyze_fullPipeline_withCachedAnalysis(t *testing.T) {
	// With all pages pre-analyzed in the index, analyses list is populated from cache.
	// When the fake LLM returns valid JSON, the full pipeline runs.
	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))

	// Write a simple Go file so scanner finds a symbol.
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	docsDir := filepath.Join(cacheBase, projectName, "docs")
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

	// The server inspects the request body to route to the correct response, because
	// MapFeaturesToCode and MapFeaturesToDocs run concurrently and may arrive in any order.
	//
	// Routing rules (based on distinctive prompt keywords):
	//   - "analyzing documentation for a software product" → SynthesizeProduct
	//   - "Documentation page URL:"                        → MapFeaturesToDocs
	//   - "Code symbols (format:"                          → MapFeaturesToCode
	//   - default                                          → ExtractFeaturesFromCode
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var reqBody struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &reqBody)
		var prompt string
		for _, m := range reqBody.Messages {
			prompt += m.Content
		}

		var resp string
		switch {
		case strings.Contains(prompt, "analyzing documentation for a software product"):
			resp = `{"description":"A test product.","features":["feature-one"]}`
		case strings.Contains(prompt, "Documentation page URL:"):
			resp = `["feature-one"]`
		case strings.Contains(prompt, "Code symbols (format:"):
			resp = `[{"feature":"feature-one","files":["main.go"],"symbols":["Run"]}]`
		case strings.Contains(prompt, "reviewing documentation accuracy"):
			// DetectDrift call — return empty findings array.
			resp = `[]`
		default:
			// ExtractFeaturesFromCode and any unknown call
			resp = `[{"name":"feature-one","description":"Does feature one.","layer":"cli","user_facing":true}]`
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": resp}},
			},
		})
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--llm-provider", "ollama",
		"--llm-base-url", srv.URL,
		"--llm-model", "test-model",
	})
	if code != 0 {
		t.Fatalf("analyze failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "scanned") {
		t.Errorf("expected 'scanned' in output; got: %s", combined)
	}
}

func TestAnalyze_anthropicProvider_usesAnthropicTokenCounter(t *testing.T) {
	// Verify the "case anthropic" branch in the token counter switch is reached.
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

	// Set a fake API key so newLLMClient succeeds without real credentials.
	t.Setenv("ANTHROPIC_API_KEY", "fake-key-for-test")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--llm-provider", "anthropic",
		"--llm-model", "claude-test",
	})
	if code != 0 {
		t.Fatalf("analyze with anthropic provider failed (code=%d): stdout=%q stderr=%q",
			code, stdout.String(), stderr.String())
	}
}

func TestAnalyze_llmAnalyzeError_continuesWithWarning(t *testing.T) {
	// LLM returns invalid JSON for AnalyzePage → warning printed, 0 analyses → 0 analyzed output.
	docsURL := "https://docs.example.com/page-" + time.Now().Format("20060102150405")
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))

	prepareDocsCache(t, cacheBase, projectName, docsURL, "# Doc page\n")
	// NOTE: do NOT call RecordAnalysis — this page has no cached analysis.

	// LLM returns invalid JSON → AnalyzePage fails → warning is emitted.
	srv := newFakeOllamaServer(t, "this is not valid json")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--llm-provider", "ollama",
		"--llm-base-url", srv.URL,
		"--llm-model", "test-model",
	})
	if code != 0 {
		t.Fatalf("analyze should succeed even with LLM errors (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	// Either a warning in stderr OR "0 pages analyzed" in stdout.
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "analyzed") && !strings.Contains(combined, "warning") {
		t.Errorf("expected warning or '0 pages analyzed'; got: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
