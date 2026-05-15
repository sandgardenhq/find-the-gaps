package analyzer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestSynthesizeProduct_ReturnsDescriptionAndFeatures(t *testing.T) {
	c := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"synthesize_response": json.RawMessage(`{"description":"A CLI for doc gap detection.","features":["gap analysis","doctor command","Homebrew install"]}`),
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
	if len(c.jsonSchemas) != 1 || c.jsonSchemas[0].Name != "synthesize_response" {
		t.Errorf("expected CompleteJSON with synthesize_response schema, got %+v", c.jsonSchemas)
	}
}

func TestSynthesizeProduct_SinglePage_OK(t *testing.T) {
	c := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"synthesize_response": json.RawMessage(`{"description":"One page product.","features":["one feature"]}`),
	}}
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

func TestSynthesizeProduct_UsesSmallTier(t *testing.T) {
	small := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"synthesize_response": json.RawMessage(`{"description":"Small tier used.","features":["x"]}`),
	}}
	typical := &fakeClient{}
	large := &fakeClient{}

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

// makeFakePageAnalyses builds n PageAnalysis values whose Summary fields each
// contain ~600 tokens of structured markdown so the chunker has real boundaries
// to slice at. Caller can pass these to SynthesizeProductForTest to exercise
// the single-pass vs map-reduce switch.
func makeFakePageAnalyses(n int) []analyzer.PageAnalysis {
	out := make([]analyzer.PageAnalysis, n)
	// ~600 tokens per summary (the repeat block expands to many sentences).
	body := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta iota kappa. ", 60)
	for i := 0; i < n; i++ {
		out[i] = analyzer.PageAnalysis{
			URL:      fmt.Sprintf("https://example.com/p%d", i),
			Summary:  body,
			Features: []string{fmt.Sprintf("Feature%d", i)},
			IsDocs:   true,
			Role:     "reference",
		}
	}
	return out
}

// TestSynthesize_SinglePass_OnSmallCorpus pins the cheap path: a small set of
// pages whose compressed summaries fit the synthesize budget must produce
// exactly one LLM call (no map-reduce reduction step).
func TestSynthesize_SinglePass_OnSmallCorpus(t *testing.T) {
	pages := makeFakePageAnalyses(20)
	c := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"synthesize_response":         json.RawMessage(`{"description":"P","features":["x"]}`),
		"synthesize_reduce_response":  json.RawMessage(`{"description":"P","features":["x"]}`),
	}}

	_, err := analyzer.SynthesizeProductForTest(context.Background(),
		&fakeTiering{small: c}, pages, 80_000)
	if err != nil {
		t.Fatalf("SynthesizeProductForTest: %v", err)
	}
	if c.callCount != 1 {
		t.Fatalf("expected single-pass for small corpus, got %d calls", c.callCount)
	}
}

// TestSynthesize_MapReduces_OnLargeCorpus pins the fallback: when even the
// compressed page summaries overflow the synthesize budget, the function must
// split pages into groups, summarize each, and reduce the partials. We expect
// >= 2 LLM calls (>=1 group call + >=1 reduce call).
func TestSynthesize_MapReduces_OnLargeCorpus(t *testing.T) {
	// 500 pages * 200-token compressed summaries + entry overhead vastly
	// exceeds a 5K-token budget. We use 5K (not 80K) so the test stays fast
	// while still exercising the same map-reduce code path.
	pages := makeFakePageAnalyses(500)
	c := &fakeClient{jsonResponseQueues: map[string][]json.RawMessage{
		"synthesize_response": {
			json.RawMessage(`{"description":"P1","features":["a"]}`),
			json.RawMessage(`{"description":"P2","features":["b"]}`),
			json.RawMessage(`{"description":"P3","features":["c"]}`),
			json.RawMessage(`{"description":"P4","features":["d"]}`),
			json.RawMessage(`{"description":"P5","features":["e"]}`),
			json.RawMessage(`{"description":"P6","features":["f"]}`),
			json.RawMessage(`{"description":"P7","features":["g"]}`),
			json.RawMessage(`{"description":"P8","features":["h"]}`),
		},
		"synthesize_reduce_response": {
			json.RawMessage(`{"description":"merged","features":["a","b","c","d","e","f","g","h"]}`),
		},
	}}

	_, err := analyzer.SynthesizeProductForTest(context.Background(),
		&fakeTiering{small: c}, pages, 5_000)
	if err != nil {
		t.Fatalf("SynthesizeProductForTest: %v", err)
	}
	if c.callCount < 2 {
		t.Fatalf("expected map-reduce (>=2 calls), got %d", c.callCount)
	}
}
