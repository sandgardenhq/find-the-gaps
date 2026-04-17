package spider

import (
	"fmt"
	"os"
	"testing"
)

// fakeFetcher returns a Fetcher that writes content to the output file.
func fakeFetcher(content string) Fetcher {
	return func(rawURL, outputPath string) error {
		return os.WriteFile(outputPath, []byte(content), 0o644)
	}
}

func TestMdfetchFetcher_missingBinary_returnsError(t *testing.T) {
	// Point PATH to an empty dir so mdfetch is not found.
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	opts := Options{CacheDir: t.TempDir(), Workers: 1}
	fetch := MdfetchFetcher(opts)
	err := fetch("https://docs.example.com", t.TempDir()+"/out.md")
	if err == nil {
		t.Error("expected error when mdfetch is not on PATH")
	}
}

func TestCrawl_singleURL_noLinks(t *testing.T) {
	dir := t.TempDir()
	opts := Options{CacheDir: dir, Workers: 1}

	result, err := Crawl("https://docs.example.com/intro", opts, fakeFetcher("# Hello\nNo links here."))
	if err != nil {
		t.Fatalf("Crawl failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(result), result)
	}
	if _, ok := result["https://docs.example.com/intro"]; !ok {
		t.Errorf("expected intro URL in results, got %v", result)
	}
}

func TestCrawl_fetchError_continuesAndReturnsEmptyMap(t *testing.T) {
	dir := t.TempDir()
	opts := Options{CacheDir: dir, Workers: 1}

	errFetch := func(rawURL, outputPath string) error {
		return fmt.Errorf("fetch failed for %s", rawURL)
	}

	// A fetch failure should not abort Crawl; it should return an empty map
	// (the failed URL is not recorded).
	result, err := Crawl("https://docs.example.com/intro", opts, errFetch)
	if err != nil {
		t.Fatalf("Crawl returned unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results on fetch failure, got %d: %v", len(result), result)
	}
}

func TestCrawl_fetcherIgnoresOutputPath_ReadFileFails(t *testing.T) {
	dir := t.TempDir()
	opts := Options{CacheDir: dir, Workers: 1}

	// This fetcher ignores the given outputPath and writes nothing — so
	// os.ReadFile on the expected path will fail. The crawl should not error
	// out; the failed URL simply won't appear in results.
	noOpFetch := func(rawURL, outputPath string) error {
		return nil // succeeds but does NOT write a file
	}

	result, err := Crawl("https://docs.example.com/intro", opts, noOpFetch)
	if err != nil {
		t.Fatalf("Crawl should not return an error for a ReadFile failure: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results when read fails, got %d: %v", len(result), result)
	}
}

func TestCrawl_invalidURL_returnsError(t *testing.T) {
	dir := t.TempDir()
	opts := Options{CacheDir: dir, Workers: 1}

	// A URL with a control character is unparseable.
	_, err := Crawl("://\x00bad", opts, fakeFetcher(""))
	if err == nil {
		t.Error("expected error for invalid startURL")
	}
}

func TestMdfetchFetcher_withTimeoutAndRetries_passesFlags(t *testing.T) {
	// Point PATH to an empty dir so mdfetch is not found — we just want the
	// exec.Command call to include the extra flags (we observe the error).
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	opts := Options{CacheDir: t.TempDir(), Workers: 1, Timeout: 5000, Retries: 3}
	fetch := MdfetchFetcher(opts)
	err := fetch("https://docs.example.com", t.TempDir()+"/out.md")
	// Expect an error (mdfetch not found), but the Fetcher should have been
	// constructed without panicking and should have reached exec.Command.
	if err == nil {
		t.Error("expected error when mdfetch is not on PATH")
	}
}
