package analyzer

import "testing"

func TestPageRole(t *testing.T) {
	cases := []struct {
		url, want string
	}{
		{"https://example.com/", "top-nav"},
		{"https://example.com/readme/", "readme"},
		{"https://example.com/docs/quickstart", "quickstart"},
		{"https://example.com/docs/getting-started/", "quickstart"},
		{"https://example.com/docs/getting_started", "quickstart"},
		{"https://example.com/docs/", "top-nav"},
		{"https://example.com/docs/api", "top-nav"},
		{"https://example.com/docs/api/auth/", "reference"},
		{"https://example.com/docs/api/auth/oauth/flows/code", "deep"},
		{"not a url", "unknown"},
	}
	for _, c := range cases {
		if got := pageRole(c.url); got != c.want {
			t.Errorf("pageRole(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}
