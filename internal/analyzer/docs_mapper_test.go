package analyzer_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDynamicClient responds based on which URL appears in the prompt.
// responses maps a url substring → the wrapped JSON payload
// (e.g. `{"features":["auth"]}`) to return when that substring appears.
type fakeDynamicClient struct {
	responses map[string]string
}

func (f *fakeDynamicClient) Complete(_ context.Context, prompt string) (string, error) {
	for url, resp := range f.responses {
		if strings.Contains(prompt, url) {
			return resp, nil
		}
	}
	return `{"features":[]}`, nil
}

func (f *fakeDynamicClient) CompleteJSON(_ context.Context, prompt string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	for url, resp := range f.responses {
		if strings.Contains(prompt, url) {
			return json.RawMessage(resp), nil
		}
	}
	return json.RawMessage(`{"features":[]}`), nil
}

func (f *fakeDynamicClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return json.RawMessage(`{"features":[]}`), nil
}

func (f *fakeDynamicClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}

// --- mapPageToFeatures tests ---

func TestMapPageToFeatures_HappyPath(t *testing.T) {
	features := []string{"authentication", "search", "billing"}
	featuresJSON, _ := json.Marshal(features)
	client := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"map_page_response": json.RawMessage(`{"features":["authentication","search"]}`),
	}}

	got, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, &fakeCounter{n: 100},
		features, featuresJSON, 50, 10_000,
		"https://example.com/auth", "This page covers login and search.",
	)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"authentication", "search"}, got)
	require.Len(t, client.jsonSchemas, 1)
	assert.Equal(t, "map_page_response", client.jsonSchemas[0].Name)
}

func TestMapPageToFeatures_EmptyResponse(t *testing.T) {
	features := []string{"authentication"}
	featuresJSON, _ := json.Marshal(features)
	client := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"map_page_response": json.RawMessage(`{"features":[]}`),
	}}

	got, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, &fakeCounter{n: 10},
		features, featuresJSON, 20, 10_000,
		"https://example.com/other", "Unrelated content.",
	)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMapPageToFeatures_InvalidJSON(t *testing.T) {
	features := []string{"authentication"}
	featuresJSON, _ := json.Marshal(features)
	client := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"map_page_response": json.RawMessage(`not json`),
	}}

	_, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, &fakeCounter{n: 10},
		features, featuresJSON, 20, 10_000,
		"https://example.com/page", "content",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestMapPageToFeatures_ContentTruncatedWhenOverBudget(t *testing.T) {
	// Budget: featureTokens(50) + promptOverhead(400) + available(550) = 1000.
	// Content is ~2500 tokens (10k chars), which exceeds available(550), so truncation fires.
	features := []string{"authentication"}
	featuresJSON, _ := json.Marshal(features)
	client := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"map_page_response": json.RawMessage(`{"features":["authentication"]}`),
	}}

	largeContent := strings.Repeat("word ", 2_000) // ~10k chars / ~2500 tokens
	_, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, &fakeCounter{n: 10},
		features, featuresJSON,
		50,    // featureTokens
		1_000, // tokenBudget — forces truncation (available = 550, content ~2500 tokens)
		"https://example.com/big", largeContent,
	)
	require.NoError(t, err)
	// Prompt sent to LLM must be shorter than the raw content.
	require.Len(t, client.receivedPrompts, 1)
	assert.Less(t, len(client.receivedPrompts[0]), len(largeContent))
}

func TestMapPageToFeatures_BudgetTooSmallReturnsEmpty(t *testing.T) {
	// When featureTokens + promptOverhead already exceeds the budget, the function
	// must return empty without calling the LLM.
	features := []string{"authentication"}
	featuresJSON, _ := json.Marshal(features)
	client := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"map_page_response": json.RawMessage(`{"features":["authentication"]}`),
	}}

	// featureTokens(500) + promptOverhead(400) = 900 > tokenBudget(100)
	got, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, &fakeCounter{n: 0},
		features, featuresJSON,
		500, // featureTokens — intentionally large
		100, // tokenBudget — too small to fit even the feature list
		"https://example.com/page", "some content",
	)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Empty(t, client.receivedPrompts, "LLM should not be called when budget is insufficient")
}

// --- MapFeaturesToDocs tests ---

func TestMapFeaturesToDocs_AggregatesAcrossPages(t *testing.T) {
	features := []string{"auth", "search", "billing"}

	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
		return p
	}

	pages := map[string]string{
		"https://example.com/auth":    write("auth.md", "covers auth"),
		"https://example.com/search":  write("search.md", "covers search"),
		"https://example.com/billing": write("billing.md", "covers billing"),
	}

	client := &fakeDynamicClient{responses: map[string]string{
		"https://example.com/auth":    `{"features":["auth"]}`,
		"https://example.com/search":  `{"features":["search"]}`,
		"https://example.com/billing": `{"features":["billing"]}`,
	}}

	fm, err := analyzer.MapFeaturesToDocs(
		context.Background(), &fakeTiering{small: client},
		features, pages, 2, 10_000, nil,
	)
	require.NoError(t, err)
	require.Len(t, fm, 3)

	byFeature := make(map[string][]string)
	for _, e := range fm {
		byFeature[e.Feature] = e.Pages
	}
	assert.Equal(t, []string{"https://example.com/auth"}, byFeature["auth"])
	assert.Equal(t, []string{"https://example.com/search"}, byFeature["search"])
	assert.Equal(t, []string{"https://example.com/billing"}, byFeature["billing"])
}

func TestMapFeaturesToDocs_SkipsMissingFile(t *testing.T) {
	features := []string{"auth"}
	pages := map[string]string{
		"https://example.com/missing": "/tmp/does-not-exist-ftg-test.md",
	}
	client := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"map_page_response": json.RawMessage(`{"features":["auth"]}`),
	}}

	fm, err := analyzer.MapFeaturesToDocs(
		context.Background(), &fakeTiering{small: client},
		features, pages, 1, 10_000, nil,
	)
	require.NoError(t, err)
	require.Len(t, fm, 1)
	assert.Empty(t, fm[0].Pages)
}

func TestMapFeaturesToDocs_EmptyFeatures(t *testing.T) {
	fm, err := analyzer.MapFeaturesToDocs(
		context.Background(), &fakeTiering{},
		[]string{}, map[string]string{"https://x.com": "/tmp/x.md"}, 1, 10_000, nil,
	)
	require.NoError(t, err)
	assert.Empty(t, fm)
}

func TestMapFeaturesToDocs_EmptyPages(t *testing.T) {
	fm, err := analyzer.MapFeaturesToDocs(
		context.Background(), &fakeTiering{},
		[]string{"auth"}, map[string]string{}, 1, 10_000, nil,
	)
	require.NoError(t, err)
	require.Len(t, fm, 1)
	assert.Empty(t, fm[0].Pages)
}

func TestMapFeaturesToDocs_OnPageCalledAfterEachResult(t *testing.T) {
	features := []string{"auth", "search"}
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
		return p
	}
	pages := map[string]string{
		"https://example.com/auth":   write("auth.md", "auth content"),
		"https://example.com/search": write("search.md", "search content"),
	}
	client := &fakeDynamicClient{responses: map[string]string{
		"https://example.com/auth":   `{"features":["auth"]}`,
		"https://example.com/search": `{"features":["search"]}`,
	}}

	var callCount int
	onPage := func(partial analyzer.DocsFeatureMap) error {
		callCount++
		require.Len(t, partial, 2, "partial must always contain all features")
		return nil
	}

	_, err := analyzer.MapFeaturesToDocs(
		context.Background(), &fakeTiering{small: client},
		features, pages, 2, 10_000, onPage,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "onPage should be called once per successfully processed page")
}

func TestMapFeaturesToDocs_OnPageErrorAborts(t *testing.T) {
	features := []string{"auth"}
	dir := t.TempDir()
	p := filepath.Join(dir, "auth.md")
	require.NoError(t, os.WriteFile(p, []byte("auth content"), 0o644))

	client := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"map_page_response": json.RawMessage(`{"features":["auth"]}`),
	}}
	boom := errors.New("disk full")

	_, err := analyzer.MapFeaturesToDocs(
		context.Background(), &fakeTiering{small: client},
		features, map[string]string{"https://example.com/auth": p},
		1, 10_000,
		func(_ analyzer.DocsFeatureMap) error { return boom },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

func TestMapFeaturesToDocs_UsesSmallTier(t *testing.T) {
	features := []string{"auth"}

	dir := t.TempDir()
	p := filepath.Join(dir, "auth.md")
	require.NoError(t, os.WriteFile(p, []byte("auth content"), 0o644))
	pages := map[string]string{"https://example.com/auth": p}

	small := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"map_page_response": json.RawMessage(`{"features":["auth"]}`),
	}}
	typical := &fakeClient{}
	large := &fakeClient{}
	tiering := &fakeTiering{small: small, typical: typical, large: large}

	_, err := analyzer.MapFeaturesToDocs(
		context.Background(), tiering,
		features, pages, 1, 10_000, nil,
	)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(small.receivedPrompts), 1,
		"MapFeaturesToDocs must route through Small()")
	assert.Equal(t, 0, len(typical.receivedPrompts),
		"MapFeaturesToDocs must not call Typical()")
	assert.Equal(t, 0, len(large.receivedPrompts),
		"MapFeaturesToDocs must not call Large()")
}

// --- DocsFeatureMap roundtrip ---

func TestDocsFeatureMapRoundtrips(t *testing.T) {
	fm := analyzer.DocsFeatureMap{
		{Feature: "authentication", Pages: []string{"https://example.com/auth"}},
		{Feature: "search", Pages: []string{}},
	}
	data, err := json.Marshal(fm)
	require.NoError(t, err)

	var got analyzer.DocsFeatureMap
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, fm, got)
}
