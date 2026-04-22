package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestLoadFeatureMapCache_FileNotExist_ReturnsFalse(t *testing.T) {
	features := []analyzer.CodeFeature{{Name: "f", Description: "Does F.", Layer: "cli", UserFacing: true}}
	_, ok := loadFeatureMapCache(filepath.Join(t.TempDir(), "featuremap.json"), features)
	if ok {
		t.Fatal("expected false for non-existent file")
	}
}

func TestLoadFeatureMapCache_FeaturesMismatch_ReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	authFeature := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	fm := analyzer.FeatureMap{{Feature: authFeature, Files: []string{"a.go"}, Symbols: []string{"Login"}}}
	if err := saveFeatureMapCache(path, []analyzer.CodeFeature{authFeature}, fm); err != nil {
		t.Fatal(err)
	}
	otherFeature := analyzer.CodeFeature{Name: "other", Description: "Other.", Layer: "cli", UserFacing: false}
	_, ok := loadFeatureMapCache(path, []analyzer.CodeFeature{otherFeature})
	if ok {
		t.Fatal("expected false when features do not match")
	}
}

func TestLoadFeatureMapCache_Match_ReturnsMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	authFeature := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	searchFeature := analyzer.CodeFeature{Name: "search", Description: "Search.", Layer: "cli", UserFacing: true}
	features := []analyzer.CodeFeature{authFeature, searchFeature}
	fm := analyzer.FeatureMap{
		{Feature: authFeature, Files: []string{"a.go"}, Symbols: []string{"Login"}},
		{Feature: searchFeature, Files: []string{"s.go"}, Symbols: []string{"Search"}},
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
	if got[0].Feature.Name != "auth" {
		t.Errorf("got[0].Feature.Name = %q, want %q", got[0].Feature.Name, "auth")
	}
	if got[0].Feature.Description != "Auth." {
		t.Errorf("got[0].Feature.Description = %q, want %q", got[0].Feature.Description, "Auth.")
	}
	if got[0].Feature.Layer != "cli" {
		t.Errorf("got[0].Feature.Layer = %q, want %q", got[0].Feature.Layer, "cli")
	}
	if !got[0].Feature.UserFacing {
		t.Error("got[0].Feature.UserFacing = false, want true")
	}
}

func TestSaveFeatureMapCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	feat := analyzer.CodeFeature{Name: "f", Description: "Does F.", Layer: "cli", UserFacing: true}
	fm := analyzer.FeatureMap{{Feature: feat, Files: []string{"a.go"}, Symbols: []string{"A"}}}
	if err := saveFeatureMapCache(path, []analyzer.CodeFeature{feat}, fm); err != nil {
		t.Fatal(err)
	}
	got, ok := loadFeatureMapCache(path, []analyzer.CodeFeature{feat})
	if !ok {
		t.Fatal("expected cache hit after save")
	}
	if len(got) != 1 || got[0].Feature.Name != "f" {
		t.Errorf("unexpected result: %v", got)
	}
	// Assert all CodeFeature fields round-trip.
	if got[0].Feature.Description != "Does F." {
		t.Errorf("Feature.Description = %q, want %q", got[0].Feature.Description, "Does F.")
	}
	if got[0].Feature.Layer != "cli" {
		t.Errorf("Feature.Layer = %q, want %q", got[0].Feature.Layer, "cli")
	}
	if !got[0].Feature.UserFacing {
		t.Error("Feature.UserFacing = false, want true")
	}
}

func TestLoadFeatureMapCache_FeatureOrderInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	authFeature := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	searchFeature := analyzer.CodeFeature{Name: "search", Description: "Search.", Layer: "cli", UserFacing: true}
	fm := analyzer.FeatureMap{{Feature: authFeature, Files: []string{"a.go"}, Symbols: nil}}
	if err := saveFeatureMapCache(path, []analyzer.CodeFeature{authFeature, searchFeature}, fm); err != nil {
		t.Fatal(err)
	}
	_, ok := loadFeatureMapCache(path, []analyzer.CodeFeature{searchFeature, authFeature})
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
	feat := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	_, ok := loadFeatureMapCache(path, []analyzer.CodeFeature{feat})
	if ok {
		t.Fatal("expected false for corrupt JSON")
	}
}

func TestLoadFeatureMapCache_NilSlicesNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	feat := analyzer.CodeFeature{Name: "f", Description: "Does F.", Layer: "cli", UserFacing: true}
	fm := analyzer.FeatureMap{{Feature: feat, Files: nil, Symbols: nil}}
	if err := saveFeatureMapCache(path, []analyzer.CodeFeature{feat}, fm); err != nil {
		t.Fatal(err)
	}
	got, ok := loadFeatureMapCache(path, []analyzer.CodeFeature{feat})
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

// TestLoadFeatureMapCache_ReadError_ReturnsFalse covers the generic os.ReadFile
// error branch (not os.ErrNotExist) in loadFeatureMapCache by pointing at a directory.
func TestLoadFeatureMapCache_ReadError_ReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	feat := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	_, ok := loadFeatureMapCache(dir, []analyzer.CodeFeature{feat})
	if ok {
		t.Error("expected cache miss when path is a directory (read error)")
	}
}

// TestLoadFeatureMapCache_NilSlicesInRawJSON covers the nil-files and nil-symbols
// normalization branches in loadFeatureMapCache by writing raw JSON with null slices,
// bypassing saveFeatureMapCache normalization.
func TestLoadFeatureMapCache_NilSlicesInRawJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "featuremap.json")
	raw := `{"features":[{"name":"f","description":"Does F.","layer":"cli","user_facing":true}],"entries":[{"feature":{"name":"f","description":"Does F.","layer":"cli","user_facing":true},"files":null,"symbols":null}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	feat := analyzer.CodeFeature{Name: "f", Description: "Does F.", Layer: "cli", UserFacing: true}
	got, ok := loadFeatureMapCache(path, []analyzer.CodeFeature{feat})
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got[0].Files == nil {
		t.Error("null files in JSON must be normalized to empty slice")
	}
	if got[0].Symbols == nil {
		t.Error("null symbols in JSON must be normalized to empty slice")
	}
}
