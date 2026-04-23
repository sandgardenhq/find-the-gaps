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
	idx := strings.Index(s, "/")
	if idx < 0 {
		return "anthropic", s, nil
	}
	provider = s[:idx]
	model = s[idx+1:]
	if provider == "" {
		return "", "", fmt.Errorf("missing provider before '/' in %q", raw)
	}
	if model == "" {
		return "", "", fmt.Errorf("missing model after '/' in %q", raw)
	}
	return provider, model, nil
}
