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
