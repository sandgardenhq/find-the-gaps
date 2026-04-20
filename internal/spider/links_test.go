package spider

import (
	"net/url"
	"testing"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

func TestExtractLinks_absoluteMarkdownLinks_sameHost(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[API](https://docs.example.com/api) [ext](https://other.com/page)`
	links := ExtractLinks(md, base)
	if len(links) != 1 || links[0] != "https://docs.example.com/api" {
		t.Errorf("got %v, want [https://docs.example.com/api]", links)
	}
}

func TestExtractLinks_relativeLinks_resolvedAgainstBase(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[ref](/reference)`
	links := ExtractLinks(md, base)
	if len(links) != 1 || links[0] != "https://docs.example.com/reference" {
		t.Errorf("got %v, want [https://docs.example.com/reference]", links)
	}
}

func TestExtractLinks_fragmentOnly_dropped(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[section](#anchor)`
	links := ExtractLinks(md, base)
	if len(links) != 0 {
		t.Errorf("expected no links, got %v", links)
	}
}

func TestExtractLinks_mailto_dropped(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[email](mailto:foo@bar.com)`
	links := ExtractLinks(md, base)
	if len(links) != 0 {
		t.Errorf("expected no links, got %v", links)
	}
}

func TestExtractLinks_deduplicatesWithinPage(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[a](https://docs.example.com/api) [b](https://docs.example.com/api)`
	links := ExtractLinks(md, base)
	if len(links) != 1 {
		t.Errorf("expected 1 link after dedup, got %v", links)
	}
}

func TestExtractLinks_bareURLs_sameHost(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `See https://docs.example.com/guide for more.`
	links := ExtractLinks(md, base)
	found := false
	for _, l := range links {
		if l == "https://docs.example.com/guide" {
			found = true
		}
	}
	if !found {
		t.Errorf("bare URL not found in %v", links)
	}
}

func TestExtractLinks_fragmentStrippedFromAbsoluteLink(t *testing.T) {
	base := mustParse(t, "https://docs.example.com/intro")
	md := `[ref](https://docs.example.com/api#section)`
	links := ExtractLinks(md, base)
	if len(links) != 1 || links[0] != "https://docs.example.com/api" {
		t.Errorf("got %v, want [https://docs.example.com/api]", links)
	}
}
