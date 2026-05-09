package forge

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setRemote(t *testing.T, dir, url string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "remote", "add", "origin", url)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
}

func TestResolve_emptyDocs_scansRepoOnDisk(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "README.md", "x")
	writeFile(t, repo, "docs/intro.md", "x")
	writeFile(t, repo, "src/main.go", "x")

	res, err := Resolve("", repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OnDisk {
		t.Fatal("expected OnDisk=true when --docs is empty")
	}
	if len(res.Pages) != 2 {
		t.Fatalf("got %d pages, want 2: %v", len(res.Pages), res.Pages)
	}
	for url := range res.Pages {
		if !strings.HasPrefix(url, "file://") {
			t.Fatalf("expected file:// URL, got %q", url)
		}
	}
}

func TestResolve_localPath_scansThatPath(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "src/main.go", "x") // would match if Resolve scanned repo
	docsDir := t.TempDir()
	writeFile(t, docsDir, "intro.md", "x")
	writeFile(t, docsDir, "guide.md", "x")

	res, err := Resolve(docsDir, repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OnDisk {
		t.Fatal("expected OnDisk=true for local --docs path")
	}
	if len(res.Pages) != 2 {
		t.Fatalf("got %d pages, want 2: %v", len(res.Pages), res.Pages)
	}
	for url := range res.Pages {
		if !strings.HasPrefix(url, "file://") {
			t.Fatalf("expected file:// URL, got %q", url)
		}
	}
}

func TestResolve_localPath_relative(t *testing.T) {
	// A bare relative path like "docs" must be treated as a path, not a URL.
	repo := t.TempDir()
	writeFile(t, repo, "docs/intro.md", "x")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	res, err := Resolve("docs", repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OnDisk {
		t.Fatal("expected OnDisk=true for relative local --docs path")
	}
	if len(res.Pages) != 1 {
		t.Fatalf("got %d pages, want 1: %v", len(res.Pages), res.Pages)
	}
}

func TestResolve_localPath_missing_clearError(t *testing.T) {
	repo := t.TempDir()
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")

	_, err := Resolve(missing, repo)
	if err == nil {
		t.Fatal("expected error for missing --docs path")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error should name the missing path %q: %v", missing, err)
	}
}

func TestResolve_match_returnsPages(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://github.com/foo/bar.git")
	writeFile(t, repo, "README.md", "x")
	writeFile(t, repo, "docs/a.md", "x")

	res, err := Resolve("https://github.com/foo/bar", repo)
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
	res, err := Resolve("https://example.com/docs", "")
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

	_, err := Resolve("https://github.com/foo/bar", repo)
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_forgeWiki_halts(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://github.com/foo/bar.git")

	_, err := Resolve("https://github.com/foo/bar/wiki", repo)
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_forgeURL_noRepo_halts(t *testing.T) {
	_, err := Resolve("https://github.com/foo/bar", "")
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_forgeURL_unparseablePath_halts(t *testing.T) {
	// owner-only path on a forge host: ParseURL rejects, Resolve wraps as
	// ErrForgeNotIngestable.
	_, err := Resolve("https://github.com/foo", "")
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_originUnparseable_halts(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	// file:// remotes are not a recognized forge shape: NormalizeRemote errors.
	setRemote(t, repo, "file:///tmp/foo.git")

	_, err := Resolve("https://github.com/foo/bar", repo)
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_invalidDocsURL_returnsParseError(t *testing.T) {
	// A control character is the simplest way to make net/url reject a URL.
	_, err := Resolve("https://example.com/\x7f", "")
	if err == nil {
		t.Fatal("expected parse error for malformed docs URL")
	}
	if errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got ErrForgeNotIngestable, want plain parse error: %v", err)
	}
}

func TestResolve_forgeHostWithPort_engagesOnDisk(t *testing.T) {
	// Even when the docs URL carries an explicit port, IsForgeHost must still
	// recognize the bare hostname so on-disk mode engages without needing --forge.
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://github.com/foo/bar.git")
	writeFile(t, repo, "README.md", "x")

	res, err := Resolve("https://github.com:443/foo/bar", repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OnDisk {
		t.Fatal("expected OnDisk=true for github.com:443")
	}
}

func TestResolve_selfHostedForge_passthrough(t *testing.T) {
	// Self-hosted forges (Gitea/Forgejo/Gogs on custom hosts) are no longer
	// recognized; the URL is treated as a normal docs site to crawl. Users
	// who want on-disk mode for a self-hosted clone should pass --docs as a
	// local path instead.
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://git.example.com/foo/bar.git")
	writeFile(t, repo, "README.md", "x")

	res, err := Resolve("https://git.example.com/foo/bar", repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.OnDisk {
		t.Fatal("self-hosted forge URL should not engage on-disk mode without --forge")
	}
}
