# Screenshots Opt-In Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Disable the missing-screenshot detection pass by default; gate it behind a single `--experimental-check-screenshots` flag (CLI) / `experimental-check-screenshots` input (Action). Remove the existing `--skip-screenshot-check` flag entirely.

**Architecture:** Single-flag inversion. The CLI registers the new bool flag; the call-sites at `internal/cli/analyze.go` flip from `!skipScreenshotCheck` to `experimentalCheckScreenshots`. Action manifest swaps the input, the composite step appends the new CLI flag conditionally, and the self-test workflow drops the obsolete input. Testscript and Go unit tests pivot accordingly. Living docs (README, CHANGELOG, VERIFICATION_PLAN) reflect the new surface.

**Tech Stack:** Go 1.26+, Cobra, testify, testscript, GitHub Actions YAML.

**Reference:** Design doc at `.plans/2026-04-30-screenshots-opt-in-design.md`.

---

## Pre-flight check

Confirm baseline is green before touching anything.

**Step 1: Run the full test suite**

Run: `go test ./... -count=1`
Expected: PASS (all green).

**Step 2: Run lint**

Run: `golangci-lint run`
Expected: zero issues.

If either fails, **stop**. Do not start the plan against a red baseline. Ask the user.

---

### Task 1: CLI flag rename (Go side)

Single atomic Go change: rename the variable, register the new flag, flip the condition, update unit tests in lockstep so the package builds at every step.

**Files:**
- Modify: `internal/cli/analyze.go:100` (var decl), `:439` (condition), `:468` (condition), `:484` (Inputs.ScreenshotsRan), `:506` (stdout label), `:538-539` (flag registration).
- Modify: `internal/cli/analyze_test.go:454,552,618` (drop the flag from args), `:469` ((skipped) assertion still applies under default-off), `:685-691` (rename the flag-existence test).
- Modify: `internal/cli/analyze_skip_drift_test.go:146,290,351` (drop the flag from args).

**Step 1: Write the failing test (rename the flag-existence test)**

In `internal/cli/analyze_test.go`, replace lines 685–691 with:

```go
func TestAnalyzeCmd_HasExperimentalCheckScreenshotsFlag(t *testing.T) {
	cmd := newAnalyzeCmd()
	f := cmd.Flags().Lookup("experimental-check-screenshots")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
	assert.Contains(t, f.Usage, "experimental")
	assert.Contains(t, f.Usage, "screenshot")
	// Old flag is removed entirely.
	old := cmd.Flags().Lookup("skip-screenshot-check")
	assert.Nil(t, old)
}
```

**Step 2: Run the test, watch it fail**

Run: `go test ./internal/cli/ -run TestAnalyzeCmd_HasExperimentalCheckScreenshotsFlag -v -count=1`
Expected: FAIL — `Lookup("experimental-check-screenshots")` returns nil because the flag isn't registered yet.

**Step 3: Edit `internal/cli/analyze.go` — variable declaration**

At line 100, replace:

```go
		skipScreenshotCheck bool
```

with:

```go
		experimentalCheckScreenshots bool
```

**Step 4: Edit `internal/cli/analyze.go` — flag registration**

At lines 538–539, replace:

```go
	cmd.Flags().BoolVar(&skipScreenshotCheck, "skip-screenshot-check", false,
		"skip the missing-screenshot detection pass")
```

with:

```go
	cmd.Flags().BoolVar(&experimentalCheckScreenshots, "experimental-check-screenshots", false,
		"enable experimental missing-screenshot detection pass")
```

**Step 5: Edit `internal/cli/analyze.go` — flip conditions**

Line 439 — change `if !skipScreenshotCheck {` → `if experimentalCheckScreenshots {`.
Line 468 — change `if !skipScreenshotCheck {` → `if experimentalCheckScreenshots {`.
Line 484 — change `ScreenshotsRan: !skipScreenshotCheck,` → `ScreenshotsRan: experimentalCheckScreenshots,`.
Line 506 — change `if skipScreenshotCheck {` → `if !experimentalCheckScreenshots {`. (The "(skipped)" label still attaches whenever the pass did not run; default-off case now drives it.)

**Step 6: Build to confirm Go side compiles**

Run: `go build ./...`
Expected: success.

**Step 7: Update `internal/cli/analyze_test.go` — drop the old flag from existing args**

In the three `run(&stdout, &stderr, []string{ ... })` blocks at lines ~440–471, ~540–567, ~610–625, remove the line `"--skip-screenshot-check",`. The default-off behavior now matches what those tests already assert (no screenshots.md, "(skipped)" annotation in stdout where checked).

Do **not** add `--experimental-check-screenshots` to any of these tests — they already exercise the default-off path, which is now the goal.

**Step 8: Update `internal/cli/analyze_skip_drift_test.go` — drop the old flag**

In the three places (lines 146, 290, 351), remove the line `"--skip-screenshot-check",`. Same reasoning: default behavior is now what these tests want.

**Step 9: Run the renamed test, watch it pass**

Run: `go test ./internal/cli/ -run TestAnalyzeCmd_HasExperimentalCheckScreenshotsFlag -v -count=1`
Expected: PASS.

**Step 10: Run the full cli package**

Run: `go test ./internal/cli/ -count=1`
Expected: all PASS. The `screenshots.md (skipped)` and `os.Stat(... screenshots.md) is NotExist` assertions in the existing tests should hold under the new default.

**Step 11: Run the full test suite to catch any other reference**

Run: `go test ./... -count=1`
Expected: all PASS (the action manifest test will still reference `skip-screenshot-check` and is updated in Task 3 — if it fails here, that's expected; if anything else fails, investigate before continuing).

If only `internal/action/manifest_test.go` fails with `input "skip-screenshot-check" missing`, proceed to step 12. Any other failure → diagnose first.

**Step 12: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go internal/cli/analyze_skip_drift_test.go
git commit -m "$(cat <<'EOF'
feat(cli): rename screenshot flag to --experimental-check-screenshots

- RED: TestAnalyzeCmd_HasExperimentalCheckScreenshotsFlag asserts new
  flag exists and old flag is gone
- GREEN: register experimentalCheckScreenshots, flip call-site
  conditions in analyze.go, drop the old flag from existing test args
- Status: cli package green; action manifest test deferred to next task
EOF
)"
```

---

### Task 2: testscript fixtures

The CLI now accepts `--experimental-check-screenshots` and rejects `--skip-screenshot-check`. Reflect that in the testscript suite.

**Files:**
- Delete: `cmd/find-the-gaps/testdata/script/skip_screenshot_check.txtar`
- Create: `cmd/find-the-gaps/testdata/script/experimental_check_screenshots.txtar`
- Leave alone: `cmd/find-the-gaps/testdata/script/default_screenshot_check.txtar` — it short-circuits without `--docs-url` and only asserts `scanned 0 files`. Still valid under the new default; nothing to change.

**Step 1: Delete the obsolete fixture**

Run: `rm cmd/find-the-gaps/testdata/script/skip_screenshot_check.txtar`

**Step 2: Write the failing fixture for the new flag**

Create `cmd/find-the-gaps/testdata/script/experimental_check_screenshots.txtar` with:

```
# analyze accepts --experimental-check-screenshots without error (no --docs-url,
# so the analyze command short-circuits after scanning — the flag just needs
# to parse).
mkdir repo
exec ftg analyze --repo repo --cache-dir $WORK/cache --experimental-check-screenshots
stdout 'scanned 0 files'
```

**Step 3: Run the testscript suite**

Run: `go test ./cmd/find-the-gaps/ -count=1`
Expected: PASS — both `default_screenshot_check.txtar` and `experimental_check_screenshots.txtar` succeed; the deleted fixture is no longer collected.

If the new fixture fails because `--experimental-check-screenshots` is not recognized, Task 1 was incomplete — back up and finish it.

**Step 4: Commit**

```bash
git add cmd/find-the-gaps/testdata/script/
git commit -m "$(cat <<'EOF'
test(testscript): pivot screenshot-flag fixtures to opt-in

- RED: experimental_check_screenshots.txtar exercises the new flag
- GREEN: deleted obsolete skip_screenshot_check.txtar
- Status: cmd/find-the-gaps testscript suite green
EOF
)"
```

---

### Task 3: Action manifest, composite step, and self-test workflow

The action exposes the input and forwards it to the CLI. Three files move together: the manifest test (assertion), `action.yml` (the manifest itself), and `.github/workflows/action-self-test.yml` (the consumer).

**Files:**
- Modify: `internal/action/manifest_test.go:34`
- Modify: `action.yml:16-19` (input declaration), `:78` (env var), `:87` (flag forwarding)
- Modify: `.github/workflows/action-self-test.yml:24` (drop the obsolete input)

**Step 1: Write the failing test (update manifest_test.go)**

In `internal/action/manifest_test.go`, replace line 34:

```go
	optional := []string{"create-issue", "skip-screenshot-check"}
```

with:

```go
	optional := []string{"create-issue", "experimental-check-screenshots"}
```

**Step 2: Run the test, watch it fail**

Run: `go test ./internal/action/ -run TestActionManifest_DeclaresExpectedInputs -v -count=1`
Expected: FAIL — `input "experimental-check-screenshots" missing` because action.yml still has the old name.

**Step 3: Edit `action.yml` — input declaration**

At lines 16–19, replace:

```yaml
  skip-screenshot-check:
    description: When 'true', skip screenshot-gap detection.
    required: false
    default: 'false'
```

with:

```yaml
  experimental-check-screenshots:
    description: When 'true', run the experimental missing-screenshot detection pass. Off by default.
    required: false
    default: 'false'
```

**Step 4: Edit `action.yml` — env var name in the analyze step**

At line 78, replace:

```yaml
        SKIP_SHOTS: ${{ inputs.skip-screenshot-check }}
```

with:

```yaml
        CHECK_SHOTS: ${{ inputs.experimental-check-screenshots }}
```

**Step 5: Edit `action.yml` — flag forwarding**

At line 87, replace:

```yaml
        if [[ "${SKIP_SHOTS}" == "true" ]]; then flags+=("--skip-screenshot-check"); fi
```

with:

```yaml
        if [[ "${CHECK_SHOTS}" == "true" ]]; then flags+=("--experimental-check-screenshots"); fi
```

**Step 6: Run the manifest test, watch it pass**

Run: `go test ./internal/action/ -run TestActionManifest_DeclaresExpectedInputs -v -count=1`
Expected: PASS.

**Step 7: Run the full action package**

Run: `go test ./internal/action/ -count=1`
Expected: PASS.

**Step 8: Edit `.github/workflows/action-self-test.yml` — drop the obsolete input**

At line 24, delete the entire line:

```yaml
          skip-screenshot-check: 'true'
```

The default behavior is now off; passing this input would be a "Unexpected input" warning anyway. The self-test continues to skip the screenshot pass via the new default.

**Step 9: Run the full test suite**

Run: `go test ./... -count=1`
Expected: ALL PASS.

**Step 10: Commit**

```bash
git add internal/action/manifest_test.go action.yml .github/workflows/action-self-test.yml
git commit -m "$(cat <<'EOF'
feat(action): rename screenshot input to experimental-check-screenshots

- RED: TestActionManifest_DeclaresExpectedInputs asserts new input name
- GREEN: rename input + env var in action.yml; drop obsolete input
  from action-self-test workflow
- Status: ./... green
EOF
)"
```

---

### Task 4: Documentation updates

Living docs only. Historical plans under `.plans/2026-04-23-*`, `2026-04-24-*`, `MISSING_SCREENSHOTS_IMPLEMENTATION_PLAN.md` are NOT touched — they record the feature as it shipped.

**Files:**
- Modify: `README.md:127, 237, 289`
- Modify: `CHANGELOG.md` (Unreleased section)
- Modify: `.plans/VERIFICATION_PLAN.md` (Scenario 5)

**Step 1: Update `README.md` — help-block excerpt**

At line 127, replace:

```
      --skip-screenshot-check   skip the missing-screenshot detection pass
```

with:

```
      --experimental-check-screenshots   enable experimental missing-screenshot detection pass
```

The help block is a verbatim copy of the binary's `--help`. If a help-sync hook regenerates it, prefer running the hook; otherwise edit by hand.

**Step 2: Update `README.md` — `screenshots.md` description**

At line 237, replace:

```
- **`screenshots.md`** — passages describing user-facing moments with no nearby screenshot. Written whenever the screenshot pass runs (zero findings produces a `_None found._` body). Not written when `--skip-screenshot-check` is passed.
```

with:

```
- **`screenshots.md`** — passages describing user-facing moments with no nearby screenshot. The detection pass is **experimental and off by default**; pass `--experimental-check-screenshots` to opt in. When the pass runs, this file is written even on zero findings (body is `_None found._`); when the pass is off, the file is not written.
```

**Step 3: Update `README.md` — Action input table**

At line 289, replace:

```
| `skip-screenshot-check` | no | `false` | Skip screenshot-gap detection |
```

with:

```
| `experimental-check-screenshots` | no | `false` | Run the experimental missing-screenshot detection pass |
```

**Step 4: Update `CHANGELOG.md` — Unreleased entry**

In the `## Unreleased` section (line 3), insert:

```markdown
### Changed
- **BREAKING:** missing-screenshot detection is now off by default and
  marked experimental. Pass `--experimental-check-screenshots` (CLI) or
  set `experimental-check-screenshots: 'true'` (Action) to opt in.
- **BREAKING:** removed `--skip-screenshot-check` flag and the
  `skip-screenshot-check` Action input. Workflows passing the old input
  will see a GitHub Actions "Unexpected input" warning until updated.
```

**Step 5: Update `.plans/VERIFICATION_PLAN.md` — Scenario 5**

Find the `### Scenario 5: Detect Missing Screenshots` section. Rewrite the **Steps** and **Success Criteria** to reflect the inverted default. The replacement reads:

```markdown
### Scenario 5: Detect Missing Screenshots (Experimental, Opt-In)

**Context**: Known-good fixture + docs site, but a page describes a UI moment with no nearby image. The screenshot pass is experimental and off by default — verification covers both the default-off path and the explicit opt-in.

**Steps**:
1. Run `find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url https://<docs>` (no extra flags).
2. Inspect `<projectDir>/`.
3. Re-run with `--experimental-check-screenshots`.
4. Inspect `<projectDir>/screenshots.md`.

**Success Criteria**:
- [ ] First run does NOT write `screenshots.md`.
- [ ] First run's stdout `reports:` block lists `screenshots.md (skipped)`.
- [ ] Second run writes `screenshots.md`.
- [ ] `screenshots.md` contains at least one gap for the known UI passage with all four fields populated.
- [ ] Second run's stdout lists `screenshots.md` without the `(skipped)` annotation.

**If Blocked**: If `screenshots.md` renders on the default run, the gating is broken. Stop and ask.
```

**Step 6: Verify nothing builds against the docs**

Run: `go test ./... -count=1`
Expected: PASS (no test reads these docs; this is a sanity rerun).

**Step 7: Commit**

```bash
git add README.md CHANGELOG.md .plans/VERIFICATION_PLAN.md
git commit -m "$(cat <<'EOF'
docs: screenshots opt-in surface — README, changelog, verification

- README: help block, screenshots.md description, Action input table
- CHANGELOG: Unreleased entry calls out the BREAKING flag rename and
  default flip
- VERIFICATION_PLAN: Scenario 5 rewritten for the default-off path
- Status: ./... green
EOF
)"
```

---

### Task 5: Final regression sweep

Confirm the whole tree is clean before handing back to the user.

**Step 1: Check for any lingering references to the old name**

Run: `git grep -nE 'skip-screenshot-check|skipScreenshotCheck|SkipScreenshotCheck' -- ':!*.plans/2026-04-23-*' ':!*.plans/2026-04-24-*' ':!*.plans/MISSING_SCREENSHOTS_IMPLEMENTATION_PLAN.md' ':!PROGRESS.md'`
Expected: zero output. Any hit outside historical plan docs and PROGRESS.md is a regression — fix before declaring done.

**Step 2: Format and lint**

Run: `gofmt -w . && goimports -w .`
Run: `golangci-lint run`
Expected: zero issues.

**Step 3: Final test pass**

Run: `go test ./... -count=1 -cover`
Expected: ALL PASS. Coverage in `internal/cli/` and `internal/action/` should not regress vs. baseline.

**Step 4: Update PROGRESS.md**

Append a new task entry per the CLAUDE.md template:

```markdown
## Task: Screenshots Opt-In - COMPLETE
- Started: <timestamp>
- Tests: all passing
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: <timestamp>
- Notes: Default flipped to off; new flag is --experimental-check-screenshots.
  --skip-screenshot-check removed entirely. Action input renamed to
  experimental-check-screenshots; self-test workflow updated. Historical
  plan docs left intact as record of original implementation.
```

**Step 5: Commit progress**

```bash
git add PROGRESS.md
git commit -m "$(cat <<'EOF'
chore(progress): record screenshots opt-in completion

- Status: all tests passing, lint clean, docs synced
EOF
)"
```

---

## Done conditions

All five must hold before declaring the plan complete:

- ✅ `go test ./... -count=1` is green
- ✅ `golangci-lint run` is clean
- ✅ No reference to `--skip-screenshot-check` or `skip-screenshot-check` anywhere except historical plan docs and PROGRESS.md
- ✅ `--experimental-check-screenshots` (CLI) and `experimental-check-screenshots` (Action) work end-to-end against the testscript fixtures and unit tests
- ✅ README, CHANGELOG, and VERIFICATION_PLAN reflect the new surface
