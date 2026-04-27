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

func TestResolveSlugs(t *testing.T) {
	in := []string{"Foo", "foo", "Bar", "FOO", "foo!"}
	got := resolveSlugs(in)
	want := []string{"foo", "foo-2", "bar", "foo-3", "foo-4"}
	for i := range in {
		if got[in[i]] != want[i] {
			t.Errorf("resolveSlugs[%d] %q = %q, want %q", i, in[i], got[in[i]], want[i])
		}
	}
}

func TestResolveSlugsDeterministic(t *testing.T) {
	// Same inputs in same order must produce same output.
	a := resolveSlugs([]string{"Alpha", "alpha", "ALPHA"})
	b := resolveSlugs([]string{"Alpha", "alpha", "ALPHA"})
	for k, v := range a {
		if b[k] != v {
			t.Errorf("non-deterministic: %q got %q vs %q", k, v, b[k])
		}
	}
}

func TestResolveSlugsAvoidsLiteralSuffixCollision(t *testing.T) {
	// A literal name that slugifies to a value matching a previously-emitted
	// collision suffix must not collapse onto the same slug. With ["Foo", "FOO",
	// "foo-2"], a base-only counter would assign "foo-2" to BOTH "FOO" (via the
	// -2 collision suffix) and "foo-2" (literal), causing two features to write
	// to the same features/foo-2.md. Each input name must map to a unique slug.
	in := []string{"Foo", "FOO", "foo-2"}
	got := resolveSlugs(in)
	owner := make(map[string]string)
	for _, n := range in {
		slug := got[n]
		if other, taken := owner[slug]; taken {
			t.Fatalf("slug collision: %q and %q both -> %q (got %v)", other, n, slug, got)
		}
		owner[slug] = n
	}
}
