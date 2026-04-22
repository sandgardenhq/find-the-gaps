package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
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
	features := []analyzer.CodeFeature{
		{Name: "feature-a", Description: "Does A.", Layer: "cli", UserFacing: true},
	}
	if err := saveCodeFeaturesCache(path, scan, features); err != nil {
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
	features := []analyzer.CodeFeature{
		{Name: "feature one", Description: "Does X.", Layer: "cli", UserFacing: true},
		{Name: "feature two", Description: "Does Y.", Layer: "analysis engine", UserFacing: false},
	}
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
	// Assert all fields round-trip correctly.
	if got[0].Name != "feature one" {
		t.Errorf("got[0].Name = %q, want %q", got[0].Name, "feature one")
	}
	if got[0].Description != "Does X." {
		t.Errorf("got[0].Description = %q, want %q", got[0].Description, "Does X.")
	}
	if got[0].Layer != "cli" {
		t.Errorf("got[0].Layer = %q, want %q", got[0].Layer, "cli")
	}
	if !got[0].UserFacing {
		t.Error("got[0].UserFacing = false, want true")
	}
	if got[1].Name != "feature two" {
		t.Errorf("got[1].Name = %q, want %q", got[1].Name, "feature two")
	}
	if got[1].UserFacing {
		t.Error("got[1].UserFacing = true, want false")
	}
}

func TestCodeFeaturesCache_FileOrderIndependent(t *testing.T) {
	// Cache built with [b.go, a.go] must be a hit when loaded with [a.go, b.go].
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	scanAB := makeScan("a.go", "b.go")
	scanBA := makeScan("b.go", "a.go")
	features := []analyzer.CodeFeature{
		{Name: "feat", Description: "Does F.", Layer: "cli", UserFacing: true},
	}
	if err := saveCodeFeaturesCache(path, scanAB, features); err != nil {
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

func TestCodeFeaturesCache_SaveError_ReturnsError(t *testing.T) {
	// Writing to a path inside a non-existent directory must fail.
	path := filepath.Join(t.TempDir(), "nonexistent", "codefeatures.json")
	features := []analyzer.CodeFeature{
		{Name: "feat", Description: "Does F.", Layer: "cli", UserFacing: true},
	}
	err := saveCodeFeaturesCache(path, makeScan("a.go"), features)
	if err == nil {
		t.Error("expected error when parent directory does not exist")
	}
}

func TestCodeFeaturesCache_ReadError_ReturnsMiss(t *testing.T) {
	// Pointing at a directory (not a file) causes os.ReadFile to fail with a
	// non-ErrNotExist error, exercising the generic read-error branch.
	dir := t.TempDir()
	_, ok := loadCodeFeaturesCache(dir, makeScan("a.go"))
	if ok {
		t.Error("expected cache miss when path is a directory")
	}
}
