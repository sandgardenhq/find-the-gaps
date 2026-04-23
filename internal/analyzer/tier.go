package analyzer

type Tier string

const (
	TierSmall   Tier = "small"
	TierTypical Tier = "typical"
	TierLarge   Tier = "large"
)

// LLMTiering exposes one LLMClient and TokenCounter per reasoning tier.
// Analyzer functions choose a tier inline next to their // PROMPT: comment.
type LLMTiering interface {
	Small() LLMClient
	Typical() LLMClient
	Large() LLMClient

	SmallCounter() TokenCounter
	TypicalCounter() TokenCounter
	LargeCounter() TokenCounter
}
