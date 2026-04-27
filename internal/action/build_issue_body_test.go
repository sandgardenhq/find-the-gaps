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

func TestBuildIssueBody_BothFilesAbsent(t *testing.T) {
	t.Setenv("RUN_URL", "https://gh.example/run/unique-both-absent-42")
	t.Setenv("COMMIT_SHA", "deadbeef")
	t.Setenv("RUN_TIMESTAMP", "2026-04-24T13:00:00Z")

	out, code := runScript(t, "build-issue-body.sh", "/nope/gaps.md", "/nope/screenshots.md")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "https://gh.example/run/unique-both-absent-42") {
		t.Errorf("body missing header / run URL: %s", out)
	}
	if strings.Contains(out, "Screenshot Gaps") {
		t.Errorf("body should NOT contain Screenshot Gaps section: %s", out)
	}
	// Neither file exists, so there should be no file content beyond the header.
	// The only content line should be the generated-by line. Splitting confirms brevity.
	if strings.Contains(out, "# Findings") || strings.Contains(out, "shots content") {
		t.Errorf("body should NOT contain any file content: %s", out)
	}
}

func TestBuildIssueBody_ScreenshotsOnly(t *testing.T) {
	dir := t.TempDir()
	shots := filepath.Join(dir, "screenshots.md")
	if err := os.WriteFile(shots, []byte("screenshot gap body"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUN_URL", "u")
	t.Setenv("COMMIT_SHA", "s")
	t.Setenv("RUN_TIMESTAMP", "t")

	out, code := runScript(t, "build-issue-body.sh", "/nope/gaps.md", shots)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "## Screenshot Gaps") {
		t.Errorf("body missing screenshots heading: %s", out)
	}
	if !strings.Contains(out, "screenshot gap body") {
		t.Errorf("body missing screenshots content: %s", out)
	}
}

func TestBuildIssueBody_MissingRunURLFailsFast(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	if err := os.WriteFile(gaps, []byte("# Findings\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure RUN_URL is unset for this subprocess by clearing it for the test duration.
	// t.Setenv registers cleanup to restore the prior value after the test.
	t.Setenv("RUN_URL", "")
	if err := os.Unsetenv("RUN_URL"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMIT_SHA", "s")
	t.Setenv("RUN_TIMESTAMP", "t")

	out, code := runScript(t, "build-issue-body.sh", gaps, "/nope")
	if code == 0 {
		t.Fatalf("expected non-zero exit when RUN_URL unset, got 0 with output: %s", out)
	}
	if !strings.Contains(out, "RUN_URL must be set") {
		t.Errorf("expected guard message 'RUN_URL must be set' in output, got: %s", out)
	}
}
