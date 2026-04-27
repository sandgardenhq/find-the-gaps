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
