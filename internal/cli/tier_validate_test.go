package cli

import (
	"strings"
	"testing"
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
