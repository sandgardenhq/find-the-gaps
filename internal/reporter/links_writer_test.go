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
	for _, banned := range []string{"## Broken", "## Auth Required"} {
		if strings.Contains(s, banned) {
			t.Fatalf("empty report must not render %q section, got:\n%s", banned, s)
		}
	}
	if !strings.Contains(s, "No dead links detected") {
		t.Fatalf("want empty-state copy, got %q", s)
	}
}

func TestWriteLinksMD_RendersBothBucketsWhenNonEmpty(t *testing.T) {
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
		"- https://docs/a",
		"- https://docs/b",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("want %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "## Redirected") {
		t.Fatalf("Redirected section should not render")
	}
	bi := strings.Index(s, "## Broken")
	ai := strings.Index(s, "## Auth Required")
	if bi >= ai {
		t.Fatalf("bucket order wrong: broken=%d auth=%d", bi, ai)
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

// TestWriteLinksMD_SectionHeadingsCarryStableAnchors pins explicit
// `{#broken}` / `{#auth-required}` heading IDs on the two section headers.
// The home page's at-a-glance Dead Links cards deep-link to these anchors
// (/links/#broken, /links/#auth-required), so the IDs are a contract: they
// must not drift with the heading text. Goldmark heading-attribute syntax
// ({#id}) is enabled by Hugo's default markup config.
func TestWriteLinksMD_SectionHeadingsCarryStableAnchors(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Broken: []linkcheck.Finding{{URL: "https://gone.example/", Detail: "404", Pages: []string{"p"}}},
		Auth:   []linkcheck.Finding{{URL: "https://private.example/", Detail: "401", Pages: []string{"p"}}},
	}
	if err := WriteLinksMD(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readString(t, filepath.Join(dir, "links.md"))
	for _, want := range []string{
		"## Broken {#broken}",
		"## Auth Required {#auth-required}",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("want %q in:\n%s", want, s)
		}
	}
}

// TestWriteLinksMD_FindingHeadingsCarryAnchors pins that every individual
// finding heading gets an explicit, non-empty `{#slug}` ID derived from its
// URL. Without it, Hugo derives an empty ID for a heading whose entire text
// is a bare URL, and the page's right-hand "On this page" TOC entry for that
// finding links to `#` instead of the finding.
func TestWriteLinksMD_FindingHeadingsCarryAnchors(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Broken: []linkcheck.Finding{{URL: "https://gone.example/foo", Detail: "404", Pages: []string{"p"}}},
	}
	if err := WriteLinksMD(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readString(t, filepath.Join(dir, "links.md"))
	want := "### https://gone.example/foo {#https-gone-example-foo}"
	if !strings.Contains(s, want) {
		t.Fatalf("want %q in:\n%s", want, s)
	}
}

// TestWriteLinksMD_FindingAnchorsAreUnique pins that two findings whose URLs
// slugify to the same base get distinct IDs, so each TOC entry lands on the
// correct finding rather than both jumping to the first.
func TestWriteLinksMD_FindingAnchorsAreUnique(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Broken: []linkcheck.Finding{
			{URL: "https://x.example/a?b", Detail: "404", Pages: []string{"p"}},
			{URL: "https://x.example/a/b", Detail: "404", Pages: []string{"p"}},
		},
	}
	if err := WriteLinksMD(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readString(t, filepath.Join(dir, "links.md"))
	if !strings.Contains(s, "{#https-x-example-a-b}") {
		t.Fatalf("want base anchor in:\n%s", s)
	}
	if !strings.Contains(s, "{#https-x-example-a-b-2}") {
		t.Fatalf("want disambiguated anchor for colliding slug in:\n%s", s)
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
