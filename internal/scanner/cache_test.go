package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanCache_saveAndLoad(t *testing.T) {
	dir := t.TempDir()
	scan := &ProjectScan{
		RepoPath:  "/repo",
		ScannedAt: time.Now().Truncate(time.Second),
		Languages: []string{"Go"},
		Files:     []ScannedFile{{Path: "main.go", Language: "Go"}},
		Graph:     ImportGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}},
	}
	c := NewScanCache(dir)
	if err := c.Save(scan); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := c.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.RepoPath != scan.RepoPath {
		t.Errorf("RepoPath: got %q, want %q", loaded.RepoPath, scan.RepoPath)
	}
	if len(loaded.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(loaded.Files))
	}
}

func TestScanCache_load_missingFile_returnsNil(t *testing.T) {
	c := NewScanCache(t.TempDir())
	loaded, err := c.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for missing cache, got %+v", loaded)
	}
}

func TestScanCache_fileMap_byPath(t *testing.T) {
	scan := &ProjectScan{
		Files: []ScannedFile{
			{Path: "a.go", ModTime: time.Now()},
			{Path: "b.go", ModTime: time.Now()},
		},
	}
	m := scan.FileMap()
	if _, ok := m["a.go"]; !ok {
		t.Error("expected a.go in file map")
	}
	if _, ok := m["b.go"]; !ok {
		t.Error("expected b.go in file map")
	}
	if len(m) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m))
	}
}

func TestScanCache_save_createsDirIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "nested", "dir")
	c := NewScanCache(dir)
	scan := &ProjectScan{Graph: ImportGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}}
	if err := c.Save(scan); err != nil {
		t.Fatalf("Save into non-existent dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scan.json")); err != nil {
		t.Errorf("scan.json not created: %v", err)
	}
}

func TestScanCache_fileMap_empty(t *testing.T) {
	scan := &ProjectScan{}
	m := scan.FileMap()
	if len(m) != 0 {
		t.Errorf("expected empty map, got %d entries", len(m))
	}
}

func TestScanCache_load_corruptJSON_returnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scan.json"), []byte("not json {{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewScanCache(dir)
	_, err := c.Load()
	if err == nil {
		t.Error("expected error for corrupt JSON, got nil")
	}
}

func TestScanCache_path(t *testing.T) {
	c := NewScanCache("/some/dir")
	if c == nil {
		t.Fatal("NewScanCache returned nil")
	}
}

func TestScanCache_load_unreadableFile_returnsError(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.json")
	if err := os.WriteFile(scanPath, []byte(`{}`), 0o000); err != nil {
		t.Fatal(err)
	}
	c := NewScanCache(dir)
	_, err := c.Load()
	// On systems where the test runs as root, chmod 000 is still readable;
	// skip the assertion in that case.
	if os.Getuid() == 0 {
		t.Skip("running as root, permission test not meaningful")
	}
	if err == nil {
		t.Error("expected error for unreadable scan.json, got nil")
	}
}
