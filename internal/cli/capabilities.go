package cli

// ModelCapabilities describes which optional LLM features a (provider, model)
// pair supports. Looked up via ResolveCapabilities at tier construction time
// and travels with the LLMClient so analysis branches without naming providers.
type ModelCapabilities struct {
	Provider string
	Model    string
	ToolUse  bool
	Vision   bool
}

// knownModels enumerates per-model capabilities for hosted providers.
// Model "*" is the wildcard for self-hosted providers (ollama, lmstudio)
// where the user picks the model and capabilities default to off.
//
// Adding a new model: add a row here. Validation falls back to "no
// capabilities" for unknown (provider, model) pairs on a known provider, so
// new models can be used immediately even before this table catches up.
var knownModels = []ModelCapabilities{
	{"anthropic", "claude-haiku-4-5", true, true},
	{"anthropic", "claude-sonnet-4-6", true, true},
	{"anthropic", "claude-opus-4-7", true, true},
	{"openai", "gpt-5", true, true},
	{"openai", "gpt-5-mini", true, true},
	{"openai", "gpt-4o", true, true},
	{"openai", "gpt-4o-mini", true, true},
	{"groq", "meta-llama/llama-4-scout-17b-16e-instruct", true, true},
	{"ollama", "*", false, false},
	{"lmstudio", "*", false, false},
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
