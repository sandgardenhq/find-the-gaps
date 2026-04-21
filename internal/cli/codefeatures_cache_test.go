package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func makeScan(paths ...string) *scanner.ProjectScan {
	files := make([]scanner.ScannedFile, len(paths))
	for i, p := range paths {
		files[i] = scanner.ScannedFile{Path: p}
	}
	return &scanner.ProjectScan{Files: files}
}

func TestCodeFeaturesCache_MissWhenFileAbsent(t *testing.T) {
	_, ok := loadCodeFeaturesCache(filepath.Join(t.TempDir(), "codefeatures.json"), makeScan("a.go"))
	if ok {
		t.Error("expected cache miss for absent file")
	}
}

func TestCodeFeaturesCache_MissWhenFilesChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	scan := makeScan("a.go", "b.go")
	if err := saveCodeFeaturesCache(path, scan, []string{"feature-a"}); err != nil {
		t.Fatal(err)
	}
	// Load with a different file list.
	_, ok := loadCodeFeaturesCache(path, makeScan("a.go", "b.go", "c.go"))
	if ok {
		t.Error("expected cache miss when file list changed")
	}
}

func TestCodeFeaturesCache_HitWhenFilesUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	scan := makeScan("a.go", "b.go")
	features := []string{"feature-a", "feature-b"}
	if err := saveCodeFeaturesCache(path, scan, features); err != nil {
		t.Fatal(err)
	}
	got, ok := loadCodeFeaturesCache(path, scan)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != len(features) {
		t.Errorf("got %d features, want %d", len(got), len(features))
	}
	for i, want := range features {
		if got[i] != want {
			t.Errorf("features[%d]: got %q, want %q", i, got[i], want)
		}
	}
}

func TestCodeFeaturesCache_FileOrderIndependent(t *testing.T) {
	// Cache built with [b.go, a.go] must be a hit when loaded with [a.go, b.go].
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	scanAB := makeScan("a.go", "b.go")
	scanBA := makeScan("b.go", "a.go")
	if err := saveCodeFeaturesCache(path, scanAB, []string{"feat"}); err != nil {
		t.Fatal(err)
	}
	_, ok := loadCodeFeaturesCache(path, scanBA)
	if !ok {
		t.Error("expected cache hit regardless of file order")
	}
}

func TestCodeFeaturesCache_NilFeatures_NormalizedToEmpty(t *testing.T) {
	// Write raw JSON with null features to simulate a degenerate cache file.
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	// Save normally first, then overwrite with a null features value.
	raw := `{"files":["a.go"],"features":null}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := loadCodeFeaturesCache(path, makeScan("a.go"))
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got == nil {
		t.Error("nil features must be normalized to empty slice")
	}
}

func TestCodeFeaturesCache_CorruptFile_ReturnsMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok := loadCodeFeaturesCache(path, makeScan("a.go"))
	if ok {
		t.Error("expected cache miss for corrupt file")
	}
}
