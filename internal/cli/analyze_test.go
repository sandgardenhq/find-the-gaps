package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestAnalyze_scanCacheDirFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(&stdout, &stderr, []string{"analyze", "--help"})
	if !strings.Contains(stdout.String(), "--scan-cache-dir") {
		t.Errorf("--scan-cache-dir flag not in help output:\n%s", stdout.String())
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
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", dir,
		"--scan-cache-dir", cacheDir,
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
	cacheDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", dir,
		"--scan-cache-dir", cacheDir,
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

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--docs-url", "https://docs.example.com",
		"--cache-dir", f.Name(),
		"--workers", "1",
	})
	if code == 0 {
		t.Error("expected non-zero exit when crawl fails")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "crawl failed") {
		t.Errorf("expected 'crawl failed' in output; got: %s", combined)
	}
}
