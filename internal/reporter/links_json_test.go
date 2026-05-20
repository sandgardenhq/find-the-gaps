package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

func TestWriteLinksJSON_AlwaysWritesBothKeys(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLinksJSON(dir, linkcheck.Report{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "links.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, key := range []string{"broken", "auth_required"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(b))
		}
	}
	if _, ok := got["redirected"]; ok {
		t.Fatalf("unexpected key 'redirected' in %s", string(b))
	}
}

func TestWriteLinksJSON_PopulatedFields(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Broken: []linkcheck.Finding{{
			URL:         "https://gone.example/",
			ErrorType:   "http_404",
			Detail:      "HTTP 404 Not Found",
			StatusChain: []int{404},
			Pages:       []string{"p1", "p2"},
		}},
	}
	if err := WriteLinksJSON(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "links.json"))
	s := string(b)
	for _, want := range []string{
		`"url": "https://gone.example/"`,
		`"error_type": "http_404"`,
		`"status_chain":`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q in:\n%s", want, s)
		}
	}
}

// TestReadLinksJSON_RoundTrip pins that ReadLinksJSON loads back the same
// shape WriteLinksJSON wrote. Render relies on this to re-emit the Dead
// Links section in report.pdf without rerunning the link probes.
func TestReadLinksJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Broken: []linkcheck.Finding{{
			URL: "https://gone.example/", ErrorType: "http_404",
			Detail: "HTTP 404", StatusChain: []int{404},
			Pages: []string{"p1", "p2"},
		}},
		Auth: []linkcheck.Finding{{
			URL: "https://locked.example/", Detail: "HTTP 401",
			Pages: []string{"p1"},
		}},
	}
	if err := WriteLinksJSON(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok, err := ReadLinksJSON(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true for a populated links.json")
	}
	if len(got.Broken) != 1 || got.Broken[0].URL != "https://gone.example/" {
		t.Fatalf("broken mismatch: %+v", got.Broken)
	}
	if len(got.Auth) != 1 || got.Auth[0].URL != "https://locked.example/" {
		t.Fatalf("auth mismatch: %+v", got.Auth)
	}
}

// TestReadLinksJSON_MissingFile returns ok=false (no error) when links.json
// is absent — callers treat that as "the link check did not run for this
// project" and render with an empty Dead Links report.
func TestReadLinksJSON_MissingFile(t *testing.T) {
	got, ok, err := ReadLinksJSON(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for missing links.json")
	}
	if len(got.Broken)+len(got.Auth) != 0 {
		t.Fatalf("expected zero-value report, got %+v", got)
	}
}
