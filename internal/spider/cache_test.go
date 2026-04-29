package spider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestURLToFilename_isStable(t *testing.T) {
	a := URLToFilename("https://docs.example.com/intro")
	b := URLToFilename("https://docs.example.com/intro")
	if a != b {
		t.Errorf("URLToFilename is not stable: %q != %q", a, b)
	}
}

func TestURLToFilename_differsAcrossURLs(t *testing.T) {
	a := URLToFilename("https://docs.example.com/intro")
	b := URLToFilename("https://docs.example.com/reference")
	if a == b {
		t.Error("URLToFilename returned same name for different URLs")
	}
}

func TestURLToFilename_hasMDExtension(t *testing.T) {
	name := URLToFilename("https://docs.example.com/intro")
	if !strings.HasSuffix(name, ".md") {
		t.Errorf("expected .md suffix, got %q", name)
	}
}

func TestLoadIndex_missingDir_returnsEmptyIndex(t *testing.T) {
	idx, err := LoadIndex(t.TempDir() + "/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
}

func TestLoadIndex_emptyDir_returnsEmptyIndex(t *testing.T) {
	idx, err := LoadIndex(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
}

func TestLoadIndex_existingIndex_loadsEntries(t *testing.T) {
	dir := t.TempDir()
	data := `{"pages":{"https://docs.example.com/intro":{"filename":"abc.md","fetched_at":"2026-01-01T00:00:00Z"}}}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !idx.Has("https://docs.example.com/intro") {
		t.Error("expected loaded index to contain the URL")
	}
}

func TestLoadIndex_legacyFlatFormat_returnsEmptyIndex(t *testing.T) {
	// Old flat format had URL keys at the top level, not under "pages".
	// The new schema silently ignores these entries rather than erroring.
	dir := t.TempDir()
	data := `{"https://docs.example.com/intro":{"filename":"abc.md","fetched_at":"2026-01-01T00:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.Has("https://docs.example.com/intro") {
		t.Error("legacy flat-format entry should not be loaded into new index")
	}
}

func TestLoadIndex_corruptedJSON_returnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("not json at all!!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadIndex(dir)
	if err == nil {
		t.Error("expected error loading corrupted index.json")
	}
}

func TestLoadIndex_mkdirFails_returnsError(t *testing.T) {
	// Create a regular file and use it as the "directory" — MkdirAll will fail.
	f, err := os.CreateTemp("", "not-a-dir-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	defer func() { _ = os.Remove(f.Name()) }()

	_, err = LoadIndex(filepath.Join(f.Name(), "subdir"))
	if err == nil {
		t.Error("expected error when MkdirAll cannot create directory")
	}
}

func TestIndex_Record_persistsAndCanBeReloaded(t *testing.T) {
	dir := t.TempDir()
	idx, _ := LoadIndex(dir)

	if err := idx.Record("https://docs.example.com/intro", "abc.md"); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Reload from disk to verify persistence.
	idx2, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if !idx2.Has("https://docs.example.com/intro") {
		t.Error("URL not found after reload")
	}
}

func TestIndex_FilePath_returnsAbsPath(t *testing.T) {
	dir := t.TempDir()
	idx, _ := LoadIndex(dir)
	_ = idx.Record("https://docs.example.com/intro", "abc.md")

	path, ok := idx.FilePath("https://docs.example.com/intro")
	if !ok {
		t.Fatal("FilePath returned ok=false for known URL")
	}
	if path != filepath.Join(dir, "abc.md") {
		t.Errorf("got %q, want %q", path, filepath.Join(dir, "abc.md"))
	}
}

func TestIndex_FilePath_unknownURL_returnsNotOK(t *testing.T) {
	dir := t.TempDir()
	idx, _ := LoadIndex(dir)
	_, ok := idx.FilePath("https://docs.example.com/missing")
	if ok {
		t.Error("expected ok=false for unknown URL")
	}
}

func TestIndex_All_returnsAllEntries(t *testing.T) {
	dir := t.TempDir()
	idx, _ := LoadIndex(dir)
	_ = idx.Record("https://docs.example.com/a", "a.md")
	_ = idx.Record("https://docs.example.com/b", "b.md")

	all := idx.All()
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}
	if all["https://docs.example.com/a"] != filepath.Join(dir, "a.md") {
		t.Errorf("wrong path for /a: %q", all["https://docs.example.com/a"])
	}
}

func TestIndex_RecordAnalysis_PersistsAndLoads(t *testing.T) {
	dir := t.TempDir()
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := idx.Record("https://example.com", "abc.md"); err != nil {
		t.Fatal(err)
	}
	if err := idx.RecordAnalysis("https://example.com", "Covers install.", []string{"Homebrew install"}); err != nil {
		t.Fatal(err)
	}

	// Reload and verify
	idx2, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}

	summary, features, ok := idx2.Analysis("https://example.com")
	if !ok {
		t.Fatal("expected analysis to be found")
	}
	if summary != "Covers install." {
		t.Errorf("Summary: got %q", summary)
	}
	if len(features) != 1 || features[0] != "Homebrew install" {
		t.Errorf("Features: got %v", features)
	}
}

func TestIndex_Record_concurrent(t *testing.T) {
	dir := t.TempDir()
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}

	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			rawURL := fmt.Sprintf("https://docs.example.com/page-%d", i)
			filename := fmt.Sprintf("file-%d.md", i)
			if err := idx.Record(rawURL, filename); err != nil {
				t.Errorf("Record(%q): %v", rawURL, err)
			}
		}(i)
	}
	wg.Wait()

	all := idx.All()
	if len(all) != n {
		t.Fatalf("expected %d entries after concurrent Record, got %d", n, len(all))
	}
	for i := range n {
		rawURL := fmt.Sprintf("https://docs.example.com/page-%d", i)
		if !idx.Has(rawURL) {
			t.Errorf("missing entry for %q", rawURL)
		}
	}
}

func TestIndex_ProductInfo_returnsStoredSummary(t *testing.T) {
	dir := t.TempDir()
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.SetProductSummary("A CLI tool.", []string{"gap analysis", "doctor"}); err != nil {
		t.Fatal(err)
	}

	desc, features := idx.ProductInfo()
	if desc != "A CLI tool." {
		t.Errorf("ProductInfo desc: got %q, want %q", desc, "A CLI tool.")
	}
	if len(features) != 2 || features[0] != "gap analysis" || features[1] != "doctor" {
		t.Errorf("ProductInfo features: got %v", features)
	}
}

func TestIndex_ProductInfo_emptyByDefault(t *testing.T) {
	idx, err := LoadIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desc, features := idx.ProductInfo()
	if desc != "" {
		t.Errorf("expected empty desc, got %q", desc)
	}
	if len(features) != 0 {
		t.Errorf("expected empty features, got %v", features)
	}
}

func TestIndex_SetProductSummary_PersistsAndLoads(t *testing.T) {
	dir := t.TempDir()
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := idx.SetProductSummary("A CLI tool.", []string{"gap analysis", "doctor"}); err != nil {
		t.Fatal(err)
	}

	idx2, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}

	if idx2.ProductSummary != "A CLI tool." {
		t.Errorf("ProductSummary: got %q", idx2.ProductSummary)
	}
	if len(idx2.AllFeatures) != 2 {
		t.Errorf("AllFeatures: got %v", idx2.AllFeatures)
	}
}
