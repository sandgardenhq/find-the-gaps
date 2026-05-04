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
}

// knownModels enumerates per-model capabilities for hosted providers.
// Model "*" is the wildcard for self-hosted providers (ollama, lmstudio)
// where the user picks the model and capabilities default to off.
//
// Adding a new model: add a row here. Validation falls back to "no
// capabilities" for unknown (provider, model) pairs on a known provider, so
// new models can be used immediately even before this table catches up.
var knownModels = []ModelCapabilities{
	{Provider: "anthropic", Model: "claude-haiku-4-5", ToolUse: true, Vision: true},
	{Provider: "anthropic", Model: "claude-sonnet-4-6", ToolUse: true, Vision: true},
	{Provider: "anthropic", Model: "claude-opus-4-7", ToolUse: true, Vision: true},
	// OpenAI's 2026 lineup. GPT-5.4 (March 2026) and GPT-5.5 (April 2026)
	// all support tool use and vision; the nano/mini/standard split is the
	// usual cheap-fast / mid / flagship ladder. Older entries (gpt-5,
	// gpt-4o family) stay so existing configs keep working.
	{Provider: "openai", Model: "gpt-5.5", ToolUse: true, Vision: true},
	{Provider: "openai", Model: "gpt-5.4", ToolUse: true, Vision: true},
	{Provider: "openai", Model: "gpt-5.4-mini", ToolUse: true, Vision: true},
	{Provider: "openai", Model: "gpt-5.4-nano", ToolUse: true, Vision: true},
	{Provider: "openai", Model: "gpt-5", ToolUse: true, Vision: true},
	{Provider: "openai", Model: "gpt-5-mini", ToolUse: true, Vision: true},
	{Provider: "openai", Model: "gpt-4o", ToolUse: true, Vision: true},
	{Provider: "openai", Model: "gpt-4o-mini", ToolUse: true, Vision: true},
	// Groq's llama-4-scout rejects max_completion_tokens > 8192 (their API
	// error: "must be less than or equal to 8192, the maximum value for
	// max_completion_tokens is less than the context_window for this model").
	{Provider: "groq", Model: "meta-llama/llama-4-scout-17b-16e-instruct", ToolUse: true, Vision: true, MaxCompletionTokens: 8192},
	{Provider: "ollama", Model: "*"},
	{Provider: "lmstudio", Model: "*"},
	// Gateway aliases are opaque to find-the-gaps: the gateway resolves the
	// alias to a real provider+model server-side. We trust the user that the
	// model behind any alias is vision- and tool-use-capable. Wildcard match
	// covers every alias name.
	{Provider: "gateway", Model: "*", ToolUse: true, Vision: true},
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
		return ModelCapabilities{Provider: provider, Model: model}, true
	}
	return ModelCapabilities{}, false
}
