package action

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasFindings_PlaceholdersOnly(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	shots := filepath.Join(dir, "screenshots.md")
	// In the new plain-markdown format, the reporter omits the
	// "## Undocumented Features" header entirely when there are no
	// undocumented features, so the placeholder body has only the
	// Stale Documentation section with "_None found._".
	gapsBody := "# Gaps Found\n\n## Stale Documentation\n\n_None found._\n"
	shotsBody := "# Missing Screenshots\n\n_None found._\n"
	if err := os.WriteFile(gaps, []byte(gapsBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shots, []byte(shotsBody), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runScript(t, "has-findings.sh", gaps, shots)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if out != "false" {
		t.Errorf("got %q, want %q", out, "false")
	}
}

func TestHasFindings_GapsHasUndocumentedFeature(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	body := "# Gaps Found\n\n## Undocumented Features\n\n### Frobnicate\n\n**Why document this:** this is a user-facing feature.\n\n## Stale Documentation\n\n_None found._\n"
	if err := os.WriteFile(gaps, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runScript(t, "has-findings.sh", gaps, "/nope/screenshots.md")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if out != "true" {
		t.Errorf("got %q, want %q", out, "true")
	}
}

func TestHasFindings_GapsHasDriftCard(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	body := "# Gaps Found\n\n## Stale Documentation\n\n### Large\n\n- **MyFeature** — signature drift\n  - _Why:_ user-impact\n\n"
	if err := os.WriteFile(gaps, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runScript(t, "has-findings.sh", gaps, "/nope/screenshots.md")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if out != "true" {
		t.Errorf("got %q, want %q", out, "true")
	}
}

func TestHasFindings_ScreenshotsHasFinding(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	shots := filepath.Join(dir, "screenshots.md")
	// In the new plain-markdown format, the reporter omits the
	// "## Undocumented Features" header entirely when there are no
	// undocumented features, so the placeholder body has only the
	// Stale Documentation section with "_None found._".
	gapsBody := "# Gaps Found\n\n## Stale Documentation\n\n_None found._\n"
	shotsBody := "# Missing Screenshots\n\n### Medium\n\n#### Page\n\n- **Page:** [https://example.com/page](https://example.com/page)\n"
	if err := os.WriteFile(gaps, []byte(gapsBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shots, []byte(shotsBody), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runScript(t, "has-findings.sh", gaps, shots)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if out != "true" {
		t.Errorf("got %q, want %q", out, "true")
	}
}

func TestHasFindings_BothFilesAbsent(t *testing.T) {
	out, code := runScript(t, "has-findings.sh", "/nope/gaps.md", "/nope/screenshots.md")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if out != "false" {
		t.Errorf("got %q, want %q", out, "false")
	}
}

func TestHasFindings_GapsAbsentScreenshotsHasFinding(t *testing.T) {
	dir := t.TempDir()
	shots := filepath.Join(dir, "screenshots.md")
	body := "# Missing Screenshots\n\n### Small\n\n#### Page\n\n- **Page:** [https://example.com/page](https://example.com/page)\n"
	if err := os.WriteFile(shots, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runScript(t, "has-findings.sh", "/nope/gaps.md", shots)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if out != "true" {
		t.Errorf("got %q, want %q", out, "true")
	}
}
