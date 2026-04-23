package analyzer_test

import "github.com/sandgardenhq/find-the-gaps/internal/analyzer"

// fakeTiering wires LLMClients into the three tiers for black-box tests.
type fakeTiering struct {
	small, typical, large                      analyzer.LLMClient
	smallCounter, typicalCounter, largeCounter analyzer.TokenCounter
}

func (f *fakeTiering) Small() analyzer.LLMClient             { return f.small }
func (f *fakeTiering) Typical() analyzer.LLMClient           { return f.typical }
func (f *fakeTiering) Large() analyzer.LLMClient             { return f.large }
func (f *fakeTiering) SmallCounter() analyzer.TokenCounter   { return f.smallCounter }
func (f *fakeTiering) TypicalCounter() analyzer.TokenCounter { return f.typicalCounter }
func (f *fakeTiering) LargeCounter() analyzer.TokenCounter   { return f.largeCounter }
