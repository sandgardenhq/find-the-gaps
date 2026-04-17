# MDFetch Spider Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement a concurrent site spider that shells out to `mdfetch` to fetch every page of a docs site, caches results to disk, and returns a `map[URL]filepath` for downstream analysis.

**Architecture:** A coordinator goroutine manages a visited-URL set and a BFS queue, feeding URLs to a pool of N worker goroutines via a `jobs` channel; workers shell out to `mdfetch`, read the resulting markdown, extract same-host links, and send results back on a `results` channel. Disk cache lives under `.find-the-gaps/cache/` (configurable); filenames are SHA-256 hashes of the URL; `index.json` records the URL→filename mapping and acts as the visited-set seed on re-runs.

**Tech Stack:** Go stdlib (`crypto/sha256`, `encoding/json`, `net/url`, `os/exec`, `regexp`, `sync`), Cobra flags on the `analyze` command.

**Working directory:** `.worktrees/feat-mdfetch-spider` (the `feat/mdfetch-spider` branch).

**Run all tests with:** `go test ./...`
**Run a single package:** `go test ./internal/spider/... -v -run TestName`
**Build check:** `go build ./...`
**Coverage:** `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`

---

### Task 1: `cache.go` — `URLToFilename`

**Files:**
- Create: `internal/spider/cache.go`
- Create: `internal/spider/cache_test.go`

**Step 1: Write the failing test**

`internal/spider/cache_test.go`:
```go
package spider

import (
	"strings"
	"testing"
)

func TestURLToFilename_isStable(t *testing.T) {
	a := URLToFilename("https://docs.example.com/intro")
	b := URLToFilename("https://docs.example.com/intro")
	if a != b {
		t.Errorf("URLToFilename is not stable: %q != %q", a, b)
	}
}

func TestURLToFilename_differsAcrossURLs(t *testing.T) {
	a := URLToFilename("https://docs.example.com/intro")
	b := URLToFilename("https://docs.example.com/reference")
	if a == b {
		t.Error("URLToFilename returned same name for different URLs")
	}
}

func TestURLToFilename_hasMDExtension(t *testing.T) {
	name := URLToFilename("https://docs.example.com/intro")
	if !strings.HasSuffix(name, ".md") {
		t.Errorf("expected .md suffix, got %q", name)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/spider/... -v -run TestURLToFilename
```
Expected: compile error — package `spider` does not exist yet.

**Step 3: Write minimal implementation**

`internal/spider/cache.go`:
```go
package spider

import (
	"crypto/sha256"
	"fmt"
)

// URLToFilename returns a stable, collision-resistant filename for rawURL.
func URLToFilename(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return fmt.Sprintf("%x.md", sum)
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/spider/... -v -run TestURLToFilename
```
Expected: all three tests PASS.

**Step 5: Commit**

```bash
git add internal/spider/cache.go internal/spider/cache_test.go
git commit -m "feat(spider): URLToFilename — SHA-256 hash of URL"
```

---

### Task 2: `cache.go` — `Index` struct and `LoadIndex`

**Files:**
- Modify: `internal/spider/cache.go`
- Modify: `internal/spider/cache_test.go`

The `Index` is loaded from `index.json` in the cache directory. If the file does not exist, an empty index is returned (not an error).

**Step 1: Write the failing tests**

Append to `internal/spider/cache_test.go`:
```go
func TestLoadIndex_missingDir_returnsEmptyIndex(t *testing.T) {
	idx, err := LoadIndex(t.TempDir() + "/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
}

func TestLoadIndex_emptyDir_returnsEmptyIndex(t *testing.T) {
	idx, err := LoadIndex(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
}

func TestLoadIndex_existingIndex_loadsEntries(t *testing.T) {
	dir := t.TempDir()
	data := `{"https://docs.example.com/intro":{"filename":"abc.md","fetched_at":"2026-01-01T00:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !idx.Has("https://docs.example.com/intro") {
		t.Error("expected loaded index to contain the URL")
	}
}
```

Add missing imports to the test file:
```go
import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/spider/... -v -run TestLoadIndex
```
Expected: compile error — `LoadIndex` and `Has` undefined.

**Step 3: Write minimal implementation**

Append to `internal/spider/cache.go`:
```go
import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type indexEntry struct {
	Filename  string    `json:"filename"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Index is an in-memory view of index.json backed by a cache directory.
type Index struct {
	dir     string
	entries map[string]indexEntry
}

// LoadIndex reads index.json from dir, or returns an empty index if the file
// does not exist. It creates dir if it does not exist.
func LoadIndex(dir string) (*Index, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	idx := &Index{dir: dir, entries: make(map[string]indexEntry)}
	data, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return idx, nil
	}
	if err != nil {
		return nil, err
	}
	return idx, json.Unmarshal(data, &idx.entries)
}

// Has reports whether rawURL is already recorded in the index.
func (idx *Index) Has(rawURL string) bool {
	_, ok := idx.entries[rawURL]
	return ok
}
```

Note: replace the bare `import` block at the top of `cache.go` with this consolidated one; remove the earlier minimal import.

**Step 4: Run test to verify it passes**

```bash
go test ./internal/spider/... -v -run TestLoadIndex
```
Expected: all three tests PASS.

**Step 5: Commit**

```bash
git add internal/spider/cache.go internal/spider/cache_test.go
git commit -m "feat(spider): Index struct + LoadIndex + Has"
```

---

### Task 3: `cache.go` — `Index.Record`, `Index.FilePath`, `Index.All`

**Files:**
- Modify: `internal/spider/cache.go`
- Modify: `internal/spider/cache_test.go`

`Record` adds a URL to the in-memory index and persists `index.json`. `FilePath` returns the absolute path for a cached URL. `All` returns every URL→filepath in the index.

**Step 1: Write the failing tests**

Append to `internal/spider/cache_test.go`:
```go
func TestIndex_Record_persistsAndCanBeReloaded(t *testing.T) {
	dir := t.TempDir()
	idx, _ := LoadIndex(dir)

	if err := idx.Record("https://docs.example.com/intro", "abc.md"); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Reload from disk to verify persistence.
	idx2, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if !idx2.Has("https://docs.example.com/intro") {
		t.Error("URL not found after reload")
	}
}

func TestIndex_FilePath_returnsAbsPath(t *testing.T) {
	dir := t.TempDir()
	idx, _ := LoadIndex(dir)
	_ = idx.Record("https://docs.example.com/intro", "abc.md")

	path, ok := idx.FilePath("https://docs.example.com/intro")
	if !ok {
		t.Fatal("FilePath returned ok=false for known URL")
	}
	if path != filepath.Join(dir, "abc.md") {
		t.Errorf("got %q, want %q", path, filepath.Join(dir, "abc.md"))
	}
}

func TestIndex_FilePath_unknownURL_returnsNotOK(t *testing.T) {
	dir := t.TempDir()
	idx, _ := LoadIndex(dir)
	_, ok := idx.FilePath("https://docs.example.com/missing")
	if ok {
		t.Error("expected ok=false for unknown URL")
	}
}

func TestIndex_All_returnsAllEntries(t *testing.T) {
	dir := t.TempDir()
	idx, _ := LoadIndex(dir)
	_ = idx.Record("https://docs.example.com/a", "a.md")
	_ = idx.Record("https://docs.example.com/b", "b.md")

	all := idx.All()
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}
	if all["https://docs.example.com/a"] != filepath.Join(dir, "a.md") {
		t.Errorf("wrong path for /a: %q", all["https://docs.example.com/a"])
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/spider/... -v -run "TestIndex_Record|TestIndex_FilePath|TestIndex_All"
```
Expected: compile error — `Record`, `FilePath`, `All` undefined.

**Step 3: Write minimal implementation**

Append to `internal/spider/cache.go`:
```go
// Record adds rawURL to the index with the given filename and saves index.json.
func (idx *Index) Record(rawURL, filename string) error {
	idx.entries[rawURL] = indexEntry{Filename: filename, FetchedAt: time.Now()}
	return idx.save()
}

// FilePath returns the absolute cache file path for rawURL, if present.
func (idx *Index) FilePath(rawURL string) (string, bool) {
	e, ok := idx.entries[rawURL]
	if !ok {
		return "", false
	}
	return filepath.Join(idx.dir, e.Filename), true
}

// All returns a map of every cached URL to its absolute file path.
func (idx *Index) All() map[string]string {
	out := make(map[string]string, len(idx.entries))
	for u, e := range idx.entries {
		out[u] = filepath.Join(idx.dir, e.Filename)
	}
	return out
}

func (idx *Index) save() error {
	data, err := json.MarshalIndent(idx.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(idx.dir, "index.json"), data, 0o644)
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/spider/... -v -run "TestIndex_Record|TestIndex_FilePath|TestIndex_All"
```
Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/spider/cache.go internal/spider/cache_test.go
git commit -m "feat(spider): Index.Record / FilePath / All"
```

---

### Task 4: `links.go` — `ExtractLinks` (markdown links + same-host filter + relative resolution)

**Files:**
- Create: `internal/spider/links.go`
- Create: `internal/spider/links_test.go`

**Step 1: Write the failing tests**

`internal/spider/links_test.go`:
```go
package spider

import (
	"net/url"
	"testing"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

func TestExtractLinks_absoluteMarkdownLinks_sameHost(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[API](https://docs.example.com/api) [ext](https://other.com/page)`
	links := ExtractLinks(md, base)
	if len(links) != 1 || links[0] != "https://docs.example.com/api" {
		t.Errorf("got %v, want [https://docs.example.com/api]", links)
	}
}

func TestExtractLinks_relativeLinks_resolvedAgainstBase(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[ref](/reference)`
	links := ExtractLinks(md, base)
	if len(links) != 1 || links[0] != "https://docs.example.com/reference" {
		t.Errorf("got %v, want [https://docs.example.com/reference]", links)
	}
}

func TestExtractLinks_fragmentOnly_dropped(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[section](#anchor)`
	links := ExtractLinks(md, base)
	if len(links) != 0 {
		t.Errorf("expected no links, got %v", links)
	}
}

func TestExtractLinks_mailto_dropped(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[email](mailto:foo@bar.com)`
	links := ExtractLinks(md, base)
	if len(links) != 0 {
		t.Errorf("expected no links, got %v", links)
	}
}

func TestExtractLinks_deduplicatesWithinPage(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[a](https://docs.example.com/api) [b](https://docs.example.com/api)`
	links := ExtractLinks(md, base)
	if len(links) != 1 {
		t.Errorf("expected 1 link after dedup, got %v", links)
	}
}

func TestExtractLinks_bareURLs_sameHost(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `See https://docs.example.com/guide for more.`
	links := ExtractLinks(md, base)
	found := false
	for _, l := range links {
		if l == "https://docs.example.com/guide" {
			found = true
		}
	}
	if !found {
		t.Errorf("bare URL not found in %v", links)
	}
}

func TestExtractLinks_fragmentStrippedFromAbsoluteLink(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[ref](https://docs.example.com/api#section)`
	links := ExtractLinks(md, base)
	if len(links) != 1 || links[0] != "https://docs.example.com/api" {
		t.Errorf("got %v, want [https://docs.example.com/api]", links)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/spider/... -v -run TestExtractLinks
```
Expected: compile error — `ExtractLinks` undefined.

**Step 3: Write minimal implementation**

`internal/spider/links.go`:
```go
package spider

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	mdLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	bareURLRe = regexp.MustCompile(`(?:^|[\s(>])(https?://[^\s)<>"']+)`)
)

// ExtractLinks parses same-host links from markdown, resolving relative URLs
// against pageURL. Returns deduplicated absolute URL strings, fragments stripped.
func ExtractLinks(markdown string, pageURL *url.URL) []string {
	seen := make(map[string]bool)
	var links []string

	add := func(raw string) {
		raw = strings.TrimRight(raw, ".,;!?)")
		ref, err := url.Parse(raw)
		if err != nil {
			return
		}
		resolved := pageURL.ResolveReference(ref)
		if resolved.Scheme == "mailto" || resolved.Host == "" {
			return
		}
		if resolved.Host != pageURL.Host {
			return
		}
		resolved.Fragment = ""
		abs := resolved.String()
		if !seen[abs] {
			seen[abs] = true
			links = append(links, abs)
		}
	}

	for _, m := range mdLinkRe.FindAllStringSubmatch(markdown, -1) {
		add(m[2])
	}
	for _, m := range bareURLRe.FindAllStringSubmatch(markdown, -1) {
		add(m[1])
	}

	return links
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/spider/... -v -run TestExtractLinks
```
Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/spider/links.go internal/spider/links_test.go
git commit -m "feat(spider): ExtractLinks — markdown + bare URLs, same-host filter"
```

---

### Task 5: `spider.go` — `Options`, `Fetcher` type, `MdfetchFetcher`

**Files:**
- Create: `internal/spider/spider.go`
- Create: `internal/spider/spider_test.go`

The `Fetcher` type is the function signature workers call. `MdfetchFetcher` is the production implementation that shells out to `mdfetch`. Tests use a fake fetcher.

**Step 1: Write the failing test**

`internal/spider/spider_test.go`:
```go
package spider

import (
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/spider/... -v -run TestMdfetchFetcher
```
Expected: compile error — `Fetcher`, `Options`, `MdfetchFetcher` undefined.

**Step 3: Write minimal implementation**

`internal/spider/spider.go`:
```go
package spider

import (
	"fmt"
	"os/exec"
	"strconv"
)

// Options configures the spider.
type Options struct {
	CacheDir string
	Workers  int    // number of parallel mdfetch workers; default 5
	Timeout  int    // mdfetch --timeout in ms; 0 = mdfetch default
	Retries  int    // mdfetch --retries; 0 = mdfetch default
}

// Fetcher fetches rawURL and writes markdown to outputPath.
type Fetcher func(rawURL, outputPath string) error

// MdfetchFetcher returns a Fetcher that shells out to the mdfetch binary.
func MdfetchFetcher(opts Options) Fetcher {
	return func(rawURL, outputPath string) error {
		args := []string{rawURL, "-o", outputPath}
		if opts.Timeout > 0 {
			args = append(args, "--timeout", strconv.Itoa(opts.Timeout))
		}
		if opts.Retries > 0 {
			args = append(args, "--retries", strconv.Itoa(opts.Retries))
		}
		cmd := exec.Command("mdfetch", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("mdfetch %s: %w\n%s", rawURL, err, out)
		}
		return nil
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/spider/... -v -run TestMdfetchFetcher
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/spider/spider.go internal/spider/spider_test.go
git commit -m "feat(spider): Options + Fetcher type + MdfetchFetcher"
```

---

### Task 6: `spider.go` — `Crawl` (single URL, no outbound links)

**Files:**
- Modify: `internal/spider/spider.go`
- Modify: `internal/spider/spider_test.go`

Implement `Crawl` and verify it fetches a single URL and records it in the cache when the fetched content contains no links.

**Step 1: Write the failing test**

Append to `internal/spider/spider_test.go`:
```go
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/spider/... -v -run TestCrawl_singleURL
```
Expected: compile error — `Crawl` undefined.

**Step 3: Write minimal implementation**

Append to `internal/spider/spider.go` (add full imports at top):
```go
import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

type crawlResult struct {
	rawURL   string
	filePath string
	links    []string
	err      error
}

// Crawl fetches startURL and every same-host link discovered in fetched pages.
// It returns a map of URL → absolute cache file path.
// fetch is called once per URL; use MdfetchFetcher for production.
func Crawl(startURL string, opts Options, fetch Fetcher) (map[string]string, error) {
	if opts.Workers <= 0 {
		opts.Workers = 5
	}
	if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
		return nil, err
	}
	idx, err := LoadIndex(opts.CacheDir)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(startURL)
	if err != nil {
		return nil, err
	}

	pages := idx.All()
	visited := make(map[string]bool, len(pages))
	for u := range pages {
		visited[u] = true
	}

	jobs := make(chan string)
	results := make(chan crawlResult)

	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rawURL := range jobs {
				results <- doFetch(rawURL, opts.CacheDir, base, fetch, idx)
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	inFlight := 0
	enqueue := func(rawURL string) {
		if !visited[rawURL] {
			visited[rawURL] = true
			inFlight++
			go func() { jobs <- rawURL }()
		}
	}

	enqueue(startURL)
	if inFlight == 0 {
		close(jobs)
		return pages, nil
	}

	for res := range results {
		inFlight--
		if res.err != nil {
			// log and continue — a single failed page does not abort the crawl
			_ = res.err
		} else {
			pages[res.rawURL] = res.filePath
			for _, link := range res.links {
				enqueue(link)
			}
		}
		if inFlight == 0 {
			close(jobs)
		}
	}

	return pages, nil
}

func doFetch(rawURL, cacheDir string, base *url.URL, fetch Fetcher, idx *Index) crawlResult {
	filename := URLToFilename(rawURL)
	filePath := fmt.Sprintf("%s/%s", cacheDir, filename)

	if err := fetch(rawURL, filePath); err != nil {
		return crawlResult{rawURL: rawURL, err: err}
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return crawlResult{rawURL: rawURL, err: err}
	}

	pageURL, _ := url.Parse(rawURL)
	links := ExtractLinks(string(content), pageURL)

	if err := idx.Record(rawURL, filename); err != nil {
		return crawlResult{rawURL: rawURL, err: err}
	}

	return crawlResult{rawURL: rawURL, filePath: filePath, links: links}
}
```

Note: use `path/filepath.Join` instead of `fmt.Sprintf` for `filePath` — replace with `filepath.Join(cacheDir, filename)` and add `"path/filepath"` to imports.

**Step 4: Run test to verify it passes**

```bash
go test ./internal/spider/... -v -run TestCrawl_singleURL
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/spider/spider.go internal/spider/spider_test.go
git commit -m "feat(spider): Crawl — single URL, worker pool skeleton"
```

---

### Task 7: `spider.go` — `Crawl` follows discovered links

**Files:**
- Modify: `internal/spider/spider_test.go`

Verify that links found in a fetched page are themselves fetched.

**Step 1: Write the failing test**

Append to `internal/spider/spider_test.go`:
```go
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/spider/... -v -run TestCrawl_followsLinks
```
Expected: FAIL — only 1 result returned (link following not wired up yet), OR this may already pass if the Task 6 implementation is correct. If it passes, skip to step 5 and just commit.

**Step 3: Fix if needed**

If the test fails, the likely cause is a race between `close(jobs)` and `enqueue` trying to send on the closed channel. The `go func() { jobs <- rawURL }()` goroutine in `enqueue` may fire after `close(jobs)`. Fix by using a `queue []string` in the coordinator instead of goroutines:

Replace the `enqueue` + coordinator loop in `Crawl` with:
```go
queue := []string{}
markAndEnqueue := func(rawURL string) {
    if !visited[rawURL] {
        visited[rawURL] = true
        queue = append(queue, rawURL)
    }
}
markAndEnqueue(startURL)

// Feed loop: send from queue, receive results.
for len(queue) > 0 || inFlight > 0 {
    var sendCh chan<- string
    var next string
    if len(queue) > 0 {
        sendCh = jobs
        next = queue[0]
    }
    select {
    case sendCh <- next:
        queue = queue[1:]
        inFlight++
    case res := <-results:
        inFlight--
        if res.err == nil {
            pages[res.rawURL] = res.filePath
            for _, link := range res.links {
                markAndEnqueue(link)
            }
        }
    }
}
close(jobs)
```
Remove the old `inFlight`, `enqueue`, and for-range-results block.

**Step 4: Run test to verify it passes**

```bash
go test ./internal/spider/... -v -run TestCrawl
```
Expected: all `TestCrawl_*` tests PASS.

**Step 5: Commit**

```bash
git add internal/spider/spider.go internal/spider/spider_test.go
git commit -m "feat(spider): Crawl follows discovered links"
```

---

### Task 8: `spider.go` — `Crawl` skips already-cached URLs

**Files:**
- Modify: `internal/spider/spider_test.go`

Re-runs should not re-fetch pages already in `index.json`.

**Step 1: Write the failing test**

Append to `internal/spider/spider_test.go`:
```go
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
```

Add `"path/filepath"` to test file imports if not already present.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/spider/... -v -run TestCrawl_skipsCachedURLs
```
Expected: FAIL — fetch called once (or result missing the cached URL).

**Step 3: Fix**

The implementation already seeds `visited` from the index and only enqueues unvisited URLs, so this test may already pass. If `result` is missing the cached URL, ensure `pages := idx.All()` is returned at the end. Verify `idx.All()` returns the preloaded entry.

**Step 4: Run all spider tests**

```bash
go test ./internal/spider/... -v
```
Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/spider/spider_test.go
git commit -m "test(spider): verify Crawl skips already-cached URLs"
```

---

### Task 9: `cli/analyze.go` — wire `--cache-dir` and `--workers` into `Crawl`

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `internal/cli/analyze_test.go` (create if absent)

**Step 1: Write the failing test**

`internal/cli/analyze_test.go`:
```go
package cli

import (
	"bytes"
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/... -v -run TestAnalyze
```
Expected: `TestAnalyze_missingFlags_returnsError` may pass already; `TestAnalyze_helpFlag_exits0` should pass. If both pass already, the test is verifying existing behavior — add the flag-presence check below to make at least one test a new RED.

**Step 3: Implement flags on `analyze`**

Replace `internal/cli/analyze.go` with:
```go
package cli

import (
	"fmt"

	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	var (
		docsURL  string
		cacheDir string
		workers  int
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a codebase against its documentation site for gaps.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if docsURL == "" {
				return fmt.Errorf("--docs-url is required")
			}
			opts := spider.Options{
				CacheDir: cacheDir,
				Workers:  workers,
			}
			pages, err := spider.Crawl(docsURL, opts, spider.MdfetchFetcher(opts))
			if err != nil {
				return fmt.Errorf("crawl failed: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "fetched %d pages\n", len(pages))
			return nil
		},
	}

	cmd.Flags().StringVar(&docsURL, "docs-url", "", "URL of the documentation site to analyze (required)")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps/cache", "directory to cache fetched pages")
	cmd.Flags().IntVar(&workers, "workers", 5, "number of parallel mdfetch workers")

	return cmd
}
```

**Step 4: Run all tests**

```bash
go test ./... && go build ./...
```
Expected: all tests PASS, build succeeds.

**Step 5: Check coverage**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```
Expected: `internal/spider` packages at ≥90% statement coverage.

**Step 6: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "feat(cli): wire --docs-url, --cache-dir, --workers into analyze"
```

---

## Done

All tasks complete when:
- `go test ./...` is fully green
- `go build ./...` succeeds
- `internal/spider` coverage ≥ 90%
- `find-the-gaps analyze --docs-url https://example.com` triggers a real crawl (manual smoke test)
