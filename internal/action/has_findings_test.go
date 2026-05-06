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
	gapsBody := "# Gaps Found\n\n## Undocumented Features\n\n_None found._\n\n## Stale Documentation\n\n_None found._\n"
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
	body := "# Gaps Found\n\n## Undocumented Features\n\n<div class=\"ftg-undoc-list\">\n\n<div class=\"ftg-undoc\"><span class=\"ftg-undoc-name\">Frobnicate</span><span class=\"ftg-undoc-msg\"> — has code implementation but no documentation page</span></div>\n\n</div>\n\n## Stale Documentation\n\n_None found._\n"
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
	body := "# Gaps Found\n\n## Undocumented Features\n\n_None found._\n\n## Stale Documentation\n\n<div class=\"ftg-priority ftg-priority--large\">\n\n### Large\n\n</div>\n\n<div class=\"ftg-stale-list\">\n\n<div class=\"ftg-stale ftg-stale--large\">\n<span class=\"ftg-stale-feature\">MyFeature</span>\n<span class=\"ftg-stale-issue\">signature drift</span>\n</div>\n\n</div>\n"
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
	gapsBody := "# Gaps Found\n\n## Undocumented Features\n\n_None found._\n\n## Stale Documentation\n\n_None found._\n"
	shotsBody := "# Missing Screenshots\n\n<div class=\"ftg-shot-list\">\n\n<div class=\"ftg-shot ftg-shot--medium\">\n<div class=\"ftg-shot-head\"><a href=\"https://example.com/page\">https://example.com/page</a></div>\n</div>\n\n</div>\n"
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
	body := "# Missing Screenshots\n\n<div class=\"ftg-shot ftg-shot--small\">hi</div>\n"
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
