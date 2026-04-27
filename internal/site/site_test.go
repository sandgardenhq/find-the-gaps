package site

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildRejectsUnknownMode(t *testing.T) {
	err := Build(context.Background(), Inputs{}, BuildOptions{Mode: Mode(99)})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !errors.Is(err, ErrUnknownMode) {
		t.Errorf("expected ErrUnknownMode, got %v", err)
	}
}

func TestBuildRejectsEmptyProjectDir(t *testing.T) {
	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  "",
		ProjectName: "x",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for empty ProjectDir")
	}
}

func TestBuildReturnsErrHugoMissing(t *testing.T) {
	defer func(orig string) { HugoBin = orig }(HugoBin)
	HugoBin = "ftg-nonexistent-hugo-binary-xyz"

	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}
	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if !errors.Is(err, ErrHugoMissing) {
		t.Errorf("expected ErrHugoMissing, got %v", err)
	}
}

func TestBuildCleansTempSourceOnHugoMissing(t *testing.T) {
	// Redirect MkdirTemp to a controlled directory so we can assert the
	// temp source dir created during Build() is removed when hugo is missing.
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)

	defer func(orig string) { HugoBin = orig }(HugoBin)
	HugoBin = "ftg-nonexistent-hugo-binary-xyz"

	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}

	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if !errors.Is(err, ErrHugoMissing) {
		t.Fatalf("expected ErrHugoMissing, got %v", err)
	}

	// No ftg-site-* leftovers in the temp root.
	matches, err := filepath.Glob(filepath.Join(tmpRoot, "ftg-site-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("temp source dir leaked on ErrHugoMissing: %v", matches)
	}
}

func TestBuildCleansTempSourceOnMaterializeFailure(t *testing.T) {
	// Redirect MkdirTemp so we can detect leaks.
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)

	// ProjectDir exists but mapping.md is missing — materialize() will fail
	// when materializeMirror tries to read it.
	projectDir := t.TempDir()

	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected materialize error")
	}

	matches, err := filepath.Glob(filepath.Join(tmpRoot, "ftg-site-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("temp source dir leaked on materialize failure: %v", matches)
	}
}

func TestBuildPreservesSourceOnHugoFailure(t *testing.T) {
	// Create a fake hugo that exits non-zero.
	tmpBin := t.TempDir()
	fake := filepath.Join(tmpBin, "hugo-fake")
	script := "#!/bin/sh\necho 'fake error' >&2\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	defer func(orig string) { HugoBin = orig }(HugoBin)
	HugoBin = fake

	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}
	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error from failing hugo")
	}
	if !strings.Contains(err.Error(), "fake error") {
		t.Errorf("error should include stderr: %v", err)
	}
	if !strings.Contains(err.Error(), "source preserved") {
		t.Errorf("error should name preserved source path: %v", err)
	}
}
