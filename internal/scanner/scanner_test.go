package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan_emptyDir_returnsEmptyScan(t *testing.T) {
	dir := t.TempDir()
	cacheDir := t.TempDir()

	scan, _, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scan.RepoPath != dir {
		t.Errorf("RepoPath: got %q, want %q", scan.RepoPath, dir)
	}
	if len(scan.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(scan.Files))
	}
}

func TestScan_goFile_extractsSymbols(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\n// Run starts the app.\nfunc Run() error { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan, _, err := Scan(dir, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(scan.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(scan.Files))
	}
	if len(scan.Files[0].Symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(scan.Files[0].Symbols))
	}
	if scan.Files[0].Symbols[0].Name != "Run" {
		t.Errorf("symbol name: got %q, want Run", scan.Files[0].Symbols[0].Name)
	}
}

func TestScan_cacheReusedOnSecondRun(t *testing.T) {
	dir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Run() error { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "scan.json")); err != nil {
		t.Fatalf("scan.json not written: %v", err)
	}

	scan2, _, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(scan2.Files) != 1 || scan2.Files[0].Symbols[0].Name != "Run" {
		t.Errorf("cache not reused correctly: %+v", scan2.Files)
	}
}

func TestScan_writesProjectMd(t *testing.T) {
	dir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "project.md")); err != nil {
		t.Fatalf("project.md not written: %v", err)
	}
}

func TestScan_noCache_forcesReparse(t *testing.T) {
	dir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Run() error { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Scan(dir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	scan2, _, err := Scan(dir, Options{CacheDir: cacheDir, NoCache: true})
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(scan2.Files) != 1 {
		t.Errorf("expected 1 file on no-cache rescan, got %d", len(scan2.Files))
	}
}

func TestScan_countLines(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"one line", 1},
		{"line1\nline2\n", 3},
		{"a\nb\nc", 3},
	}
	for _, tt := range tests {
		got := countLines([]byte(tt.in))
		if got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
