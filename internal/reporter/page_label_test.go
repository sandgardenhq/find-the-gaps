package reporter_test

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
)

// TestPageLabelFromURL pins how docs page URLs are turned into human-
// readable card headings. The screenshots page used to repeat the URL as
// a heading above each card; the heading is now a derived label so the
// card has a "title" while the URL stays accessible as a link inside the
// card body.
//
// Rules:
//   - Last meaningful path segment, with `-`/`_` swapped for spaces and the
//     first letter upper-cased ("getting-started" → "Getting started").
//   - Trailing slash is ignored.
//   - File extensions like `.html`, `.md`, `.htm` are stripped from the last
//     segment so `installation.html` → "Installation".
//   - Empty path / root URL falls back to the host.
//   - Unparseable strings round-trip unchanged.
func TestPageLabelFromURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://docs.example.com/getting-started/installation/", "Installation"},
		{"https://docs.example.com/api/users", "Users"},
		{"https://docs.example.com/quickstart", "Quickstart"},
		{"https://example.com/docs/start", "Start"},
		{"https://example.com/auth-tokens", "Auth tokens"},
		{"https://example.com/setup_guide", "Setup guide"},
		{"https://example.com/docs/installation.html", "Installation"},
		{"https://example.com/docs/intro.md", "Intro"},
		{"https://example.com/", "example.com"},
		{"https://example.com", "example.com"},
		{"", ""},
		{"not a url", "not a url"},
	}
	for _, tc := range cases {
		got := reporter.PageLabelFromURL(tc.in)
		if got != tc.want {
			t.Errorf("PageLabelFromURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
