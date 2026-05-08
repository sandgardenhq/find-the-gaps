package forge

import (
	"errors"
	"os/exec"
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

func TestResolve_forgeURL_noRepo_halts(t *testing.T) {
	_, err := Resolve("https://github.com/foo/bar", "", "")
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_forgeURL_unparseablePath_halts(t *testing.T) {
	// owner-only path on a forge host: ParseURL rejects, Resolve wraps as
	// ErrForgeNotIngestable.
	_, err := Resolve("https://github.com/foo", "", "")
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_originUnparseable_halts(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	// file:// remotes are not a recognized forge shape: NormalizeRemote errors.
	setRemote(t, repo, "file:///tmp/foo.git")

	_, err := Resolve("https://github.com/foo/bar", repo, "")
	if !errors.Is(err, ErrForgeNotIngestable) {
		t.Fatalf("got %v, want ErrForgeNotIngestable", err)
	}
}

func TestResolve_invalidDocsURL_returnsParseError(t *testing.T) {
	// A control character is the simplest way to make net/url reject a URL.
	_, err := Resolve("https://example.com/\x7f", "", "")
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

	res, err := Resolve("https://github.com:443/foo/bar", repo, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OnDisk {
		t.Fatal("expected OnDisk=true for github.com:443")
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

func TestResolve_forgeFlag_unknownValue_rejected(t *testing.T) {
	// --forge must accept only the documented allowlist
	// (github|gitlab|bitbucket|gitea|forgejo|gogs). An arbitrary string
	// should produce a clear error rather than silently bypassing host
	// detection.
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://git.example.com/foo/bar.git")
	writeFile(t, repo, "README.md", "x")

	_, err := Resolve("https://git.example.com/foo/bar", repo, "potato")
	if err == nil {
		t.Fatal("expected error for unknown --forge value")
	}
	if !strings.Contains(err.Error(), "potato") {
		t.Fatalf("error should name the bad value: %v", err)
	}
}

func TestResolve_forgeFlag_acceptsAllowlistedValues(t *testing.T) {
	for _, v := range []string{"github", "gitlab", "bitbucket", "gitea", "forgejo", "gogs"} {
		t.Run(v, func(t *testing.T) {
			repo := t.TempDir()
			gitInit(t, repo)
			setRemote(t, repo, "https://git.example.com/foo/bar.git")
			writeFile(t, repo, "README.md", "x")

			res, err := Resolve("https://git.example.com/foo/bar", repo, v)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", v, err)
			}
			if !res.OnDisk {
				t.Fatalf("Resolve(%q): expected OnDisk=true", v)
			}
		})
	}
}

func TestResolve_forgeFlag_caseInsensitive(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	setRemote(t, repo, "https://git.example.com/foo/bar.git")
	writeFile(t, repo, "README.md", "x")

	res, err := Resolve("https://git.example.com/foo/bar", repo, "GiTeA")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OnDisk {
		t.Fatal("expected OnDisk=true for case-mixed --forge value")
	}
}
