package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAnalyzedProjects_emptyDir_returnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := ListAnalyzedProjects(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d projects, want 0", len(got))
	}
}

func TestListAnalyzedProjects_missingDir_returnsEmpty(t *testing.T) {
	got, err := ListAnalyzedProjects(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should be a soft empty result, got error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d projects, want 0", len(got))
	}
}

func TestListAnalyzedProjects_onlyDirsWithSiteCount(t *testing.T) {
	cache := t.TempDir()
	// project A — has site/
	if err := os.MkdirAll(filepath.Join(cache, "alpha", "site"), 0o755); err != nil {
		t.Fatal(err)
	}
	// project B — has scan/ but no site/, must be ignored
	if err := os.MkdirAll(filepath.Join(cache, "beta", "scan"), 0o755); err != nil {
		t.Fatal(err)
	}
	// project C — has site/
	if err := os.MkdirAll(filepath.Join(cache, "gamma", "site"), 0o755); err != nil {
		t.Fatal(err)
	}
	// loose file
	if err := os.WriteFile(filepath.Join(cache, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListAnalyzedProjects(cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d projects, want 2 (alpha, gamma)", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "gamma" {
		t.Errorf("got %+v, want [alpha gamma] in order", got)
	}
	if got[0].SiteDir != filepath.Join(cache, "alpha", "site") {
		t.Errorf("alpha SiteDir = %q", got[0].SiteDir)
	}
}
