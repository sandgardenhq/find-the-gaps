package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestSupportedLanguages_dropsGenericEntry(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: []string{"Go", "Generic", "Python"}}
	got := supportedLanguages(scan)
	want := []string{"Go", "Python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supportedLanguages() = %v, want %v", got, want)
	}
}

func TestSupportedLanguages_returnsEmptyWhenOnlyGeneric(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: []string{"Generic"}}
	if got := supportedLanguages(scan); len(got) != 0 {
		t.Fatalf("supportedLanguages() = %v, want []", got)
	}
}

func TestSupportedLanguages_returnsEmptyWhenNoLanguages(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: nil}
	if got := supportedLanguages(scan); len(got) != 0 {
		t.Fatalf("supportedLanguages() = %v, want []", got)
	}
}

func TestAnalyze_haltsOnUnsupportedLanguageRepo(t *testing.T) {
	repo := t.TempDir()
	cacheBase := t.TempDir()

	// Markdown + JSON only. No file matches any dedicated extractor.
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "data.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repo,
		"--cache-dir", cacheBase,
		"--docs-url", "https://example.com/docs",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	se := stderr.String()
	for _, want := range []string{
		"no supported programming languages",
		"Go, Python, TypeScript",
		"https://github.com/sandgardenhq/find-the-gaps/issues",
	} {
		if !strings.Contains(se, want) {
			t.Errorf("stderr missing %q\nfull stderr:\n%s", want, se)
		}
	}

	// project.md SHOULD have been written by the scan (we deliberately let
	// the scan persist its output before the halt — it documents what we
	// found).
	projectMD := filepath.Join(cacheBase, filepath.Base(repo), "scan", "project.md")
	if _, err := os.Stat(projectMD); err != nil {
		t.Errorf("expected project.md to exist after halt: %v", err)
	}

	// mapping.md / gaps.md MUST NOT exist — the LLM passes never ran.
	for _, name := range []string{"mapping.md", "gaps.md"} {
		p := filepath.Join(cacheBase, filepath.Base(repo), name)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("unexpected %s exists after halt (err=%v)", name, err)
		}
	}
}
