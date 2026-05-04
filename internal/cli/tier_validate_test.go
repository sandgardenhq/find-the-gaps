package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTierConfigs_Defaults(t *testing.T) {
	err := validateTierConfigs("", "", "") // all empty → defaults applied
	if err != nil {
		t.Fatalf("default tier values should validate: %v", err)
	}
}

func TestValidateTierConfigs_UnknownProvider(t *testing.T) {
	err := validateTierConfigs("bogus/whatever", "", "")
	if err == nil || !strings.Contains(err.Error(), "small") {
		t.Fatalf("expected error naming 'small' tier for unknown provider, got %v", err)
	}
}

func TestValidateTierConfigs_TypicalNeedsToolUse(t *testing.T) {
	err := validateTierConfigs("", "ollama/llama3.1", "")
	if err == nil {
		t.Fatal("expected error: ollama does not support tool use in typical tier")
	}
	if !strings.Contains(err.Error(), "typical") || !strings.Contains(err.Error(), "tool use") {
		t.Fatalf("error should mention 'typical' and 'tool use': %v", err)
	}
	if !strings.Contains(err.Error(), "drift investigator") {
		t.Fatalf("error should mention 'drift investigator': %v", err)
	}
}

func TestValidateTierConfigs_SmallCanBeNonToolUse(t *testing.T) {
	if err := validateTierConfigs("ollama/llama3.1", "", ""); err != nil {
		t.Fatalf("ollama in small tier should be allowed: %v", err)
	}
}

func TestValidateTierConfigs_LargeCanBeNonToolUse(t *testing.T) {
	// The large tier no longer needs tool use — it only runs a single
	// non-tool CompleteJSON call (the drift judge).
	if err := validateTierConfigs("", "", "ollama/llama3.1"); err != nil {
		t.Fatalf("ollama in large tier should be allowed: %v", err)
	}
}

func TestValidateTierConfigs_RejectsUnknownProvider(t *testing.T) {
	err := validateTierConfigs("nope/foo", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
	assert.Contains(t, err.Error(), "nope")
}

// TestValidateTierConfigs_ValidProvidersListIsCommaSeparated pins the
// human-readable format of the "valid: ..." list. The list MUST be rendered
// as comma-separated provider names ("anthropic, openai, ...") not Go's
// default %v slice formatting ("[anthropic openai ...]"). The square-bracket
// form leaks Go syntax into the user-facing error message.
func TestValidateTierConfigs_ValidProvidersListIsCommaSeparated(t *testing.T) {
	err := validateTierConfigs("nope/foo", "", "")
	require.Error(t, err)
	msg := err.Error()
	assert.NotContains(t, msg, "[", "valid-providers list must not be wrapped in square brackets (Go slice format)")
	assert.NotContains(t, msg, "]", "valid-providers list must not be wrapped in square brackets (Go slice format)")
	// Must contain at least two known providers separated by ", ".
	assert.Contains(t, msg, "anthropic, ")
}

func TestValidateTierConfigs_TypicalRequiresToolUse(t *testing.T) {
	err := validateTierConfigs("", "ollama/llama3", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool use")
	assert.Contains(t, err.Error(), "typical")
}

func TestValidateTierConfigs_AllowsGroqOnTypical(t *testing.T) {
	err := validateTierConfigs("", "groq/meta-llama/llama-4-scout-17b-16e-instruct", "")
	assert.NoError(t, err)
}

func TestValidateTierConfigs_AllowsUnknownModelOnKnownProvider(t *testing.T) {
	err := validateTierConfigs("anthropic/claude-future-9-9", "", "")
	assert.NoError(t, err)
}

func TestValidateTierConfigs_DefaultsAreValid(t *testing.T) {
	err := validateTierConfigs("", "", "")
	assert.NoError(t, err)
}

func TestTierFallbacks_OpenAIWhenOnlyOpenAIKeySet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "test-openai")
	small, typical, large := tierFallbacks()
	if small != defaultSmallTierOpenAI {
		t.Errorf("small fallback: want %q, got %q", defaultSmallTierOpenAI, small)
	}
	if typical != defaultTypicalTierOpenAI {
		t.Errorf("typical fallback: want %q, got %q", defaultTypicalTierOpenAI, typical)
	}
	if large != defaultLargeTierOpenAI {
		t.Errorf("large fallback: want %q, got %q", defaultLargeTierOpenAI, large)
	}
}

func TestTierFallbacks_AnthropicWhenBothKeysSet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic")
	t.Setenv("OPENAI_API_KEY", "test-openai")
	small, typical, large := tierFallbacks()
	if small != "anthropic/claude-haiku-4-5" {
		t.Errorf("small: want anthropic default, got %q", small)
	}
	if typical != "anthropic/claude-sonnet-4-6" {
		t.Errorf("typical: want anthropic default, got %q", typical)
	}
	if large != "anthropic/claude-opus-4-7" {
		t.Errorf("large: want anthropic default, got %q", large)
	}
}

func TestTierFallbacks_AnthropicWhenOnlyAnthropicKeySet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic")
	t.Setenv("OPENAI_API_KEY", "")
	small, _, _ := tierFallbacks()
	if small != "anthropic/claude-haiku-4-5" {
		t.Errorf("small: want anthropic default, got %q", small)
	}
}

func TestTierFallbacks_AnthropicWhenNeitherKeySet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	small, _, _ := tierFallbacks()
	if small != "anthropic/claude-haiku-4-5" {
		t.Errorf("small: want anthropic default when neither key set, got %q", small)
	}
}
