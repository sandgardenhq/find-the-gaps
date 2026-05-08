package forge

import "testing"

func TestIsForgeHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"github.com", true},
		{"www.github.com", true},
		{"gitlab.com", true},
		{"bitbucket.org", true},
		{"codeberg.org", true},
		{"git.sr.ht", true},
		{"GitHub.com", true}, // case-insensitive
		{"example.com", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			if got := IsForgeHost(tc.host); got != tc.want {
				t.Fatalf("IsForgeHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}
