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
