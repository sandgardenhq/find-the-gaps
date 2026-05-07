package analyzer

import "fmt"

// ErrTokenBudgetExceeded is returned by budgetedClient when a request's
// estimated input token count exceeds the gated per-model budget. Callers
// handle this error per call site:
//
//   - drift investigator: stop the loop, hand partial observations to the
//     judge (mirrors ErrMaxRounds semantics);
//   - drift judge:       fall back to a chunked-judging compaction path;
//   - page analyzer / screenshot pass / mapper batch: log + skip that unit,
//     run continues.
//
// Detect via errors.Is against a zero-value sentinel:
//
//	if errors.Is(err, ErrTokenBudgetExceeded{}) { ... }
//
// See .plans/2026-05-07-token-budget-design.md for the full per-call-site
// failure-mode matrix.
type ErrTokenBudgetExceeded struct {
	// Provider and Model identify the LLM that refused the payload. Used in
	// the user-facing message so a hint like "rerun with --llm-large=…" can
	// be rendered.
	Provider, Model string
	// Counted is the estimator's payload size in tokens.
	Counted int
	// Budget is the post-margin cap (already 0.9 × MaxInputTokens).
	Budget int
	// Where is a free-form site label ("drift-investigator", "judge",
	// "page-analyzer", …). Travels into log lines and the error message so
	// users can tell which unit of work refused.
	Where string
}

// Error renders a single-line user-facing message naming the provider/model,
// the offending count, the budget, and the call site.
func (e ErrTokenBudgetExceeded) Error() string {
	return fmt.Sprintf("token budget exceeded for %s/%s in %s: %d tokens > budget %d",
		e.Provider, e.Model, e.Where, e.Counted, e.Budget)
}

// Is allows errors.Is to match any ErrTokenBudgetExceeded against a
// zero-value sentinel without comparing field values, so callers can ask
// "is this the budget error?" without reproducing the exact counts.
func (e ErrTokenBudgetExceeded) Is(target error) bool {
	_, ok := target.(ErrTokenBudgetExceeded)
	return ok
}
