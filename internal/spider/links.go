package spider

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	mdLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	bareURLRe = regexp.MustCompile(`(?:^|[\s(>])(https?://[^\s)<>"']+)`)
)

// ExtractLinks parses same-host links from markdown, resolving relative URLs
// against pageURL. Returns deduplicated absolute URL strings, fragments stripped.
func ExtractLinks(markdown string, pageURL *url.URL) []string {
	seen := make(map[string]bool)
	var links []string

	add := func(raw string) {
		raw = strings.TrimRight(raw, ".,;!?)")
		ref, err := url.Parse(raw)
		if err != nil {
			return
		}
		// Drop pure fragment references (e.g., "#anchor") — they point within the same page.
		if ref.Scheme == "" && ref.Host == "" && ref.Path == "" && ref.Fragment != "" {
			return
		}
		resolved := pageURL.ResolveReference(ref)
		if resolved.Scheme == "mailto" || resolved.Host == "" {
			return
		}
		if resolved.Host != pageURL.Host {
			return
		}
		resolved.Fragment = ""
		abs := resolved.String()
		if !seen[abs] {
			seen[abs] = true
			links = append(links, abs)
		}
	}

	for _, m := range mdLinkRe.FindAllStringSubmatch(markdown, -1) {
		add(m[2])
	}
	for _, m := range bareURLRe.FindAllStringSubmatch(markdown, -1) {
		add(m[1])
	}

	return links
}
