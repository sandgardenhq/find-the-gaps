# Dynamic Turn Budget for Drift Detection

**Date:** 2026-04-27
**Branch:** `dynamic-turn-budget`

## Problem

Drift detection runs an agent loop per feature with a hardcoded ceiling of 30
rounds (`driftMaxRounds` in `internal/analyzer/drift.go:15`). Big features —
many code files mapped to many doc pages — exhaust the budget before reporting
all findings, and the loop returns truncated results with an `ErrMaxRounds`
warning. Findings are silently lost.

The fix: compute the budget per feature from the actual work the agent has to
do, so big features get more headroom and small features stop carrying unused
slack.

## Approach

Replace the constant with a pure function of two inputs: the number of code
files attached to the feature and the number of doc pages remaining after
filtering. Each `read_file` or `read_page` tool call costs one round; each
`add_finding` call costs one round. Add fixed slack for re-reads and the
closing plain-text turn. Clamp at a generous ceiling so a misconfigured
feature mapping cannot trigger runaway cost.

```
budget = files + pages + expected_findings + slack
       = files + pages + 5 + 3
       = clamp(..., 0, 100)
```

### Worked examples

| files | pages | budget | vs. old (30) |
|------:|------:|-------:|-------------:|
|     1 |     1 |     10 | -20          |
|     8 |     4 |     20 | -10          |
|    15 |    10 |     33 |  +3          |
|    40 |    30 |     78 | +48          |
|    60 |    50 |    100 | +70 (clamped) |

The status quo (30) is a hard ceiling that's both too generous for small
features and too tight for big ones. The new formula tracks workload.

## Design

### `budgetForFeature` (new, in `internal/analyzer/drift.go`)

```go
const (
    driftBudgetExpectedFindings = 5
    driftBudgetSlack            = 3
    driftBudgetCeiling          = 100
)

func budgetForFeature(files, pages int) int {
    budget := files + pages + driftBudgetExpectedFindings + driftBudgetSlack
    if budget > driftBudgetCeiling {
        return driftBudgetCeiling
    }
    return budget
}
```

Pure, deterministic, trivially unit-testable. Constants are named so future
tuning has obvious knobs.

### Integration site (`detectDriftForFeature`)

Currently:

```go
_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(driftMaxRounds))
```

Becomes:

```go
budget := budgetForFeature(len(entry.Files), len(pages))
_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(budget))
```

`pages` at the call site is the post-filtered, post-classified slice (release
notes stripped, classifier already ran). That's the correct input — we budget
for what the agent will actually read, not what it could have read before
filtering.

### Logging

Replace the existing per-feature info line at `drift.go:75` so the budget is
visible during normal runs:

```go
log.Infof("  checking drift for feature %q (%d files, %d pages, budget %d rounds)",
    entry.Feature.Name, len(entry.Files), len(pages), budget)
```

Update the `ErrMaxRounds` warning at `drift.go:157` to include the budget and
input counts:

```go
log.Warnf("drift agent exceeded budget of %d rounds for feature %q (%d files, %d pages); returning %d accumulated findings",
    budget, entry.Feature.Name, len(entry.Files), len(pages), len(findings))
```

### Cleanup

Delete the `driftMaxRounds` constant. `defaultAgentMaxRounds = 30` in
`agent_loop.go:40` stays — that's the fallback for callers that don't pass
`WithMaxRounds`, and is unrelated to drift.

## Testing

### Unit (TDD, in `drift_test.go`)

Table-driven test against `budgetForFeature` covering every regime:

```go
{"minimum",            1,  1,  10}
{"medium",             8,  4,  20}
{"grows past old cap",15, 10,  33}
{"large but uncapped",40, 30,  78}
{"one below ceiling", 45, 46,  99}
{"exactly at ceiling",46, 46, 100}
{"clamped",           60, 50, 100}
```

The clamp boundary is the regression risk — the table pins it explicitly.

### Existing tests

`TestDetectDrift` and the `detectDriftForFeature` tests already exercise the
full call path with stub `ToolLLMClient` implementations. They assert on
outcomes, not round counts, so they keep working unchanged.

### Verification plan

No changes required. None of the ten scenarios in `.plans/VERIFICATION_PLAN.md`
reference the round budget. A correct implementation should make scenarios
2/3/4 *more* likely to pass on big features without changing their criteria.

## TDD order

1. **RED** — write the table test against a non-existent `budgetForFeature`;
   compile fails.
2. **GREEN** — write the function and constants. Watch the table go green.
3. **REFACTOR** — none expected; the function is already minimal.
4. **Wire** — swap the call site in `detectDriftForFeature`. Existing drift
   tests stay green.
5. **Log** — update the `Infof` and `Warnf` lines.
6. **Delete** — remove `driftMaxRounds`.
7. Run `go test ./... -count=1` and `golangci-lint run`. Coverage must stay
   ≥90% on `internal/analyzer`.

## Out of scope

- Configurability via flag (`--drift-max-rounds`). Agreed-upon defaults are
  enough; revisit if users ask.
- Content-size-aware weighting (a 5000-line file vs. a 50-line stub). Each
  read costs one round regardless of content; thinking-round overhead is
  second-order. Revisit if real runs show truncation correlates with file
  size rather than file count.
- Adaptive budgets driven by historical findings rate. No history exists
  yet.
