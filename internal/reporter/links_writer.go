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

	if len(rep.Broken) > 0 {
		b.WriteString("## Broken\n\n")
		for _, f := range rep.Broken {
			writeFinding(&b, f)
		}
	}
	if len(rep.Auth) > 0 {
		b.WriteString("## Auth Required\n\n")
		for _, f := range rep.Auth {
			writeFinding(&b, f)
		}
	}

	return os.WriteFile(filepath.Join(dir, "links.md"), []byte(b.String()), 0o644)
}

func writeFinding(b *strings.Builder, f linkcheck.Finding) {
	fmt.Fprintf(b, "### %s\n\n", f.URL)
	if f.Detail != "" {
		fmt.Fprintf(b, "**Reason:** %s\n\n", f.Detail)
	}
	b.WriteString("**Pages:**\n\n")
	for _, p := range f.Pages {
		fmt.Fprintf(b, "- %s\n", p)
	}
	b.WriteString("\n")
}
