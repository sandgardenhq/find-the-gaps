package analyzer

import (
	"net/url"
	"strings"
)

// pageRole classifies a docs page URL into one of:
//
//	"readme" | "quickstart" | "top-nav" | "reference" | "deep" | "unknown"
//
// purely from the URL string. Used as a prominence hint to the priority-rating
// LLM prompts; never authoritative.
func pageRole(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	low := strings.ToLower(u.Path)
	if strings.Contains(low, "readme") {
		return "readme"
	}
	if strings.Contains(low, "quickstart") ||
		strings.Contains(low, "getting-started") ||
		strings.Contains(low, "getting_started") {
		return "quickstart"
	}
	segs := strings.FieldsFunc(low, func(r rune) bool { return r == '/' })
	switch {
	case len(segs) <= 2:
		return "top-nav"
	case len(segs) >= 5:
		return "deep"
	default:
		return "reference"
	}
}
