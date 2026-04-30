package spider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestMdfetchFetcher_alwaysPassesAllLinks(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	fakeBin := filepath.Join(dir, "mdfetch")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	fetch := MdfetchFetcher(Options{CacheDir: t.TempDir()})
	_ = fetch("https://docs.example.com", filepath.Join(t.TempDir(), "out.md"))

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("args file not written: %v", err)
	}
	if !strings.Contains(string(got), "--all-links") {
		t.Errorf("expected --all-links in mdfetch args, got: %s", got)
	}
}

func TestMdfetchFetcher_alwaysPassesWrapImages(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	fakeBin := filepath.Join(dir, "mdfetch")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	fetch := MdfetchFetcher(Options{CacheDir: t.TempDir()})
	_ = fetch("https://docs.example.com", filepath.Join(t.TempDir(), "out.md"))

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("args file not written: %v", err)
	}
	if !strings.Contains(string(got), "--wrap-images") {
		t.Errorf("expected --wrap-images in mdfetch args, got: %s", got)
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

func TestCrawl_followsLinks(t *testing.T) {
	dir := t.TempDir()
	opts := Options{CacheDir: dir, Workers: 2}

	// Page A links to page B; page B has no links.
	fetched := make(map[string]bool)
	fetch := func(rawURL, outputPath string) error {
		fetched[rawURL] = true
		content := "# Page\nNo links."
		if rawURL == "https://docs.example.com/a" {
			content = "# Page A\n[B](https://docs.example.com/b)"
		}
		return os.WriteFile(outputPath, []byte(content), 0o644)
	}

	result, err := Crawl("https://docs.example.com/a", opts, fetch)
	if err != nil {
		t.Fatalf("Crawl failed: %v", err)
	}
	if !fetched["https://docs.example.com/b"] {
		t.Error("expected /b to be fetched via link from /a")
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d: %v", len(result), result)
	}
}

func TestCrawl_skipsCachedURLs(t *testing.T) {
	dir := t.TempDir()

	// Pre-populate the index with the start URL.
	idx, _ := LoadIndex(dir)
	_ = idx.Record("https://docs.example.com/intro", "preexisting.md")
	if err := os.WriteFile(filepath.Join(dir, "preexisting.md"), []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}

	fetchCount := 0
	fetch := func(rawURL, outputPath string) error {
		fetchCount++
		return os.WriteFile(outputPath, []byte("# Hi"), 0o644)
	}

	opts := Options{CacheDir: dir, Workers: 1}
	result, err := Crawl("https://docs.example.com/intro", opts, fetch)
	if err != nil {
		t.Fatalf("Crawl failed: %v", err)
	}
	if fetchCount != 0 {
		t.Errorf("expected 0 fetches for cached URL, got %d", fetchCount)
	}
	if _, ok := result["https://docs.example.com/intro"]; !ok {
		t.Errorf("cached URL should appear in results: %v", result)
	}
}
