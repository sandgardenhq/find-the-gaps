package forge

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Remote is a normalized git remote URL.
type Remote struct {
	Host  string // lowercased
	Owner string
	Repo  string
}

var sshRemoteRe = regexp.MustCompile(`^(?:[^@]+@)?([^:]+):([^/]+)/(.+?)(?:\.git)?$`)

// NormalizeRemote parses an HTTPS or SSH git remote URL into (host, owner, repo).
// Strips a trailing ".git" suffix. Returns an error if raw is not a recognized
// remote shape.
func NormalizeRemote(raw string) (Remote, error) {
	if raw == "" {
		return Remote{}, fmt.Errorf("empty remote")
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "ssh://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Remote{}, fmt.Errorf("parse remote: %w", err)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return Remote{}, fmt.Errorf("remote %q: missing owner/repo", raw)
		}
		return Remote{
			Host:  strings.ToLower(u.Host),
			Owner: parts[0],
			Repo:  strings.TrimSuffix(parts[1], ".git"),
		}, nil
	}
	// scp-style: git@host:owner/repo[.git]
	if m := sshRemoteRe.FindStringSubmatch(raw); m != nil {
		return Remote{
			Host:  strings.ToLower(m[1]),
			Owner: m[2],
			Repo:  strings.TrimSuffix(m[3], ".git"),
		}, nil
	}
	return Remote{}, fmt.Errorf("unrecognized remote shape: %q", raw)
}
