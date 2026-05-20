package pdf

import (
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

func TestDeadLinks_RenderedWhenNonEmpty(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.pdf")
	in := Inputs{
		ProjectName: "test",
		DeadLinks: linkcheck.Report{
			Broken: []linkcheck.Finding{{
				URL:       "https://gone.example/",
				ErrorType: "http_404",
				Detail:    "HTTP 404 Not Found",
				Pages:     []string{"https://docs.example/a"},
			}},
			Auth: []linkcheck.Finding{{
				URL:    "https://private.example/",
				Detail: "HTTP 401 Unauthorized",
				Pages:  []string{"https://docs.example/a"},
			}},
		},
	}
	if err := WriteReport(dir, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	// TOC must include the Dead Links entry. We assert via collectTOCEntries
	// rather than the rendered byte stream — fpdf compresses content streams,
	// so substring searches on the binary file are unreliable.
	entries := collectTOCEntries(in)
	var sawTop, sawBroken, sawAuth bool
	for _, e := range entries {
		switch e.Label {
		case "Dead Links":
			if e.Depth == 0 {
				sawTop = true
			}
		case "Broken":
			if e.Depth == 1 && e.Anchor == "deadlinks-broken" {
				sawBroken = true
			}
		case "Auth Required":
			if e.Depth == 1 && e.Anchor == "deadlinks-auth" {
				sawAuth = true
			}
		}
	}
	if !sawTop || !sawBroken || !sawAuth {
		t.Fatalf("TOC entries missing: top=%v broken=%v auth=%v",
			sawTop, sawBroken, sawAuth)
	}
	if _, err := filepath.Abs(out); err != nil {
		t.Fatalf("abs: %v", err)
	}
}

func TestDeadLinks_OmittedWhenAllBucketsEmpty(t *testing.T) {
	in := Inputs{ProjectName: "test"}
	entries := collectTOCEntries(in)
	for _, e := range entries {
		if e.Label == "Dead Links" {
			t.Fatalf("Dead Links entry should not appear in empty TOC; got %+v", e)
		}
	}
	// Render must succeed and not panic when DeadLinks is zero.
	if err := WriteReport(t.TempDir(), in); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestDeadLinks_OmitsEmptyBucketEntriesInTOC(t *testing.T) {
	in := Inputs{
		ProjectName: "test",
		DeadLinks: linkcheck.Report{
			Auth: []linkcheck.Finding{{URL: "https://x/", Pages: []string{"p1"}}},
		},
	}
	entries := collectTOCEntries(in)
	for _, e := range entries {
		if e.Anchor == "deadlinks-broken" {
			t.Fatalf("empty bucket anchored in TOC: %+v", e)
		}
	}
}
