package analyzer

import (
	"errors"
	"strings"
	"testing"
)

// TestErrTokenBudgetExceeded_ImplementsError pins the error message format
// and the errors.Is contract. Callers detect the error via errors.Is against
// a zero-value sentinel; the message is rendered to the user when a single-
// shot caller refuses with a hint.
func TestErrTokenBudgetExceeded_ImplementsError(t *testing.T) {
	err := ErrTokenBudgetExceeded{
		Provider: "openai",
		Model:    "gpt-5.5",
		Counted:  294098,
		Budget:   234000,
		Where:    "drift-investigator",
	}
	msg := err.Error()
	for _, want := range []string{"openai", "gpt-5.5", "294098", "234000", "drift-investigator"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Error() = %q, missing %q", msg, want)
		}
	}
	if !errors.Is(err, ErrTokenBudgetExceeded{}) {
		t.Fatalf("errors.Is should match against zero-value sentinel")
	}
}

// TestErrTokenBudgetExceeded_IsMatchesAcrossFieldDifferences pins that two
// instances with different field values still match via errors.Is, so
// callers can detect the budget condition without knowing exact counts.
func TestErrTokenBudgetExceeded_IsMatchesAcrossFieldDifferences(t *testing.T) {
	a := ErrTokenBudgetExceeded{Provider: "anthropic", Counted: 100}
	b := ErrTokenBudgetExceeded{Provider: "openai", Counted: 200}
	if !errors.Is(a, b) {
		t.Fatalf("errors.Is should match irrespective of field values")
	}
}
