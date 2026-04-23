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

func TestValidateTierConfigs_LargeNeedsToolUse(t *testing.T) {
	err := validateTierConfigs("", "", "ollama/llama3.1")
	if err == nil {
		t.Fatal("expected error: ollama does not support tool use in large tier")
	}
	if !strings.Contains(err.Error(), "large") || !strings.Contains(err.Error(), "tool use") {
		t.Fatalf("error should mention 'large' and 'tool use': %v", err)
	}
}

func TestValidateTierConfigs_SmallCanBeNonToolUse(t *testing.T) {
	if err := validateTierConfigs("ollama/llama3.1", "", ""); err != nil {
		t.Fatalf("ollama in small tier should be allowed: %v", err)
	}
}
