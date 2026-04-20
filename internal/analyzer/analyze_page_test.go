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

	got, err := analyzer.AnalyzePage(context.Background(), c, "https://docs.example.com/install", "# Install\nUse brew.")
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
	got, err := analyzer.AnalyzePage(context.Background(), c, "https://example.com", "content")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Features) != 0 {
		t.Errorf("expected empty features, got %v", got.Features)
	}
}

func TestAnalyzePage_ClientError_Propagates(t *testing.T) {
	c := &fakeClient{forcedErr: errors.New("timeout")}
	_, err := analyzer.AnalyzePage(context.Background(), c, "https://example.com", "content")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnalyzePage_InvalidJSON_ReturnsError(t *testing.T) {
	c := &fakeClient{responses: []string{"not json"}}
	_, err := analyzer.AnalyzePage(context.Background(), c, "https://example.com", "content")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestAnalyzePage_MissingFeaturesKey_ReturnsEmptySlice(t *testing.T) {
	c := &fakeClient{responses: []string{`{"summary":"A page with no features key."}`}}
	got, err := analyzer.AnalyzePage(context.Background(), c, "https://example.com", "content")
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
