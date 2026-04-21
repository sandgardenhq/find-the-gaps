package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestLoadFeatureMapCache_FileNotExist_ReturnsFalse(t *testing.T) {
	_, ok := loadFeatureMapCache(filepath.Join(t.TempDir(), "featuremap.json"), []string{"f"})
	if ok {
		t.Fatal("expected false for non-existent file")
	}
}

func TestLoadFeatureMapCache_FeaturesMismatch_ReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	fm := analyzer.FeatureMap{{Feature: "auth", Files: []string{"a.go"}, Symbols: []string{"Login"}}}
	if err := saveFeatureMapCache(path, []string{"auth"}, fm); err != nil {
		t.Fatal(err)
	}
	_, ok := loadFeatureMapCache(path, []string{"other"})
	if ok {
		t.Fatal("expected false when features do not match")
	}
}

func TestLoadFeatureMapCache_Match_ReturnsMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	features := []string{"auth", "search"}
	fm := analyzer.FeatureMap{
		{Feature: "auth", Files: []string{"a.go"}, Symbols: []string{"Login"}},
		{Feature: "search", Files: []string{"s.go"}, Symbols: []string{"Search"}},
	}
	if err := saveFeatureMapCache(path, features, fm); err != nil {
		t.Fatal(err)
	}
	got, ok := loadFeatureMapCache(path, features)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Feature != "auth" {
		t.Errorf("got[0].Feature = %q, want %q", got[0].Feature, "auth")
	}
}

func TestSaveFeatureMapCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	fm := analyzer.FeatureMap{{Feature: "f", Files: []string{"a.go"}, Symbols: []string{"A"}}}
	if err := saveFeatureMapCache(path, []string{"f"}, fm); err != nil {
		t.Fatal(err)
	}
	got, ok := loadFeatureMapCache(path, []string{"f"})
	if !ok {
		t.Fatal("expected cache hit after save")
	}
	if len(got) != 1 || got[0].Feature != "f" {
		t.Errorf("unexpected result: %v", got)
	}
}

func TestLoadFeatureMapCache_FeatureOrderInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	fm := analyzer.FeatureMap{{Feature: "auth", Files: []string{"a.go"}, Symbols: nil}}
	if err := saveFeatureMapCache(path, []string{"auth", "search"}, fm); err != nil {
		t.Fatal(err)
	}
	_, ok := loadFeatureMapCache(path, []string{"search", "auth"})
	if !ok {
		t.Fatal("expected cache hit for same features in different order")
	}
}

func TestLoadFeatureMapCache_CorruptFile_ReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	if err := os.WriteFile(path, []byte("not json {{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok := loadFeatureMapCache(path, []string{"auth"})
	if ok {
		t.Fatal("expected false for corrupt JSON")
	}
}

func TestLoadFeatureMapCache_NilSlicesNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	fm := analyzer.FeatureMap{{Feature: "f", Files: nil, Symbols: nil}}
	if err := saveFeatureMapCache(path, []string{"f"}, fm); err != nil {
		t.Fatal(err)
	}
	got, ok := loadFeatureMapCache(path, []string{"f"})
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got[0].Files == nil {
		t.Error("Files must be normalized to empty slice, not nil")
	}
	if got[0].Symbols == nil {
		t.Error("Symbols must be normalized to empty slice, not nil")
	}
}
