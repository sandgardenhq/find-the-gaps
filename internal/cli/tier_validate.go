package cli

import "fmt"

// Default tier strings used when a flag/config/env is empty.
const (
	defaultSmallTier   = "anthropic/claude-haiku-4-5"
	defaultTypicalTier = "anthropic/claude-sonnet-4-6"
	defaultLargeTier   = "anthropic/claude-opus-4-7"
)

// validateTierConfigs parses each tier string, applies defaults for empties,
// and enforces that the typical tier's provider supports tool use (it runs
// the drift investigator's tool-use loop). Returns typed errors naming the
// offending tier.
func validateTierConfigs(small, typical, large string) error {
	for _, tc := range []struct {
		name, raw string
		fallback  string
		needsTool bool
	}{
		{"small", small, defaultSmallTier, false},
		{"typical", typical, defaultTypicalTier, true},
		{"large", large, defaultLargeTier, false},
	} {
		s := tc.raw
		if s == "" {
			s = tc.fallback
		}
		provider, _, err := parseTierString(s)
		if err != nil {
			return fmt.Errorf("tier %q: %w", tc.name, err)
		}
		if !isKnownProvider(provider) {
			return fmt.Errorf("tier %q: unknown provider %q (valid: anthropic, openai, ollama, lmstudio)", tc.name, provider)
		}
		if tc.needsTool && !providerSupportsToolUse(provider) {
			return fmt.Errorf("tier %q: provider %q does not support tool use; the drift investigator requires anthropic or openai", tc.name, provider)
		}
	}
	return nil
}

func isKnownProvider(p string) bool {
	switch p {
	case "anthropic", "openai", "ollama", "lmstudio":
		return true
	default:
		return false
	}
}
