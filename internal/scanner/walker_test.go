package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWalk_findsFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "")
	writeFile(t, dir, "util.go", "")

	var found []string
	stats, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(found)
	if got := []string{"main.go", "util.go"}; !equal(found, got) {
		t.Fatalf("found %v, want %v", found, got)
	}
	if stats.Scanned != 2 {
		t.Errorf("Scanned = %d, want 2", stats.Scanned)
	}
}

func TestWalk_skipsDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "")
	writeFile(t, dir, "package-lock.json", "")
	writeFile(t, dir, "logo.png", "")
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "node_modules/lib.js", "")

	var found []string
	stats, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if f != "main.go" {
			t.Errorf("only main.go should survive, found %q", f)
		}
	}
	if stats.Skipped["defaults"] == 0 {
		t.Errorf("expected non-zero defaults skips, got %v", stats.Skipped)
	}
}

func TestWalk_respectsGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "secret.txt\n")
	writeFile(t, dir, "main.go", "")
	writeFile(t, dir, "secret.txt", "")

	var found []string
	stats, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if f == "secret.txt" {
			t.Errorf(".gitignore not respected: %q", f)
		}
	}
	if stats.Skipped[".gitignore"] != 1 {
		t.Errorf("Skipped[.gitignore] = %d, want 1", stats.Skipped[".gitignore"])
	}
}

func TestWalk_ftgignoreNegatesDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "vendor/x.go", "")
	writeFile(t, dir, ".ftgignore", "!vendor/\n")
	writeFile(t, dir, "main.go", "")

	var found []string
	if _, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	hasVendor := false
	for _, f := range found {
		if strings.HasPrefix(f, "vendor/") {
			hasVendor = true
		}
	}
	if !hasVendor {
		t.Errorf("vendor/x.go should be re-included; got %v", found)
	}
}

func TestWalk_skipsGitDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".git/config", "")
	writeFile(t, dir, "main.go", "")

	var found []string
	if _, err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if strings.HasPrefix(f, ".git") {
			t.Errorf(".git should be skipped, found %q", f)
		}
	}
}

func TestWalk_callbackError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "")
	if _, err := Walk(dir, func(_ string, _ os.FileInfo) error {
		return os.ErrInvalid
	}); err == nil {
		t.Error("expected callback error to propagate")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeFile is a test helper that creates a file with the given content.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
