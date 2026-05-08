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
