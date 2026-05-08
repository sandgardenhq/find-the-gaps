package forge

import (
	"fmt"
	"net/url"
	"strings"
)

// URL represents a parsed forge URL.
type URL struct {
	Host    string // lowercased
	Owner   string
	Repo    string // .git suffix stripped
	Ref     string // branch or tag from /tree/<ref>/... or /blob/<ref>/...
	Subpath string // path under <ref>; empty for repo root
	IsWiki  bool   // true when the URL points at /<owner>/<repo>/wiki
}

// ParseURL parses a forge URL into its constituent parts. Returns an error if
// the URL does not name at least an owner and repo.
func ParseURL(raw string) (URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return URL{}, fmt.Errorf("parse url: %w", err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return URL{}, fmt.Errorf("forge url %q: missing <owner>/<repo>", raw)
	}
	out := URL{
		Host:  strings.ToLower(u.Host),
		Owner: parts[0],
		Repo:  strings.TrimSuffix(parts[1], ".git"),
	}
	if len(parts) >= 3 && parts[2] == "wiki" {
		out.IsWiki = true
		return out, nil
	}
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		out.Ref = parts[3]
		if len(parts) > 4 {
			out.Subpath = strings.Join(parts[4:], "/")
		}
	}
	return out, nil
}
