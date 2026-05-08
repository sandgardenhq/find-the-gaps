# Forge-Aware Docs Ingestion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Stop `ftg analyze` from crawling source-control forges; read markdown from `--repo` on disk when the docs URL points at the same repo, halt with a clear message otherwise.

**Architecture:** A new `internal/forge` package owns forge-host detection, URL parsing, remote normalization, and on-disk markdown walking. `internal/cli/analyze.go` adds a routing branch *before* the spider call: if the docs URL is a forge URL, either build the page map from disk (matching repo) or halt (anything else). The spider is untouched.

**Tech Stack:** Go 1.26+, stdlib `net/url` and `os/exec`, existing `spider.URLToFilename` helper for stable URL→file mapping. Tests use `testify/require` and `testscript` (see `cmd/ftg/testdata/script/*.txtar`).

**Companion design:** `.plans/FORGE_AWARE_DOCS_INGESTION_DESIGN.md`.

**TDD discipline:** Every production change is gated on a failing test first. See @CLAUDE.md "ABSOLUTE RULES — NO EXCEPTIONS". Commit after every RED→GREEN cycle.

---

## Task 1: Detect known forge hosts

**Files:**
- Create: `internal/forge/host.go`
- Create: `internal/forge/host_test.go`

**Step 1: Write the failing test**

```go
// internal/forge/host_test.go
package forge

import "testing"

func TestIsForgeHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"github.com", true},
		{"www.github.com", true},
		{"gitlab.com", true},
		{"bitbucket.org", true},
		{"codeberg.org", true},
		{"git.sr.ht", true},
		{"GitHub.com", true}, // case-insensitive
		{"example.com", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			if got := IsForgeHost(tc.host); got != tc.want {
				t.Fatalf("IsForgeHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/forge/...`
Expected: FAIL — package `forge` does not exist.

**Step 3: Write minimal implementation**

```go
// internal/forge/host.go
package forge

import "strings"

var forgeHosts = map[string]struct{}{
	"github.com":     {},
	"www.github.com": {},
	"gitlab.com":     {},
	"bitbucket.org":  {},
	"codeberg.org":   {},
	"git.sr.ht":      {},
}

// IsForgeHost reports whether host is a known source-control forge whose URLs
// must not be crawled. Comparison is case-insensitive.
func IsForgeHost(host string) bool {
	_, ok := forgeHosts[strings.ToLower(host)]
	return ok
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/forge/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/forge/host.go internal/forge/host_test.go
git commit -m "feat(forge): detect known source-control forge hosts

- RED: TestIsForgeHost across github/gitlab/bitbucket/codeberg/sr.ht
- GREEN: case-insensitive lookup table
- Status: 1 test passing, build successful"
```

---

## Task 2: Parse forge URLs into (host, owner, repo, ref, sub)

**Files:**
- Create: `internal/forge/url.go`
- Create: `internal/forge/url_test.go`

**Step 1: Write the failing test**

```go
// internal/forge/url_test.go
package forge

import "testing"

func TestParseURL(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantRef   string
		wantSub   string
		wantWiki  bool
	}{
		{
			name:      "repo root",
			raw:       "https://github.com/foo/bar",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
		},
		{
			name:      "tree with subpath",
			raw:       "https://github.com/foo/bar/tree/main/docs",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantRef:   "main",
			wantSub:   "docs",
		},
		{
			name:      "blob single file",
			raw:       "https://github.com/foo/bar/blob/main/README.md",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantRef:   "main",
			wantSub:   "README.md",
		},
		{
			name:      "wiki",
			raw:       "https://github.com/foo/bar/wiki",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantWiki:  true,
		},
		{
			name:      "trailing .git stripped",
			raw:       "https://github.com/foo/bar.git",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
		},
		{
			name:      "trailing slash on repo root",
			raw:       "https://github.com/foo/bar/",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantHost:  "github.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseURL(tc.raw)
			if err != nil {
				t.Fatalf("ParseURL(%q) returned error: %v", tc.raw, err)
			}
			if got.Host != tc.wantHost || got.Owner != tc.wantOwner || got.Repo != tc.wantRepo {
				t.Fatalf("got %+v want host=%s owner=%s repo=%s",
					got, tc.wantHost, tc.wantOwner, tc.wantRepo)
			}
			if got.Ref != tc.wantRef || got.Subpath != tc.wantSub || got.IsWiki != tc.wantWiki {
				t.Fatalf("got %+v want ref=%s sub=%s wiki=%v",
					got, tc.wantRef, tc.wantSub, tc.wantWiki)
			}
		})
	}
}

func TestParseURL_rejectsNonForgePaths(t *testing.T) {
	// Only owner, no repo
	if _, err := ParseURL("https://github.com/foo"); err == nil {
		t.Fatal("expected error for owner-only URL")
	}
	// Empty path
	if _, err := ParseURL("https://github.com/"); err == nil {
		t.Fatal("expected error for empty path")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/forge/...`
Expected: FAIL — `ParseURL` undefined.

**Step 3: Write minimal implementation**

```go
// internal/forge/url.go
package forge

import (
	"fmt"
	"net/url"
	"strings"
)

// URL represents a parsed forge URL.
type URL struct {
	Host    string // lowercased
	Owner   string
	Repo    string // .git suffix stripped
	Ref     string // branch or tag from /tree/<ref>/... or /blob/<ref>/...
	Subpath string // path under <ref>; empty for repo root
	IsWiki  bool   // true when the URL points at /<owner>/<repo>/wiki
}

// ParseURL parses a forge URL into its constituent parts. Returns an error if
// the URL does not name at least an owner and repo.
func ParseURL(raw string) (URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return URL{}, fmt.Errorf("parse url: %w", err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return URL{}, fmt.Errorf("forge url %q: missing <owner>/<repo>", raw)
	}
	out := URL{
		Host:  strings.ToLower(u.Host),
		Owner: parts[0],
		Repo:  strings.TrimSuffix(parts[1], ".git"),
	}
	if len(parts) >= 3 && parts[2] == "wiki" {
		out.IsWiki = true
		return out, nil
	}
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		out.Ref = parts[3]
		if len(parts) > 4 {
			out.Subpath = strings.Join(parts[4:], "/")
		}
	}
	return out, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/forge/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/forge/url.go internal/forge/url_test.go
git commit -m "feat(forge): parse forge URLs into host/owner/repo/ref/sub

- RED: TestParseURL covers root, tree, blob, wiki, .git suffix, trailing slash
- GREEN: split path, recognize tree|blob|wiki shapes
- Status: 2 tests passing, build successful"
```

---

## Task 3: Normalize a git remote URL

**Files:**
- Create: `internal/forge/remote.go`
- Create: `internal/forge/remote_test.go`

**Step 1: Write the failing test**

```go
// internal/forge/remote_test.go
package forge

import "testing"

func TestNormalizeRemote(t *testing.T) {
	cases := []struct {
		raw       string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"https://github.com/foo/bar.git", "github.com", "foo", "bar", false},
		{"https://github.com/foo/bar", "github.com", "foo", "bar", false},
		{"git@github.com:foo/bar.git", "github.com", "foo", "bar", false},
		{"git@gitlab.com:group/proj.git", "gitlab.com", "group", "proj", false},
		{"ssh://git@github.com/foo/bar.git", "github.com", "foo", "bar", false},
		{"https://GitHub.com/Foo/Bar.git", "github.com", "Foo", "Bar", false}, // host lowercased, owner/repo preserved
		{"file:///tmp/foo", "", "", "", true},
		{"", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := NormalizeRemote(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Host != tc.wantHost || got.Owner != tc.wantOwner || got.Repo != tc.wantRepo {
				t.Fatalf("got %+v want host=%s owner=%s repo=%s",
					got, tc.wantHost, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/forge/...`
Expected: FAIL — `NormalizeRemote` undefined.

**Step 3: Write minimal implementation**

```go
// internal/forge/remote.go
package forge

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Remote is a normalized git remote URL.
type Remote struct {
	Host  string // lowercased
	Owner string
	Repo  string
}

var sshRemoteRe = regexp.MustCompile(`^(?:[^@]+@)?([^:]+):([^/]+)/(.+?)(?:\.git)?$`)

// NormalizeRemote parses an HTTPS or SSH git remote URL into (host, owner, repo).
// Strips a trailing ".git" suffix. Returns an error if raw is not a recognized
// remote shape.
func NormalizeRemote(raw string) (Remote, error) {
	if raw == "" {
		return Remote{}, fmt.Errorf("empty remote")
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "ssh://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Remote{}, fmt.Errorf("parse remote: %w", err)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return Remote{}, fmt.Errorf("remote %q: missing owner/repo", raw)
		}
		return Remote{
			Host:  strings.ToLower(u.Host),
			Owner: parts[0],
			Repo:  strings.TrimSuffix(parts[1], ".git"),
		}, nil
	}
	// scp-style: git@host:owner/repo[.git]
	if m := sshRemoteRe.FindStringSubmatch(raw); m != nil {
		return Remote{
			Host:  strings.ToLower(m[1]),
			Owner: m[2],
			Repo:  strings.TrimSuffix(m[3], ".git"),
		}, nil
	}
	return Remote{}, fmt.Errorf("unrecognized remote shape: %q", raw)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/forge/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/forge/remote.go internal/forge/remote_test.go
git commit -m "feat(forge): normalize git remote URLs (https + scp ssh)

- RED: TestNormalizeRemote across https, ssh://, scp-style git@host:o/r
- GREEN: branch on prefix, regex for scp form, strip trailing .git
- Status: 3 tests passing, build successful"
```

---

## Task 4: Read `origin` from a local repo

**Files:**
- Create: `internal/forge/origin.go`
- Create: `internal/forge/origin_test.go`

**Step 1: Write the failing test**

```go
// internal/forge/origin_test.go
package forge

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--allow-empty", "-q", "-m", "init"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
}

func TestReadOrigin_present(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	cmd := exec.Command("git", "remote", "add", "origin", "https://github.com/foo/bar.git")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	got, err := ReadOrigin(dir)
	if err != nil {
		t.Fatalf("ReadOrigin: %v", err)
	}
	if got != "https://github.com/foo/bar.git" {
		t.Fatalf("got %q", got)
	}
}

func TestReadOrigin_missing(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	if _, err := ReadOrigin(dir); err == nil {
		t.Fatal("expected error when origin is unset")
	}
}

func TestReadOrigin_notARepo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	if _, err := ReadOrigin(dir); err == nil {
		t.Fatal("expected error for non-repo path")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/forge/...`
Expected: FAIL — `ReadOrigin` undefined.

**Step 3: Write minimal implementation**

```go
// internal/forge/origin.go
package forge

import (
	"fmt"
	"os/exec"
	"strings"
)

// ReadOrigin returns the URL of the "origin" remote in the git repo at dir.
// Returns an error when dir is not a git repo or origin is not configured.
func ReadOrigin(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin (%s): %w: %s",
			dir, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/forge/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/forge/origin.go internal/forge/origin_test.go
git commit -m "feat(forge): read origin remote URL from a local git repo

- RED: TestReadOrigin covers present/missing/not-a-repo cases
- GREEN: shell out to 'git remote get-url origin'
- Status: 6 tests passing, build successful"
```

---

## Task 5: Same-repo matcher

**Files:**
- Create: `internal/forge/match.go`
- Create: `internal/forge/match_test.go`

**Step 1: Write the failing test**

```go
// internal/forge/match_test.go
package forge

import "testing"

func TestSameRepo(t *testing.T) {
	cases := []struct {
		name   string
		docs   URL
		remote Remote
		want   bool
	}{
		{
			name:   "exact match",
			docs:   URL{Host: "github.com", Owner: "foo", Repo: "bar"},
			remote: Remote{Host: "github.com", Owner: "foo", Repo: "bar"},
			want:   true,
		},
		{
			name:   "case-insensitive owner/repo",
			docs:   URL{Host: "github.com", Owner: "Foo", Repo: "Bar"},
			remote: Remote{Host: "github.com", Owner: "foo", Repo: "bar"},
			want:   true,
		},
		{
			name:   "host mismatch",
			docs:   URL{Host: "github.com", Owner: "foo", Repo: "bar"},
			remote: Remote{Host: "gitlab.com", Owner: "foo", Repo: "bar"},
			want:   false,
		},
		{
			name:   "owner mismatch",
			docs:   URL{Host: "github.com", Owner: "foo", Repo: "bar"},
			remote: Remote{Host: "github.com", Owner: "baz", Repo: "bar"},
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameRepo(tc.docs, tc.remote); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/forge/...`
Expected: FAIL — `SameRepo` undefined.

**Step 3: Write minimal implementation**

```go
// internal/forge/match.go
package forge

import "strings"

// SameRepo reports whether docs and remote refer to the same forge repository.
// Host comparison is exact (already lowercased); owner/repo are case-insensitive.
func SameRepo(docs URL, remote Remote) bool {
	return docs.Host == remote.Host &&
		strings.EqualFold(docs.Owner, remote.Owner) &&
		strings.EqualFold(docs.Repo, remote.Repo)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/forge/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/forge/match.go internal/forge/match_test.go
git commit -m "feat(forge): match parsed docs URL against a normalized origin

- RED: TestSameRepo covers exact, case-insensitive, host/owner mismatch
- GREEN: triple-equality check, case-insensitive owner/repo
- Status: 7 tests passing, build successful"
```

---

## Task 6: Walk on-disk markdown into a page map

**Files:**
- Create: `internal/forge/walk.go`
- Create: `internal/forge/walk_test.go`

**Step 1: Write the failing test**

```go
// internal/forge/walk_test.go
package forge

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalk_findsDocFiles(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "README.md", "# top")
	writeFile(t, repo, "docs/intro.md", "# intro")
	writeFile(t, repo, "docs/guide.rst", "guide")
	writeFile(t, repo, "docs/api.adoc", "api")
	writeFile(t, repo, "docs/page.mdx", "x")
	writeFile(t, repo, "docs/notes.txt", "noise") // skipped
	writeFile(t, repo, "src/main.go", "noise")    // skipped

	got, err := Walk(repo, "", "main", "github.com", "foo", "bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d entries, want 5: %v", len(got), got)
	}
	want := "https://github.com/foo/bar/blob/main/README.md"
	if path, ok := got[want]; !ok {
		t.Fatalf("missing %q in %v", want, got)
	} else if _, err := os.Stat(path); err != nil {
		t.Fatalf("synthesized path not on disk: %v", err)
	}
}

func TestWalk_subpath_limitsTree(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "README.md", "x")
	writeFile(t, repo, "docs/a.md", "x")
	writeFile(t, repo, "docs/sub/b.md", "x")

	got, err := Walk(repo, "docs", "main", "github.com", "foo", "bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2: %v", len(got), got)
	}
	if _, ok := got["https://github.com/foo/bar/blob/main/docs/a.md"]; !ok {
		t.Fatal("missing docs/a.md")
	}
	if _, ok := got["https://github.com/foo/bar/blob/main/docs/sub/b.md"]; !ok {
		t.Fatal("missing docs/sub/b.md")
	}
}

func TestWalk_singleFile(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "README.md", "x")
	writeFile(t, repo, "docs/a.md", "x")

	got, err := Walk(repo, "README.md", "main", "github.com", "foo", "bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d, want 1: %v", len(got), got)
	}
	if _, ok := got["https://github.com/foo/bar/blob/main/README.md"]; !ok {
		t.Fatal("missing README.md")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/forge/...`
Expected: FAIL — `Walk` undefined.

**Step 3: Write minimal implementation**

```go
// internal/forge/walk.go
package forge

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var docExtensions = map[string]struct{}{
	".md":       {},
	".markdown": {},
	".mdx":      {},
	".rst":      {},
	".adoc":     {},
	".asciidoc": {},
}

// Walk returns a map from synthesized forge URL → absolute file path for every
// documentation file under repo/sub. When sub names a single file it returns a
// one-entry map. ref is the branch name baked into synthesized URLs.
func Walk(repo, sub, ref, host, owner, name string) (map[string]string, error) {
	if ref == "" {
		ref = "main"
	}
	root := filepath.Join(repo, sub)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}

	out := make(map[string]string)
	add := func(rel string) {
		ext := strings.ToLower(filepath.Ext(rel))
		if _, ok := docExtensions[ext]; !ok {
			return
		}
		url := fmt.Sprintf("https://%s/%s/%s/blob/%s/%s",
			host, owner, name, ref, filepath.ToSlash(rel))
		out[url] = filepath.Join(repo, rel)
	}

	if !info.IsDir() {
		// Single-file URL.
		add(sub)
		return out, nil
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip common build/dependency dirs even without .gitignore plumbing.
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		add(rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/forge/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/forge/walk.go internal/forge/walk_test.go
git commit -m "feat(forge): walk on-disk repo for documentation files

- RED: TestWalk_findsDocFiles + subpath + single file
- GREEN: filepath.WalkDir filtered by md/mdx/markdown/rst/adoc/asciidoc;
  skips .git, node_modules, vendor; synthesizes blob/<ref>/<rel> URLs
- Status: 10 tests passing, build successful"
```

---

## Task 7: One-call orchestrator (`Resolve`)

This composes the previous tasks into the single function `internal/cli/analyze.go` will call.

**Files:**
- Create: `internal/forge/resolve.go`
- Create: `internal/forge/resolve_test.go`

**Step 1: Write the failing test**

```go
// internal/forge/resolve_test.go
package forge

import (
	"errors"
	"os/exec"
	"testing"
)

func setRemote(t *testing.T, dir, url string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "remote", "add", "origin", url)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
}

func TestResolve_match_returnsPages(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://github.com/foo/bar.git")
	writeFile(t, repo, "README.md", "x")
	writeFile(t, repo, "docs/a.md", "x")

	res, err := Resolve("https://github.com/foo/bar", repo, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OnDisk {
		t.Fatal("expected OnDisk=true")
	}
	if len(res.Pages) != 2 {
		t.Fatalf("got %d pages, want 2", len(res.Pages))
	}
}

func TestResolve_nonForge_passthrough(t *testing.T) {
	res, err := Resolve("https://example.com/docs", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.OnDisk {
		t.Fatal("non-forge URL should not engage on-disk mode")
	}
}

func TestResolve_forgeMismatch_halts(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://github.com/other/repo.git")

	_, err := Resolve("https://github.com/foo/bar", repo, "")
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_forgeWiki_halts(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://github.com/foo/bar.git")

	_, err := Resolve("https://github.com/foo/bar/wiki", repo, "")
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_forgeFlag_bypassesHostCheck(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://git.example.com/foo/bar.git")
	writeFile(t, repo, "README.md", "x")

	res, err := Resolve("https://git.example.com/foo/bar", repo, "gitea")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OnDisk || len(res.Pages) != 1 {
		t.Fatalf("got %+v", res)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/forge/...`
Expected: FAIL — `Resolve`, `Result`, `ErrForgeNotIngestable` undefined.

**Step 3: Write minimal implementation**

```go
// internal/forge/resolve.go
package forge

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrForgeNotIngestable is returned when the docs URL points at a forge but the
// on-disk shortcut cannot be used. Callers should print a message and exit
// non-zero.
var ErrForgeNotIngestable = errors.New("forge URL is not ingestable on disk")

// Result is the outcome of forge resolution.
type Result struct {
	// OnDisk is true when the caller should skip the spider crawl and use Pages
	// directly. False means the docs URL was not a forge URL — the caller should
	// continue with its normal crawl path.
	OnDisk bool
	// Pages is the synthesized url→filepath map populated when OnDisk is true.
	Pages map[string]string
	// Notice is a human-readable line the caller should print when OnDisk is
	// true, e.g. "docs-url is a forge URL; reading markdown from --repo on disk."
	Notice string
}

// Resolve decides how to ingest docsURL.
//
//   - When docsURL is not a forge URL (and forgeFlag is empty), Result.OnDisk is
//     false and the caller should crawl normally.
//   - When docsURL is a forge URL and --repo is a clone of the same repository,
//     Result.OnDisk is true with Pages populated.
//   - In every other forge case (no --repo, mismatched origin, wiki path, no
//     git, etc.), returns ErrForgeNotIngestable.
//
// forgeFlag is the value of --forge (empty when unset). When non-empty, host
// detection is bypassed and the URL's path is parsed as a forge URL.
func Resolve(docsURL, repoPath, forgeFlag string) (Result, error) {
	parsed, perr := url.Parse(docsURL)
	if perr != nil {
		return Result{}, fmt.Errorf("parse docs-url: %w", perr)
	}
	host := strings.ToLower(parsed.Host)
	if forgeFlag == "" && !IsForgeHost(host) {
		return Result{OnDisk: false}, nil
	}

	purl, err := ParseURL(docsURL)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrForgeNotIngestable, err)
	}
	if purl.IsWiki {
		return Result{}, fmt.Errorf("%w: wiki URL %s", ErrForgeNotIngestable, docsURL)
	}
	if repoPath == "" {
		return Result{}, fmt.Errorf("%w: --repo not provided", ErrForgeNotIngestable)
	}
	originURL, err := ReadOrigin(repoPath)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrForgeNotIngestable, err)
	}
	remote, err := NormalizeRemote(originURL)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrForgeNotIngestable, err)
	}
	if !SameRepo(purl, remote) {
		return Result{}, fmt.Errorf("%w: --repo origin is %s/%s/%s, docs-url targets %s/%s/%s",
			ErrForgeNotIngestable,
			remote.Host, remote.Owner, remote.Repo,
			purl.Host, purl.Owner, purl.Repo)
	}

	pages, err := Walk(repoPath, purl.Subpath, purl.Ref, purl.Host, purl.Owner, purl.Repo)
	if err != nil {
		return Result{}, fmt.Errorf("walk on-disk docs: %w", err)
	}
	return Result{
		OnDisk: true,
		Pages:  pages,
		Notice: fmt.Sprintf("docs-url is a forge URL; reading markdown from %s on disk.", repoPath),
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/forge/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/forge/resolve.go internal/forge/resolve_test.go
git commit -m "feat(forge): resolve docs URL → on-disk page map or halt sentinel

- RED: TestResolve covers match, non-forge, mismatch, wiki, --forge bypass
- GREEN: compose ParseURL+ReadOrigin+NormalizeRemote+SameRepo+Walk
- Status: 14 tests passing, build successful"
```

---

## Task 8: Wire `Resolve` into `analyze`

**Files:**
- Modify: `internal/cli/analyze.go:93-188`
- Modify: `internal/cli/analyze.go:655-674` (flag block)
- Test: `internal/cli/analyze_forge_test.go` (new)

**Step 1: Write the failing test**

The test exercises the analyze command end-to-end up to the page map, asserting that a forge URL with a matching repo skips the spider and emits the notice. Mirror the structure of `internal/cli/analyze_test.go` and `internal/cli/analyze_classifier_test.go`.

```go
// internal/cli/analyze_forge_test.go
package cli

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyze_forgeURL_matchingRepo_skipsCrawl(t *testing.T) {
	repo := t.TempDir()
	for _, c := range [][]string{
		{"git", "init", "-q"},
		{"git", "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--allow-empty", "-q", "-m", "init"},
		{"git", "remote", "add", "origin", "https://github.com/foo/bar.git"},
	} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	if err := writeTestRepoMarkdown(repo); err != nil {
		t.Fatal(err)
	}

	cmd := newAnalyzeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--repo", repo,
		"--docs-url", "https://github.com/foo/bar",
		"--cache-dir", filepath.Join(t.TempDir(), "cache"),
		"--no-site",
	})

	// Run with the test tier stub (mirror existing analyze_test.go pattern).
	withStubTiering(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute: %v\n%s", err, out.String())
		}
	})
	if !strings.Contains(out.String(), "reading markdown from") {
		t.Fatalf("missing on-disk notice in output:\n%s", out.String())
	}
	if strings.Contains(out.String(), "crawling https://github.com/foo/bar") {
		t.Fatalf("spider was invoked despite forge URL:\n%s", out.String())
	}
}

func TestAnalyze_forgeURL_noRepoMatch_halts(t *testing.T) {
	repo := t.TempDir()
	for _, c := range [][]string{
		{"git", "init", "-q"},
		{"git", "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--allow-empty", "-q", "-m", "init"},
		{"git", "remote", "add", "origin", "https://github.com/other/proj.git"},
	} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	cmd := newAnalyzeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--repo", repo,
		"--docs-url", "https://github.com/foo/bar",
		"--cache-dir", filepath.Join(t.TempDir(), "cache"),
		"--no-site",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected non-nil error from forge mismatch")
	}
	if !strings.Contains(err.Error(), "Find the Gaps can't crawl source-control forges") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
```

The test depends on existing helpers (`writeTestRepoMarkdown`, `withStubTiering`) — copy or extend the conventions in `internal/cli/analyze_test.go` if those helpers don't exist yet. Goal: avoid touching real LLMs/spider in unit tests.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestAnalyze_forgeURL`
Expected: FAIL — `analyze` still calls `spider.Crawl` for any URL; the on-disk notice is never printed.

**Step 3: Write minimal implementation**

In `internal/cli/analyze.go`, add `--forge` flag (Step 3a) and replace the spider invocation block at lines 179–189 with a routing branch (Step 3b).

3a — flag wiring (around `cmd.Flags()` block at lines 655–674):

```go
var forgeFlag string
// ... existing flags ...
cmd.Flags().StringVar(&forgeFlag, "forge", "",
	"override forge detection: github|gitlab|bitbucket|gitea|forgejo|gogs (use for self-hosted forges)")
```

3b — routing branch. Replace lines 179–189:

```go
var pages map[string]string
docsDir := filepath.Join(projectDir, "docs")

resolved, err := forge.Resolve(docsURL, repoPath, forgeFlag)
if err != nil {
	if errors.Is(err, forge.ErrForgeNotIngestable) {
		return fmt.Errorf(
			"Find the Gaps can't crawl source-control forges "+
				"(github.com, gitlab.com, bitbucket.org, codeberg.org, git.sr.ht). "+
				"To analyze these docs, clone the repo locally and pass --repo /path/to/it. "+
				"(%v)", err)
	}
	return err
}
if resolved.OnDisk {
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), resolved.Notice)
	pages = resolved.Pages
} else {
	log.Infof("crawling %s", docsURL)
	spiderOpts := spider.Options{
		CacheDir: docsDir,
		Workers:  workers,
	}
	pages, err = spider.Crawl(docsURL, spiderOpts, spider.MdfetchFetcher(spiderOpts))
	if err != nil {
		return fmt.Errorf("crawl failed: %w", err)
	}
	log.Debug("crawl complete", "pages", len(pages))
}
```

Add `"github.com/sandgardenhq/find-the-gaps/internal/forge"` to the import block.

Subtle: `spider.LoadIndex(docsDir)` runs immediately after this block. In on-disk mode the docsDir won't have an index. Two options — pick the one that fits cleanest:

a. Always create `docsDir` before `LoadIndex` (already happens via `os.MkdirAll` inside `LoadIndex`). The empty index starts with no analyses, so all on-disk pages are treated as cache misses and analyzed. That matches user intent.

b. Skip the page-analysis cache entirely in on-disk mode. Adds a branch but avoids index churn for repeat runs.

Default to (a): no extra branching. Re-runs benefit from cached page analyses keyed by the synthesized URL.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run TestAnalyze_forgeURL`
Expected: PASS — both forge tests green; existing `analyze_*_test.go` still green.

Then full suite:
Run: `go test ./...`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_forge_test.go
git commit -m "feat(cli): route forge docs URLs through on-disk ingestion or halt

- RED: TestAnalyze_forgeURL_matchingRepo_skipsCrawl + noRepoMatch_halts
- GREEN: forge.Resolve before spider.Crawl; print notice or return
  ErrForgeNotIngestable-wrapping error
- Status: full suite passing, build successful"
```

---

## Task 9: testscript end-to-end coverage

**Files:**
- Create: `cmd/ftg/testdata/script/analyze_forge_match.txtar`
- Create: `cmd/ftg/testdata/script/analyze_forge_mismatch.txtar`
- Create: `cmd/ftg/testdata/script/analyze_forge_wiki.txtar`

**Step 1: Write the failing scripts**

`analyze_forge_match.txtar`:

```
# A forge URL whose --repo matches its origin reads docs from disk and prints
# the on-disk notice. The spider must NOT log "crawling".

env FIND_THE_GAPS_QUIET=1
env FIND_THE_GAPS_LLM_SMALL=stub/echo
env FIND_THE_GAPS_LLM_TYPICAL=stub/echo
env FIND_THE_GAPS_LLM_LARGE=stub/echo

cd repo
exec git init -q
exec git -c user.email=a@b -c user.name=a commit --allow-empty -q -m init
exec git remote add origin https://github.com/foo/bar.git
cd ..

ftg analyze --repo repo --docs-url https://github.com/foo/bar --cache-dir cache --no-site
stdout 'reading markdown from'
! stdout 'crawling https://github.com/foo/bar'

-- repo/README.md --
# Hello

-- repo/docs/intro.md --
# Intro
```

`analyze_forge_mismatch.txtar`:

```
# A forge URL with a different origin halts non-zero with the standard message.

cd repo
exec git init -q
exec git -c user.email=a@b -c user.name=a commit --allow-empty -q -m init
exec git remote add origin https://github.com/other/proj.git
cd ..

! ftg analyze --repo repo --docs-url https://github.com/foo/bar --cache-dir cache --no-site
stderr 'can''t crawl source-control forges'
stderr 'clone the repo locally'
```

`analyze_forge_wiki.txtar`:

```
# Wiki URLs always halt, even when --repo matches the parent repo.

cd repo
exec git init -q
exec git -c user.email=a@b -c user.name=a commit --allow-empty -q -m init
exec git remote add origin https://github.com/foo/bar.git
cd ..

! ftg analyze --repo repo --docs-url https://github.com/foo/bar/wiki --cache-dir cache --no-site
stderr 'can''t crawl source-control forges'
```

**Step 2: Run scripts to verify they fail (or pass on the right axis)**

Run: `go test ./cmd/ftg/... -run TestScript`
Expected: the new scripts fail because the existing testscript harness probably doesn't yet stub LLM tiers via env. If the scripts fail for environmental reasons, look at existing `analyze_stub.txtar` for the canonical setup pattern and copy it.

**Step 3: Wire fixtures**

Use the same fixture pattern as `cmd/ftg/testdata/script/analyze_stub.txtar`. The `stub/echo` tier values come from the test main wiring; adjust as needed so the analyze pipeline does not call out to a real LLM.

**Step 4: Run scripts to verify they pass**

Run: `go test ./cmd/ftg/... -run TestScript`
Expected: PASS — new forge scripts green; existing scripts unaffected.

**Step 5: Commit**

```bash
git add cmd/ftg/testdata/script/analyze_forge_*.txtar
git commit -m "test(ftg): testscript scenarios for forge-aware ingestion

- analyze_forge_match: forge URL + matching origin → on-disk notice
- analyze_forge_mismatch: forge URL + different origin → halt + message
- analyze_forge_wiki: wiki URL → halt
- Status: testscript suite passing"
```

---

## Task 10: Documentation

**Files:**
- Modify: `README.md`
- Modify: `CHANGELOG.md`

**Step 1: README — add a section under Usage**

Add a short section explaining that forge URLs are not crawled; pass `--repo` pointing at a clone of the same repo, and the tool reads markdown from disk. Mention `--forge` for self-hosted forges. One example each:

```markdown
### Documentation hosted on a forge (GitHub, GitLab, etc.)

Find the Gaps does not crawl source-control forges — the link graph there is
the entire forge. When `--docs-url` points at github.com / gitlab.com /
bitbucket.org / codeberg.org / git.sr.ht, the tool reads markdown from `--repo`
on disk:

    ftg analyze --repo . --docs-url https://github.com/sandgardenhq/find-the-gaps

For a self-hosted forge (Gitea, Forgejo, GitLab CE on your own domain), pass
`--forge` to bypass host detection:

    ftg analyze --repo . --docs-url https://git.example.com/foo/bar --forge gitea
```

**Step 2: CHANGELOG — add an Unreleased entry**

```markdown
## Unreleased

### Changed
- `analyze` no longer crawls source-control forges. When `--docs-url` is on a
  known forge (github.com, gitlab.com, bitbucket.org, codeberg.org, git.sr.ht)
  and `--repo` is a clone of the same repository, markdown is read from disk
  instead. In every other forge case the run halts with a clear message.
- New `--forge` flag for self-hosted forges that share the GitHub URL shape.
```

**Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: forge-aware docs ingestion (README + CHANGELOG)

- README: explain on-disk mode for forge URLs, --forge for self-hosted
- CHANGELOG: add Unreleased entry covering the behavior change
- Status: docs-only commit"
```

---

## Task 11: Update PROGRESS.md

Per @CLAUDE.md "Progress Documentation": append a Task entry for this feature with timestamp, tests, coverage, build status. One commit at the end of the feature is fine; per-task entries during execution are even better.

```bash
git add PROGRESS.md
git commit -m "docs(progress): forge-aware docs ingestion complete"
```

---

## Verification gate

Before declaring the feature done, run:

```
go test ./...
go test -coverprofile=coverage.out ./internal/forge/... && go tool cover -func=coverage.out
go build ./...
golangci-lint run
```

All four must pass. Statement coverage on `internal/forge/` must be ≥90% (per @CLAUDE.md).

Manual sanity check (real binary, real LLMs, picks up `.plans/VERIFICATION_PLAN.md` discipline):

```
go build -o ftg ./cmd/ftg
./ftg analyze --repo . --docs-url https://github.com/sandgardenhq/find-the-gaps --no-site
# Expect: "reading markdown from" notice, no "crawling github.com" line.

./ftg analyze --repo . --docs-url https://github.com/torvalds/linux --no-site
# Expect: halt with "can't crawl source-control forges" message, exit 1.
```

---

## Out of scope (tracked for follow-ups)

- Wiki ingestion. Halt message is the v1 answer.
- SourceHut on-disk path support (`/~user/repo/tree/...` URL shape differs).
- GitHub/GitLab API fallback for users who cannot clone (rate limits, auth).
- Adding `.gitignore`-aware walking via `git ls-files` — current `WalkDir` skips `.git`/`node_modules`/`vendor` which covers the practical noise without invoking git.
- A new `analyze --docs-path /local/path` flag for fully off-line / offline-wiki workflows.
