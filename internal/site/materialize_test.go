package site

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestMaterializeMirrorWritesExpectedTree(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()

	// Pre-write the user-facing markdown the way the reporter would.
	for _, name := range []string{"mapping.md", "gaps.md"} {
		if err := os.WriteFile(filepath.Join(projectDir, name),
			[]byte("# "+name+"\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	in := Inputs{
		Summary: analyzer.ProductSummary{Description: "demo"},
		Mapping: analyzer.FeatureMap{},
	}
	opts := BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
	}

	if err := materialize(srcDir, in, opts); err != nil {
		t.Fatal(err)
	}

	// hugo.toml present and contains "Mapping"
	cfg, err := os.ReadFile(filepath.Join(srcDir, "hugo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(cfg), `name = "Mapping"`) {
		t.Errorf("hugo.toml missing Mapping menu:\n%s", cfg)
	}

	// content/_index.md present
	if _, err := os.Stat(filepath.Join(srcDir, "content", "_index.md")); err != nil {
		t.Error(err)
	}
	// content/mapping.md present and starts with frontmatter
	mapping, err := os.ReadFile(filepath.Join(srcDir, "content", "mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(mapping), `title = "Mapping"`) {
		t.Errorf("mapping.md missing title frontmatter:\n%s", mapping)
	}
	// theme present
	if _, err := os.Stat(filepath.Join(srcDir, "themes", "hextra", "theme.toml")); err != nil {
		t.Error(err)
	}
	// screenshots NOT present (ScreenshotsRan = false in this test)
	if _, err := os.Stat(filepath.Join(srcDir, "content", "screenshots.md")); !os.IsNotExist(err) {
		t.Errorf("screenshots.md should not exist when ScreenshotsRan=false; err=%v", err)
	}
}

func TestMaterializeMirrorWithScreenshots(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()
	for _, name := range []string{"mapping.md", "gaps.md", "screenshots.md"} {
		_ = os.WriteFile(filepath.Join(projectDir, name), []byte("# "+name+"\n"), 0o644)
	}

	err := materialize(srcDir, Inputs{ScreenshotsRan: true}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "content", "screenshots.md")); err != nil {
		t.Errorf("screenshots.md should exist: %v", err)
	}
	cfg, _ := os.ReadFile(filepath.Join(srcDir, "hugo.toml"))
	if !contains(string(cfg), `name = "Screenshots"`) {
		t.Errorf("hugo.toml should declare Screenshots menu:\n%s", cfg)
	}
}

func TestMaterializeExpandedWritesFeaturePages(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()

	// mapping.md is unused by expanded mode but exists alongside gaps.md the
	// way the reporter lays them down.
	if err := os.WriteFile(filepath.Join(projectDir, "mapping.md"),
		[]byte("# mapping.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// gaps.md must contain a quoted feature name so linkFeatureNames has
	// something to rewrite into a /features/<slug>/ link.
	if err := os.WriteFile(filepath.Join(projectDir, "gaps.md"),
		[]byte("# Gaps\n\n\"User Auth\" has drift.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{
				Feature: analyzer.CodeFeature{Name: "User Auth", Layer: "service", UserFacing: true},
				Files:   []string{"internal/auth/login.go"},
				Symbols: []string{"Login"},
			},
			{
				Feature: analyzer.CodeFeature{Name: "user auth", Layer: "ui", UserFacing: true},
				Files:   []string{"web/auth.tsx"},
			},
		},
		AllDocFeatures: []string{"User Auth"},
	}
	err := materialize(srcDir, in, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeExpanded,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Features index
	if _, err := os.Stat(filepath.Join(srcDir, "content", "features", "_index.md")); err != nil {
		t.Errorf("features/_index.md missing: %v", err)
	}
	// Per-feature pages with collision-resolved slugs
	if _, err := os.Stat(filepath.Join(srcDir, "content", "features", "user-auth.md")); err != nil {
		t.Errorf("user-auth.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "content", "features", "user-auth-2.md")); err != nil {
		t.Errorf("user-auth-2.md missing (collision): %v", err)
	}
	// Gaps page (linked feature names rendered into gaps.md content dir)
	gaps, err := os.ReadFile(filepath.Join(srcDir, "content", "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(gaps), "/features/user-auth/") {
		t.Errorf("gaps.md should hyperlink feature names:\n%s", gaps)
	}
	// hugo.toml taxonomies
	cfg, _ := os.ReadFile(filepath.Join(srcDir, "hugo.toml"))
	if !contains(string(cfg), "[taxonomies]") {
		t.Errorf("hugo.toml missing taxonomies:\n%s", cfg)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
