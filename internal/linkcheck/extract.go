// Package linkcheck probes every link discovered in the crawled docs site and
// classifies failures so a maintainer can see broken, auth-walled, and
// redirected URLs at a glance. See .plans/2026-05-19-dead-link-check-design.md.
package linkcheck

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	mdLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	bareURLRe = regexp.MustCompile(`(?:^|[\s(>])(https?://[^\s)<>"']+)`)
)

// Extract returns every absolute HTTP(S) URL referenced by markdown rendered
// at pageURL. Same-host and outbound links are both returned. Fragments are
// stripped before dedupe. URLs whose host is loopback or RFC1918 are skipped,
// as are mailto:/tel:/javascript:/data: schemes.
func Extract(markdown string, pageURL *url.URL) []string {
	seen := make(map[string]bool)
	var links []string

	add := func(raw string) {
		raw = strings.TrimRight(raw, ".,;!?)")
		ref, err := url.Parse(raw)
		if err != nil {
			return
		}
		if ref.Scheme == "" && ref.Host == "" && ref.Path == "" && ref.Fragment != "" {
			return
		}
		resolved := pageURL.ResolveReference(ref)
		switch resolved.Scheme {
		case "http", "https":
			// ok
		default:
			return
		}
		if resolved.Host == "" || isPrivateHost(resolved.Hostname()) {
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

// isPrivateHost reports whether host is loopback, link-local, or RFC1918.
// It also matches the literal hostnames "localhost" and "localhost.localdomain".
func isPrivateHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "localhost.localdomain":
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
