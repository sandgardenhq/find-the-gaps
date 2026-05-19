package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

func TestWriteLinksJSON_AlwaysWritesAllThreeKeys(t *testing.T) {
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
	for _, key := range []string{"broken", "auth_required", "redirected"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(b))
		}
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
		Redirected: []linkcheck.Finding{{
			URL:         "https://old/x",
			FinalURL:    "https://new/x",
			StatusChain: []int{301, 200},
			Detail:      "redirected",
			Pages:       []string{"p1"},
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
		`"final_url": "https://new/x"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q in:\n%s", want, s)
		}
	}
}
