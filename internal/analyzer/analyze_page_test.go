package analyzer_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestAnalyzePage_ExtractsSummaryAndFeatures(t *testing.T) {
	c := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"analyze_page_response": json.RawMessage(`{"summary":"Covers Homebrew install.","features":["Homebrew install","go install"]}`),
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
	if len(c.receivedPrompts) == 0 {
		t.Fatal("expected at least one prompt to be sent")
	}
	if !strings.Contains(c.receivedPrompts[0], "https://docs.example.com/install") {
		t.Errorf("prompt must contain the page URL, got: %s", c.receivedPrompts[0][:100])
	}
	if len(c.jsonSchemas) != 1 || c.jsonSchemas[0].Name != "analyze_page_response" {
		t.Errorf("expected CompleteJSON with analyze_page_response schema, got %+v", c.jsonSchemas)
	}
}

func TestAnalyzePage_EmptyFeatures_OK(t *testing.T) {
	c := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"analyze_page_response": json.RawMessage(`{"summary":"A page.","features":[]}`),
	}}
	got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://example.com", "content")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Features) != 0 {
		t.Errorf("expected empty features, got %v", got.Features)
	}
	if got.Features == nil {
		t.Error("Features must be a non-nil empty slice, not nil")
	}
}

func TestAnalyzePage_ClientError_Propagates(t *testing.T) {
	c := &fakeClient{forcedErr: errors.New("timeout")}
	_, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c}, "https://example.com", "content")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnalyzePage_UsesSmallTier(t *testing.T) {
	small := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"analyze_page_response": json.RawMessage(`{"summary":"Small tier used.","features":["x"]}`),
	}}
	typical := &fakeClient{}
	large := &fakeClient{}

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
