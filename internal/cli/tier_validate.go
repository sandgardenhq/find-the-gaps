package cli

import "fmt"

// Default tier strings used when a flag/config/env is empty.
const (
	defaultSmallTier   = "anthropic/claude-haiku-4-5"
	defaultTypicalTier = "anthropic/claude-sonnet-4-6"
	defaultLargeTier   = "anthropic/claude-opus-4-7"
)

// knownProviders returns the deduplicated provider list for "valid: ..."
// error messages. Built from knownModels so adding a provider only requires
// a row in the registry.
func knownProviders() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range knownModels {
		if _, ok := seen[m.Provider]; ok {
			continue
		}
		seen[m.Provider] = struct{}{}
		out = append(out, m.Provider)
	}
	return out
}

// validateTierConfigs parses each tier string, applies defaults for empties,
// and enforces that the typical tier's model supports tool use (it runs the
// drift investigator's tool-use loop). Capability lookups are driven by the
// per-model registry in capabilities.go. Returns typed errors naming the
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
		provider, model, err := parseTierString(s)
		if err != nil {
			return fmt.Errorf("tier %q: %w", tc.name, err)
		}
		caps, ok := ResolveCapabilities(provider, model)
		if !ok {
			return fmt.Errorf("tier %q: unknown provider %q (valid: %v)", tc.name, provider, knownProviders())
		}
		if tc.needsTool && !caps.ToolUse {
			return fmt.Errorf("tier %q: model %q on provider %q does not support tool use; the drift investigator requires a tool-use-capable model", tc.name, model, provider)
		}
	}
	return nil
}
