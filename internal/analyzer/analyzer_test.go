package analyzer_test

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestTypes_PageAnalysis(t *testing.T) {
	pa := analyzer.PageAnalysis{
		URL:      "https://docs.example.com/install",
		Summary:  "Covers installation.",
		Features: []string{"Homebrew install", "go install"},
	}
	if pa.URL == "" {
		t.Fatal("URL must not be empty")
	}
	if len(pa.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(pa.Features))
	}
}

func TestTypes_ProductSummary(t *testing.T) {
	ps := analyzer.ProductSummary{
		Description: "A CLI tool for finding doc gaps.",
		Features:    []string{"gap analysis", "doctor command"},
	}
	if len(ps.Features) == 0 {
		t.Fatal("features must not be empty")
	}
}

func TestTypes_FeatureMap(t *testing.T) {
	fm := analyzer.FeatureMap{
		{
			Feature: analyzer.CodeFeature{Name: "gap analysis", Description: "Identifies doc gaps.", Layer: "analysis engine", UserFacing: false},
			Files:   []string{"internal/analyzer/analyzer.go"},
			Symbols: []string{"AnalyzePage"},
		},
	}
	if len(fm) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(fm))
	}
	if fm[0].Feature.Name != "gap analysis" {
		t.Errorf("expected feature name 'gap analysis', got %q", fm[0].Feature.Name)
	}
}

func TestLLMClient_FakeImplementsInterface(t *testing.T) {
	var _ analyzer.LLMClient = &fakeClient{}
}
