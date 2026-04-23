package analyzer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestSynthesizeProduct_ReturnsDescriptionAndFeatures(t *testing.T) {
	c := &fakeClient{responses: []string{
		`{"description":"A CLI for doc gap detection.","features":["gap analysis","doctor command","Homebrew install"]}`,
	}}

	pages := []analyzer.PageAnalysis{
		{URL: "https://example.com/install", Summary: "Covers install.", Features: []string{"Homebrew install"}},
		{URL: "https://example.com/usage", Summary: "Covers usage.", Features: []string{"gap analysis", "doctor command"}},
	}

	got, err := analyzer.SynthesizeProduct(context.Background(), &fakeTiering{small: c}, pages)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description == "" {
		t.Error("Description must not be empty")
	}
	if len(got.Features) == 0 {
		t.Error("Features must not be empty")
	}
}

func TestSynthesizeProduct_SinglePage_OK(t *testing.T) {
	c := &fakeClient{responses: []string{`{"description":"One page product.","features":["one feature"]}`}}
	pages := []analyzer.PageAnalysis{{URL: "https://example.com", Summary: "One page.", Features: []string{"one feature"}}}
	_, err := analyzer.SynthesizeProduct(context.Background(), &fakeTiering{small: c}, pages)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSynthesizeProduct_ClientError_Propagates(t *testing.T) {
	c := &fakeClient{forcedErr: errors.New("network down")}
	_, err := analyzer.SynthesizeProduct(context.Background(), &fakeTiering{small: c}, []analyzer.PageAnalysis{
		{URL: "https://example.com", Summary: "page.", Features: nil},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSynthesizeProduct_InvalidJSON_ReturnsError(t *testing.T) {
	c := &fakeClient{responses: []string{"oops"}}
	_, err := analyzer.SynthesizeProduct(context.Background(), &fakeTiering{small: c}, []analyzer.PageAnalysis{
		{URL: "https://example.com", Summary: "page.", Features: nil},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSynthesizeProduct_NilFeatures_NormalizedToEmpty(t *testing.T) {
	// LLM omits the "features" key entirely; the nil slice must be normalized to [].
	c := &fakeClient{responses: []string{`{"description":"A product."}`}}
	pages := []analyzer.PageAnalysis{{URL: "https://example.com", Summary: "page.", Features: nil}}
	got, err := analyzer.SynthesizeProduct(context.Background(), &fakeTiering{small: c}, pages)
	if err != nil {
		t.Fatal(err)
	}
	if got.Features == nil {
		t.Error("Features must be normalized to empty slice, not nil")
	}
}

func TestSynthesizeProduct_UsesSmallTier(t *testing.T) {
	small := &fakeClient{responses: []string{`{"description":"Small tier used.","features":["x"]}`}}
	typical := &fakeClient{responses: []string{`{"description":"Typical tier used.","features":["y"]}`}}
	large := &fakeClient{responses: []string{`{"description":"Large tier used.","features":["z"]}`}}

	tiering := &fakeTiering{small: small, typical: typical, large: large}

	pages := []analyzer.PageAnalysis{
		{URL: "https://example.com", Summary: "A page.", Features: []string{"x"}},
	}

	_, err := analyzer.SynthesizeProduct(context.Background(), tiering, pages)
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
