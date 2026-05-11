# Content-Based Page Role Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Every code change must follow this project's TDD Iron Law in @CLAUDE.md — failing test FIRST, RIGHT-reason failure verified, minimal code to pass, REFACTOR with tests green. Commit after each RED-GREEN-REFACTOR cycle.

**Goal:** Replace URL-only `pageRole(url)` with a content-classified `role` field carried on `PageAnalysis`, surfaced to drift and screenshot prompts through a per-run resolver and an added field on `DocPage`.

**Architecture:** Piggyback on the existing per-page LLM pass (`AnalyzePage`). Add a `role` field to its structured-output schema with a fixed enum. The CLI builds a `RoleResolver` from the per-page analyses cache and threads it into `DetectDrift`; for the screenshot pipeline the CLI sets `DocPage.Role` from the same cache and the three prompt builders read it from the page struct. Delete `pageRole(url)`.

**Tech Stack:** Go 1.26+, Bifrost SDK, Cobra, testify, testscript. See @CLAUDE.md "Commands" block for `go test ./...`, `go build ./...`, `golangci-lint run`.

**Design reference:** @.plans/2026-05-11-page-role-from-content-design.md

---

## Pre-flight

- Working branch: `improve-page-role-detection` (already checked out).
- All work happens in this worktree: `/Users/brittcrawford/conductor/workspaces/find-the-gaps/bucharest-v2`.
- Test command: `go test ./...`
- Build command: `go build ./...`
- Lint command: `golangci-lint run`
- Format: `gofmt -w . && goimports -w .`

After each task, update `PROGRESS.md` per @CLAUDE.md §8.

---

## Task 1: Add `role` to `AnalyzePage` schema and response struct

**Files:**
- Modify: `internal/analyzer/analyze_page.go`
- Modify: `internal/analyzer/types.go` (add `Role` to `PageAnalysis`)
- Test: `internal/analyzer/analyze_page_test.go`

**Step 1.1: Read current state**

- Read `internal/analyzer/analyze_page.go` lines 12–35 (response struct + schema).
- Read `internal/analyzer/types.go` lines 13–20 (PageAnalysis).
- Read `internal/analyzer/analyze_page_test.go` lines 90–170 (existing schema/migration cases).

**Step 1.2: Add the failing test — happy path**

Append to `internal/analyzer/analyze_page_test.go`:

```go
func TestAnalyzePage_ParsesRole(t *testing.T) {
	c := &fakeClient{responsesByName: map[string]json.RawMessage{
		"analyze_page_response": json.RawMessage(
			`{"summary":"Quickstart page.","features":["install"],"is_docs":true,"role":"quickstart"}`),
	}}
	got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c},
		"https://docs.example.com/intro", "content")
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "quickstart" {
		t.Errorf("Role = %q, want %q", got.Role, "quickstart")
	}
}
```

**Step 1.3: Run; expect FAIL**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage_ParsesRole -count=1
```

Expected: FAIL with `got.Role undefined (type PageAnalysis has no field or method Role)`.

**Step 1.4: Minimal implementation — add struct fields**

In `internal/analyzer/types.go`, change `PageAnalysis` to:

```go
type PageAnalysis struct {
	URL      string
	Summary  string
	Features []string
	IsDocs   bool
	Role     string
}
```

In `internal/analyzer/analyze_page.go`, change `analyzePageResponse` to:

```go
type analyzePageResponse struct {
	Summary  string   `json:"summary"`
	Features []string `json:"features"`
	IsDocs   *bool    `json:"is_docs"`
	// Pointer so we can detect "missing from response" and apply the
	// inclusive-by-default rule (treat as "other") instead of erroring.
	Role *string `json:"role"`
}
```

Update the schema constant — add `role` to `properties` and `required`:

```go
var analyzePageSchema = JSONSchema{
	Name: "analyze_page_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "summary":  {"type": "string"},
        "features": {"type": "array", "items": {"type": "string"}},
        "is_docs":  {"type": "boolean"},
        "role": {
          "type": "string",
          "enum": ["landing","quickstart","tutorial","how-to","concept","reference","changelog","faq","other"]
        }
      },
      "required": ["summary", "features", "is_docs", "role"],
      "additionalProperties": false
    }`),
}
```

In `AnalyzePage`, after the `isDocs` migration block, add the role migration:

```go
role := "other"
if resp.Role != nil {
	role = *resp.Role
}
```

And include it in the returned struct:

```go
return PageAnalysis{
	URL:      pageURL,
	Summary:  resp.Summary,
	Features: resp.Features,
	IsDocs:   isDocs,
	Role:     role,
}, nil
```

**Step 1.5: Run test; expect PASS**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage_ParsesRole -count=1
```

Expected: PASS.

**Step 1.6: Add second failing test — missing `role` migration**

Append to `internal/analyzer/analyze_page_test.go`:

```go
func TestAnalyzePage_MissingRole_DefaultsToOther(t *testing.T) {
	c := &fakeClient{responsesByName: map[string]json.RawMessage{
		"analyze_page_response": json.RawMessage(
			`{"summary":"Old cache shape.","features":[],"is_docs":true}`),
	}}
	got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c},
		"https://docs.example.com/x", "content")
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "other" {
		t.Errorf("Role = %q, want %q (inclusive-by-default for missing field)", got.Role, "other")
	}
}
```

**Step 1.7: Run all `AnalyzePage` tests**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage -count=1
```

Expected: PASS for both new tests AND every existing `AnalyzePage` test (the migration default keeps old cached responses parsing). If an existing test fails because it asserted absence of `Role`, **stop and re-read** — the migration is wrong if it broke something.

**Step 1.8: Commit**

```bash
git add internal/analyzer/analyze_page.go internal/analyzer/types.go internal/analyzer/analyze_page_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add role field to AnalyzePage structured output

- RED: TestAnalyzePage_ParsesRole; TestAnalyzePage_MissingRole_DefaultsToOther
- GREEN: PageAnalysis.Role, analyzePageResponse.Role (pointer for migration),
  schema enum (landing|quickstart|tutorial|how-to|concept|reference|
  changelog|faq|other), inclusive-by-default to "other"

Refs: .plans/2026-05-11-page-role-from-content-design.md
EOF
)"
```

---

## Task 2: Teach the `AnalyzePage` prompt to emit `role`

**Files:**
- Modify: `internal/analyzer/analyze_page.go` (prompt body)
- Test: `internal/analyzer/analyze_page_test.go`

**Step 2.1: Write the failing test**

Append to `internal/analyzer/analyze_page_test.go`:

```go
func TestAnalyzePage_PromptIncludesRoleRubric(t *testing.T) {
	var captured string
	c := &fakeClient{
		responsesByName: map[string]json.RawMessage{
			"analyze_page_response": json.RawMessage(
				`{"summary":"x","features":[],"is_docs":true,"role":"other"}`),
		},
		capturePromptInto: &captured,
	}
	_, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c},
		"https://example.com", "content")
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		`"role":`,
		"landing", "quickstart", "tutorial", "how-to",
		"concept", "reference", "changelog", "faq", "other",
		"Judge from the content",
	}
	for _, w := range wants {
		if !strings.Contains(captured, w) {
			t.Errorf("prompt missing %q", w)
		}
	}
}
```

> If `fakeClient.capturePromptInto` does not yet exist, add it to the existing fake. Grep `internal/analyzer/analyze_page_test.go` for `fakeClient` definition; add a `capturePromptInto *string` field and assign the prompt in `CompleteJSON` before returning the canned response. Keep the change strictly additive to existing tests — they should still pass unchanged.

**Step 2.2: Run; expect FAIL**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage_PromptIncludesRoleRubric -count=1
```

Expected: FAIL on missing rubric strings.

**Step 2.3: Update the prompt body**

In `internal/analyzer/analyze_page.go`, locate the existing prompt template (line 43 onward). Add a new bullet to the `Populate the response with:` section after the `is_docs` bullet:

```go
- "role": the kind of page this is — one of "landing", "quickstart", "tutorial", "how-to", "concept", "reference", "changelog", "faq", "other". Judge from the content; use the URL only as a tiebreaker.
```

Append a new rubric block AFTER the `Rule for is_docs:` block and BEFORE the existing examples list:

```
Role definitions:
- "landing": the docs-site home, or a top-level overview page introducing the product or its docs section.
- "quickstart": a first-time-user install + first command/run page; the reader's goal is "get something working in N minutes".
- "tutorial": a walked-through, end-to-end guided learning of a single task. Reader is following along to learn.
- "how-to": a focused recipe for one task on an existing setup; reader already knows the basics.
- "concept": background, architecture, design rationale, or model explanation; light on procedure.
- "reference": exhaustive API / CLI / config / option listing; not a guide.
- "changelog": release notes, version history, or "what's new".
- "faq": Q&A format or a troubleshooting list.
- "other": anything else, including non-docs pages (marketing, blog, team, careers, legal). Pages with is_docs=false should typically be "other".
```

Update the `fmt.Sprintf` invocation — no change to format args (the URL is still already in the prompt; role doesn't need a separate input).

**Step 2.4: Run new test; expect PASS**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage_PromptIncludesRoleRubric -count=1
```

Expected: PASS.

**Step 2.5: Run all `AnalyzePage` tests**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage -count=1
```

Expected: ALL PASS. Any regression here means the prompt change accidentally moved something a sibling test asserts on.

**Step 2.6: Commit**

```bash
git add internal/analyzer/analyze_page.go internal/analyzer/analyze_page_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): teach AnalyzePage prompt to classify page role

- RED: TestAnalyzePage_PromptIncludesRoleRubric
- GREEN: new "role" bullet + 9-label rubric block in the page-analysis
  prompt; judge from content, URL as tiebreaker
EOF
)"
```

---

## Task 3: Replace `pageRole(url)` with `RoleResolver`

**Files:**
- Modify (rewrite): `internal/analyzer/page_role.go`
- Modify (rewrite): `internal/analyzer/page_role_test.go`

**Step 3.1: Write the failing test**

Replace `internal/analyzer/page_role_test.go` contents with:

```go
package analyzer

import "testing"

func TestRoleResolver_KnownURL_ReturnsRole(t *testing.T) {
	r := NewRoleResolver(map[string]string{
		"https://docs/x": "quickstart",
		"https://docs/y": "reference",
	})
	if got := r("https://docs/x"); got != "quickstart" {
		t.Errorf("r(x) = %q, want quickstart", got)
	}
	if got := r("https://docs/y"); got != "reference" {
		t.Errorf("r(y) = %q, want reference", got)
	}
}

func TestRoleResolver_UnknownURL_ReturnsOther(t *testing.T) {
	r := NewRoleResolver(map[string]string{"https://docs/x": "quickstart"})
	if got := r("https://docs/missing"); got != "other" {
		t.Errorf("unknown url = %q, want other", got)
	}
}

func TestRoleResolver_EmptyURL_ReturnsOther(t *testing.T) {
	r := NewRoleResolver(map[string]string{})
	if got := r(""); got != "other" {
		t.Errorf("empty url = %q, want other", got)
	}
}

func TestRoleResolver_EmptyStoredRole_ReturnsOther(t *testing.T) {
	// AnalyzePage skipped (token budget) → zero-value Role = "".
	// Resolver normalizes empty to "other".
	r := NewRoleResolver(map[string]string{"https://docs/skipped": ""})
	if got := r("https://docs/skipped"); got != "other" {
		t.Errorf("empty role = %q, want other", got)
	}
}

func TestRoleResolver_NilMap_ReturnsOther(t *testing.T) {
	r := NewRoleResolver(nil)
	if got := r("https://docs/any"); got != "other" {
		t.Errorf("nil map = %q, want other", got)
	}
}
```

**Step 3.2: Run; expect FAIL**

```bash
go test ./internal/analyzer/ -run TestRoleResolver -count=1
```

Expected: FAIL — `NewRoleResolver` not defined; also compilation fails for old `pageRole` callers if you removed it eagerly. The next step adds the new function but leaves `pageRole(url)` temporarily intact so the package still compiles.

**Step 3.3: Replace `page_role.go` contents**

Overwrite `internal/analyzer/page_role.go` with:

```go
package analyzer

// RoleResolver resolves a docs page URL to its content-classified role.
// Built once per run from the per-page AnalyzePage cache; consumed by the
// drift judge and screenshot detection prompts as a prominence hint.
//
// Unknown URLs, empty URLs, and stored empty strings all resolve to "other"
// — matching the inclusive-by-default rule applied in AnalyzePage when a
// response is missing the role field (e.g. a token-budget skip or an old
// cached response).
type RoleResolver func(pageURL string) string

// NewRoleResolver builds a resolver from a URL→role map. Callers typically
// pass map[url]PageAnalysis.Role after the per-page analysis pass completes.
func NewRoleResolver(roles map[string]string) RoleResolver {
	return func(pageURL string) string {
		if pageURL == "" {
			return "other"
		}
		if roles == nil {
			return "other"
		}
		role, ok := roles[pageURL]
		if !ok || role == "" {
			return "other"
		}
		return role
	}
}
```

> Note: do NOT delete `pageRole(url)` yet — drift.go and screenshot_gaps.go still call it. Removal is Task 6. Keep it co-located but unused-by-tests until then.

Actually — the test file no longer references `pageRole`, but `drift.go:562` and `screenshot_gaps.go:744,877,1154` still do. So we leave a stub in place until Tasks 4 and 5 swap callers over.

For this task, append the stub back into `page_role.go`:

```go
// Deprecated: superseded by RoleResolver. Will be removed in Task 6 once
// all callers have been migrated to RoleResolver.
//
// Kept temporarily so drift.go and screenshot_gaps.go still compile during
// the staged migration.
func pageRole(_ string) string { return "other" }
```

This intentionally returns `"other"` for everything — Tasks 4 and 5 replace each caller before the stub is deleted. The unit tests that asserted URL-derived behavior (`TestPageRole`, `TestPageRoleSummary`) will need updating in Tasks 4 and 6.

**Step 3.4: Run; expect PASS for resolver tests**

```bash
go test ./internal/analyzer/ -run TestRoleResolver -count=1
```

Expected: PASS.

**Step 3.5: Verify package still compiles**

```bash
go build ./...
```

Expected: BUILD OK. Other tests in the package may now fail because they assumed URL-derived role behavior (`TestPageRoleSummary`, `TestScreenshotPromptIncludesRoleHint`, etc.). Note them but don't fix them in this task — they get rewritten in Tasks 4 and 5.

**Step 3.6: Commit**

```bash
git add internal/analyzer/page_role.go internal/analyzer/page_role_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): introduce RoleResolver; stub legacy pageRole(url)

- RED: TestRoleResolver_* (known, unknown, empty url, empty stored role, nil map)
- GREEN: NewRoleResolver builds a URL→role lookup with "other" fallback at
  every defensive edge; legacy pageRole() now a temporary stub returning
  "other" while callers migrate in subsequent commits

Refs: .plans/2026-05-11-page-role-from-content-design.md
EOF
)"
```

---

## Task 4: Plumb `RoleResolver` through the drift pipeline

**Files:**
- Modify: `internal/analyzer/drift.go` (`pageRoleSummary`, `renderJudgePrompt`, `judgeOneShot`, `judgeFeatureDrift`, `DetectDrift`)
- Modify: `internal/analyzer/drift_priority_test.go` (`TestPageRoleSummary`)
- Modify: `internal/cli/analyze.go` (build resolver from analyses; pass to `DetectDrift`)
- Possibly test: any existing `TestDetectDrift_*` integration test signatures

**Step 4.1: Write the failing test — `pageRoleSummary` consumes a resolver**

Replace `TestPageRoleSummary` in `internal/analyzer/drift_priority_test.go`:

```go
func TestPageRoleSummary(t *testing.T) {
	r := NewRoleResolver(map[string]string{
		"https://x/intro":   "quickstart",
		"https://x/api/ref": "reference",
	})
	got := pageRoleSummary(r, []string{"https://x/intro", "https://x/api/ref", "https://x/unknown"})
	for _, want := range []string{
		"https://x/intro -> quickstart",
		"https://x/api/ref -> reference",
		"https://x/unknown -> other",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pageRoleSummary missing %q\ngot:\n%s", want, got)
		}
	}
}
```

**Step 4.2: Run; expect FAIL**

```bash
go test ./internal/analyzer/ -run TestPageRoleSummary -count=1
```

Expected: FAIL (signature mismatch).

**Step 4.3: Update `pageRoleSummary` signature**

In `internal/analyzer/drift.go` (lines 552–565), change to:

```go
// pageRoleSummary returns a human-readable list of "<url> -> <role>" lines
// for the pages observed during drift investigation. Roles come from the
// per-run RoleResolver (built from the page-analysis cache). Fed into the
// judge prompt so the priority rubric can weight prominent pages higher.
func pageRoleSummary(roles RoleResolver, pages []string) string {
	if len(pages) == 0 {
		return "Page role hints: (no specific pages)"
	}
	var b strings.Builder
	b.WriteString("Page role hints:\n")
	for _, p := range pages {
		fmt.Fprintf(&b, "- %s -> %s\n", p, roles(p))
	}
	return b.String()
}
```

**Step 4.4: Thread resolver through `judgeFeatureDrift` → `renderJudgePrompt`**

In `internal/analyzer/drift.go`:

- `renderJudgePrompt(feature CodeFeature, observations []driftObservation) string` becomes `renderJudgePrompt(feature CodeFeature, observations []driftObservation, roles RoleResolver) string`. Inside, replace `pageRoleSummary(uniqueObservationPages(observations))` with `pageRoleSummary(roles, uniqueObservationPages(observations))`.

- `judgeOneShot` adds `roles RoleResolver` parameter; pass to `renderJudgePrompt`; pass when `chunkObservationsToFit` re-renders (check if that helper renders too — if yes, thread through it).

- `judgeFeatureDrift` adds `roles RoleResolver` parameter; pass to `judgeOneShot` in both the fast-path and chunked-compaction calls.

**Step 4.5: Thread resolver through `DetectDrift`**

Change `DetectDrift` signature in `internal/analyzer/drift.go` (line 114):

```go
func DetectDrift(
	ctx context.Context,
	tiering LLMTiering,
	featureMap FeatureMap,
	docsMap DocsFeatureMap,
	pageReader func(url string) (string, error),
	roles RoleResolver,
	repoRoot string,
	workers int,
	cached map[string]CachedDriftEntry,
	onFinding DriftProgressFunc,
	onFeatureDone DriftFeatureDoneFunc,
) ([]DriftFinding, error) {
```

At the call site to `judgeFeatureDrift` (line 245), pass `roles`:

```go
issues, err := judgeFeatureDrift(ctx, judge, entry.Feature, observations, roles)
```

**Step 4.6: Update the CLI caller**

In `internal/cli/analyze.go`, find the `analyzer.DetectDrift(...)` call (grep). Just before it, build the resolver from the in-memory page analyses map. The CLI already keeps a `pages []analyzer.PageAnalysis` (or equivalent) — grep `PageAnalysis` in `internal/cli/analyze.go` to confirm the variable name. Then:

```go
rolesByURL := make(map[string]string, len(pageAnalyses))
for _, pa := range pageAnalyses {
	rolesByURL[pa.URL] = pa.Role
}
roleResolver := analyzer.NewRoleResolver(rolesByURL)
```

Pass `roleResolver` as the new `roles` argument to `DetectDrift`.

**Step 4.7: Run all analyzer tests**

```bash
go test ./internal/analyzer/ -count=1
```

Expected: `TestPageRoleSummary` PASSES. Other drift-judge tests may need signature updates — fix any compilation breakage by passing a stub `analyzer.NewRoleResolver(nil)` in their setup (since they don't care about role content, just need a value). Do this minimally — change only what won't compile.

**Step 4.8: Build the whole tree**

```bash
go build ./...
```

Expected: BUILD OK.

**Step 4.9: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_priority_test.go internal/cli/analyze.go
git commit -m "$(cat <<'EOF'
feat(drift): drive judge-prompt role hints from per-page analyses

- RED: TestPageRoleSummary now drives roles via RoleResolver
- GREEN: pageRoleSummary, renderJudgePrompt, judgeOneShot,
  judgeFeatureDrift, DetectDrift all accept a RoleResolver; CLI builds
  it from the page-analysis cache before invoking DetectDrift

The judge prompt's "Page role hints:" block now lists content-classified
roles (landing/quickstart/tutorial/how-to/concept/reference/changelog/
faq/other) instead of URL-segment heuristics.

Refs: .plans/2026-05-11-page-role-from-content-design.md
EOF
)"
```

---

## Task 5: Add `Role` to `DocPage`; consume it in screenshot prompts

**Files:**
- Modify: `internal/analyzer/types.go` (or wherever `DocPage` lives — grep first)
- Modify: `internal/analyzer/screenshot_gaps.go` (`buildScreenshotPrompt`, `buildDetectionPromptWithVerdicts`, `buildRelevancePrompt`)
- Modify: `internal/analyzer/screenshot_gaps_test.go`
- Modify: `internal/analyzer/screenshot_priority_test.go`
- Modify: `internal/analyzer/image_issue_priority_test.go`
- Modify: `internal/cli/analyze.go` (populate `DocPage.Role` from analyses before screenshot phase)

**Step 5.1: Locate `DocPage`**

```bash
# Grep for the type declaration
```

```bash
grep -n "^type DocPage" internal/analyzer/*.go
```

Read the struct. It almost certainly has `URL`, `Path`, `Content`.

**Step 5.2: Write the failing test — `buildScreenshotPrompt` uses `Role` from page**

In `internal/analyzer/screenshot_priority_test.go` (line 52 currently uses URL-derived role), replace with a test that drives role through the page struct. But first note: `buildScreenshotPrompt` currently takes `(pageURL, content, coverage, codeBlocks)` — its signature needs to change to take a `DocPage` (or to take a `role` string explicitly).

Decision: change `buildScreenshotPrompt(page DocPage, coverage map[string][]imageRef, codeBlocks []codeBlockRef)`. Same for `buildDetectionPromptWithVerdicts(page DocPage, refs []imageRef, verdicts []ImageVerdict, codeBlocks []codeBlockRef)`. `buildRelevancePrompt` already takes `page DocPage`.

Replace the existing role-hint test with:

```go
func TestBuildScreenshotPrompt_IncludesRoleFromPage(t *testing.T) {
	page := DocPage{
		URL:     "https://x/anywhere",
		Content: "body",
		Role:    "quickstart",
	}
	out := buildScreenshotPrompt(page, nil, nil)
	if !strings.Contains(out, "page_role: quickstart") {
		t.Errorf("missing role hint; got:\n%s", out)
	}
}

func TestBuildDetectionPromptWithVerdicts_IncludesRoleFromPage(t *testing.T) {
	page := DocPage{
		URL:     "https://x/docs/api",
		Content: "body",
		Role:    "reference",
	}
	refs := []imageRef{{Src: "/a.png", AltText: "alt", SectionHeading: "h", ParagraphIndex: 0, OriginalIndex: 1}}
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}}
	out := buildDetectionPromptWithVerdicts(page, refs, verdicts, nil)
	if !strings.Contains(out, "page_role: reference") {
		t.Errorf("missing role hint; got:\n%s", out)
	}
}
```

**Step 5.3: Run; expect FAIL**

```bash
go test ./internal/analyzer/ -run 'TestBuildScreenshotPrompt_IncludesRoleFromPage|TestBuildDetectionPromptWithVerdicts_IncludesRoleFromPage' -count=1
```

Expected: FAIL — `DocPage.Role` undefined and/or signature mismatch.

**Step 5.4: Add `Role` to `DocPage`**

In wherever `DocPage` is declared, add:

```go
type DocPage struct {
	URL     string
	Path    string
	Content string
	Role    string // content-classified role from AnalyzePage; resolves to "" for un-analyzed pages
}
```

(Keep existing fields; this is strictly additive.)

**Step 5.5: Update prompt-builder signatures**

In `internal/analyzer/screenshot_gaps.go`:

- `func buildScreenshotPrompt(page DocPage, coverage map[string][]imageRef, codeBlocks []codeBlockRef) string` — read `page.URL`, `page.Content`, `page.Role` from the struct. Replace `pageRole(pageURL)` in the `fmt.Sprintf` args with a defensive `normalizeRole(page.Role)`:

```go
func normalizeRole(r string) string {
	if r == "" {
		return "other"
	}
	return r
}
```

(Add this as a small unexported helper at the top of the file.)

The `fmt.Sprintf` substitution `pageRole(pageURL)` becomes `normalizeRole(page.Role)`. Same edit at the verdict-prompt call site (line 877) and the relevance-prompt call site (line 1154).

- `func buildDetectionPromptWithVerdicts(page DocPage, refs []imageRef, verdicts []ImageVerdict, codeBlocks []codeBlockRef) string` — same treatment. Update the internal delegation: when `verdicts` is empty, call `buildScreenshotPrompt(page, buildCoverageMap(refs), codeBlocks)`.

- `buildRelevancePrompt(page DocPage, batch []imageRef) string` — already takes `page`; replace `pageRole(page.URL)` with `normalizeRole(page.Role)`.

Update every internal caller:
- `detectionPass` (line 1271): `prompt := buildDetectionPromptWithVerdicts(page, refs, verdicts, codeBlocks)`.
- `relevancePass`: `prompt := buildRelevancePrompt(page, batch)` (already correct).
- `fitContentToBudget` overhead computation (line 890): the call `countTokens(buildScreenshotPrompt(pageURL, "", coverage, codeBlocks))` becomes `countTokens(buildScreenshotPrompt(DocPage{URL: pageURL, Role: page.Role}, coverage, codeBlocks))`. Audit this carefully — `fitContentToBudget` might be called from a context that has only `pageURL` and no `Role`. Check:

```bash
grep -n "fitContentToBudget" internal/analyzer/screenshot_gaps.go
```

If only called from `detectionPass`, change its signature to accept `page DocPage` instead of `pageURL string`. The token-budget computation needs `page.Role` to be present so the overhead size is correct.

**Step 5.6: Update CLI to populate `Role` on `DocPage` before screenshot phase**

In `internal/cli/analyze.go`, just before the `analyzer.DetectScreenshotGaps(...)` call (line 646): walk the `docPages []DocPage` slice and stamp `Role` from the analyses map (`rolesByURL` from Task 4 — reuse it):

```go
for i := range docPages {
	docPages[i].Role = rolesByURL[docPages[i].URL]
}
```

If `rolesByURL` was scoped to the drift block, hoist its build earlier (right after the per-page analysis loop completes). Both drift and screenshots consume it.

**Step 5.7: Update remaining prompt tests**

Sweep through:
- `internal/analyzer/screenshot_gaps_test.go` lines 242, 255, 260, 270, 279, 284, 295, 657, 665, 666, 673, 684, 689, 693, 694, 704, 721, 741, 742, 766 — every call to `buildScreenshotPrompt(...)` and `buildDetectionPromptWithVerdicts(...)`. Replace the URL+content positional args with a `DocPage{URL: ..., Content: ..., Role: "other"}` literal (use `"other"` since these tests don't care about the role value, just the rest of the prompt shape).
- `internal/analyzer/screenshot_priority_test.go` line 52, 67 — same treatment, but for the one or two tests that intentionally asserted URL-derived role behavior (e.g. `/quickstart/` URL → role hint `quickstart` in prompt), rewrite them to drive `DocPage.Role = "quickstart"` and assert `page_role: quickstart` appears. Drop URL-based assertions.
- `internal/analyzer/image_issue_priority_test.go` line 43 — `buildRelevancePrompt(page, batch)` already passes `page`; just add `Role: "..."` to the `page` literal where the test sets it up.
- `internal/analyzer/screenshot_gaps_integration_test.go` — grep for `DocPage{` literals and stamp `Role: "other"` where applicable. Integration tests don't depend on role value; they just need the field present.

**Step 5.8: Build + run analyzer tests**

```bash
go build ./...
go test ./internal/analyzer/ -count=1
```

Expected: BUILD OK; ALL analyzer tests PASS. Fix any straggler `DocPage{}` literals that the compiler complains about (Go zero-value `Role: ""` is fine; explicit `Role: "other"` is preferred only when the test asserts on role content).

**Step 5.9: Run full test suite**

```bash
go test ./... -count=1
```

Expected: ALL PASS. If `internal/cli` tests fail because of the new resolver wiring, fix them minimally (most likely just adding the `rolesByURL` build at fixture setup).

**Step 5.10: Commit**

```bash
git add internal/analyzer/ internal/cli/analyze.go
git commit -m "$(cat <<'EOF'
feat(screenshots): drive screenshot-prompt role hints from page analyses

- RED: TestBuildScreenshotPrompt_IncludesRoleFromPage;
       TestBuildDetectionPromptWithVerdicts_IncludesRoleFromPage
- GREEN: DocPage gains Role field; buildScreenshotPrompt,
  buildDetectionPromptWithVerdicts, buildRelevancePrompt all read role
  from the page struct (with normalizeRole("") -> "other"); CLI
  populates DocPage.Role from the per-page analyses cache before
  DetectScreenshotGaps

All four screenshot prompt callsites (legacy detection, verdict-enriched
detection, vision relevance, prompt-budget overhead) now use the
content-classified role instead of URL-segment heuristics.

Refs: .plans/2026-05-11-page-role-from-content-design.md
EOF
)"
```

---

## Task 6: Delete the legacy `pageRole(url)` stub

**Files:**
- Modify: `internal/analyzer/page_role.go`

**Step 6.1: Verify no remaining callers**

```bash
grep -rn "pageRole(" internal/ cmd/ | grep -v "_test.go" | grep -v "// Deprecated"
```

Expected: zero hits. If anything still calls `pageRole(url)`, finish migrating it before deleting.

**Step 6.2: Delete the stub**

In `internal/analyzer/page_role.go`, remove the deprecated `pageRole` function. The file should now contain only `RoleResolver` and `NewRoleResolver`.

**Step 6.3: Build + test**

```bash
go build ./...
go test ./... -count=1
```

Expected: BUILD OK; ALL PASS.

**Step 6.4: Lint**

```bash
golangci-lint run
```

Expected: zero issues. Fix anything reported (most likely an unused `net/url` import in `page_role.go` — remove it).

**Step 6.5: Commit**

```bash
git add internal/analyzer/page_role.go
git commit -m "$(cat <<'EOF'
refactor(analyzer): remove URL-only pageRole(); all callers on RoleResolver

The URL-segment classifier is fully superseded by RoleResolver, which
reads the content-classified role emitted by AnalyzePage. No remaining
callers; deleted.

Refs: .plans/2026-05-11-page-role-from-content-design.md
EOF
)"
```

---

## Task 7: Update Verification Plan

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`

**Step 7.1: Append note to Scenario 14**

Find "Scenario 14: Priority Calibration Smoke Test" in `.plans/VERIFICATION_PLAN.md`. Under **Context**, add one sentence at the end:

> Roles now come from the per-page LLM analysis output (content-classified `role` field on `PageAnalysis`), not from URL-segment heuristics — a regression in role classification surfaces here as a shift in priority bucketing.

No new scenario required. Save.

**Step 7.2: Commit**

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "docs(plans): note content-classified roles in Scenario 14 context"
```

---

## Task 8: Format, lint, and final verification

**Step 8.1: Format**

```bash
gofmt -w . && goimports -w .
```

**Step 8.2: Lint**

```bash
golangci-lint run
```

Expected: zero issues.

**Step 8.3: Full test suite, no cache**

```bash
go test ./... -count=1
```

Expected: ALL PASS.

**Step 8.4: Coverage check**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -20
```

Expected: per-package coverage for `internal/analyzer` stays ≥90% (CLAUDE.md §3).

**Step 8.5: Update `PROGRESS.md`**

Append a final task block per CLAUDE.md §8 format, summarizing all tasks completed, tests added, coverage, build/lint status, and timestamp.

**Step 8.6: Commit anything pending**

```bash
git status
# If formatter / PROGRESS.md / coverage.out changes exist:
git add -A
git commit -m "chore: format, update PROGRESS for page-role-from-content task"
```

**Step 8.7: Real-binary verification (Scenarios 9 + 14)**

Per @CLAUDE.md and the design doc: run Scenarios 9 and 14 from `.plans/VERIFICATION_PLAN.md` against a real docs site. Manual: build `find-the-gaps`, run against the pinned fixture, inspect `gaps.md` and `drift.json` for sane priority bucketing.

```bash
go build -o ./bin/find-the-gaps ./cmd/find-the-gaps
./bin/find-the-gaps analyze --repo <fixture> --docs <url> -v
```

Inspect the per-page audit log (`-v`) for the new role labels. If priority bucketing looks degraded compared to a pre-change baseline run, **stop and report** — Task 8 may not close until tuning is reviewed.

---

## DRY / YAGNI checklist

- ✅ Single resolver type, used by both drift and screenshots.
- ✅ `normalizeRole("")→"other"` exists in exactly one place.
- ✅ `pageRole(url)` deleted, not kept as a fallback.
- ✅ No new prominence score — judged by the priority rubric already.
- ✅ No new cache file; the existing per-page analysis cache carries `role`.
- ✅ No verification-plan scenario added — existing Scenarios 2/3/14 cover regressions.

## Rollback plan

If a real-site verification shows degraded behavior:

1. `git revert` the screenshot/drift wiring commits (Tasks 4 and 5).
2. Leave Task 1 + Task 2 in place — the `role` field is harmless when unread.
3. The legacy `pageRole(url)` URL classifier was deleted; revert Task 6 too, or reintroduce a thin function from history.

Each task is a single commit; granular revert is straightforward.
