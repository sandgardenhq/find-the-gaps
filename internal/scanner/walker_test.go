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
	err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(found)
	want := []string{"main.go", "util.go"}
	if len(found) != len(want) {
		t.Fatalf("found %v, want %v", found, want)
	}
	for i := range want {
		if found[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, found[i], want[i])
		}
	}
}

func TestWalk_respectsGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "*.log\nbuild/\n")
	writeFile(t, dir, "main.go", "")
	writeFile(t, dir, "app.log", "")
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "build/output.txt", "")

	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if f == "app.log" || strings.HasPrefix(f, "build/") || f == "build" {
			t.Errorf("gitignore pattern not respected: %q found", f)
		}
	}
	var hasMain bool
	for _, f := range found {
		if f == "main.go" {
			hasMain = true
		}
	}
	if !hasMain {
		t.Errorf("main.go should be found but was not; got: %v", found)
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
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if f == ".git/config" {
			t.Errorf(".git directory should be skipped, found %q", f)
		}
	}
}

func TestWalk_skipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".hidden/secret.go", "")
	writeFile(t, dir, "visible.go", "")

	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if filepath.Dir(f) == ".hidden" {
			t.Errorf("hidden dir should be skipped, found %q", f)
		}
	}
}

func TestWalk_emptyDir(t *testing.T) {
	dir := t.TempDir()
	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("expected 0 files, got %d: %v", len(found), found)
	}
}

func TestWalk_nestedDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal/pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "internal/pkg/foo.go", "")
	writeFile(t, dir, "main.go", "")

	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(found)
	want := []string{"internal/pkg/foo.go", "main.go"}
	if len(found) != len(want) {
		t.Fatalf("found %v, want %v", found, want)
	}
}

func TestWalk_callbackError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "")
	sentinel := os.ErrInvalid
	err := Walk(dir, func(_ string, _ os.FileInfo) error {
		return sentinel
	})
	if err == nil {
		t.Error("expected error from callback to propagate, got nil")
	}
}

func TestWalk_unreadableGitignore_doesNotError(t *testing.T) {
	dir := t.TempDir()
	// Create a directory named .gitignore — CompileIgnoreFile will fail to read it.
	if err := os.MkdirAll(filepath.Join(dir, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "main.go", "")
	// Walk should still succeed — loadGitignore gracefully returns nil on error.
	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk with unreadable .gitignore: %v", err)
	}
	if len(found) != 1 || found[0] != "main.go" {
		t.Errorf("expected [main.go], got %v", found)
	}
}

func TestWalk_noGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "")
	// No .gitignore file — should still walk successfully.
	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk with no .gitignore: %v", err)
	}
	if len(found) != 1 || found[0] != "main.go" {
		t.Errorf("expected [main.go], got %v", found)
	}
}

func TestWalk_gitignoreMatchedDir_skipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "vendor/\n")
	if err := os.MkdirAll(filepath.Join(dir, "vendor/pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "vendor/pkg/lib.go", "")
	writeFile(t, dir, "main.go", "")

	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if strings.HasPrefix(f, "vendor/") {
			t.Errorf("vendor dir should be gitignored, found %q", f)
		}
	}
}

func TestWalk_skipsKnownDependencyDirs(t *testing.T) {
	cases := []struct {
		name    string
		dirName string
	}{
		{"go vendor", "vendor"},
		{"node_modules", "node_modules"},
		{"python pycache", "__pycache__"},
		{"rust target", "target"},
		{"python venv", "venv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, tc.dirName), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, dir, filepath.Join(tc.dirName, "source.go"), "")
			writeFile(t, dir, "main.go", "")

			var found []string
			if err := Walk(dir, func(path string, _ os.FileInfo) error {
				found = append(found, path)
				return nil
			}); err != nil {
				t.Fatalf("Walk: %v", err)
			}
			for _, f := range found {
				if strings.HasPrefix(f, tc.dirName+string(filepath.Separator)) || f == tc.dirName {
					t.Errorf("dependency dir %q should be skipped, found %q", tc.dirName, f)
				}
			}
			var hasMain bool
			for _, f := range found {
				if f == "main.go" {
					hasMain = true
				}
			}
			if !hasMain {
				t.Errorf("main.go should be found but was not; got: %v", found)
			}
		})
	}
}

func TestWalk_skipsNestedDependencyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "pkg/node_modules/lib.js", "")
	writeFile(t, dir, "pkg/index.ts", "")

	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if strings.Contains(f, "node_modules") {
			t.Errorf("nested node_modules should be skipped, found %q", f)
		}
	}
	var hasIndex bool
	for _, f := range found {
		if f == filepath.Join("pkg", "index.ts") {
			hasIndex = true
		}
	}
	if !hasIndex {
		t.Errorf("pkg/index.ts should be found but was not; got: %v", found)
	}
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
