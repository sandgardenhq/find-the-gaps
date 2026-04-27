package analyzer

import "context"

// CodeFeature is a product feature identified from the codebase.
type CodeFeature struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Layer       string `json:"layer"`
	UserFacing  bool   `json:"user_facing"`
}

// PageAnalysis is the LLM-extracted summary and feature list for one documentation page.
type PageAnalysis struct {
	URL      string
	Summary  string
	Features []string
}

// ProductSummary is the synthesized product description and deduplicated feature list.
type ProductSummary struct {
	Description string
	Features    []string
}

// FeatureEntry maps one product feature to the code files and symbols that implement it.
type FeatureEntry struct {
	Feature CodeFeature
	Files   []string
	Symbols []string
}

// FeatureMap is the complete feature-to-code mapping for a project.
type FeatureMap []FeatureEntry

// DocsFeatureEntry maps one product feature to the documentation pages that cover it.
type DocsFeatureEntry struct {
	Feature string   `json:"feature"`
	Pages   []string `json:"pages"`
}

// DocsFeatureMap is the complete feature-to-docs mapping for a project.
type DocsFeatureMap []DocsFeatureEntry

// ChatMessage is one turn in a tool-use conversation.
type ChatMessage struct {
	Role       string // "user", "assistant", "tool"
	Content    string
	ToolCalls  []ToolCall // set when Role=="assistant" and LLM requests tools
	ToolCallID string     // set when Role=="tool" (response to a tool call)
	// CacheBreakpoint marks this message as the last block of a cacheable
	// prefix. The Bifrost client materializes it into Anthropic's
	// content-block cache_control during request rendering; other providers
	// ignore it. The flag lives on the provider-neutral message so callers
	// don't need to know about Bifrost or Anthropic internals.
	CacheBreakpoint bool
}

// ToolHandler runs one tool call. The returned string is sent back to the LLM
// as the tool result. A non-nil error aborts the agent loop and is propagated
// to the caller. Errors that should be reported TO the LLM (e.g. "file not
// found") must be returned as the result string, not as a Go error.
type ToolHandler func(ctx context.Context, args string) (string, error)

// Tool defines a callable function the LLM may invoke during a tool-use
// conversation. Execute is invoked by the agent loop when the LLM calls this
// tool by name; it may be nil for advisory-only declarations.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
	Execute     ToolHandler
}

// ToolCall is one tool invocation requested by the LLM.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // raw JSON
}

// DriftIssue is one specific inaccuracy found between a feature's code and its documentation.
type DriftIssue struct {
	Page  string `json:"page"`  // URL of the doc page ("" if cross-page)
	Issue string `json:"issue"` // inaccuracy described in documentation language
}

// DriftFinding groups all drift issues found for one feature.
type DriftFinding struct {
	Feature string
	Issues  []DriftIssue
}

// ScreenshotGap is one place in a docs page where a screenshot should exist but does not.
type ScreenshotGap struct {
	PageURL       string `json:"page_url"`
	PagePath      string `json:"page_path"`
	QuotedPassage string `json:"quoted_passage"`
	ShouldShow    string `json:"should_show"`
	SuggestedAlt  string `json:"suggested_alt"`
	InsertionHint string `json:"insertion_hint"`
}
