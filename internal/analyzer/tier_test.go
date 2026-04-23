package analyzer

import "testing"

func TestTierConstantsAreUnique(t *testing.T) {
	if TierSmall == TierTypical || TierTypical == TierLarge || TierSmall == TierLarge {
		t.Fatalf("tier constants must be unique: small=%q typical=%q large=%q", TierSmall, TierTypical, TierLarge)
	}
}

func TestTierConstantsAreLowercaseSlugs(t *testing.T) {
	for _, tier := range []Tier{TierSmall, TierTypical, TierLarge} {
		if string(tier) == "" {
			t.Fatalf("empty tier constant")
		}
	}
}

// compile-time interface check
var _ LLMTiering = (*fakeLLMTiering)(nil)

type fakeLLMTiering struct {
	small, typical, large                      LLMClient
	smallCounter, typicalCounter, largeCounter TokenCounter
}

func (f *fakeLLMTiering) Small() LLMClient             { return f.small }
func (f *fakeLLMTiering) Typical() LLMClient           { return f.typical }
func (f *fakeLLMTiering) Large() LLMClient             { return f.large }
func (f *fakeLLMTiering) SmallCounter() TokenCounter   { return f.smallCounter }
func (f *fakeLLMTiering) TypicalCounter() TokenCounter { return f.typicalCounter }
func (f *fakeLLMTiering) LargeCounter() TokenCounter   { return f.largeCounter }

func TestLLMTieringInterfaceShape(t *testing.T) {
	var ft LLMTiering = &fakeLLMTiering{}
	if ft.Small() != nil || ft.Typical() != nil || ft.Large() != nil {
		t.Fatalf("zero-value tiering should return nil clients")
	}
}
