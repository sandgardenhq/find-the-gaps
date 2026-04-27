package site

import (
	"context"
	"errors"
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
