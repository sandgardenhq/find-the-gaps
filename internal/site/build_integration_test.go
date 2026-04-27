//go:build integration
// +build integration

package site

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestBuildMirrorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires hugo on $PATH")
	}
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n\nbody\n"), 0o644)
	}
	err := Build(context.Background(),
		Inputs{Summary: analyzer.ProductSummary{Description: "demo"}},
		BuildOptions{
			ProjectDir:  projectDir,
			ProjectName: "demo",
			Mode:        ModeMirror,
			GeneratedAt: time.Now(),
		})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"site/index.html", "site/mapping/index.html", "site/gaps/index.html"} {
		if _, err := os.Stat(filepath.Join(projectDir, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	// site-src must NOT persist by default
	if _, err := os.Stat(filepath.Join(projectDir, "site-src")); !os.IsNotExist(err) {
		t.Errorf("site-src should not exist by default; err=%v", err)
	}
}

func TestBuildKeepSourcePersists(t *testing.T) {
	if testing.Short() {
		t.Skip("requires hugo on $PATH")
	}
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}
	err := Build(context.Background(), Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		KeepSource:  true,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "site-src", "hugo.toml")); err != nil {
		t.Errorf("site-src/hugo.toml missing: %v", err)
	}
}

func TestBuildExpandedFeaturePagesRendered(t *testing.T) {
	if testing.Short() {
		t.Skip("requires hugo on $PATH")
	}
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "Login", Layer: "service", UserFacing: true}, Files: []string{"a.go"}},
		},
	}
	err := Build(context.Background(), in, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeExpanded,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "site", "features", "login", "index.html")); err != nil {
		t.Errorf("expanded feature page missing: %v", err)
	}
}
