package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

func TestWriteLinksMD_EmptyReportProducesEmptyButValidFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLinksMD(dir, linkcheck.Report{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "links.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "# Dead Links\n") {
		t.Fatalf("want leading H1, got %q", s)
	}
	for _, banned := range []string{"## Broken", "## Auth Required", "## Redirected"} {
		if strings.Contains(s, banned) {
			t.Fatalf("empty report must not render %q section, got:\n%s", banned, s)
		}
	}
	if !strings.Contains(s, "No dead links detected") {
		t.Fatalf("want empty-state copy, got %q", s)
	}
}

func TestWriteLinksMD_RendersAllThreeBucketsWhenNonEmpty(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Broken: []linkcheck.Finding{{
			URL:       "https://gone.example/",
			ErrorType: "http_404",
			Detail:    "HTTP 404 Not Found",
			Pages:     []string{"https://docs/a", "https://docs/b"},
		}},
		Auth: []linkcheck.Finding{{
			URL:    "https://private.example/",
			Detail: "HTTP 401 Unauthorized",
			Pages:  []string{"https://docs/a"},
		}},
		Redirected: []linkcheck.Finding{{
			URL:      "https://old.example/x",
			FinalURL: "https://new.example/x",
			Detail:   "redirected",
			Pages:    []string{"https://docs/a"},
		}},
	}
	if err := WriteLinksMD(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readString(t, filepath.Join(dir, "links.md"))
	for _, want := range []string{
		"## Broken",
		"### https://gone.example/",
		"**Reason:** HTTP 404 Not Found",
		"## Auth Required",
		"## Redirected",
		"**Redirects to:** https://new.example/x",
		"- https://docs/a",
		"- https://docs/b",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("want %q in:\n%s", want, s)
		}
	}
	bi := strings.Index(s, "## Broken")
	ai := strings.Index(s, "## Auth Required")
	ri := strings.Index(s, "## Redirected")
	if bi >= ai || ai >= ri {
		t.Fatalf("bucket order wrong: broken=%d auth=%d redirected=%d", bi, ai, ri)
	}
}

func TestWriteLinksMD_OmitsEmptyBuckets(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Auth: []linkcheck.Finding{{URL: "https://private.example/", Detail: "401", Pages: []string{"p1"}}},
	}
	if err := WriteLinksMD(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readString(t, filepath.Join(dir, "links.md"))
	if strings.Contains(s, "## Broken") {
		t.Fatalf("Broken section should be omitted")
	}
	if strings.Contains(s, "## Redirected") {
		t.Fatalf("Redirected section should be omitted")
	}
	if !strings.Contains(s, "## Auth Required") {
		t.Fatalf("Auth Required section should be present")
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
