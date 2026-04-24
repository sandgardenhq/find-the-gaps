// internal/action/build_issue_body_test.go
package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildIssueBody_GapsOnly(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	if err := os.WriteFile(gaps, []byte("# Findings\n- foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUN_URL", "https://gh.example/run/1")
	t.Setenv("COMMIT_SHA", "abc123")
	t.Setenv("RUN_TIMESTAMP", "2026-04-24T12:00:00Z")

	out, code := runScript(t, "build-issue-body.sh", gaps, "/nonexistent/screenshots.md")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "https://gh.example/run/1") {
		t.Errorf("body missing run URL: %s", out)
	}
	if !strings.Contains(out, "abc123") {
		t.Errorf("body missing commit sha: %s", out)
	}
	if !strings.Contains(out, "# Findings") {
		t.Errorf("body missing gaps content: %s", out)
	}
	if strings.Contains(out, "Screenshot Gaps") {
		t.Errorf("body should NOT have screenshots section: %s", out)
	}
}

func TestBuildIssueBody_WithScreenshots(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	shots := filepath.Join(dir, "screenshots.md")
	_ = os.WriteFile(gaps, []byte("gaps content"), 0o644)
	_ = os.WriteFile(shots, []byte("shots content"), 0o644)
	t.Setenv("RUN_URL", "u")
	t.Setenv("COMMIT_SHA", "s")
	t.Setenv("RUN_TIMESTAMP", "t")

	out, code := runScript(t, "build-issue-body.sh", gaps, shots)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "## Screenshot Gaps") {
		t.Errorf("body missing screenshots heading: %s", out)
	}
	if !strings.Contains(out, "shots content") {
		t.Errorf("body missing screenshots content: %s", out)
	}
}

func TestBuildIssueBody_EmptyGapsFile(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	_ = os.WriteFile(gaps, []byte(""), 0o644)
	t.Setenv("RUN_URL", "u")
	t.Setenv("COMMIT_SHA", "s")
	t.Setenv("RUN_TIMESTAMP", "t")

	out, code := runScript(t, "build-issue-body.sh", gaps, "/nope")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "u") {
		t.Errorf("body missing run URL: %s", out)
	}
}
