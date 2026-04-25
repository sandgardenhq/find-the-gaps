// internal/site/slug_test.go
package site

import "testing"

func TestFeatureSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Simple Name", "simple-name"},
		{"  Trim Edges  ", "trim-edges"},
		{"Mixed CASE", "mixed-case"},
		{"Multiple   Spaces", "multiple-spaces"},
		{"Punctuation!?, here.", "punctuation-here"},
		{"Hyphen--Run", "hyphen-run"},
		{"unicode café", "unicode-cafe"},
		{"123 numbers ok", "123-numbers-ok"},
		{"", ""},
		{"!!!", ""},
	}
	for _, c := range cases {
		got := featureSlug(c.in)
		if got != c.want {
			t.Errorf("featureSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
