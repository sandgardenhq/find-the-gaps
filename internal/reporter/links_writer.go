package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

// WriteLinksMD renders rep to <dir>/links.md. Empty buckets are omitted.
// When every bucket is empty, the file still renders with a leading H1 and
// an explicit "no dead links" line so the site/PDF surfaces have content
// to embed.
func WriteLinksMD(dir string, rep linkcheck.Report) error {
	var b strings.Builder
	b.WriteString("# Dead Links\n\n")

	total := len(rep.Broken) + len(rep.Auth)
	if total == 0 {
		b.WriteString("_No dead links detected._\n")
		return os.WriteFile(filepath.Join(dir, "links.md"), []byte(b.String()), 0o644)
	}

	// Pre-seed the section anchors so a finding URL that happens to slugify
	// to "broken" / "auth-required" can never shadow a section heading.
	used := map[string]bool{"broken": true, "auth-required": true}

	if len(rep.Broken) > 0 {
		b.WriteString("## Broken {#broken}\n\n")
		for _, f := range rep.Broken {
			writeFinding(&b, f, uniqueAnchor(used, urlAnchor(f.URL)))
		}
	}
	if len(rep.Auth) > 0 {
		b.WriteString("## Auth Required {#auth-required}\n\n")
		for _, f := range rep.Auth {
			writeFinding(&b, f, uniqueAnchor(used, urlAnchor(f.URL)))
		}
	}

	return os.WriteFile(filepath.Join(dir, "links.md"), []byte(b.String()), 0o644)
}

// writeFinding renders one finding. id is an explicit Goldmark heading anchor
// ({#id}); a bare-URL heading otherwise yields an empty Hugo ID, which breaks
// the page's "On this page" TOC link for that finding.
func writeFinding(b *strings.Builder, f linkcheck.Finding, id string) {
	fmt.Fprintf(b, "### %s {#%s}\n\n", f.URL, id)
	if f.Detail != "" {
		fmt.Fprintf(b, "**Reason:** %s\n\n", f.Detail)
	}
	b.WriteString("**Pages:**\n\n")
	for _, p := range f.Pages {
		fmt.Fprintf(b, "- %s\n", p)
	}
	b.WriteString("\n")
}

// uniqueAnchor returns base if unused, otherwise base-2, base-3, ... so every
// finding on the page gets a distinct anchor. An empty base (urlAnchor returns
// "" for an all-punctuation URL) falls back to "link" so the anchor is never
// empty. Deterministic for a given input order (the report buckets are
// pre-sorted), so cold/warm and parallel/serial runs produce byte-identical
// output.
func uniqueAnchor(used map[string]bool, base string) string {
	if base == "" {
		base = "link"
	}
	cand := base
	for i := 2; used[cand]; i++ {
		cand = fmt.Sprintf("%s-%d", base, i)
	}
	used[cand] = true
	return cand
}
