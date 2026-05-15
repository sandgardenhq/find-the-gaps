// Package chunker provides structure-aware markdown splitting for LLM prompts.
// It uses the same cl100k_base tokenizer as internal/analyzer so estimates
// agree across packages.
package chunker

import "github.com/tiktoken-go/tokenizer"

var enc = mustEncoder()

func mustEncoder() tokenizer.Codec {
	c, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		panic("chunker: cl100k_base load failed: " + err.Error())
	}
	return c
}

// EstimateTokens returns the approximate cl100k_base token count for s.
// Used at every call site to decide whether to chunk before an LLM call.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	ids, _, err := enc.Encode(s)
	if err != nil {
		return 1
	}
	return len(ids)
}
