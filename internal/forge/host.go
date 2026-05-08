package forge

import "strings"

var forgeHosts = map[string]struct{}{
	"github.com":     {},
	"www.github.com": {},
	"gitlab.com":     {},
	"bitbucket.org":  {},
	"codeberg.org":   {},
	"git.sr.ht":      {},
}

// IsForgeHost reports whether host is a known source-control forge whose URLs
// must not be crawled. Comparison is case-insensitive.
func IsForgeHost(host string) bool {
	_, ok := forgeHosts[strings.ToLower(host)]
	return ok
}
