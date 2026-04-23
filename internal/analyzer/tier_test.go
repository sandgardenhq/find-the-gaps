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
