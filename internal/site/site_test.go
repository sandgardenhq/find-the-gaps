package site

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
