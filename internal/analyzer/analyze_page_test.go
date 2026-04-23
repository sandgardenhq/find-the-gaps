package analyzer_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestAnalyzePage_ExtractsSummaryAndFeatures(t *testing.T) {
	c := &fakeClient{responses: []string{
		`{"summary":"Covers Homebrew install.","features":["Homebrew install","go install"]}`,
	}}

	got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://docs.example.com/install", "# Install\nUse brew.")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://docs.example.com/install" {
		t.Errorf("URL: got %q", got.URL)
	}
	if got.Summary != "Covers Homebrew install." {
		t.Errorf("Summary: got %q", got.Summary)
	}
	if len(got.Features) != 2 || got.Features[0] != "Homebrew install" {
		t.Errorf("Features: got %v", got.Features)
	}
	// After the existing assertions, add:
	if len(c.receivedPrompts) == 0 {
		t.Fatal("expected at least one prompt to be sent")
	}
	if !strings.Contains(c.receivedPrompts[0], "https://docs.example.com/install") {
		t.Errorf("prompt must contain the page URL, got: %s", c.receivedPrompts[0][:100])
	}
}

func TestAnalyzePage_EmptyFeatures_OK(t *testing.T) {
	c := &fakeClient{responses: []string{`{"summary":"A page.","features":[]}`}}
	got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://example.com", "content")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Features) != 0 {
		t.Errorf("expected empty features, got %v", got.Features)
	}
}

func TestAnalyzePage_ClientError_Propagates(t *testing.T) {
	c := &fakeClient{forcedErr: errors.New("timeout")}
	_, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://example.com", "content")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnalyzePage_InvalidJSON_ReturnsError(t *testing.T) {
	c := &fakeClient{responses: []string{"not json"}}
	_, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://example.com", "content")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestAnalyzePage_StripsMarkdownCodeFence(t *testing.T) {
	fenced := "```json\n{\"summary\":\"Fenced.\",\"features\":[\"a\"]}\n```"
	c := &fakeClient{responses: []string{fenced}}
	got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://example.com", "content")
	if err != nil {
		t.Fatalf("expected fenced JSON to parse, got error: %v", err)
	}
	if got.Summary != "Fenced." {
		t.Errorf("Summary: got %q, want %q", got.Summary, "Fenced.")
	}
	if len(got.Features) != 1 || got.Features[0] != "a" {
		t.Errorf("Features: got %v", got.Features)
	}
}

func TestAnalyzePage_StripsBareCodeFence(t *testing.T) {
	fenced := "```\n{\"summary\":\"Bare.\",\"features\":[]}\n```"
	c := &fakeClient{responses: []string{fenced}}
	got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://example.com", "content")
	if err != nil {
		t.Fatalf("expected bare-fenced JSON to parse, got error: %v", err)
	}
	if got.Summary != "Bare." {
		t.Errorf("Summary: got %q", got.Summary)
	}
}

func TestAnalyzePage_MissingFeaturesKey_ReturnsEmptySlice(t *testing.T) {
	c := &fakeClient{responses: []string{`{"summary":"A page with no features key."}`}}
	got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://example.com", "content")
	if err != nil {
		t.Fatal(err)
	}
	if got.Features == nil {
		t.Error("Features must be a non-nil empty slice, not nil")
	}
	if len(got.Features) != 0 {
		t.Errorf("expected 0 features, got %v", got.Features)
	}
}

func TestAnalyzePage_UsesSmallTier(t *testing.T) {
	small := &fakeClient{responses: []string{`{"summary":"Small tier used.","features":["x"]}`}}
	typical := &fakeClient{responses: []string{`{"summary":"Typical tier used.","features":["y"]}`}}
	large := &fakeClient{responses: []string{`{"summary":"Large tier used.","features":["z"]}`}}

	tiering := &fakeTiering{small: small, typical: typical, large: large}

	_, err := analyzer.AnalyzePage(context.Background(), tiering, "https://example.com", "content")
	if err != nil {
		t.Fatal(err)
	}

	if len(small.receivedPrompts) != 1 {
		t.Errorf("expected small tier to receive 1 prompt, got %d", len(small.receivedPrompts))
	}
	if len(typical.receivedPrompts) != 0 {
		t.Errorf("typical tier must not receive prompts, got %d", len(typical.receivedPrompts))
	}
	if len(large.receivedPrompts) != 0 {
		t.Errorf("large tier must not receive prompts, got %d", len(large.receivedPrompts))
	}
}
