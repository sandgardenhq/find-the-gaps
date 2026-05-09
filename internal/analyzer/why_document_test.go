package analyzer_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// TestWhyDocument_ReturnsRationalePerFeature pins that the function asks
// the small tier for a per-feature rationale and returns a map keyed by
// feature name.
func TestWhyDocument_ReturnsRationalePerFeature(t *testing.T) {
	c := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"why_document_response": json.RawMessage(`{"rationales":[
			{"name":"auth","rationale":"Users sign in via this flow daily; without docs they cannot diagnose login failures."},
			{"name":"billing","rationale":"Pricing surfaces depend on this; misconfigured limits silently break paid plans."}
		]}`),
	}}
	features := []analyzer.CodeFeature{
		{Name: "auth", Description: "Login and session management.", Layer: "service", UserFacing: true},
		{Name: "billing", Description: "Tracks per-customer usage and invoices.", Layer: "service", UserFacing: true},
	}

	got, err := analyzer.WhyDocument(context.Background(), &fakeTiering{small: c}, features)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rationales, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got["auth"], "login failures") {
		t.Errorf("auth rationale unexpected: %q", got["auth"])
	}
	if !strings.Contains(got["billing"], "paid plans") {
		t.Errorf("billing rationale unexpected: %q", got["billing"])
	}
	if len(c.jsonSchemas) != 1 || c.jsonSchemas[0].Name != "why_document_response" {
		t.Errorf("expected one CompleteJSON call with why_document_response schema, got %+v", c.jsonSchemas)
	}
}

// TestWhyDocument_EmptyFeatures_NoCall pins that calling WhyDocument with
// no features short-circuits without invoking the LLM. The undocumented
// list is often empty, and we should not pay an LLM round-trip in that
// case.
func TestWhyDocument_EmptyFeatures_NoCall(t *testing.T) {
	c := &fakeClient{}
	got, err := analyzer.WhyDocument(context.Background(), &fakeTiering{small: c}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %+v", got)
	}
	if len(c.receivedPrompts) != 0 {
		t.Errorf("expected zero LLM calls, got %d", len(c.receivedPrompts))
	}
}

// TestWhyDocument_ClientError_Propagates pins that an LLM-tier failure
// surfaces as an error rather than being swallowed.
func TestWhyDocument_ClientError_Propagates(t *testing.T) {
	c := &fakeClient{forcedErr: errors.New("network down")}
	_, err := analyzer.WhyDocument(context.Background(), &fakeTiering{small: c}, []analyzer.CodeFeature{
		{Name: "auth", UserFacing: true},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestWhyDocument_UsesSmallTier pins the tier choice. Rationales are a
// short, low-stakes generative task — the smallest tier is appropriate.
func TestWhyDocument_UsesSmallTier(t *testing.T) {
	small := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"why_document_response": json.RawMessage(`{"rationales":[{"name":"auth","rationale":"x"}]}`),
	}}
	typical := &fakeClient{}
	large := &fakeClient{}
	_, err := analyzer.WhyDocument(context.Background(), &fakeTiering{small: small, typical: typical, large: large},
		[]analyzer.CodeFeature{{Name: "auth", UserFacing: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(small.receivedPrompts) != 1 {
		t.Errorf("expected small tier 1 call, got %d", len(small.receivedPrompts))
	}
	if len(typical.receivedPrompts) != 0 || len(large.receivedPrompts) != 0 {
		t.Errorf("typical/large tiers must not be called; got %d/%d", len(typical.receivedPrompts), len(large.receivedPrompts))
	}
}

// TestWhyDocument_MissingFeatureInResponse_OK pins that the function
// tolerates an LLM response that omits a feature: the returned map only
// contains the features the model spoke about. Callers (the reporter) are
// responsible for falling back to a generic blurb when the key is missing.
func TestWhyDocument_MissingFeatureInResponse_OK(t *testing.T) {
	c := &fakeClient{jsonResponses: map[string]json.RawMessage{
		"why_document_response": json.RawMessage(`{"rationales":[{"name":"auth","rationale":"x"}]}`),
	}}
	got, err := analyzer.WhyDocument(context.Background(), &fakeTiering{small: c}, []analyzer.CodeFeature{
		{Name: "auth", UserFacing: true},
		{Name: "billing", UserFacing: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["auth"]; !ok {
		t.Errorf("expected auth in map, got %+v", got)
	}
	if _, ok := got["billing"]; ok {
		t.Errorf("billing was not in response, must not appear in map; got %+v", got)
	}
}
