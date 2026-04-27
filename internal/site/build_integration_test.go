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

// TestBuildRelativeProjectDirWritesToExpectedPath guards against a regression
// where hugo's --destination was relative and got resolved against --source,
// causing the rendered site to land inside the temp source dir and be silently
// deleted on cleanup. analyze.go uses ".find-the-gaps/<repo>" by default — a
// relative path — so this is the production case.
func TestBuildRelativeProjectDirWritesToExpectedPath(t *testing.T) {
	if testing.Short() {
		t.Skip("requires hugo on $PATH")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	relPD := ".cache/proj"
	if err := os.MkdirAll(relPD, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mapping.md", "gaps.md", "screenshots.md"} {
		if err := os.WriteFile(filepath.Join(relPD, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err = Build(context.Background(), Inputs{
		Mapping:        analyzer.FeatureMap{{Feature: analyzer.CodeFeature{Name: "f"}}},
		ScreenshotsRan: true,
	}, BuildOptions{
		ProjectDir:  relPD,
		ProjectName: "proj",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Build err: %v", err)
	}
	idx := filepath.Join(relPD, "site", "index.html")
	if _, err := os.Stat(idx); err != nil {
		t.Fatalf("expected site at %s after Build with relative ProjectDir; got: %v", idx, err)
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
