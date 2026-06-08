package cli

import (
	"fmt"
	"os"
	"strings"
)

// Default tier strings used when a flag/config/env is empty.
const (
	defaultSmallTier   = "anthropic/claude-haiku-4-5"
	defaultTypicalTier = "anthropic/claude-sonnet-4-6"
	defaultLargeTier   = "anthropic/claude-opus-4-7"

	// 2026 lineup. nano/mini/5.5 mirrors the Anthropic haiku/sonnet/opus
	// ladder: the small tier is cheap/fast and vision-capable, the typical
	// tier is mid-cost with tool use, and the large tier is the flagship.
	defaultSmallTierOpenAI   = "openai/gpt-5.4-nano"
	defaultTypicalTierOpenAI = "openai/gpt-5.4-mini"
	defaultLargeTierOpenAI   = "openai/gpt-5.5"

	// Gemini's flash-lite / 3.5-flash / pro-preview ladder. typical must be
	// tool-use-capable (drift investigator) — 3.5 Flash is and has the
	// strongest agentic performance in the lineup. large is pinned to the
	// preview Pro ID because the stable Pro tier is still preview-only as of
	// June 2026; that's the user-approved trade for the newest flagship.
	defaultSmallTierGemini   = "gemini/gemini-3.1-flash-lite"
	defaultTypicalTierGemini = "gemini/gemini-3.5-flash"
	defaultLargeTierGemini   = "gemini/gemini-3.1-pro-preview"
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

// tierFallbacks picks the default (small, typical, large) tier strings based
// on which provider keys are present in the environment. The precedence is a
// first-key-present cascade: Anthropic → OpenAI → Gemini. Anthropic wins
// whenever ANTHROPIC_API_KEY is set; OpenAI defaults engage only when it is the
// sole hosted key among {Anthropic, OpenAI}; Gemini defaults engage only when
// GEMINI_API_KEY is the sole key set. With no key set at all, the Anthropic
// defaults stand (and surface the missing-key setup hint downstream). This
// preserves the prior Anthropic/OpenAI behavior byte-for-byte.
func tierFallbacks() (small, typical, large string) {
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return defaultSmallTier, defaultTypicalTier, defaultLargeTier
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return defaultSmallTierOpenAI, defaultTypicalTierOpenAI, defaultLargeTierOpenAI
	}
	if os.Getenv("GEMINI_API_KEY") != "" {
		return defaultSmallTierGemini, defaultTypicalTierGemini, defaultLargeTierGemini
	}
	return defaultSmallTier, defaultTypicalTier, defaultLargeTier
}

// validateTierConfigs parses each tier string, applies defaults for empties,
// and enforces that the typical tier's model supports tool use (it runs the
// drift investigator's tool-use loop). Capability lookups are driven by the
// per-model registry in capabilities.go. Returns typed errors naming the
// offending tier.
func validateTierConfigs(small, typical, large string) error {
	smallFB, typicalFB, largeFB := tierFallbacks()
	for _, tc := range []struct {
		name, raw string
		fallback  string
		needsTool bool
	}{
		{"small", small, smallFB, false},
		{"typical", typical, typicalFB, true},
		{"large", large, largeFB, false},
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
			return fmt.Errorf("tier %q: unknown provider %q (valid: %s)", tc.name, provider, strings.Join(knownProviders(), ", "))
		}
		if tc.needsTool && !caps.ToolUse {
			// ResolveCapabilities returns ToolUse:false both for genuinely
			// known non-tool-use models (self-hosted ollama/lmstudio "*" rows)
			// and for the fallback it synthesizes for an unrecognized model. An
			// unrecognized model isn't "does not support tool use" — it's an
			// unknown model, so say that instead and point at the real ones.
			if !isKnownModel(provider, model) {
				return fmt.Errorf("tier %q: unknown model %q for provider %q (valid models: %s)", tc.name, model, provider, strings.Join(knownModelsForProvider(provider), ", "))
			}
			return fmt.Errorf("tier %q: model %q on provider %q does not support tool use; the drift investigator requires a tool-use-capable model", tc.name, model, provider)
		}
	}
	return nil
}
