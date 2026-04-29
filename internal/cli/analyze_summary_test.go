package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/ignore"
)

func ignoreStats(scanned int, skipped map[string]int) ignore.Stats {
	return ignore.Stats{Scanned: scanned, Skipped: skipped}
}

func TestAnalyze_printsScanSummary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd := newAnalyzeCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--repo", dir, "--cache-dir", filepath.Join(t.TempDir(), "cache")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "scanned ") || !strings.Contains(out, "skipped ") {
		t.Errorf("expected scan summary in output; got:\n%s", out)
	}
	if !strings.Contains(out, "defaults:") {
		t.Errorf("expected defaults segment in summary; got:\n%s", out)
	}
}

func TestAnalyze_quietSuppressesSummary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIND_THE_GAPS_QUIET", "1")

	var buf bytes.Buffer
	cmd := newAnalyzeCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--repo", dir, "--cache-dir", filepath.Join(t.TempDir(), "cache")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Contains(buf.String(), "skipped ") {
		t.Errorf("FIND_THE_GAPS_QUIET should suppress summary; got:\n%s", buf.String())
	}
}

func TestFormatScanSummary_thousandsSeparators(t *testing.T) {
	s := ignoreStats(412, map[string]int{
		"defaults":   1801,
		".gitignore": 38,
		".ftgignore": 8,
	})
	want := "scanned 412 files, skipped 1,847 (defaults: 1,801, .gitignore: 38, .ftgignore: 8)"
	if got := formatScanSummary(s); got != want {
		t.Errorf("formatScanSummary mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatScanSummary_smallCountsHaveNoCommas(t *testing.T) {
	s := ignoreStats(3, map[string]int{".ftgignore": 1})
	want := "scanned 3 files, skipped 1 (.ftgignore: 1)"
	if got := formatScanSummary(s); got != want {
		t.Errorf("formatScanSummary mismatch\n got: %q\nwant: %q", got, want)
	}
}
