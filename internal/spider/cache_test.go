package spider

import (
	"os"
	"path/filepath"
	"strings"
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
	data := `{"https://docs.example.com/intro":{"filename":"abc.md","fetched_at":"2026-01-01T00:00:00Z"}}`
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
