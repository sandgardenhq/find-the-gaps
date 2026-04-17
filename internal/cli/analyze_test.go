package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestAnalyze_missingFlags_returnsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// analyze with no --docs-url flag should fail with a usage error, not a panic.
	code := run(&stdout, &stderr, []string{"analyze"})
	if code == 0 {
		t.Error("expected non-zero exit when required flags are missing")
	}
}

func TestAnalyze_helpFlag_exits0(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--help"})
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
}

// TestAnalyze_flagsExist verifies that --docs-url, --cache-dir, and --workers
// flags are registered on the analyze subcommand. The stub doesn't register
// these flags, so this test is RED until the full implementation is wired in.
func TestAnalyze_flagsExist(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// Pass --docs-url with a value; if the flag is not registered cobra will
	// error with "unknown flag".
	code := run(&stdout, &stderr, []string{"analyze", "--docs-url", "https://docs.example.com", "--help"})
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, "unknown flag") {
		t.Errorf("--docs-url flag not registered; got: %s", combined)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0; output=%q", code, combined)
	}
}

// TestAnalyze_crawlFails covers the "crawl failed" error path in RunE by
// pointing --cache-dir at a regular file (so os.MkdirAll fails inside Crawl).
func TestAnalyze_crawlFails_returnsError(t *testing.T) {
	// Create a regular file and use it as the cache-dir to force MkdirAll to fail.
	f, err := os.CreateTemp(t.TempDir(), "not-a-dir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--docs-url", "https://docs.example.com",
		"--cache-dir", f.Name(), // a regular file, not a directory — MkdirAll fails
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
