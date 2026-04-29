package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
