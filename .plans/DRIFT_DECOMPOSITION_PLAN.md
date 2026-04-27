# Drift Decomposition Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Split the per-feature drift agent into a Typical-tier investigator (tool-use loop, gathers evidence) and a Large-tier judge (single non-tool call, adjudicates), to cut Opus tokens and per-feature latency.

**Architecture:** Replace `detectDriftForFeature` (one Large-tier agent loop, up to 30 rounds, tool set `read_file`/`read_page`/`add_finding`) with a two-stage flow: `investigateFeatureDrift` runs the loop on `tiering.Typical()` with tool `note_observation` (records evidence, not findings) and returns `[]driftObservation`; `judgeFeatureDrift` makes one `CompleteJSON` call on `tiering.Large()` to turn observations into `[]DriftIssue`. Tier-validation's tool-use requirement moves from Large to Typical. Round cap rises from 30 to 50.

**Tech Stack:** Go 1.26, testify, charmbracelet/log. No new dependencies.

**Reference:** Design at `.plans/DRIFT_DECOMPOSITION_DESIGN.md`.

**Project rules (read before starting):**
- `CLAUDE.md` is the contract. TDD is mandatory: failing test first, watch it fail, minimal code to pass, commit. No exceptions.
- Test files live next to production files (`foo.go` → `foo_test.go`), package `analyzer_test` for black-box tests.
- Every LLM prompt — static or templated — must have a `// PROMPT:` comment immediately above it.
- Commit after each TDD cycle. Never amend; always new commits. Branch is `cape-town-v1`; do not commit to `main`.
- Commands: `go test ./...`, `go test -cover ./...`, `go build ./...`, `golangci-lint run`, `gofmt -w . && goimports -w .`.

---

## Task 1: Add `driftObservation` type and `note_observation` tool

**Goal:** Introduce the new evidence-collection contract without removing anything yet. The new tool and type live next to the old `addFindingTool` / `DriftIssue`. Existing behavior must not change.

**Files:**
- Modify: `internal/analyzer/drift.go`
- Test: `internal/analyzer/drift_test.go`

### Step 1: Write the failing test

Add to `internal/analyzer/drift_test.go` (package `analyzer_test`). Note: `noteObservationTool` is unexported, so we test it indirectly through a small package-internal export OR we accept that this tool is exercised through the larger investigator test in Task 2. Choose the latter — skip the unit test for the tool here and write it as part of Task 2's investigator test, which exercises the tool through `runAgentLoop`. Move directly to Task 2.

**(Skip Task 1 implementation until Task 2 needs it.)**

---

## Task 2: Add `investigateFeatureDrift`

**Goal:** New function that runs the agent loop with `read_file`, `read_page`, `note_observation` tools and returns `[]driftObservation`. Coexists with `detectDriftForFeature` for now.

**Files:**
- Modify: `internal/analyzer/drift.go` (add new types, tool, function)
- Test: `internal/analyzer/drift_test.go`

### Step 1: Write the failing test for the happy path

Add to `internal/analyzer/drift_test.go`:

```go
// noteObservation builds a ChatMessage that invokes note_observation with one
// observation. Test helper.
func noteObservation(page, docQuote, codeQuote, concern string) analyzer.ChatMessage {
	args, _ := json.Marshal(map[string]string{
		"page":       page,
		"doc_quote":  docQuote,
		"code_quote": codeQuote,
		"concern":    concern,
	})
	return analyzer.ChatMessage{
		Role: "assistant",
		ToolCalls: []analyzer.ToolCall{
			{ID: "obs_" + concern, Name: "note_observation", Arguments: string(args)},
		},
	}
}

func TestInvestigateFeatureDrift_RecordsObservations(t *testing.T) {
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs/x", "doc says A", "code says B", "mismatch on A vs B"),
			driftDone(),
		},
	}
	entry := analyzer.FeatureEntry{
		Feature: analyzer.CodeFeature{Name: "auth", Description: "login"},
		Files:   []string{"auth.go"},
		Symbols: []string{"Login"},
	}
	pageReader := func(url string) (string, error) { return "# Docs", nil }

	obs, err := analyzer.InvestigateFeatureDrift(context.Background(), client, entry, []string{"https://docs/x"}, pageReader, t.TempDir())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "https://docs/x", obs[0].Page)
	assert.Equal(t, "doc says A", obs[0].DocQuote)
	assert.Equal(t, "code says B", obs[0].CodeQuote)
	assert.Equal(t, "mismatch on A vs B", obs[0].Concern)
}
```

This requires exporting `InvestigateFeatureDrift` and `DriftObservation` (capitalized) — follow Go convention: export only when needed for tests in `analyzer_test`. Add a test-only export shim if you prefer to keep them unexported: `internal/analyzer/drift_export_test.go` with `var InvestigateFeatureDrift = investigateFeatureDrift` etc.

**Recommendation:** use the test-only export shim pattern (matches `agent_loop_export_test.go:10`).

### Step 2: Run the test, verify it fails

```
go test ./internal/analyzer/ -run TestInvestigateFeatureDrift -v
```

Expected: compile error (`investigateFeatureDrift` and `DriftObservation` don't exist yet).

### Step 3: Implement the type, tool, and function

In `internal/analyzer/drift.go`, add (do not yet remove the old code):

```go
// driftObservation is one piece of evidence the investigator surfaces for the
// judge to adjudicate. Both quotes are required and must be verbatim — they are
// the entire input the judge sees about this candidate.
type driftObservation struct {
	Page      string `json:"page"`
	DocQuote  string `json:"doc_quote"`
	CodeQuote string `json:"code_quote"`
	Concern   string `json:"concern"`
}

// noteObservationTool returns a Tool that appends each LLM-recorded observation
// to out. Bad arguments are reported back to the LLM as a tool result string so
// the loop continues.
func noteObservationTool(out *[]driftObservation) Tool {
	return Tool{
		Name:        "note_observation",
		Description: "Record one piece of evidence about possible documentation drift. Both doc_quote and code_quote must be verbatim. Call once per distinct observation. When you have nothing more to record, reply with plain text.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"page":       map[string]any{"type": "string", "description": "Doc page URL the observation refers to, or empty string if page-agnostic."},
				"doc_quote":  map[string]any{"type": "string", "description": "Verbatim passage from the docs."},
				"code_quote": map[string]any{"type": "string", "description": "Verbatim excerpt from the source code."},
				"concern":    map[string]any{"type": "string", "description": "One sentence: what looks off."},
			},
			"required": []string{"page", "doc_quote", "code_quote", "concern"},
		},
		Execute: func(_ context.Context, rawArgs string) (string, error) {
			var o driftObservation
			if err := json.Unmarshal([]byte(rawArgs), &o); err != nil {
				return fmt.Sprintf("invalid arguments: %v", err), nil
			}
			*out = append(*out, o)
			return "recorded", nil
		},
	}
}

func investigateFeatureDrift(
	ctx context.Context,
	client ToolLLMClient,
	entry FeatureEntry,
	pages []string,
	pageReader func(url string) (string, error),
	repoRoot string,
) ([]driftObservation, error) {
	var pageSummaries []string
	for _, url := range pages {
		pageSummaries = append(pageSummaries, fmt.Sprintf("- %s", url))
	}

	var observations []driftObservation
	tools := []Tool{
		readFileTool(repoRoot),
		readPageTool(pageReader),
		noteObservationTool(&observations),
	}

	// PROMPT: Investigates a feature for documentation drift by reading source files and doc pages, recording each piece of evidence via note_observation. The investigator gathers; it does not adjudicate.
	systemPrompt := fmt.Sprintf(`You are investigating documentation accuracy for a software feature.

Feature: %s
Code description: %s
Implemented in: %s
Symbols: %s

Documentation pages:
%s

You have tools available to read source files and documentation pages in full.
Use them to investigate as needed.

Your job is to surface candidate documentation drift. For each thing that
*might* be wrong or missing in the docs, call note_observation with:
- page: the doc URL (or empty string)
- doc_quote: the exact passage from the docs that concerns you
- code_quote: the exact excerpt from the source code that contradicts or
  is missing from the docs
- concern: one sentence describing what looks off

Quote verbatim. Include enough context in code_quote that someone reading
just the observation can understand the contradiction (e.g. include the full
function signature line, not just an identifier).

Do not decide whether something IS drift — just record what looks suspicious.
A reviewer will adjudicate later.

When you have nothing more to record, reply with plain text (e.g. "done").
If you find nothing suspicious, reply with plain text immediately without
calling note_observation at all.`,
		entry.Feature.Name,
		entry.Feature.Description,
		strings.Join(entry.Files, ", "),
		strings.Join(entry.Symbols, ", "),
		strings.Join(pageSummaries, "\n"),
	)

	messages := []ChatMessage{{Role: "user", Content: systemPrompt}}

	_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(driftMaxRounds))
	if errors.Is(err, ErrMaxRounds) {
		log.Warnf("drift investigator exceeded %d rounds for feature %q; handing %d observations to judge", driftMaxRounds, entry.Feature.Name, len(observations))
		return observations, nil
	}
	if err != nil {
		return nil, err
	}
	return observations, nil
}
```

Add the test-only export in `internal/analyzer/drift_export_test.go`:

```go
package analyzer

// Test-only exports for analyzer_test (drift package).
var InvestigateFeatureDrift = investigateFeatureDrift

type DriftObservation = driftObservation
```

### Step 4: Run the test, verify it passes

```
go test ./internal/analyzer/ -run TestInvestigateFeatureDrift -v
```

Expected: PASS.

### Step 5: Add the round-cap test

```go
func TestInvestigateFeatureDrift_MaxRoundsHit_ReturnsAccumulated(t *testing.T) {
	// Two observations recorded, then loop exhausts without "done".
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("p1", "d1", "c1", "concern 1"),
			noteObservation("p2", "d2", "c2", "concern 2"),
			// Subsequent rounds: keep calling read_file forever (driftStubClient
			// reuses last element when responses is exhausted, so script enough
			// observations to exceed the cap deterministically). Use a helper.
		},
	}
	// Replace responses with a slice large enough to drive past the cap.
	for i := 0; i < 60; i++ {
		client.responses = append(client.responses,
			noteObservation(fmt.Sprintf("p%d", i+3), "d", "c", "concern"))
	}

	entry := analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"a.go"}}
	obs, err := analyzer.InvestigateFeatureDrift(context.Background(), client, entry, []string{"https://x"}, func(string) (string, error) { return "", nil }, t.TempDir())
	require.NoError(t, err, "round-cap exhaustion must not be a hard error")
	assert.GreaterOrEqual(t, len(obs), 2, "all observations recorded before cap must be returned")
}
```

Run: `go test ./internal/analyzer/ -run TestInvestigateFeatureDrift -v`. Expected: PASS.

### Step 6: Format, lint, commit

```
gofmt -w . && goimports -w .
golangci-lint run ./internal/analyzer/...
go test ./internal/analyzer/ -count=1
git add internal/analyzer/drift.go internal/analyzer/drift_test.go internal/analyzer/drift_export_test.go
git commit -m "feat(drift): add investigator stage with note_observation tool

- RED: TestInvestigateFeatureDrift covers happy path + round-cap accumulation
- GREEN: investigateFeatureDrift runs the agent loop with note_observation
  on a passed-in ToolLLMClient, returns []driftObservation
- Coexists with old detectDriftForFeature; not yet wired into DetectDrift

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 3: Add `judgeFeatureDrift`

**Goal:** Single non-tool `CompleteJSON` call on a passed-in `LLMClient` that turns `[]driftObservation` into `[]DriftIssue`.

**Files:**
- Modify: `internal/analyzer/drift.go`
- Test: `internal/analyzer/drift_test.go`

### Step 1: Write the failing test for the empty-observations short-circuit

```go
func TestJudgeFeatureDrift_NoObservations_SkipsLLM(t *testing.T) {
	client := &driftStubClient{} // any call increments completeCalls
	feature := analyzer.CodeFeature{Name: "auth", Description: "login"}

	issues, err := analyzer.JudgeFeatureDrift(context.Background(), client, feature, nil)
	require.NoError(t, err)
	assert.Nil(t, issues)
	assert.Equal(t, 0, client.completeCalls, "Judge must not call the LLM with zero observations")
}
```

### Step 2: Write the failing test for the happy path

```go
func TestJudgeFeatureDrift_ProducesIssues(t *testing.T) {
	// driftStubClient.CompleteJSON dispatches to Complete; set a completeFunc
	// that returns canned JSON.
	client := &driftStubClient{
		completeFunc: func(_ context.Context, prompt string) (string, error) {
			// Sanity: dossier must mention the feature name and an observation quote.
			if !strings.Contains(prompt, "auth") || !strings.Contains(prompt, "doc says X") {
				return "", fmt.Errorf("prompt missing expected fields: %s", prompt)
			}
			return `{"issues":[{"page":"https://docs/x","issue":"docs claim X but code does Y"}]}`, nil
		},
	}
	feature := analyzer.CodeFeature{Name: "auth", Description: "login"}
	obs := []analyzer.DriftObservation{
		{Page: "https://docs/x", DocQuote: "doc says X", CodeQuote: "code does Y", Concern: "mismatch"},
	}

	issues, err := analyzer.JudgeFeatureDrift(context.Background(), client, feature, obs)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "https://docs/x", issues[0].Page)
	assert.Contains(t, issues[0].Issue, "docs claim X but code does Y")
}
```

### Step 3: Verify both fail

```
go test ./internal/analyzer/ -run TestJudgeFeatureDrift -v
```

Expected: compile error (`judgeFeatureDrift` doesn't exist).

### Step 4: Implement

In `internal/analyzer/drift.go`:

```go
var judgeSchema = JSONSchema{
	Name:        "drift_judge_issues",
	Description: "Final adjudicated drift issues for one feature.",
	Schema: json.RawMessage(`{
      "type": "object",
      "properties": {
        "issues": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "page":  {"type": "string"},
              "issue": {"type": "string"}
            },
            "required": ["page", "issue"],
            "additionalProperties": false
          }
        }
      },
      "required": ["issues"],
      "additionalProperties": false
    }`),
}

type judgeResponse struct {
	Issues []DriftIssue `json:"issues"`
}

func judgeFeatureDrift(
	ctx context.Context,
	client LLMClient,
	feature CodeFeature,
	observations []driftObservation,
) ([]DriftIssue, error) {
	if len(observations) == 0 {
		return nil, nil
	}

	var b strings.Builder
	for i, o := range observations {
		fmt.Fprintf(&b, "[%d] page: %s\n    docs say: %q\n    code shows: %q\n    concern: %s\n",
			i+1, o.Page, o.DocQuote, o.CodeQuote, o.Concern)
	}

	// PROMPT: Adjudicates a list of candidate drift observations for one feature, dropping false alarms, merging duplicates, and emitting actionable documentation feedback as DriftIssues.
	prompt := fmt.Sprintf(`You are reviewing candidate documentation drift observations for one software feature.

Feature: %s
Description: %s

Candidate drift observations from investigation:
%s

For each observation, decide: real drift, false alarm, or duplicate of another.
Emit one DriftIssue per real drift. Merge duplicates into a single issue.
Drop false alarms entirely.

Each emitted issue must be actionable documentation feedback — describe what
is wrong or missing in the docs, not what the code does. One or two sentences.

If every observation is a false alarm, emit an empty "issues" array.`,
		feature.Name, feature.Description, b.String())

	raw, err := client.CompleteJSON(ctx, prompt, judgeSchema)
	if err != nil {
		return nil, fmt.Errorf("judgeFeatureDrift %q: %w", feature.Name, err)
	}
	var resp judgeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("judgeFeatureDrift %q: invalid JSON response: %w", feature.Name, err)
	}
	return resp.Issues, nil
}
```

Add to `drift_export_test.go`:

```go
var JudgeFeatureDrift = judgeFeatureDrift
```

### Step 5: Run tests, verify pass

```
go test ./internal/analyzer/ -run TestJudgeFeatureDrift -v
```

Expected: PASS.

### Step 6: Format, lint, commit

```
gofmt -w . && goimports -w .
golangci-lint run ./internal/analyzer/...
go test ./internal/analyzer/ -count=1
git add internal/analyzer/drift.go internal/analyzer/drift_test.go internal/analyzer/drift_export_test.go
git commit -m "feat(drift): add judge stage as single CompleteJSON call

- RED: TestJudgeFeatureDrift covers empty short-circuit + happy path
- GREEN: judgeFeatureDrift packages observations into a dossier prompt,
  calls CompleteJSON with judgeSchema, returns []DriftIssue
- Empty observations skip the LLM call entirely

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 4: Wire investigator + judge into `DetectDrift`

**Goal:** Replace the call to `detectDriftForFeature` with `investigateFeatureDrift` (on Typical) + `judgeFeatureDrift` (on Large). Update tier-routing tests. Old code stays for one more task to allow easy rollback if the test refactor fails.

**Files:**
- Modify: `internal/analyzer/drift.go` (DetectDrift body)
- Test: `internal/analyzer/drift_test.go`

### Step 1: Update the failing test for tier routing

Replace `TestDetectDrift_UsesLargeAndSmall` (drift_test.go:552) with:

```go
func TestDetectDrift_UsesSmallTypicalLarge(t *testing.T) {
	// Verify tier dispatch:
	//   classifyDriftPages    -> Small
	//   investigator agent    -> Typical
	//   judge CompleteJSON    -> Large
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs/x", "doc", "code", "mismatch"),
			driftDone(),
		},
	}
	large := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			return `{"issues":[{"page":"https://docs/x","issue":"docs are stale"}]}`, nil
		},
	}
	tiering := &fakeTiering{small: small, typical: typical, large: large}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs/x"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth\nreal docs.", nil }

	findings, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Len(t, findings[0].Issues, 1)
	assert.Equal(t, "docs are stale", findings[0].Issues[0].Issue)

	assert.GreaterOrEqual(t, small.completeCalls, 1, "Small must classify pages")
	assert.GreaterOrEqual(t, typical.calls, 1, "Typical must run the investigator agent loop")
	assert.GreaterOrEqual(t, large.completeCalls, 1, "Large must run the judge CompleteJSON call")
}

func TestDetectDrift_NoObservations_SkipsJudge(t *testing.T) {
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{driftDone()}, // investigator records nothing
	}
	large := &driftStubClient{} // any call would increment completeCalls
	tiering := &fakeTiering{small: small, typical: typical, large: large}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs/x"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	findings, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, 0, large.completeCalls, "Judge must not be called when investigator emits zero observations")
}
```

Also rename `TestDetectDrift_LargeWithoutToolSupport_Errors` (drift_test.go:591) to `TestDetectDrift_TypicalWithoutToolSupport_Errors` and update its `fakeTiering` to put the non-tool client in `typical:` and assert the error message contains `"typical"` and `"tool use"`. (Large is now allowed to be non-tool.)

### Step 2: Run, verify failures

```
go test ./internal/analyzer/ -run TestDetectDrift -v
```

Expected: FAIL on `TestDetectDrift_UsesSmallTypicalLarge` (Typical not used yet) and the renamed Typical-tool-support test.

### Step 3: Update `DetectDrift`

Replace the body's tier acquisition and per-feature dispatch (drift.go:42-88):

```go
func DetectDrift(
	ctx context.Context,
	tiering LLMTiering,
	featureMap FeatureMap,
	docsMap DocsFeatureMap,
	pageReader func(url string) (string, error),
	repoRoot string,
	onFinding DriftProgressFunc,
) ([]DriftFinding, error) {
	investigator, ok := tiering.Typical().(ToolLLMClient)
	if !ok {
		return nil, fmt.Errorf("DetectDrift: typical tier does not support tool use (required for the drift investigator); configure --llm-typical with a tool-use-capable provider (anthropic or openai)")
	}
	judge := tiering.Large()
	classifier := tiering.Small()

	docPages := make(map[string][]string, len(docsMap))
	for _, entry := range docsMap {
		if len(entry.Pages) > 0 {
			docPages[entry.Feature] = entry.Pages
		}
	}

	var findings []DriftFinding

	for _, entry := range featureMap {
		if len(entry.Files) == 0 {
			continue
		}
		pages, ok := docPages[entry.Feature.Name]
		if !ok || len(pages) == 0 {
			continue
		}
		pages = filterDriftPages(pages)
		if len(pages) == 0 {
			continue
		}
		pages = classifyDriftPages(ctx, classifier, pages, pageReader)
		if len(pages) == 0 {
			continue
		}

		log.Infof("  investigating drift for feature %q (%d pages)", entry.Feature.Name, len(pages))
		observations, err := investigateFeatureDrift(ctx, investigator, entry, pages, pageReader, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
		}
		issues, err := judgeFeatureDrift(ctx, judge, entry.Feature, observations)
		if err != nil {
			return nil, fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
		}
		if len(issues) > 0 {
			findings = append(findings, DriftFinding{Feature: entry.Feature.Name, Issues: issues})
			if onFinding != nil {
				if err := onFinding(findings); err != nil {
					return nil, fmt.Errorf("DetectDrift: onFinding: %w", err)
				}
			}
		}
	}

	return findings, nil
}
```

### Step 4: Run, verify pass

```
go test ./internal/analyzer/ -run TestDetectDrift -v
```

Expected: all PASS. If the older `TestDetectDrift_DocumentedFeature_ReturnsIssues`-style tests still reference `addFinding(...)` chat helpers, those must be migrated to the investigator+judge pattern (use `noteObservation` for the typical client and a canned JSON response on the large client). Walk through each remaining DetectDrift test in `drift_test.go` and update it to the new contract.

### Step 5: Format, lint, commit

```
gofmt -w . && goimports -w .
golangci-lint run ./internal/analyzer/...
go test ./internal/analyzer/ -count=1
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "refactor(drift): wire investigator (Typical) + judge (Large)

- RED: tier-routing tests assert Small for classifier, Typical for the
  investigator agent loop, Large for the judge CompleteJSON call; new
  test asserts judge is skipped when investigator emits zero observations
- GREEN: DetectDrift composes investigateFeatureDrift + judgeFeatureDrift;
  tool-use requirement moved from Large to Typical with updated error msg
- Old detectDriftForFeature still present, removed in next task

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 5: Remove dead code

**Goal:** Delete `detectDriftForFeature`, `addFindingTool`, and any helpers only they used. Bump `driftMaxRounds` from 30 to 50. Update CLAUDE.md / README references if any.

**Files:**
- Modify: `internal/analyzer/drift.go`
- Modify: `internal/analyzer/drift_test.go` (remove `addFinding` helper if unused)

### Step 1: Confirm callers

```
go test ./... -count=1
```

If green, proceed. If red, the new path missed a case — fix tests first.

### Step 2: Delete

In `drift.go`:
- Delete `detectDriftForFeature` (the old function).
- Delete `addFindingTool`.
- Change `const driftMaxRounds = 30` → `const driftMaxRounds = 50`.

In `drift_test.go`:
- Delete `addFinding` helper if no remaining test uses it (grep first).

### Step 3: Build, lint, test

```
go build ./...
golangci-lint run ./...
go test ./... -count=1
go test -coverprofile=coverage.out ./internal/analyzer/... && go tool cover -func=coverage.out | tail -1
```

Coverage on `internal/analyzer` must remain ≥ 90%.

### Step 4: Commit

```
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "refactor(drift): remove old single-stage agent

- Delete detectDriftForFeature and addFindingTool; investigator+judge
  is now the only path
- Raise driftMaxRounds 30 -> 50; rounds run on the cheaper Typical tier
- Coverage maintained

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 6: Move tool-use validation from Large to Typical

**Goal:** Update `tier_validate.go` and its tests so the tool-use requirement applies to Typical, not Large.

**Files:**
- Modify: `internal/cli/tier_validate.go`
- Test: `internal/cli/tier_validate_test.go` (or wherever the existing tests live — grep)

### Step 1: Find existing tests

```
grep -rn "providerSupportsToolUse\|validateTierConfigs" internal/cli/
```

Identify the test file. Skim and find: assertions like "tier large with ollama errors", "drift detection requires anthropic or openai".

### Step 2: Update those tests to fail

Flip the failing-tier name from `large` to `typical`. The expected error message should contain `"typical"` and `"tool use"`. Add or keep at least one test that confirms a non-tool provider on Large is now allowed.

### Step 3: Run, verify failures

```
go test ./internal/cli/... -count=1 -run Tier
```

Expected: FAIL on the moved-tool-use tests.

### Step 4: Update `validateTierConfigs`

```go
func validateTierConfigs(small, typical, large string) error {
	for _, tc := range []struct {
		name, raw string
		fallback  string
		needsTool bool
	}{
		{"small", small, defaultSmallTier, false},
		{"typical", typical, defaultTypicalTier, true},
		{"large", large, defaultLargeTier, false},
	} {
		s := tc.raw
		if s == "" {
			s = tc.fallback
		}
		provider, _, err := parseTierString(s)
		if err != nil {
			return fmt.Errorf("tier %q: %w", tc.name, err)
		}
		if !isKnownProvider(provider) {
			return fmt.Errorf("tier %q: unknown provider %q (valid: anthropic, openai, ollama, lmstudio)", tc.name, provider)
		}
		if tc.needsTool && !providerSupportsToolUse(provider) {
			return fmt.Errorf("tier %q: provider %q does not support tool use; the drift investigator requires anthropic or openai", tc.name, provider)
		}
	}
	return nil
}
```

### Step 5: Run, verify pass

```
go test ./internal/cli/... -count=1
```

Expected: PASS.

### Step 6: Update CLI help text and README references

```
grep -rn "tool use" --include="*.go" .
grep -rn "tool use" README.md docs/ 2>/dev/null
grep -rn "llm-large" --include="*.go" --include="*.md" .
```

Anywhere it says "the Large tier requires tool use", change to "the Typical tier requires tool use" (or rephrase to "the drift investigator requires a tool-use-capable provider; this lives on the Typical tier").

### Step 7: Format, lint, commit

```
gofmt -w . && goimports -w .
golangci-lint run ./...
go test ./... -count=1
git add internal/cli/ README.md docs/
git commit -m "refactor(cli): tier tool-use requirement moves Large -> Typical

- RED: validation tests expect the typical tier to be rejected when its
  provider lacks tool use; large tier accepts any provider
- GREEN: validateTierConfigs flips the needsTool flag from large to typical
- README/help text updated to match

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 7: End-to-end verification

**Goal:** Confirm the change works end-to-end against real LLMs and a real fixture, per `.plans/VERIFICATION_PLAN.md`. No mocks.

**Files:** none (this is a manual / scripted run).

### Step 1: Build the binary

```
go build -o /tmp/ftg ./cmd/find-the-gaps
/tmp/ftg --version
```

### Step 2: Run a focused drift scenario

Use the existing fixture flow from `.plans/VERIFICATION_PLAN.md` Scenario 3 (Detect Stale Example). Confirm:

- Investigator log line appears: `"investigating drift for feature ..."`.
- If a feature would have warned in the old run, the new warning wording appears: `"drift investigator exceeded N rounds for feature X; handing K observations to judge"`.
- Final `gaps.md` contains a drift finding for the feature with the modified signature.

### Step 3: Smoke-test cost shape

For one feature, eyeball the request volume in your provider dashboard or via Bifrost logs:

- Sonnet/Typical: many short tool-use turns.
- Opus/Large: exactly one `CompleteJSON` request per feature with non-empty observations, zero requests for features the investigator cleared.

### Step 4: Update PROGRESS.md per CLAUDE.md §8

Append a new Task entry summarizing what changed, tests added, coverage, status.

### Step 5: Open PR

```
git push -u origin cape-town-v1
gh pr create --base main --title "refactor(drift): split into investigator (Typical) + judge (Large)" --body "$(cat <<'EOF'
## Summary
- Decomposes per-feature drift detection into a Typical-tier investigator (tool-use loop, gathers evidence via note_observation) and a Large-tier judge (single CompleteJSON, adjudicates)
- Cuts Opus tokens and per-feature latency; rounds now run on Sonnet
- Tier validation: tool-use requirement moves from Large to Typical
- Round cap raised 30 -> 50 (cheaper rounds)

Design: `.plans/DRIFT_DECOMPOSITION_DESIGN.md`
Plan:   `.plans/DRIFT_DECOMPOSITION_PLAN.md`

## Test plan
- [ ] `go test ./...` green
- [ ] `golangci-lint run` clean
- [ ] Coverage on `internal/analyzer` ≥ 90%
- [ ] Manual run of `.plans/VERIFICATION_PLAN.md` Scenario 3 against a real fixture confirms drift finding still produced
- [ ] Investigator log line and new warning wording observed in stderr
EOF
)"
```

---

## Out of scope

- Cross-feature parallelism (the `for _, entry := range featureMap` loop stays sequential).
- Per-observation parallel Opus calls.
- Retry policies on judge failures.
- Cross-feature finding deduplication.
