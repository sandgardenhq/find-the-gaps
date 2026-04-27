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
	gapsBody := "# Gaps Found\n\n## Undocumented Code\n\n### User-facing\n\n_None found._\n\n### Not user-facing\n\n_None found._\n\n## Unmapped Features\n\n_None found._\n\n## Stale Documentation\n\n_None found._\n"
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
	body := "# Gaps Found\n\n## Undocumented Code\n\n### User-facing\n\n- \"Frobnicate\" has code implementation but no documentation page\n\n### Not user-facing\n\n_None found._\n\n## Unmapped Features\n\n_None found._\n\n## Stale Documentation\n\n_None found._\n"
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

func TestHasFindings_GapsHasDriftHeading(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	body := "# Gaps Found\n\n## Undocumented Code\n\n### User-facing\n\n_None found._\n\n### Not user-facing\n\n_None found._\n\n## Unmapped Features\n\n_None found._\n\n## Stale Documentation\n\n### MyFeature\n\n- docs/page.md — signature drift\n\n"
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
	gapsBody := "# Gaps Found\n\n## Undocumented Code\n\n### User-facing\n\n_None found._\n\n### Not user-facing\n\n_None found._\n\n## Unmapped Features\n\n_None found._\n\n## Stale Documentation\n\n_None found._\n"
	shotsBody := "# Missing Screenshots\n\n### https://docs.example.com/page\n\n- **Passage:** \"click the button\"\n  - **Screenshot should show:** the button\n  - **Alt text:** Button to do thing\n  - **Insert:** after the paragraph\n\n"
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
	body := "# Missing Screenshots\n\n### https://docs.example.com/page\n\n- **Passage:** \"hi\"\n"
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
