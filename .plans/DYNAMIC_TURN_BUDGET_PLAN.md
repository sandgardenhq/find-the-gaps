# Dynamic Turn Budget Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the hardcoded `driftMaxRounds = 30` in drift detection with a per-feature round budget computed from the actual work the agent has to do (`files + pages + expected_findings + slack`, clamped at 100), so big features stop hitting `ErrMaxRounds` and losing findings.

**Architecture:** A new pure helper `budgetForFeature(files, pages int) int` lives in `internal/analyzer/drift.go`. The existing call site in `detectDriftForFeature` computes the budget from `len(entry.Files)` and `len(pages)` (the post-filtered, post-classified slice) and passes it to `WithMaxRounds`. Two log lines are updated to surface the budget. The old `driftMaxRounds` constant is deleted.

**Tech Stack:** Go 1.26+, `testify` for assertions. TDD is mandatory per `CLAUDE.md` — RED → GREEN → REFACTOR for every task. Commit after every successful cycle.

**Design reference:** `.plans/DYNAMIC_TURN_BUDGET_DESIGN.md`

---

## Canonical reference

### Constants (final values)

```go
driftBudgetExpectedFindings = 5
driftBudgetSlack            = 3
driftBudgetCeiling          = 100
```

### Formula

```
budget = clamp(files + pages + 5 + 3, ..., 100)
```

### Test table (the cases that pin the formula)

| name                  | files | pages | want |
|-----------------------|------:|------:|-----:|
| minimum               |     1 |     1 |   10 |
| medium                |     8 |     4 |   20 |
| grows past old cap    |    15 |    10 |   33 |
| large but uncapped    |    40 |    30 |   78 |
| one below ceiling     |    45 |    46 |   99 |
| exactly at ceiling    |    46 |    46 |  100 |
| clamped above ceiling |    60 |    50 |  100 |

---

## Task 1: RED — Add the failing budget test

**Files:**
- Modify: `internal/analyzer/export_test.go` — expose the unexported helper.
- Modify: `internal/analyzer/drift_test.go` — add the table test.

**Why two files:** the test package is black-box (`package analyzer_test`), and `budgetForFeature` is unexported. The codebase pattern (see `export_test.go`) is to add an `Exported<Name>` wrapper in `package analyzer` so black-box tests can reach it.

**Step 1.1: Add the export wrapper**

Append to `internal/analyzer/export_test.go`:

```go
// ExportedBudgetForFeature exposes budgetForFeature for black-box tests.
func ExportedBudgetForFeature(files, pages int) int {
	return budgetForFeature(files, pages)
}
```

**Step 1.2: Add the table test**

Append to `internal/analyzer/drift_test.go`:

```go
func TestBudgetForFeature(t *testing.T) {
	cases := []struct {
		name         string
		files, pages int
		want         int
	}{
		{"minimum", 1, 1, 10},
		{"medium", 8, 4, 20},
		{"grows past old cap", 15, 10, 33},
		{"large but uncapped", 40, 30, 78},
		{"one below ceiling", 45, 46, 99},
		{"exactly at ceiling", 46, 46, 100},
		{"clamped above ceiling", 60, 50, 100},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := analyzer.ExportedBudgetForFeature(tc.files, tc.pages)
			assert.Equal(t, tc.want, got, "budgetForFeature(%d, %d)", tc.files, tc.pages)
		})
	}
}
```

**Step 1.3: Run, verify RED (compile failure)**

```bash
go test ./internal/analyzer/ -run TestBudgetForFeature -count=1
```

Expected output contains a build error like:

```
internal/analyzer/export_test.go:NN:NN: undefined: budgetForFeature
```

If the test compiles and runs, RED is wrong — the helper somehow already exists. Stop and investigate.

**Step 1.4: Commit RED**

```bash
git add internal/analyzer/export_test.go internal/analyzer/drift_test.go
git commit -m "test(analyzer): RED — table test for budgetForFeature

- RED: TestBudgetForFeature pins formula and clamp boundary
- Build fails: undefined budgetForFeature"
```

---

## Task 2: GREEN — Implement `budgetForFeature`

**Files:**
- Modify: `internal/analyzer/drift.go` — add constants and helper.

**Step 2.1: Add the constants**

Replace the line:

```go
const driftMaxRounds = 30
```

with:

```go
const (
	// driftBudgetExpectedFindings is the headroom reserved for add_finding
	// tool calls when computing a feature's drift agent round budget.
	driftBudgetExpectedFindings = 5

	// driftBudgetSlack covers re-reads, the closing plain-text turn, and any
	// other non-read overhead the agent incurs during a drift check.
	driftBudgetSlack = 3

	// driftBudgetCeiling is the hard upper bound on the per-feature drift
	// agent round budget. Protects against runaway cost when a feature
	// mapping has unrealistically many files or pages.
	driftBudgetCeiling = 100

	// driftMaxRounds is retained as a temporary alias during migration. It
	// is removed in a later task once all call sites use budgetForFeature.
	driftMaxRounds = 30
)
```

(We keep `driftMaxRounds` here briefly so the unrelated existing code keeps compiling. Task 5 deletes it.)

**Step 2.2: Add the helper**

Below the constants, before the `DriftProgressFunc` type:

```go
// budgetForFeature returns the agent round budget for a single feature's
// drift check. Each read_file and read_page tool call costs one round; each
// add_finding call costs one round; slack covers re-reads and the closing
// turn. The result is clamped at driftBudgetCeiling to bound runaway cost
// when a feature has an unrealistic number of inputs.
func budgetForFeature(files, pages int) int {
	budget := files + pages + driftBudgetExpectedFindings + driftBudgetSlack
	if budget > driftBudgetCeiling {
		return driftBudgetCeiling
	}
	return budget
}
```

**Step 2.3: Run, verify GREEN**

```bash
go test ./internal/analyzer/ -run TestBudgetForFeature -count=1 -v
```

Expected:

```
=== RUN   TestBudgetForFeature
=== RUN   TestBudgetForFeature/minimum
=== RUN   TestBudgetForFeature/medium
=== RUN   TestBudgetForFeature/grows_past_old_cap
=== RUN   TestBudgetForFeature/large_but_uncapped
=== RUN   TestBudgetForFeature/one_below_ceiling
=== RUN   TestBudgetForFeature/exactly_at_ceiling
=== RUN   TestBudgetForFeature/clamped_above_ceiling
--- PASS: TestBudgetForFeature (0.00s)
    --- PASS: TestBudgetForFeature/minimum (0.00s)
    ... (all subcases PASS)
PASS
```

If any subcase fails, the formula or the constants are wrong. Re-derive the expected value from `files + pages + 5 + 3` capped at 100.

**Step 2.4: Commit GREEN**

```bash
git add internal/analyzer/drift.go
git commit -m "feat(analyzer): add budgetForFeature for drift detection

- GREEN: budgetForFeature returns files + pages + 5 + 3, clamped at 100
- TestBudgetForFeature: 7 subcases passing
- driftMaxRounds retained temporarily during migration"
```

---

## Task 3: Wire the helper into `detectDriftForFeature`

**Files:**
- Modify: `internal/analyzer/drift.go` — change one call site.

**Step 3.1: Replace the call site**

Find the line in `detectDriftForFeature` (currently around `drift.go:155`):

```go
	_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(driftMaxRounds))
```

Replace with:

```go
	budget := budgetForFeature(len(entry.Files), len(pages))
	_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(budget))
```

**Step 3.2: Verify all drift tests still pass**

```bash
go test ./internal/analyzer/ -run 'TestDetectDrift|TestBudgetForFeature|Drift' -count=1
```

Expected: PASS. The existing drift tests don't assert on the round count, so they should be unaffected.

If any existing test fails, the wiring is wrong. Investigate before continuing — do NOT adjust the test to make it pass.

**Step 3.3: Commit**

```bash
git add internal/analyzer/drift.go
git commit -m "feat(analyzer): use dynamic budget for drift agent rounds

- detectDriftForFeature now passes budgetForFeature(files, pages)
  to WithMaxRounds instead of the constant 30
- Big features no longer hit ErrMaxRounds prematurely
- Existing drift tests unchanged and passing"
```

---

## Task 4: Update log lines to surface the budget

**Files:**
- Modify: `internal/analyzer/drift.go` — two log lines.

**Step 4.1: Update the per-feature info log**

In `DetectDrift`, find (currently around `drift.go:75`):

```go
		log.Infof("  checking drift for feature %q (%d pages)", entry.Feature.Name, len(pages))
```

This sits *outside* `detectDriftForFeature` (in `DetectDrift`) so it does not have direct access to the budget without recomputing. Move the log into `detectDriftForFeature` so it can read `budget` directly. Replace the existing line in `DetectDrift` with nothing (delete it), and add the following in `detectDriftForFeature` *immediately after* `budget := budgetForFeature(...)`:

```go
	log.Infof("  checking drift for feature %q (%d files, %d pages, budget %d rounds)",
		entry.Feature.Name, len(entry.Files), len(pages), budget)
```

**Step 4.2: Update the truncation warning**

Find (currently around `drift.go:157`):

```go
	if errors.Is(err, ErrMaxRounds) {
		log.Warnf("drift agent exceeded %d rounds for feature %q; returning %d accumulated findings", driftMaxRounds, entry.Feature.Name, len(findings))
		return findings, nil
	}
```

Replace with:

```go
	if errors.Is(err, ErrMaxRounds) {
		log.Warnf("drift agent exceeded budget of %d rounds for feature %q (%d files, %d pages); returning %d accumulated findings",
			budget, entry.Feature.Name, len(entry.Files), len(pages), len(findings))
		return findings, nil
	}
```

`budget` is in scope from step 3.1.

**Step 4.3: Verify drift tests still pass**

```bash
go test ./internal/analyzer/ -count=1
```

Expected: full suite green. Log lines aren't asserted on, so behavior is unchanged.

**Step 4.4: Commit**

```bash
git add internal/analyzer/drift.go
git commit -m "feat(analyzer): log dynamic budget in drift info and warning lines

- Per-feature info line now reports files, pages, and budget
- ErrMaxRounds warning reports the exceeded budget by value, not the
  removed constant"
```

---

## Task 5: Delete the obsolete `driftMaxRounds` constant

**Files:**
- Modify: `internal/analyzer/drift.go` — remove migration alias.

**Step 5.1: Confirm no references remain**

```bash
grep -rn "driftMaxRounds" internal/ cmd/
```

Expected: only the constant declaration in `drift.go`. If anything else surfaces, it was missed in tasks 3–4 and must be migrated first.

**Step 5.2: Delete the constant**

In `drift.go`, remove the trailing `driftMaxRounds = 30` line from the `const (...)` block introduced in step 2.1, including its comment if any.

**Step 5.3: Build, test, lint**

```bash
go build ./...
go test ./... -count=1
golangci-lint run
```

Expected: build succeeds, full test suite green, lint clean.

If `golangci-lint` flags an unused constant somewhere else, investigate — task 5.1 missed something.

**Step 5.4: Commit**

```bash
git add internal/analyzer/drift.go
git commit -m "refactor(analyzer): remove obsolete driftMaxRounds constant

- All drift call sites now use budgetForFeature
- Build, full test suite, and golangci-lint clean"
```

---

## Task 6: Coverage check

**Step 6.1: Run coverage on the analyzer package**

```bash
go test -coverprofile=coverage.out ./internal/analyzer/ && go tool cover -func=coverage.out | grep -E 'budgetForFeature|drift\.go'
```

Expected: `budgetForFeature` line shows `100.0%` (the table covers every branch — the clamp `if`, the fall-through return, and minimum-input boundary). The rest of `drift.go` should retain its prior coverage.

If `budgetForFeature` is below 100%, a branch is uncovered. Add the missing case to the table in Task 1.

**Step 6.2: Confirm package-level coverage stays ≥90%**

```bash
go test -coverprofile=coverage.out ./internal/analyzer/ && go tool cover -func=coverage.out | tail -1
```

Expected: `total: (statements) NN.N%` where NN.N ≥ 90.0.

If coverage dropped below 90%, the new log line or budget assignment may have introduced an uncovered branch in `detectDriftForFeature`. Inspect, add a targeted test if needed.

**Step 6.3: No commit needed** — coverage is a gate, not an artifact.

---

## Task 7: Update PROGRESS.md

**Files:**
- Modify or create: `PROGRESS.md` at repo root.

**Step 7.1: Append the entry**

```markdown
## Task: Dynamic Turn Budget for Drift Detection - COMPLETE
- Started: 2026-04-27
- Tests: TestBudgetForFeature (7 subcases) + existing drift tests, all passing
- Coverage: budgetForFeature 100%, internal/analyzer ≥90%
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: 2026-04-27
- Notes: Replaced fixed driftMaxRounds=30 with files+pages+5+3 clamped at
  100. Big features (>22 inputs) now get more rounds than the old cap.
  Plan: .plans/DYNAMIC_TURN_BUDGET_PLAN.md
  Design: .plans/DYNAMIC_TURN_BUDGET_DESIGN.md
```

**Step 7.2: Commit**

```bash
git add PROGRESS.md
git commit -m "docs(progress): record dynamic turn budget completion"
```

---

## Final verification

After all tasks complete, run:

```bash
go build ./...
go test ./... -count=1
golangci-lint run
gofmt -l . | grep . && echo 'unformatted files exist' || echo 'gofmt clean'
```

All four must succeed. Then push the branch and open a PR per `CLAUDE.md` rules:

```bash
git push -u origin dynamic-turn-budget
gh pr create --base main --title "feat(analyzer): dynamic turn budget for drift detection" --body "$(cat <<'EOF'
## Summary
- Replaces hardcoded `driftMaxRounds = 30` with a per-feature budget: `files + pages + 5 + 3`, clamped at 100.
- Big features no longer hit `ErrMaxRounds` and lose findings; small features stop carrying unused slack.
- Per-feature `Infof` and the `ErrMaxRounds` `Warnf` now log the budget for observability.

## Design
See `.plans/DYNAMIC_TURN_BUDGET_DESIGN.md`.

## Test plan
- [x] `TestBudgetForFeature` table covers minimum, mid-range, growth past old cap, and clamp boundary
- [x] Existing drift tests pass unchanged
- [x] `go test ./...` green
- [x] `golangci-lint run` clean
- [x] Coverage on `internal/analyzer` ≥ 90%; `budgetForFeature` at 100%
EOF
)"
```

---

## Out of scope (do not implement)

- `--drift-max-rounds` CLI flag. Defaults are sufficient; revisit only if users ask.
- Content-size-aware weighting (lines per file, etc.). Each tool call costs one round regardless of content size.
- Adaptive budgets driven by historical findings rates. No history exists.
