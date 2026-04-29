package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatch_emptyMatcher_returnsNoSkip(t *testing.T) {
	m := &Matcher{}
	got := m.Match("main.go", false)
	if got.Skip {
		t.Errorf("empty matcher should not skip; got %+v", got)
	}
	if got.Reason != "" {
		t.Errorf("empty matcher reason should be empty; got %q", got.Reason)
	}
}

func TestMatch_singleLayer_matchesPositive(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "*.log\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("app.log", false)
	if !got.Skip {
		t.Errorf("expected skip for app.log; got %+v", got)
	}
	if got.Reason != "defaults" {
		t.Errorf("reason = %q, want %q", got.Reason, "defaults")
	}
}

func TestMatch_singleLayer_noMatch(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "*.log\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("main.go", false)
	if got.Skip {
		t.Errorf("expected no skip for main.go; got %+v", got)
	}
}

func TestMatch_laterLayerNegatesEarlier(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults":   "vendor/\n",
		".ftgignore": "!vendor/\n",
	}, []string{"defaults", ".ftgignore"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("vendor/lib.go", false)
	if got.Skip {
		t.Errorf("later !vendor/ should re-include; got %+v", got)
	}
	if got.Reason != ".ftgignore" {
		t.Errorf("reason = %q, want %q", got.Reason, ".ftgignore")
	}
}

func TestMatch_earlierLayerCannotNegateLater(t *testing.T) {
	// Sanity: a defaults negation does NOT undo a .ftgignore positive match.
	m, err := newMatcherFromLayers(map[string]string{
		"defaults":   "!something\n",
		".ftgignore": "something\n",
	}, []string{"defaults", ".ftgignore"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("something", false)
	if !got.Skip {
		t.Errorf("later positive should win; got %+v", got)
	}
}

func TestMatch_directoryPattern(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "build/\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	if got := m.Match("build", true); !got.Skip {
		t.Errorf("build/ should match dir 'build'; got %+v", got)
	}
	if got := m.Match("build/output.txt", false); !got.Skip {
		t.Errorf("build/ should match files inside; got %+v", got)
	}
	if got := m.Match("build", false); got.Skip {
		t.Errorf("build/ pattern should not match a regular file named 'build'; got %+v", got)
	}
}

func TestMatch_floatingBasename(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "node_modules/\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	if got := m.Match("pkg/node_modules", true); !got.Skip {
		t.Errorf("nested node_modules dir should match; got %+v", got)
	}
}

func TestStats_initialState(t *testing.T) {
	var s Stats
	if s.Scanned != 0 {
		t.Errorf("Scanned = %d, want 0", s.Scanned)
	}
	if got := s.SkippedTotal(); got != 0 {
		t.Errorf("SkippedTotal = %d, want 0", got)
	}
}

func TestStats_recordSkip(t *testing.T) {
	var s Stats
	s.RecordSkip("defaults")
	s.RecordSkip("defaults")
	s.RecordSkip(".gitignore")
	if got := s.SkippedTotal(); got != 3 {
		t.Errorf("SkippedTotal = %d, want 3", got)
	}
	if got := s.Skipped["defaults"]; got != 2 {
		t.Errorf("Skipped[defaults] = %d, want 2", got)
	}
}

func TestStats_recordScanned(t *testing.T) {
	var s Stats
	s.RecordScanned()
	s.RecordScanned()
	if s.Scanned != 2 {
		t.Errorf("Scanned = %d, want 2", s.Scanned)
	}
}

func TestLoad_noFiles(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m == nil {
		t.Fatal("Load returned nil matcher")
	}
	// Defaults are always present; behaviour validated in Task 8.
	if got := m.Match("README.md", false); got.Skip {
		t.Errorf("README.md should not be skipped by minimal defaults; got %+v", got)
	}
}

func TestLoad_loadsGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("custom.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := m.Match("custom.txt", false)
	if !got.Skip || got.Reason != ".gitignore" {
		t.Errorf("expected skip via .gitignore; got %+v", got)
	}
}

func TestLoad_loadsFtgignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".ftgignore"), []byte("custom.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := m.Match("custom.txt", false)
	if !got.Skip || got.Reason != ".ftgignore" {
		t.Errorf("expected skip via .ftgignore; got %+v", got)
	}
}

func TestLoad_ftgignoreNegatesGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("data/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ftgignore"), []byte("!data/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := m.Match("data/x.txt", false)
	if got.Skip {
		t.Errorf("data/ should be re-included; got %+v", got)
	}
}
