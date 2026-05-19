package linkcheck

import (
	"net/url"
	"reflect"
	"sort"
	"testing"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	return u
}

func TestExtract_PullsBothMarkdownAndBareURLs(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/intro/")
	md := "See [the guide](/guide/) and [Cobra](https://github.com/spf13/cobra).\n" +
		"Inline: https://pkg.go.dev/net/http\n"

	got := Extract(md, page)
	sort.Strings(got)
	want := []string{
		"https://docs.example.com/guide/",
		"https://github.com/spf13/cobra",
		"https://pkg.go.dev/net/http",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtract_SkipsNonHTTPSchemes(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/")
	md := "[email me](mailto:x@y.com) and [call](tel:+15555550100) and " +
		"[js](javascript:void(0)) and [data](data:text/plain,hello)"

	got := Extract(md, page)
	if len(got) != 0 {
		t.Fatalf("expected no URLs, got %v", got)
	}
}

func TestExtract_SkipsLocalhostAndRFC1918(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/")
	md := "[a](http://localhost:8080/x) [b](http://127.0.0.1/) " +
		"[c](http://10.0.0.1/) [d](http://192.168.1.1/) [e](http://172.16.0.1/)"

	got := Extract(md, page)
	if len(got) != 0 {
		t.Fatalf("expected all private addresses skipped, got %v", got)
	}
}

func TestExtract_StripsFragments(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/")
	md := "[a](https://x.example.com/page#section-a) " +
		"[b](https://x.example.com/page#section-b)"

	got := Extract(md, page)
	if len(got) != 1 || got[0] != "https://x.example.com/page" {
		t.Fatalf("expected single dedup'd URL, got %v", got)
	}
}

func TestExtract_DedupesAcrossMarkdownAndBareForms(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/")
	md := "[github](https://github.com/x/y) — also: https://github.com/x/y."

	got := Extract(md, page)
	if len(got) != 1 || got[0] != "https://github.com/x/y" {
		t.Fatalf("expected single dedup'd URL, got %v", got)
	}
}

func TestExtract_ResolvesRelativeReferences(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/sub/")
	md := "[rel](../other/) [abs-path](/root/) [abs-url](https://other.example.com/x)"

	got := Extract(md, page)
	sort.Strings(got)
	want := []string{
		"https://docs.example.com/other/",
		"https://docs.example.com/root/",
		"https://other.example.com/x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
