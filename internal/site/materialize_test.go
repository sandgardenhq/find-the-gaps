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

// TestMaterializeExpandedWithDriftAndDocsMap exercises the driftByFeature and
// docPagesByFeature loops plus the per-feature DocURLs/Drift rendering path
// that TestMaterializeExpandedWritesFeaturePages leaves uncovered.
func TestMaterializeExpandedWithDriftAndDocsMap(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(projectDir, "gaps.md"),
		[]byte("# Gaps\n\n\"Login\" has drift.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{
				Feature: analyzer.CodeFeature{Name: "Login", Layer: "service", UserFacing: true},
				Files:   []string{"internal/auth/login.go"},
				Symbols: []string{"Login"},
			},
		},
		AllDocFeatures: []string{"Login"},
		DocsMap: analyzer.DocsFeatureMap{
			{Feature: "Login", Pages: []string{"https://example.com/docs/login"}},
		},
		Drift: []analyzer.DriftFinding{
			{
				Feature: "Login",
				Issues: []analyzer.DriftIssue{
					{Page: "https://example.com/docs/login", Issue: "old signature"},
					{Page: "https://example.com/docs/login", Issue: "removed parameter"},
				},
			},
		},
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

	page, err := os.ReadFile(filepath.Join(srcDir, "content", "features", "login.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(page)
	if !contains(body, "old signature") {
		t.Errorf("feature page missing drift issue:\n%s", body)
	}
	if !contains(body, "https://example.com/docs/login") {
		t.Errorf("feature page missing DocURL:\n%s", body)
	}
}

// TestMaterializeExpandedSkipsEmptySlugFeature exercises the
// `if slug == "" { continue }` branch in materializeExpanded.
func TestMaterializeExpandedSkipsEmptySlugFeature(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(projectDir, "gaps.md"),
		[]byte("# Gaps\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := Inputs{
		Mapping: analyzer.FeatureMap{
			// Name reduces to empty slug — featureSlug strips all symbols.
			{Feature: analyzer.CodeFeature{Name: "!!!", Layer: "ui", UserFacing: true}, Files: []string{"a.go"}},
			// Real one so the index isn't empty.
			{Feature: analyzer.CodeFeature{Name: "Real", Layer: "ui", UserFacing: true}, Files: []string{"b.go"}},
		},
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

	// The empty-slug feature must NOT have a page.
	entries, err := os.ReadDir(filepath.Join(srcDir, "content", "features"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	for _, n := range names {
		if n == ".md" {
			t.Errorf("found page for empty-slug feature: %v", names)
		}
	}
	// Real one must exist.
	if _, err := os.Stat(filepath.Join(srcDir, "content", "features", "real.md")); err != nil {
		t.Errorf("real feature page missing: %v", err)
	}
}

// TestMaterializeExpandedWithScreenshots exercises the entire screenshots
// branch in materializeExpanded — multiple gaps per page and gaps spread
// across multiple pages, with one whose URL slugifies to empty.
func TestMaterializeExpandedWithScreenshots(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(projectDir, "gaps.md"),
		[]byte("# Gaps\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := Inputs{
		Mapping:        analyzer.FeatureMap{},
		ScreenshotsRan: true,
		Screenshots: []analyzer.ScreenshotGap{
			{PageURL: "https://example.com/docs/start", QuotedPassage: "open dashboard", ShouldShow: "dashboard view", SuggestedAlt: "dashboard", InsertionHint: "after intro"},
			{PageURL: "https://example.com/docs/start", QuotedPassage: "click save", ShouldShow: "save button", SuggestedAlt: "save button", InsertionHint: "next to button"},
			{PageURL: "https://example.com/docs/admin", QuotedPassage: "settings page", ShouldShow: "settings UI", SuggestedAlt: "settings", InsertionHint: "top of page"},
			// URL that slugifies to "" — exercises slug == "" → "page" branch.
			{PageURL: "!!!", QuotedPassage: "weird", ShouldShow: "weird thing", SuggestedAlt: "weird", InsertionHint: "weird"},
		},
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

	// _index.md and per-page slugs exist
	if _, err := os.Stat(filepath.Join(srcDir, "content", "screenshots", "_index.md")); err != nil {
		t.Errorf("screenshots/_index.md missing: %v", err)
	}
	// Two distinct pages → two slugged files (start and admin).
	if _, err := os.Stat(filepath.Join(srcDir, "content", "screenshots", "https-example-com-docs-start.md")); err != nil {
		t.Errorf("screenshots/start.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "content", "screenshots", "https-example-com-docs-admin.md")); err != nil {
		t.Errorf("screenshots/admin.md missing: %v", err)
	}
	// Empty-slug URL fell back to "page".
	if _, err := os.Stat(filepath.Join(srcDir, "content", "screenshots", "page.md")); err != nil {
		t.Errorf("screenshots/page.md (empty-slug fallback) missing: %v", err)
	}
	// First page should contain both gaps.
	body, err := os.ReadFile(filepath.Join(srcDir, "content", "screenshots", "https-example-com-docs-start.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(body), "open dashboard") || !contains(string(body), "click save") {
		t.Errorf("multi-gap page missing one of the gaps:\n%s", body)
	}
}

// TestMaterializeMirrorMissingReportFile exercises the error branch when a
// required report markdown file is absent from ProjectDir.
func TestMaterializeMirrorMissingReportFile(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()
	// Intentionally do NOT write mapping.md / gaps.md.

	err := materialize(srcDir, Inputs{}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeMirror,
		GeneratedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when mapping.md is missing")
	}
	if !contains(err.Error(), "mapping.md") {
		t.Errorf("expected error to name mapping.md, got %v", err)
	}
}

// TestMaterializeExpandedMissingGapsFile exercises the error branch when
// gaps.md is missing in ProjectDir during expanded mode.
func TestMaterializeExpandedMissingGapsFile(t *testing.T) {
	srcDir := t.TempDir()
	projectDir := t.TempDir()
	// gaps.md intentionally missing.

	err := materialize(srcDir, Inputs{Mapping: analyzer.FeatureMap{}}, BuildOptions{
		ProjectDir:  projectDir,
		ProjectName: "demo",
		Mode:        ModeExpanded,
		GeneratedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when gaps.md is missing")
	}
	if !contains(err.Error(), "gaps.md") {
		t.Errorf("expected error to name gaps.md, got %v", err)
	}
}

// TestLinkFeatureNamesEmptyEntries asserts that entries with empty name or
// empty slug in the slugs map are skipped (no rewriting attempted).
func TestLinkFeatureNamesEmptyEntries(t *testing.T) {
	body := `"Real" appears here. "" should not be rewritten.`
	slugs := map[string]string{
		"Real":  "real",
		"":      "ignored", // empty name — skip
		"Empty": "",        // empty slug — skip
	}
	got := linkFeatureNames(body, slugs)
	if !contains(got, `"[Real](/features/real/)"`) {
		t.Errorf("real should be linked: %s", got)
	}
	// Sanity: empty name/slug entries didn't insert anything weird.
	if contains(got, "/features//") {
		t.Errorf("empty slug leaked into output: %s", got)
	}
}

// TestResolveSlugsEmptyName covers the early-return branch in resolveSlugs
// where featureSlug returns "".
func TestResolveSlugsEmptyName(t *testing.T) {
	got := resolveSlugs([]string{"!!!", "Real"})
	if got["!!!"] != "" {
		t.Errorf("expected empty slug for symbols-only name; got %q", got["!!!"])
	}
	if got["Real"] != "real" {
		t.Errorf("expected 'real' for 'Real'; got %q", got["Real"])
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
