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
	// Anthropic windows differ across the 4.x family: Haiku 4.5 still
	// publishes 200k, but Sonnet 4.6 and Opus 4.7 went to a 1M context window
	// at standard pricing (no long-context premium). Each value sits ~10%
	// under the published cap so output tokens, tool defs, and per-provider
	// serialization overhead don't push the wire-level total past the limit.
	{Provider: "anthropic", Model: "claude-haiku-4-5", ToolUse: true, Vision: true, MaxInputTokens: 180000},
	{Provider: "anthropic", Model: "claude-sonnet-4-6", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "anthropic", Model: "claude-opus-4-7", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	// OpenAI's 2026 lineup. GPT-5.4 (March 2026) and GPT-5.5 (April 2026)
	// all support tool use and vision; the nano/mini/standard split is the
	// usual cheap-fast / mid / flagship ladder. Older entries (gpt-5,
	// gpt-4o family) stay so existing configs keep working.
	//
	// Per-model windows differ enough to matter:
	//   - gpt-5.5: 922k input / 128k output. OpenAI charges 2x input + 1.5x
	//     output on prompts above 272k input tokens, so picking 900k means
	//     long drift runs CAN tip into the premium tier; that's an explicit
	//     trade for the larger window.
	//   - gpt-5.4: standard window is 272k; 1M is opt-in via params we do
	//     not pass, so we cap at the standard tier.
	//   - gpt-5.4-mini / gpt-5.4-nano: 400k context, no long-context premium.
	//   - gpt-5 / gpt-5-mini: 400k total but the API enforces a 272k input
	//     cap (rest is reserved for the 128k output ceiling).
	//   - gpt-4o family: 128k shared in/out.
	{Provider: "openai", Model: "gpt-5.5", ToolUse: true, Vision: true, MaxInputTokens: 900000},
	{Provider: "openai", Model: "gpt-5.4", ToolUse: true, Vision: true, MaxInputTokens: 260000},
	{Provider: "openai", Model: "gpt-5.4-mini", ToolUse: true, Vision: true, MaxInputTokens: 360000},
	{Provider: "openai", Model: "gpt-5.4-nano", ToolUse: true, Vision: true, MaxInputTokens: 360000},
	{Provider: "openai", Model: "gpt-5", ToolUse: true, Vision: true, MaxInputTokens: 260000},
	{Provider: "openai", Model: "gpt-5-mini", ToolUse: true, Vision: true, MaxInputTokens: 260000},
	{Provider: "openai", Model: "gpt-4o", ToolUse: true, Vision: true, MaxInputTokens: 115000},
	{Provider: "openai", Model: "gpt-4o-mini", ToolUse: true, Vision: true, MaxInputTokens: 115000},
	// Groq's llama-4-scout rejects max_completion_tokens > 8192 (their API
	// error: "must be less than or equal to 8192, the maximum value for
	// max_completion_tokens is less than the context_window for this model").
	// The model's 131k context window leaves room for a 120k input budget.
	{Provider: "groq", Model: "meta-llama/llama-4-scout-17b-16e-instruct", ToolUse: true, Vision: true, MaxCompletionTokens: 8192, MaxInputTokens: 120000},
	// Self-hosted providers leave MaxInputTokens at 0 — the user picks the
	// model and the harness has no reliable way to know its limit.
	{Provider: "ollama", Model: "*"},
	{Provider: "lmstudio", Model: "*"},
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
