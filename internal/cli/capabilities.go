package cli

// ModelCapabilities describes which optional LLM features a (provider, model)
// pair supports. Looked up via ResolveCapabilities at tier construction time
// and travels with the LLMClient so analysis branches without naming providers.
type ModelCapabilities struct {
	Provider string
	Model    string
	ToolUse  bool
	Vision   bool
	// MaxCompletionTokens is the per-model output cap. Zero means "use the
	// BifrostClient default". Set explicitly only for models whose API rejects
	// the default 32k request (e.g., Groq's llama-4-scout caps at 8192).
	MaxCompletionTokens int
	// MaxInputTokens is the per-model input cap including system prompt,
	// tool definitions, and accumulated chat history. Zero disables the
	// budget gate (used for self-hosted ollama/lmstudio "*" rows where the
	// user picks the model). The decorator gates sends at 0.9 × this value;
	// see .plans/2026-05-07-token-budget-design.md.
	MaxInputTokens int
}

// knownModels enumerates per-model capabilities for hosted providers.
// Model "*" is the wildcard for self-hosted providers (ollama, lmstudio)
// where the user picks the model and capabilities default to off.
//
// Adding a new model: add a row here. Validation falls back to "no
// capabilities" for unknown (provider, model) pairs on a known provider, so
// new models can be used immediately even before this table catches up.
var knownModels = []ModelCapabilities{
	// Anthropic Claude 4.x family. Every Claude model supports text + image
	// input and tool use. Context windows split by tier: Opus 4.6/4.7/4.8 and
	// Sonnet 4.6 publish a 1M window at standard pricing (no long-context
	// premium), while Haiku 4.5 and the 200k-window legacy/deprecated rows sit
	// at 200k. Each MaxInputTokens value sits ~10% under the published cap so
	// output tokens, tool defs, and per-provider serialization overhead don't
	// push the wire-level total past the limit (900k under 1M; 180k under
	// 200k). Verified against platform.claude.com model overview, June 2026.
	//
	// Current flagships:
	{Provider: "anthropic", Model: "claude-opus-4-8", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "anthropic", Model: "claude-sonnet-4-6", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "anthropic", Model: "claude-haiku-4-5", ToolUse: true, Vision: true, MaxInputTokens: 180000},
	// Legacy but still callable (migrate when convenient):
	{Provider: "anthropic", Model: "claude-opus-4-7", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "anthropic", Model: "claude-opus-4-6", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "anthropic", Model: "claude-sonnet-4-5", ToolUse: true, Vision: true, MaxInputTokens: 180000},
	{Provider: "anthropic", Model: "claude-opus-4-5", ToolUse: true, Vision: true, MaxInputTokens: 180000},
	// Deprecated (still resolvable until their retirement dates): Opus 4.1
	// retires 2026-08-05; Sonnet 4 and Opus 4 retire 2026-06-15.
	{Provider: "anthropic", Model: "claude-opus-4-1", ToolUse: true, Vision: true, MaxInputTokens: 180000},
	{Provider: "anthropic", Model: "claude-sonnet-4", ToolUse: true, Vision: true, MaxInputTokens: 180000},
	{Provider: "anthropic", Model: "claude-opus-4", ToolUse: true, Vision: true, MaxInputTokens: 180000},
	// OpenAI's 2026 lineup. Every GPT-5 / GPT-4o model supports tool use
	// (function calling) and image input. Older entries (gpt-5, gpt-4o family)
	// stay so existing configs keep working. Verified against
	// developers.openai.com model pages, June 2026.
	//
	// Per-model windows differ enough to matter:
	//   - gpt-5.5 / gpt-5.4: 1.05M context / 128k output. (gpt-5.4 grew from
	//     the old 272k standard tier to the full 1M-class window.) OpenAI
	//     charges premium rates on prompts above 272k input tokens, so 900k
	//     means long drift runs CAN tip into the premium tier; that's an
	//     explicit trade for the larger window. The -pro reasoning variants
	//     share their base model's window.
	//   - gpt-5.4-mini / gpt-5.4-nano / gpt-5.2: 400k context / 128k output,
	//     no long-context premium → 360k input budget.
	//   - gpt-5 / gpt-5-mini / gpt-5-nano: 400k total but the API enforces a
	//     272k input cap (rest is reserved for the 128k output ceiling) → 260k.
	//   - gpt-4o family: 128k shared in/out → 115k.
	{Provider: "openai", Model: "gpt-5.5", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "openai", Model: "gpt-5.5-pro", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "openai", Model: "gpt-5.4", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "openai", Model: "gpt-5.4-pro", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "openai", Model: "gpt-5.4-mini", ToolUse: true, Vision: true, MaxInputTokens: 360000},
	{Provider: "openai", Model: "gpt-5.4-nano", ToolUse: true, Vision: true, MaxInputTokens: 360000},
	{Provider: "openai", Model: "gpt-5.2", ToolUse: true, Vision: true, MaxInputTokens: 360000},
	{Provider: "openai", Model: "gpt-5", ToolUse: true, Vision: true, MaxInputTokens: 260000},
	{Provider: "openai", Model: "gpt-5-mini", ToolUse: true, Vision: true, MaxInputTokens: 260000},
	{Provider: "openai", Model: "gpt-5-nano", ToolUse: true, Vision: true, MaxInputTokens: 260000},
	{Provider: "openai", Model: "gpt-4o", ToolUse: true, Vision: true, MaxInputTokens: 115000},
	{Provider: "openai", Model: "gpt-4o-mini", ToolUse: true, Vision: true, MaxInputTokens: 115000},
	// Groq's llama-4-scout rejects max_completion_tokens > 8192 (their API
	// error: "must be less than or equal to 8192, the maximum value for
	// max_completion_tokens is less than the context_window for this model").
	// The model's 131k context window leaves room for a 120k input budget.
	{Provider: "groq", Model: "meta-llama/llama-4-scout-17b-16e-instruct", ToolUse: true, Vision: true, MaxCompletionTokens: 8192, MaxInputTokens: 120000},
	// Google Gemini's 2026 lineup. The flash-lite / 3.5-flash / pro-preview
	// ladder mirrors the haiku/sonnet/opus split: cheap-fast small, tool-use
	// typical (the typical tier runs the drift investigator), flagship large.
	// Every 2.5/3.x Gemini model publishes a 1,048,576-token input window and
	// 65,536-token output, so MaxInputTokens sits at 900000 (~14% under the
	// published cap, matching Sonnet/GPT-5.5) and no MaxCompletionTokens
	// override is needed (the 65,536 output max clears our 32k default send,
	// unlike Groq's 8,192 cap). All accept image input and support function
	// calling. Verified against ai.google.dev / Firebase AI Logic model
	// tables, June 2026.
	{Provider: "gemini", Model: "gemini-3.1-pro-preview", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "gemini", Model: "gemini-3-pro-preview", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "gemini", Model: "gemini-3.5-flash", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "gemini", Model: "gemini-3-flash-preview", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "gemini", Model: "gemini-3.1-flash-lite", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "gemini", Model: "gemini-2.5-pro", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "gemini", Model: "gemini-2.5-flash", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "gemini", Model: "gemini-2.5-flash-lite", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	// Self-hosted providers leave MaxInputTokens at 0 — the user picks the
	// model and the harness has no reliable way to know its limit.
	{Provider: "ollama", Model: "*"},
	{Provider: "lmstudio", Model: "*"},
}

// isKnownModel reports whether (provider, model) matches an explicit row in
// knownModels — an exact model match or a self-hosted "*" wildcard. It
// distinguishes a recognized capability row from the conservative fallback
// ResolveCapabilities returns for an unrecognized model on a known provider,
// so callers can tell "we know this model can't do X" from "we've never heard
// of this model".
func isKnownModel(provider, model string) bool {
	for _, m := range knownModels {
		if m.Provider == provider && (m.Model == model || m.Model == "*") {
			return true
		}
	}
	return false
}

// knownModelsForProvider returns the concrete model IDs registered for a
// provider, in registry order. Wildcard "*" rows (self-hosted providers) are
// omitted because they name no specific model. Used to build the
// "valid models: ..." hint in tier-validation errors.
func knownModelsForProvider(provider string) []string {
	var out []string
	for _, m := range knownModels {
		if m.Provider == provider && m.Model != "*" {
			out = append(out, provider+"/"+m.Model)
		}
	}
	return out
}

// ResolveCapabilities returns the capability flags for (provider, model).
// The bool is true when the provider is recognized; for known providers with
// an unknown model, it returns a zero-value ModelCapabilities and true so
// the caller can run with no optional features rather than failing the run.
func ResolveCapabilities(provider, model string) (ModelCapabilities, bool) {
	var providerKnown bool
	for _, m := range knownModels {
		if m.Provider == provider {
			providerKnown = true
			if m.Model == model || m.Model == "*" {
				return m, true
			}
		}
	}
	if providerKnown {
		// Conservative default for an unknown model on a known provider:
		// 100k is below GPT-4o's 128k floor and any modern hosted production
		// model. Lets users add a brand-new model row without immediately
		// reproducing the 294k incident, and keeps the budget gate active.
		return ModelCapabilities{Provider: provider, Model: model, MaxInputTokens: 100000}, true
	}
	return ModelCapabilities{}, false
}
