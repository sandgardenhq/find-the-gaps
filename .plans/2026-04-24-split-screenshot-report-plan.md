# Split Screenshot Report Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move "Missing Screenshots" findings out of `gaps.md` and into a new sibling report `screenshots.md`. `gaps.md` keeps Undocumented Code, Unmapped Features, and Stale Documentation.

**Architecture:** Split `reporter.WriteGaps` (drop the `screenshotGaps` argument) and add a new `reporter.WriteScreenshots` function. Caller in `internal/cli/analyze.go` invokes the second function only when `--skip-screenshot-check` was false. CLI output changes from a single-line `reports: ...` to a multi-line block with a `(skipped)` annotation on the screenshots line when applicable.

**Tech Stack:** Go 1.26+, `testify` assertions, `testscript` for CLI e2e. TDD is mandatory per `CLAUDE.md` — RED → GREEN → REFACTOR for every task. Commit after every successful cycle.

**Design reference:** `.plans/2026-04-24-split-screenshot-report-design.md`

---

## Write Policy (Canonical Reference)

| Case | `gaps.md` | `screenshots.md` |
|---|---|---|
| Pass ran, ≥1 gap    | written | written (findings)    |
| Pass ran, zero gaps | written | written (_None found._) |
| Pass skipped        | written | **not written**       |

## CLI Output (Canonical Reference)

Pass ran:

```
scanned 42 files, fetched 17 pages, 8 features mapped
reports:
  <dir>/mapping.md
  <dir>/gaps.md
  <dir>/screenshots.md
```

Pass skipped:

```
scanned 42 files, fetched 17 pages, 8 features mapped
reports:
  <dir>/mapping.md
  <dir>/gaps.md
  <dir>/screenshots.md (skipped)
```

---

## Task 1: Red — New test `TestWriteScreenshots_CreatesFile_WithFindings`

**Files:**
- Modify: `internal/reporter/reporter_test.go` — add new test. Do NOT remove the old ones yet.

**Step 1: Add the failing test**

Add below the existing screenshot tests (~ line 436 area):

```go
func TestWriteScreenshots_CreatesFile_WithFindings(t *testing.T) {
	dir := t.TempDir()
	gaps := []analyzer.ScreenshotGap{
		{
			PageURL:       "https://example.com/quickstart",
			PagePath:      "/cache/quickstart.md",
			QuotedPassage: "Run the command and see the output.",
			ShouldShow:    "Terminal showing the analyze summary with findings count.",
			SuggestedAlt:  "Terminal output of find-the-gaps analyze",
			InsertionHint: "after the paragraph ending '...see the output.'",
		},
		{
			PageURL:       "https://example.com/quickstart",
			PagePath:      "/cache/quickstart.md",
			QuotedPassage: "The dashboard shows open PRs.",
			ShouldShow:    "Dashboard with two open PRs visible.",
			SuggestedAlt:  "Dashboard with open PRs",
			InsertionHint: "after the heading '## Dashboard'",
		},
		{
			PageURL:       "https://example.com/setup",
			PagePath:      "/cache/setup.md",
			QuotedPassage: "Configure the CLI.",
			ShouldShow:    "The config file open in an editor.",
			SuggestedAlt:  "Configuration file",
			InsertionHint: "after the code block",
		},
	}
	require.NoError(t, reporter.WriteScreenshots(dir, gaps))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	// Root heading is promoted to # (the doc is now its own root).
	assert.Contains(t, s, "# Missing Screenshots")
	// Page grouping, first-occurrence order.
	assert.Regexp(t, `### https://example.com/quickstart[\s\S]*### https://example.com/setup`, s)
	// Each gap's four fields render.
	assert.Contains(t, s, "Run the command and see the output.")
	assert.Contains(t, s, "Terminal showing the analyze summary")
	assert.Contains(t, s, "Terminal output of find-the-gaps analyze")
	assert.Contains(t, s, "after the paragraph ending '...see the output.'")
}
```

**Step 2: Run to confirm RED**

```
go test ./internal/reporter/ -run TestWriteScreenshots_CreatesFile_WithFindings
```

Expected: FAIL with `undefined: reporter.WriteScreenshots`.

**Step 3: Commit the failing test**

```
git add internal/reporter/reporter_test.go
git commit -m "test(reporter): add failing test for WriteScreenshots with findings"
```

---

## Task 2: Green — Implement `WriteScreenshots` (minimal)

**Files:**
- Modify: `internal/reporter/reporter.go` — add new exported function. Do NOT change `WriteGaps` signature yet (keep both behaviors simultaneously until later tasks).

**Step 1: Add the function**

Append at the end of `internal/reporter/reporter.go` (after `WriteGaps`):

```go
// WriteScreenshots writes screenshots.md to dir. Call ONLY when the screenshot
// pass actually ran — a skipped pass must produce NO file. Zero-length gaps is
// valid and produces a "_None found._" body.
func WriteScreenshots(dir string, gaps []analyzer.ScreenshotGap) error {
	var sb strings.Builder
	sb.WriteString("# Missing Screenshots\n\n")

	if len(gaps) == 0 {
		sb.WriteString("_None found._\n")
		return os.WriteFile(filepath.Join(dir, "screenshots.md"), []byte(sb.String()), 0o644)
	}

	// Preserve first-occurrence page order.
	seen := map[string]bool{}
	var order []string
	byPage := map[string][]analyzer.ScreenshotGap{}
	for _, g := range gaps {
		if !seen[g.PageURL] {
			seen[g.PageURL] = true
			order = append(order, g.PageURL)
		}
		byPage[g.PageURL] = append(byPage[g.PageURL], g)
	}
	for _, page := range order {
		fmt.Fprintf(&sb, "### %s\n\n", page)
		for _, g := range byPage[page] {
			fmt.Fprintf(&sb, "- **Passage:** %q\n", g.QuotedPassage)
			fmt.Fprintf(&sb, "  - **Screenshot should show:** %s\n", g.ShouldShow)
			fmt.Fprintf(&sb, "  - **Alt text:** %s\n", g.SuggestedAlt)
			fmt.Fprintf(&sb, "  - **Insert:** %s\n\n", g.InsertionHint)
		}
	}
	return os.WriteFile(filepath.Join(dir, "screenshots.md"), []byte(sb.String()), 0o644)
}
```

**Step 2: Run test to verify GREEN**

```
go test ./internal/reporter/ -run TestWriteScreenshots_CreatesFile_WithFindings
```

Expected: PASS.

**Step 3: Run the full reporter suite (must stay green)**

```
go test ./internal/reporter/
```

Expected: all PASS.

**Step 4: Commit**

```
git add internal/reporter/reporter.go
git commit -m "feat(reporter): add WriteScreenshots for per-page screenshot findings report"
```

---

## Task 3: Red — Test for zero-gap "_None found._" body

**Files:**
- Modify: `internal/reporter/reporter_test.go`

**Step 1: Add the failing test**

```go
func TestWriteScreenshots_Empty_WritesNoneFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, reporter.WriteScreenshots(dir, nil))

	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)

	assert.Contains(t, s, "# Missing Screenshots")
	assert.Contains(t, s, "_None found._")
	// No per-page headers when there are no findings.
	assert.NotContains(t, s, "### ")
}
```

**Step 2: Run test — verify GREEN immediately**

```
go test ./internal/reporter/ -run TestWriteScreenshots_Empty_WritesNoneFound
```

Expected: PASS (Task 2's minimal impl already handles `len(gaps) == 0`). This is allowed: the test exercises a branch not covered by Task 1 and confirms the behavior. If it fails unexpectedly, fix Task 2's implementation instead of loosening the test.

**Step 3: Commit**

```
git add internal/reporter/reporter_test.go
git commit -m "test(reporter): cover WriteScreenshots empty-findings branch"
```

---

## Task 4: Red — Test that page order is first-occurrence

**Files:**
- Modify: `internal/reporter/reporter_test.go`

**Step 1: Add the failing test**

```go
func TestWriteScreenshots_PreservesPageOrder(t *testing.T) {
	dir := t.TempDir()
	gaps := []analyzer.ScreenshotGap{
		{PageURL: "https://example.com/second", QuotedPassage: "second-page passage."},
		{PageURL: "https://example.com/first", QuotedPassage: "first-page passage."},
		{PageURL: "https://example.com/second", QuotedPassage: "second-page passage 2."},
	}
	require.NoError(t, reporter.WriteScreenshots(dir, gaps))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	// /second appears first because it shows up first in the input.
	assert.Regexp(t, `### https://example.com/second[\s\S]*### https://example.com/first`, s)
}
```

**Step 2: Run test**

```
go test ./internal/reporter/ -run TestWriteScreenshots_PreservesPageOrder
```

Expected: PASS (Task 2's implementation already preserves order).

**Step 3: Commit**

```
git add internal/reporter/reporter_test.go
git commit -m "test(reporter): assert WriteScreenshots preserves first-occurrence page order"
```

---

## Task 5: Red — `WriteGaps` must no longer emit `Missing Screenshots`

Now we turn to removing the section from `gaps.md`. This is the signature-change step. We do it in two sub-steps: first add a failing test that uses the **new** signature, then flip the function.

**Files:**
- Modify: `internal/reporter/reporter_test.go`

**Step 1: Add the failing test**

```go
func TestWriteGaps_NoLongerRendersScreenshotsSection(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	// New 4-arg signature — no screenshot argument at all.
	require.NoError(t, reporter.WriteGaps(dir, mapping, []string{"search"}, nil))

	body, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "Missing Screenshots")
}
```

**Step 2: Run — verify RED**

```
go test ./internal/reporter/ -run TestWriteGaps_NoLongerRendersScreenshotsSection
```

Expected: FAIL — will not compile because `WriteGaps` still takes 5 args.

**Step 3: Commit the failing test**

```
git add internal/reporter/reporter_test.go
git commit -m "test(reporter): add failing test for 4-arg WriteGaps (no screenshots section)"
```

---

## Task 6: Green — Change `WriteGaps` signature and remove screenshot section

**Files:**
- Modify: `internal/reporter/reporter.go`
- Modify: `internal/reporter/reporter_test.go` (update all other `WriteGaps` callers)
- Modify: `internal/cli/analyze.go` (update the two call sites)

**Step 1: Update `reporter.WriteGaps` signature and drop the screenshot section**

In `internal/reporter/reporter.go`:

- Change signature to: `func WriteGaps(dir string, mapping analyzer.FeatureMap, allDocFeatures []string, drift []analyzer.DriftFinding) error`.
- Update the doc comment: remove the "Missing Screenshots" line; keep the other three section descriptions.
- Delete the entire block that begins `// Missing screenshots — omitted when there are none.` through the end of its `if len(screenshotGaps) > 0 { ... }` block (lines ~157–180).
- Remove the now-unused `screenshotGaps []analyzer.ScreenshotGap` parameter.

**Step 2: Fix existing reporter test callers**

Every existing call of `reporter.WriteGaps(...)` in `internal/reporter/reporter_test.go` passes a 5th argument (today it's usually `nil` or `gaps`). Drop the 5th argument from all of them.

Grep first to find them:

```
grep -n 'reporter.WriteGaps' internal/reporter/reporter_test.go
```

Update every hit to the 4-arg form. The two obsolete screenshot-section tests (`TestWriteGaps_MissingScreenshotsSection`, `TestWriteGaps_MissingScreenshotsEmpty_OmitsSection`) are removed entirely in Task 7 — for this task, just convert their `WriteGaps` call to 4 args temporarily to keep the file compiling; Task 7 will delete them wholesale.

**Step 3: Fix the two `WriteGaps` callers in `internal/cli/analyze.go`**

Current state (line ~315):

```go
driftOnFinding := func(accumulated []analyzer.DriftFinding) error {
    return reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, accumulated, nil)
}
```

Change to:

```go
driftOnFinding := func(accumulated []analyzer.DriftFinding) error {
    return reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, accumulated)
}
```

Current state (line ~359):

```go
if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings, screenshotGaps); err != nil {
    return fmt.Errorf("write gaps: %w", err)
}
```

Change to:

```go
if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings); err != nil {
    return fmt.Errorf("write gaps: %w", err)
}
```

Do NOT add a `WriteScreenshots` call yet — that comes in Task 8.

**Step 4: Build and run**

```
go build ./...
go test ./internal/reporter/ ./internal/cli/
```

Expected: build succeeds; Task 5's new test now PASSES; all other reporter tests still pass; CLI tests compile and pass (the screenshot-related behavior tests from Task 9 are not added yet, so CLI is unaffected here).

If any existing CLI test fails because it was asserting on the absence of `screenshots.md`, that's expected — leave those failures for the task that adds the CLI changes. If a failure is unrelated, stop and debug.

**Step 5: Commit**

```
git add internal/reporter/reporter.go internal/reporter/reporter_test.go internal/cli/analyze.go
git commit -m "refactor(reporter): drop screenshotGaps arg from WriteGaps; remove Missing Screenshots section"
```

---

## Task 7: Green — Delete the obsolete `WriteGaps` screenshot tests

**Files:**
- Modify: `internal/reporter/reporter_test.go`

**Step 1: Delete two tests**

Delete `TestWriteGaps_MissingScreenshotsSection` (the full function) and `TestWriteGaps_MissingScreenshotsEmpty_OmitsSection` (the full function). They assert behavior that no longer exists.

**Step 2: Build and run**

```
go test ./internal/reporter/
```

Expected: all PASS. No compile errors.

**Step 3: Commit**

```
git add internal/reporter/reporter_test.go
git commit -m "test(reporter): remove obsolete WriteGaps screenshot-section tests"
```

---

## Task 8: Green — Wire `WriteScreenshots` into `analyze`

**Files:**
- Modify: `internal/cli/analyze.go`

**Step 1: Call `WriteScreenshots` after `WriteGaps`, gated on the skip flag**

Locate the block (~line 359) that now reads:

```go
if err := reporter.WriteMapping(projectDir, productSummary, featureMap, docsFeatureMap); err != nil {
    return fmt.Errorf("write mapping: %w", err)
}
if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings); err != nil {
    return fmt.Errorf("write gaps: %w", err)
}
```

Add, immediately after:

```go
if !skipScreenshotCheck {
    if err := reporter.WriteScreenshots(projectDir, screenshotGaps); err != nil {
        return fmt.Errorf("write screenshots: %w", err)
    }
}
```

**Step 2: Replace the single-line `reports:` print with the multi-line block**

Locate (~line 363):

```go
_, _ = fmt.Fprintf(cmd.OutOrStdout(),
    "scanned %d files, fetched %d pages, %d features mapped\nreports: %s/mapping.md, %s/gaps.md\n",
    len(scan.Files), len(pages), len(featureMap), projectDir, projectDir)
```

Replace with:

```go
screenshotsLine := fmt.Sprintf("  %s/screenshots.md", projectDir)
if skipScreenshotCheck {
    screenshotsLine += " (skipped)"
}
_, _ = fmt.Fprintf(cmd.OutOrStdout(),
    "scanned %d files, fetched %d pages, %d features mapped\nreports:\n  %s/mapping.md\n  %s/gaps.md\n%s\n",
    len(scan.Files), len(pages), len(featureMap),
    projectDir, projectDir, screenshotsLine)
```

**Step 3: Build and run the existing CLI suite**

```
go build ./...
go test ./internal/cli/
```

Expected: build passes. Most tests pass. The existing tests that `strings.Contains(..., "scanned")` still pass because that substring is still present. Any test that grep'd for the exact old `reports: ...` single-line format may need adjustment — if that happens, fix those tests to accept the new block (they should still only assert on the substrings they care about).

**Step 4: Commit**

```
git add internal/cli/analyze.go
git commit -m "feat(cli): write screenshots.md separately and list reports on multiple lines"
```

---

## Task 9: Red — CLI test asserts `screenshots.md` is written on the happy path

**Files:**
- Modify: `internal/cli/analyze_test.go`

**Step 1: Add a new assertion inside `TestAnalyze_screenshotCheck_exercisesPath`**

Find the block right before the function ends (after the `if !strings.Contains(combined, "scanned")` check). Append:

```go
// screenshots.md must exist because the screenshot pass ran.
if _, err := os.Stat(filepath.Join(projectDir, "screenshots.md")); err != nil {
    t.Errorf("expected screenshots.md to exist; got: %v", err)
}
// Stdout must list it in the reports block.
if !strings.Contains(combined, "screenshots.md") {
    t.Errorf("expected 'screenshots.md' in output; got: %s", combined)
}
// It must NOT be annotated as skipped on the happy path.
if strings.Contains(combined, "screenshots.md (skipped)") {
    t.Errorf("unexpected 'skipped' annotation on happy path; got: %s", combined)
}
```

**Step 2: Run test**

```
go test ./internal/cli/ -run TestAnalyze_screenshotCheck_exercisesPath
```

Expected: PASS (because Task 8 already wired the behavior). This test effectively **pins** the Task-8 behavior so it cannot regress.

If it fails, go back to Task 8 and fix — do not loosen this test.

**Step 3: Commit**

```
git add internal/cli/analyze_test.go
git commit -m "test(cli): assert screenshots.md is written on happy path"
```

---

## Task 10: Red — CLI test asserts `screenshots.md` is NOT written when skipped

**Files:**
- Modify: `internal/cli/analyze_test.go`

`TestAnalyze_allCached_noLLMCalls` already runs with `--skip-screenshot-check` (see line ~396). Reuse it: it's the perfect place to assert the skipped-branch behavior.

**Step 1: Add assertions to `TestAnalyze_allCached_noLLMCalls`**

At the end of the function, append:

```go
// Screenshot pass was skipped — screenshots.md must NOT exist.
if _, err := os.Stat(filepath.Join(projectDir, "screenshots.md")); !os.IsNotExist(err) {
    t.Errorf("expected screenshots.md to NOT exist when skipped; Stat err=%v", err)
}
// Stdout lists it as (skipped).
if !strings.Contains(combined, "screenshots.md (skipped)") {
    t.Errorf("expected '(skipped)' annotation in output; got: %s", combined)
}
```

If `projectDir` is not already defined in scope, compute it locally:

```go
projectDir := filepath.Join(cacheBase, projectName)
```

**Step 2: Run test**

```
go test ./internal/cli/ -run TestAnalyze_allCached_noLLMCalls
```

Expected: PASS.

**Step 3: Commit**

```
git add internal/cli/analyze_test.go
git commit -m "test(cli): assert screenshots.md omitted (and marked skipped) when pass is skipped"
```

---

## Task 11: Coverage check

**Step 1: Run coverage on the affected packages**

```
go test -coverprofile=coverage.out ./internal/reporter/ ./internal/cli/
go tool cover -func=coverage.out | grep -E 'reporter\.go|analyze\.go'
```

**Step 2: Verify the gate**

Expected: ≥90% statement coverage on `internal/reporter/reporter.go`. Coverage on touched lines of `internal/cli/analyze.go` should be maintained at its pre-change baseline (run the comparison against `origin/main` if uncertain).

If coverage dipped below 90% on `reporter.go`, add targeted tests until it clears the gate. Do not lower the gate.

**Step 3: No commit required** unless new tests were added to lift coverage — in which case commit them with:

```
git commit -m "test(reporter): add coverage for WriteScreenshots branches"
```

---

## Task 12: Update the verification plan

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`

**Step 1: Replace Scenario 5 with the updated steps**

Find the existing "Scenario 5: Detect Missing Screenshots" block. Replace with:

```markdown
### Scenario 5: Detect Missing Screenshots

**Context**: Known-good fixture + docs site, but a page describes a UI moment with no nearby image.

**Steps**:
1. Run `find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url https://<docs>`.
2. Inspect `<projectDir>/screenshots.md`.
3. Re-run with `--skip-screenshot-check`.
4. Inspect the output directory.

**Success Criteria**:
- [ ] First run writes `screenshots.md`; `gaps.md` does NOT contain a `Missing Screenshots` section.
- [ ] `screenshots.md` contains at least one gap for the known UI passage with all four fields populated.
- [ ] Stdout lists `screenshots.md` in the `reports:` block.
- [ ] Second run does NOT write `screenshots.md`.
- [ ] Second run's stdout lists `screenshots.md (skipped)`.

**If Blocked**: If `screenshots.md` renders on the skipped run, the gating is broken. Stop and ask.
```

**Step 2: Commit**

```
git add .plans/VERIFICATION_PLAN.md
git commit -m "docs(verification): update Scenario 5 for split screenshots.md report"
```

---

## Task 13: Final gates — build, lint, full test suite

**Step 1: Build**

```
go build ./...
```

Expected: no errors.

**Step 2: Lint**

```
golangci-lint run
```

Expected: zero issues. Fix anything it reports, commit each fix as its own commit (`style: ...` or similar).

**Step 3: Full test suite**

```
go test ./...
```

Expected: all PASS.

**Step 4: Full coverage**

```
go test -cover ./...
```

Expected: every package ≥90% statement coverage.

**Step 5: Update `PROGRESS.md`**

Append a new section per `CLAUDE.md` §8 format:

```markdown
## Task: Split screenshots into their own report - COMPLETE
- Started: 2026-04-24
- Tests: <N> passing, 0 failing
- Coverage: <percentage on affected packages>
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: 2026-04-24
- Notes: `gaps.md` no longer contains a Missing Screenshots section; new `screenshots.md` written whenever the screenshot pass runs (including zero-findings case). Skipped pass writes no file and is annotated in the CLI reports block.
```

**Step 6: Commit**

```
git add PROGRESS.md
git commit -m "docs(progress): record split-screenshots task completion"
```

---

## Task 14: Open PR

Push the branch and open the PR against `main`. Per `CLAUDE.md`:

- Use a **merge commit**, not squash.
- PR title: `feat(reporter): split missing-screenshot findings into their own report`
- PR body summary: three bullets — the motivation (signal-to-noise), the shape of the change (new `screenshots.md`, unchanged `gaps.md` minus one section), and the CLI output change.
- Test plan: reference `.plans/VERIFICATION_PLAN.md` Scenario 5.

```
gh pr create --base main --title "feat(reporter): split missing-screenshot findings into their own report" --body "$(cat <<'EOF'
## Summary
- Screenshot findings move from a section in `gaps.md` to a new sibling `screenshots.md`. Motivation: drift and screenshot lists each run long, making `gaps.md` hard to scan.
- `screenshots.md` is written whenever the screenshot pass actually runs (including a `_None found._` body for zero findings). When `--skip-screenshot-check` is set, the file is NOT written.
- CLI output's `reports:` suffix is now a multi-line block; the screenshots line carries a `(skipped)` annotation when applicable.

## Test plan
- [x] Unit: `go test ./internal/reporter/`
- [x] Unit: `go test ./internal/cli/`
- [x] Full: `go test ./...`
- [x] Coverage ≥90% on affected packages
- [ ] Real-system: run Scenario 5 from `.plans/VERIFICATION_PLAN.md`
EOF
)"
```

---

## Post-Execution Checklist

Before declaring this plan complete, every item must be true:

- [ ] All 14 tasks' commits exist on the branch.
- [ ] `go test ./...` — PASS.
- [ ] `go build ./...` — PASS.
- [ ] `golangci-lint run` — clean.
- [ ] `go test -cover ./internal/reporter/` shows ≥90%.
- [ ] `gaps.md` produced by the tool no longer contains a `Missing Screenshots` section.
- [ ] `screenshots.md` is produced whenever the pass runs, and NOT produced when skipped.
- [ ] CLI `reports:` output is multi-line with `(skipped)` annotation when applicable.
- [ ] `.plans/VERIFICATION_PLAN.md` Scenario 5 is updated.
- [ ] `PROGRESS.md` has an entry for this task.
- [ ] PR is open against `main` with a merge-commit (not squash).
