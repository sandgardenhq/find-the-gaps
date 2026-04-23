package cli

import (
	"fmt"
	"strings"
)

// parseTierString splits a "provider/model" string. A bare model (no "/") defaults
// to provider "anthropic". Splits on the first "/" only so models containing
// additional slashes or colons (e.g. "llama3.1:8b") survive intact.
func parseTierString(raw string) (provider, model string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", fmt.Errorf("empty tier value")
	}
	provider, model, ok := strings.Cut(s, "/")
	if !ok {
		return "anthropic", s, nil
	}
	if provider == "" {
		return "", "", fmt.Errorf("missing provider before '/' in %q", raw)
	}
	if model == "" {
		return "", "", fmt.Errorf("missing model after '/' in %q", raw)
	}
	return provider, model, nil
}

// providerSupportsToolUse reports whether the Bifrost integration for this
// provider currently supports tool calling (required by drift detection).
func providerSupportsToolUse(provider string) bool {
	switch provider {
	case "anthropic", "openai":
		return true
	default:
		return false
	}
}
