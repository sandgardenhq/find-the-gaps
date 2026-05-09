# Code-Block Coverage for Screenshot Detection — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.
>
> **Project rules to obey at all times** (see `CLAUDE.md`):
> - **TDD is mandatory.** Every task is RED → verify-RED → GREEN → verify-GREEN → REFACTOR → commit. No production code without a failing test first. Violations require deletion + restart.
> - **Commit after each task.** Conventional Commits style; format described below.
> - **Coverage ≥90% statements** for any package touched.
> - **Plans live in `.plans/`.** Design doc: `.plans/CODEBLOCK_SCREENSHOT_COVERAGE_DESIGN.md`.

**Goal:** Stop the screenshot-gap detector from flagging missing screenshots when the visual moment is already shown verbatim in a nearby code block (terminal output, JSON / YAML response, HTML / JSX preview).

**Architecture:** Mirror the existing image-coverage system. Extract fenced code blocks during page parsing, capture only locality metadata (language, line count, section heading, paragraph index), inject a deterministic "Existing code blocks on this page" list into both detection prompts, and generalize the prompt's "covered passage" rule + "Do NOT flag" list to include topically-matching code blocks. Add a `suppressed_by_code_block` array to the output schema that flows into the same `PossiblyCovered` channel as image suppressions, plus two new audit-only stats fields.

**Tech Stack:** Go 1.26+, testify, existing `internal/analyzer` package and prompt-engineering machinery. No new dependencies. No new LLM call. No vision dependency.

**Test commands** (project conventions, see `CLAUDE.md`):
- Run targeted tests: `go test ./internal/analyzer/... -run <Name> -count=1 -v`
- Run package: `go test ./internal/analyzer/... -count=1`
- Coverage: `go test -coverprofile=coverage.out ./internal/analyzer/... && go tool cover -func=coverage.out`
- Build gate: `go build ./...`
- Lint: `golangci-lint run`

**Commit message format** (per CLAUDE.md §9):
```
feat(analyzer): <short description>

- RED: <which test was written first>
- GREEN: <minimal code to pass>
- Status: <N> tests passing, build successful
```

---

## Task 1: Extract code blocks from markdown

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` (add `codeBlockRef` type and `extractCodeBlocks` function near `extractImages`, around line 119)
- Test: `internal/analyzer/screenshot_gaps_test.go` (add new tests near the `TestExtractImages_*` block, around line 100)

**Why a new function instead of merging into `extractImages`:** keeps the public surface small (each function does one thing), keeps existing tests on `extractImages` byte-stable, and makes the locality-only contract for code blocks obvious in the type. The two functions still share the same fence-state-machine pattern; they walk the markdown independently.

**Step 1: Write failing tests**

Add to `internal/analyzer/screenshot_gaps_test.go`:

```go
func TestExtractCodeBlocks_BacktickFence(t *testing.T) {
	md := "# Quickstart\n\nRun this:\n\n" +
		"```bash\n" +
		"brew install foo\n" +
		"foo --version\n" +
		"```\n\n" +
		"Done.\n"
	got := extractCodeBlocks(md)
	assert.Equal(t, []codeBlockRef{
		{Language: "bash", LineCount: 2, SectionHeading: "Quickstart", ParagraphIndex: 2, OriginalIndex: 1},
	}, got)
}

func TestExtractCodeBlocks_TildeFence(t *testing.T) {
	md := "# Config\n\n" +
		"~~~yaml\n" +
		"name: foo\n" +
		"version: 1\n" +
		"~~~\n"
	got := extractCodeBlocks(md)
	assert.Equal(t, []codeBlockRef{
		{Language: "yaml", LineCount: 2, SectionHeading: "Config", ParagraphIndex: 1, OriginalIndex: 1},
	}, got)
}

func TestExtractCodeBlocks_NoLanguage(t *testing.T) {
	md := "# X\n\n```\nhello\n```\n"
	got := extractCodeBlocks(md)
	require.Len(t, got, 1)
	assert.Equal(t, "", got[0].Language)
	assert.Equal(t, 1, got[0].LineCount)
}

func TestExtractCodeBlocks_MultipleBlocksAcrossSections(t *testing.T) {
	md := "# A\n\n```bash\ncmd\n```\n\n## B\n\n" +
		"```json\n{\"x\":1}\n{\"y\":2}\n```\n"
	got := extractCodeBlocks(md)
	require.Len(t, got, 2)
	assert.Equal(t, "A", got[0].SectionHeading)
	assert.Equal(t, "bash", got[0].Language)
	assert.Equal(t, 1, got[0].OriginalIndex)
	assert.Equal(t, "B", got[1].SectionHeading)
	assert.Equal(t, "json", got[1].Language)
	assert.Equal(t, 2, got[1].LineCount)
	assert.Equal(t, 2, got[1].OriginalIndex)
}

func TestExtractCodeBlocks_DoesNotCaptureBody(t *testing.T) {
	// Locality only; body lives in page content. Type has no body field — this
	// test pins the contract that the type layout cannot regress to one.
	md := "```bash\nsecret\n```\n"
	got := extractCodeBlocks(md)
	require.Len(t, got, 1)
	// Compile-time check: codeBlockRef must not have a body-shaped field.
	// If someone adds one, this test is a documentation anchor for why not.
	_ = got[0].Language
	_ = got[0].LineCount
}

func TestExtractCodeBlocks_EmptyOnNoFences(t *testing.T) {
	got := extractCodeBlocks("# Title\n\nProse only.\n")
	assert.Empty(t, got)
}

func TestExtractCodeBlocks_UnclosedFenceIsIgnored(t *testing.T) {
	// Defensive: a malformed page with an unclosed fence should not panic
	// and should not emit a partial ref. (The existing extractImages tolerates
	// this; mirror that contract.)
	md := "# X\n\n```bash\nbrew install foo\n"
	got := extractCodeBlocks(md)
	assert.Empty(t, got)
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/... -run TestExtractCodeBlocks -count=1 -v`
Expected: compile error — `undefined: extractCodeBlocks` and `undefined: codeBlockRef`. This is the right kind of failure.

**Step 3: Write minimal implementation**

Add to `internal/analyzer/screenshot_gaps.go` immediately after `extractImages` (around line 215):

```go
// codeBlockRef is one fenced code block on a docs page, captured for the
// purpose of feeding deterministic locality data into the screenshot-gap
// detection prompt. A passage in prose may be considered "already covered"
// by a code block in the same section heading or within ±3 paragraphs whose
// language plausibly matches the moment (bash/console for terminal output,
// json/yaml for response shapes, html/jsx for rendered UI).
//
// Body content is intentionally NOT captured: every code block already
// appears verbatim in the page content sent to the model, and duplicating
// bodies in the coverage list would blow ScreenshotPromptBudget on
// reference-heavy pages.
type codeBlockRef struct {
	Language       string // from the fence opener; "" when absent
	LineCount      int    // body lines, excluding the opener and closer fences
	SectionHeading string // most recent ATX heading above the block; "" if none
	ParagraphIndex int    // 0-based block position, same scheme as imageRef
	OriginalIndex  int    // 1-based "code-N" label for prompt locality lists
}

// extractCodeBlocks returns one codeBlockRef per fenced block in md, walking
// the markdown with the same fence state machine as extractImages. The two
// functions share style but not state: each emits its own ref slice so tests
// on one don't churn when the other changes.
//
// Unclosed fences are ignored: the trailing block has no closer, so we do
// not emit a partial ref. This matches extractImages' tolerance for malformed
// markdown (no panics, no half-written state).
func extractCodeBlocks(md string) []codeBlockRef {
	var refs []codeBlockRef
	currentHeading := ""
	inFence := false
	pIdx := 0
	hadContentInBlock := false
	var (
		fenceLang  string
		fenceLines int
	)

	for line := range strings.SplitSeq(md, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if !inFence {
				// Opening fence: capture language hint (everything after the
				// fence marker, trimmed). pIdx is incremented at the next
				// blank line OR when we leave the block; preserve the index
				// at which this block sits.
				marker := "```"
				if strings.HasPrefix(trimmed, "~~~") {
					marker = "~~~"
				}
				fenceLang = strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
				fenceLines = 0
				inFence = true
				hadContentInBlock = true
			} else {
				// Closing fence: emit the ref.
				refs = append(refs, codeBlockRef{
					Language:       fenceLang,
					LineCount:      fenceLines,
					SectionHeading: currentHeading,
					ParagraphIndex: pIdx,
				})
				inFence = false
			}
			continue
		}

		if inFence {
			fenceLines++
			continue
		}

		if trimmed == "" {
			if hadContentInBlock {
				pIdx++
				hadContentInBlock = false
			}
			continue
		}
		if atxHeadingRe.MatchString(trimmed) {
			currentHeading = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		}
		hadContentInBlock = true
	}

	for i := range refs {
		refs[i].OriginalIndex = i + 1
	}
	return refs
}
```

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/... -run TestExtractCodeBlocks -count=1 -v`
Expected: all 7 tests pass.

Also run the full package once to make sure existing image-extraction tests are untouched:
Run: `go test ./internal/analyzer/... -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): extract fenced code blocks for screenshot-coverage prompts

- RED: TestExtractCodeBlocks_* (7 cases — backtick/tilde fences, no language,
  multi-section, body-not-captured contract, empty page, unclosed fence)
- GREEN: codeBlockRef type + extractCodeBlocks single-pass fence walker
- Status: package tests passing, build successful
EOF
)"
```

---

## Task 2: Render code-block coverage in the no-vision prompt

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go:552-617` (`buildScreenshotPrompt`)
- Test: `internal/analyzer/screenshot_gaps_test.go` (extend the existing `buildScreenshotPrompt` golden tests near line 135)

**Step 1: Write failing tests**

Add to `internal/analyzer/screenshot_gaps_test.go`:

```go
func TestBuildScreenshotPrompt_NoCodeBlocks(t *testing.T) {
	got := buildScreenshotPrompt("https://x/p", "content", nil, nil)
	assert.Contains(t, got, "Existing code blocks on this page (if any):")
	assert.Contains(t, got, "No code blocks on this page.")
}

func TestBuildScreenshotPrompt_ListsCodeBlocksWithLocality(t *testing.T) {
	blocks := []codeBlockRef{
		{Language: "bash", LineCount: 12, SectionHeading: "Quickstart", ParagraphIndex: 4, OriginalIndex: 1},
		{Language: "json", LineCount: 8, SectionHeading: "Response", ParagraphIndex: 7, OriginalIndex: 2},
	}
	got := buildScreenshotPrompt("https://x/p", "content", nil, blocks)
	assert.Contains(t, got, `- code-1, section "Quickstart", paragraph 4: language=bash, 12 lines`)
	assert.Contains(t, got, `- code-2, section "Response", paragraph 7: language=json, 8 lines`)
}

func TestBuildScreenshotPrompt_EmptyHeadingFallback(t *testing.T) {
	blocks := []codeBlockRef{
		{Language: "bash", LineCount: 3, SectionHeading: "", ParagraphIndex: 0, OriginalIndex: 1},
	}
	got := buildScreenshotPrompt("https://x/p", "content", nil, blocks)
	assert.Contains(t, got, `section "(no heading)"`)
}
```

The signature change (adding a fourth parameter) will break the existing call sites in tests. Update them in the same edit — these are the tests that previously passed `coverage` only:

```go
// internal/analyzer/screenshot_gaps_test.go: every call to buildScreenshotPrompt
// gains a final `nil` argument:
got := buildScreenshotPrompt(pageURL, content, coverage, nil)
got := buildScreenshotPrompt("https://example.com/x", "# X\n\nHello.\n", nil, nil)
got := buildScreenshotPrompt("https://x/p", "content...", buildCoverageMap(refs), nil)
got := buildScreenshotPrompt("https://example.com/x", "# X\n\nHello.\n", nil, nil)
"legacy": buildScreenshotPrompt("https://x", "content", nil, nil),
// internal/analyzer/screenshot_priority_test.go:52
out := buildScreenshotPrompt("https://x/quickstart", "body", nil, nil)
// internal/analyzer/screenshot_gaps.go: callers (fitContentToBudget, buildDetectionPromptWithVerdicts fallback) gain a nil 4th arg too.
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/... -count=1`
Expected: compile error on call-site arity mismatch (fix all call sites in this same edit) followed by failure on the three new tests once compilation passes.

If you see "undefined: buildScreenshotPrompt with 4 args" — that is RED for the right reason.

**Step 3: Write minimal implementation**

Modify `buildScreenshotPrompt` in `internal/analyzer/screenshot_gaps.go`:

```go
func buildScreenshotPrompt(pageURL, content string, coverage map[string][]imageRef, codeBlocks []codeBlockRef) string {
	var coverageSummary string
	if len(coverage) == 0 {
		coverageSummary = "No existing images on this page."
	} else {
		// (existing image-listing logic unchanged)
		// ...
	}

	codeBlocksSummary := "No code blocks on this page."
	if len(codeBlocks) > 0 {
		var lines []string
		for _, b := range codeBlocks {
			heading := b.SectionHeading
			if heading == "" {
				heading = "(no heading)"
			}
			lang := b.Language
			if lang == "" {
				lang = "(none)"
			}
			lines = append(lines, fmt.Sprintf("- code-%d, section %q, paragraph %d: language=%s, %d lines",
				b.OriginalIndex, heading, b.ParagraphIndex, lang, b.LineCount))
		}
		codeBlocksSummary = strings.Join(lines, "\n")
	}

	// PROMPT: (extend existing PROMPT comment to mention code-block coverage signals)
	return fmt.Sprintf(`You are reviewing a documentation page to find the small number of places where a screenshot would meaningfully help the reader — places where prose alone leaves a real gap. Be selective. Most pages should produce zero gaps.

URL: %s

Existing images on this page (if any):
%s

Existing code blocks on this page (if any):
%s

Page content:
%s

(rest of existing prompt unchanged for now — Task 5 updates the rules)`,
		pageURL, coverageSummary, codeBlocksSummary, content)
}
```

Important: in Task 2 we are ONLY adding the structured locality block. The "Do NOT flag" rule list and the covered-passage rule are updated in Task 5 (after the vision prompt has the same plumbing). Keeping these orthogonal makes commits reviewable.

Update the two internal callers (`fitContentToBudget` and `buildDetectionPromptWithVerdicts`'s fallback path on the verdicts==0 branch) to pass `nil` for now; Task 3 wires the real value through.

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/... -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go internal/analyzer/screenshot_priority_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): list code blocks in no-vision screenshot detection prompt

- RED: TestBuildScreenshotPrompt_NoCodeBlocks /
  TestBuildScreenshotPrompt_ListsCodeBlocksWithLocality /
  TestBuildScreenshotPrompt_EmptyHeadingFallback
- GREEN: extend buildScreenshotPrompt signature with codeBlocks []codeBlockRef
  and render an "Existing code blocks on this page" section parallel to images
- Status: analyzer tests passing, build successful
EOF
)"
```

---

## Task 3: Render code-block coverage in the vision prompt

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go:629-735` (`buildDetectionPromptWithVerdicts`)
- Test: `internal/analyzer/screenshot_gaps_test.go` (extend the verdict-prompt test block)

**Step 1: Write failing tests**

```go
func TestBuildDetectionPromptWithVerdicts_NoCodeBlocks(t *testing.T) {
	refs := []imageRef{{AltText: "x", Src: "x.png", OriginalIndex: 1, SectionHeading: "S", ParagraphIndex: 0}}
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}}
	got := buildDetectionPromptWithVerdicts("https://x/p", "content", refs, verdicts, nil)
	assert.Contains(t, got, "Existing code blocks on this page (if any):")
	assert.Contains(t, got, "No code blocks on this page.")
}

func TestBuildDetectionPromptWithVerdicts_ListsCodeBlocks(t *testing.T) {
	refs := []imageRef{{AltText: "x", Src: "x.png", OriginalIndex: 1}}
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}}
	blocks := []codeBlockRef{
		{Language: "json", LineCount: 5, SectionHeading: "Response", ParagraphIndex: 3, OriginalIndex: 1},
	}
	got := buildDetectionPromptWithVerdicts("https://x/p", "content", refs, verdicts, blocks)
	assert.Contains(t, got, `- code-1, section "Response", paragraph 3: language=json, 5 lines`)
}

func TestBuildDetectionPromptWithVerdicts_EmptyVerdictsDelegatesWithBlocks(t *testing.T) {
	// When verdicts is empty, delegate to buildScreenshotPrompt — but pass
	// codeBlocks through. Pin the contract so a future refactor doesn't
	// silently strip them on the no-verdict branch.
	blocks := []codeBlockRef{{Language: "bash", LineCount: 1, OriginalIndex: 1}}
	got := buildDetectionPromptWithVerdicts("https://x/p", "content", nil, nil, blocks)
	want := buildScreenshotPrompt("https://x/p", "content", nil, blocks)
	assert.Equal(t, want, got)
}
```

Update existing call sites of `buildDetectionPromptWithVerdicts` in tests (3 sites in `screenshot_gaps_test.go`, 1 in `screenshot_priority_test.go`) to add a final `nil` arg.

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/... -count=1`
Expected: compile error on call-site arity, then failure of the three new tests once arity is fixed.

**Step 3: Write minimal implementation**

In `buildDetectionPromptWithVerdicts`:

```go
func buildDetectionPromptWithVerdicts(pageURL, content string, refs []imageRef, verdicts []ImageVerdict, codeBlocks []codeBlockRef) string {
	if len(verdicts) == 0 {
		return buildScreenshotPrompt(pageURL, content, buildCoverageMap(refs), codeBlocks)
	}
	// ... existing verdict-rendering logic ...
	codeBlocksSummary := "No code blocks on this page."
	if len(codeBlocks) > 0 {
		var lines []string
		for _, b := range codeBlocks {
			heading := b.SectionHeading
			if heading == "" {
				heading = "(no heading)"
			}
			lang := b.Language
			if lang == "" {
				lang = "(none)"
			}
			lines = append(lines, fmt.Sprintf("- code-%d, section %q, paragraph %d: language=%s, %d lines",
				b.OriginalIndex, heading, b.ParagraphIndex, lang, b.LineCount))
		}
		codeBlocksSummary = strings.Join(lines, "\n")
	}
	return fmt.Sprintf(`... (extend the existing template) ...

Existing code blocks on this page (if any):
%s

Page content:
%s
...`, ..., codeBlocksSummary, content, ...)
}
```

Refactor: the code-block summary builder is now duplicated across `buildScreenshotPrompt` and `buildDetectionPromptWithVerdicts`. Extract a small `renderCodeBlockCoverage(blocks []codeBlockRef) string` helper. Use it in both places. (DRY per CLAUDE.md.)

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/... -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go internal/analyzer/screenshot_priority_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): list code blocks in vision-path screenshot detection prompt

- RED: TestBuildDetectionPromptWithVerdicts_NoCodeBlocks /
  TestBuildDetectionPromptWithVerdicts_ListsCodeBlocks /
  TestBuildDetectionPromptWithVerdicts_EmptyVerdictsDelegatesWithBlocks
- GREEN: extend signature with codeBlocks; share rendering via
  renderCodeBlockCoverage helper
- Status: analyzer tests passing, build successful
EOF
)"
```

---

## Task 4: Add `suppressed_by_code_block` to the output schema

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go:795-844` (`screenshotGapsResponse` struct + `screenshotGapsSchema` JSON Schema)
- Test: `internal/analyzer/screenshot_gaps_test.go` (extend existing schema-shape tests if any; otherwise add one)

**Step 1: Write failing test**

```go
func TestScreenshotGapsResponse_DecodesSuppressedByCodeBlock(t *testing.T) {
	raw := []byte(`{
	  "gaps": [],
	  "suppressed_by_image": [],
	  "suppressed_by_code_block": [
	    {"quoted_passage": "p", "should_show": "s", "suggested_alt": "a",
	     "insertion_hint": "h", "priority": "small", "priority_reason": "r"}
	  ]
	}`)
	var resp screenshotGapsResponse
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.Len(t, resp.SuppressedByCodeBlock, 1)
	assert.Equal(t, "p", resp.SuppressedByCodeBlock[0].QuotedPassage)
}

func TestScreenshotGapsSchema_AllowsSuppressedByCodeBlock(t *testing.T) {
	// The JSON Schema must declare the new array (additionalProperties:false
	// would reject it otherwise).
	doc := string(screenshotGapsSchema.Doc)
	assert.Contains(t, doc, `"suppressed_by_code_block"`)
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/... -run "TestScreenshotGapsResponse_DecodesSuppressedByCodeBlock|TestScreenshotGapsSchema_AllowsSuppressedByCodeBlock" -count=1 -v`
Expected: failures — `SuppressedByCodeBlock` undefined and schema doesn't contain the key.

**Step 3: Write minimal implementation**

```go
type screenshotGapsResponse struct {
	Gaps                  []screenshotResponseItem `json:"gaps"`
	SuppressedByImage     []screenshotResponseItem `json:"suppressed_by_image"`
	SuppressedByCodeBlock []screenshotResponseItem `json:"suppressed_by_code_block"`
}

// screenshotGapsSchema gains a third array with identical item shape.
// "required" includes "suppressed_by_code_block" so providers always emit it
// (empty array on no suppressions).
```

Update the `Doc` JSON literal — copy the `suppressed_by_image` array definition wholesale and rename the key.

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/... -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add suppressed_by_code_block to screenshot detection schema

- RED: TestScreenshotGapsResponse_DecodesSuppressedByCodeBlock /
  TestScreenshotGapsSchema_AllowsSuppressedByCodeBlock
- GREEN: add SuppressedByCodeBlock array to wire struct + JSON schema, parallel
  to the existing suppressed_by_image channel
- Status: analyzer tests passing, build successful
EOF
)"
```

---

## Task 5: Update prompt rules — generalize covered-passage and Do-NOT-flag

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` — both `buildScreenshotPrompt` (around line 577) and `buildDetectionPromptWithVerdicts` (around line 688) prompt template strings, AND the `// PROMPT:` comments above each
- Test: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Write failing tests**

```go
func TestBuildScreenshotPrompt_GeneralizesCoveredRuleToCodeBlocks(t *testing.T) {
	got := buildScreenshotPrompt("https://x/p", "content", nil, nil)
	// Coverage rule must explicitly call out code-block coverage:
	assert.Contains(t, got, "code block")
	// Do NOT flag list expanded:
	assert.Contains(t, got, "API responses, config files, or data shapes")
	assert.Contains(t, got, "Rendered UI whose source is already shown")
}

func TestBuildDetectionPromptWithVerdicts_GeneralizesCoveredRule(t *testing.T) {
	refs := []imageRef{{AltText: "x", Src: "x.png", OriginalIndex: 1}}
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}}
	got := buildDetectionPromptWithVerdicts("https://x/p", "content", refs, verdicts, nil)
	assert.Contains(t, got, "code block")
	assert.Contains(t, got, "API responses, config files, or data shapes")
	assert.Contains(t, got, "Rendered UI whose source is already shown")
	// Verdict-aware prompt must also instruct the model to populate the new array:
	assert.Contains(t, got, `"suppressed_by_code_block"`)
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/... -run "GeneralizesCovered" -count=1 -v`
Expected: 3 substring assertions fail.

**Step 3: Write minimal implementation**

Update the prompt templates. The generalized covered-passage rule (legacy prompt):

> A passage may already be visually covered by an existing image OR a nearby code block.
> - Image coverage: an image's alt text and src plausibly describe the same UI moment AND the image appears in the same section heading or within 3 paragraphs before/after.
> - Code-block coverage: a code block sits in the same section heading or within 3 paragraphs AND its language plausibly matches the moment in prose — `bash`/`console`/`shell`/`text`/`sh` for terminal output; `json`/`yaml`/`toml`/`xml` for response or config shapes; `html`/`jsx`/`tsx`/`vue`/`svelte`/`css` for rendered UI source. The full block content appears verbatim in the page content above; judge topical fit by reading it directly.
> An off-topic nearby image OR an off-topic nearby code block does NOT cover the passage.

The "Do NOT flag" list (replace the single terminal-output bullet with three):

> - Terminal sessions whose output is shown inline in a nearby code block.
> - API responses, config files, or data shapes already shown verbatim in a nearby `json`/`yaml`/`toml`/`xml` code block under the locality rule above.
> - Rendered UI whose source is already shown in a nearby `html`/`jsx`/`tsx`/`vue`/`svelte`/`css` code block where the prose describes how the resulting UI looks.

For the verdict-aware prompt (`buildDetectionPromptWithVerdicts`), additionally:
- Update the "KEY QUESTION" paragraph to reference both image-verdict-matches AND code-block coverage.
- Add an instruction to populate `suppressed_by_code_block` with code-block-suppressed moments. Schema: same six fields as `gaps`. Audit-only.

Update both `// PROMPT:` comments above the template strings to mention code-block coverage.

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/... -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): generalize screenshot covered-passage rule to code blocks

- RED: TestBuildScreenshotPrompt_GeneralizesCoveredRuleToCodeBlocks /
  TestBuildDetectionPromptWithVerdicts_GeneralizesCoveredRule
- GREEN: extend covered-passage rule + Do-NOT-flag list to cover terminal
  output, API/config shapes, and rendered UI source code blocks; instruct
  vision prompt to populate suppressed_by_code_block
- Status: analyzer tests passing, build successful
EOF
)"
```

---

## Task 6: Stats fields and orchestrator wiring

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` — `ScreenshotPageStats` struct (around line 1083), `detectionPass` (around line 1109), `DetectScreenshotGaps` (around line 1219)
- Test: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Write failing tests**

Use the existing stub `LLMClient` pattern from `screenshot_gaps_integration_test.go` (look there for the stub shape). New test case:

```go
func TestDetectScreenshotGaps_RoutesSuppressedByCodeBlockIntoPossiblyCovered(t *testing.T) {
	stub := &screenshotStubClient{
		// stub returns one suppressed_by_code_block item, zero gaps, zero suppressed_by_image
		jsonResponse: `{
		  "gaps": [],
		  "suppressed_by_image": [],
		  "suppressed_by_code_block": [
		    {"quoted_passage": "Run brew install foo", "should_show": "shell output",
		     "suggested_alt": "terminal", "insertion_hint": "after install line",
		     "priority": "small", "priority_reason": "low signal"}
		  ]
		}`,
	}
	page := DocPage{URL: "https://x/p", Content: "# Quickstart\n\nRun brew install foo\n\n```bash\nbrew install foo\n```\n"}
	res, err := DetectScreenshotGaps(context.Background(), stub, []DocPage{page}, 1, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, res.PossiblyCovered, 1)
	assert.Equal(t, "Run brew install foo", res.PossiblyCovered[0].QuotedPassage)
	require.Len(t, res.AuditStats, 1)
	assert.Equal(t, 1, res.AuditStats[0].CodeBlocksSeen)
	assert.Equal(t, 1, res.AuditStats[0].SuppressedByCodeBlock)
	// The aggregate possibly_covered count must include the code-block suppression:
	assert.Equal(t, 1, res.AuditStats[0].PossiblyCovered)
}

func TestDetectScreenshotGaps_CountsCodeBlocksSeenEvenWhenZeroSuppressions(t *testing.T) {
	stub := &screenshotStubClient{jsonResponse: `{"gaps":[],"suppressed_by_image":[],"suppressed_by_code_block":[]}`}
	page := DocPage{URL: "https://x/p", Content: "# Q\n\n```bash\nfoo\n```\n\n```json\n{}\n```\n"}
	res, err := DetectScreenshotGaps(context.Background(), stub, []DocPage{page}, 1, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, res.AuditStats, 1)
	assert.Equal(t, 2, res.AuditStats[0].CodeBlocksSeen)
	assert.Equal(t, 0, res.AuditStats[0].SuppressedByCodeBlock)
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/... -run "TestDetectScreenshotGaps_RoutesSuppressedByCodeBlock|TestDetectScreenshotGaps_CountsCodeBlocksSeen" -count=1 -v`
Expected: compile error on the new stats fields, then failure once the fields exist but aren't populated.

**Step 3: Write minimal implementation**

Add fields to `ScreenshotPageStats`:

```go
type ScreenshotPageStats struct {
	PageURL               string
	VisionEnabled         bool
	RelevanceBatches      int
	ImagesSeen            int
	CodeBlocksSeen        int  // NEW
	ImageIssues           int
	MissingScreenshots    int
	PossiblyCovered       int  // union: image + code-block suppressions
	SuppressedByCodeBlock int  // NEW: disaggregated code-block-only count
	DetectionSkipped      bool
}
```

Update `detectionPass` signature to accept `codeBlocks []codeBlockRef` and return `(gaps, suppressedByImage, suppressedByCodeBlock []ScreenshotGap, skipped bool, err error)` — pass `codeBlocks` through to `buildDetectionPromptWithVerdicts` (and via the fallback to `buildScreenshotPrompt`). Decode all three response arrays. Run the same `validateScreenshotGap` + `unescapeLiteralWhitespace` treatment on each.

Update `DetectScreenshotGaps`:
1. Call `extractCodeBlocks(page.Content)` once per page; set `stats.CodeBlocksSeen`.
2. Pass `codeBlocks` to `detectionPass`.
3. Append the code-block-suppressed slice to `result.PossiblyCovered` alongside the image-suppressed slice.
4. Set `stats.SuppressedByCodeBlock = len(suppressedByCodeBlock)`.
5. Set `stats.PossiblyCovered = len(suppressedByImage) + len(suppressedByCodeBlock)` (the union).
6. Update the cache-hit branch to read both stats fields back from the cached entry — no special-casing needed since `ScreenshotPageStats` is value-copied.

Also update `fitContentToBudget` if it calls `buildScreenshotPrompt` for overhead measurement — pass the `codeBlocks` slice so the budget reflects reality.

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/... -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): wire code-block coverage through DetectScreenshotGaps

- RED: TestDetectScreenshotGaps_RoutesSuppressedByCodeBlockIntoPossiblyCovered /
  TestDetectScreenshotGaps_CountsCodeBlocksSeenEvenWhenZeroSuppressions
- GREEN: extractCodeBlocks per page; pass through detectionPass + prompts;
  decode suppressed_by_code_block; merge into PossiblyCovered; populate
  CodeBlocksSeen + SuppressedByCodeBlock stats
- Status: analyzer tests passing, build successful
EOF
)"
```

---

## Task 7: Audit log line surfaces the new signal

**Files:**
- Modify: `internal/cli/screenshot_audit.go:20-38` (`emitScreenshotAuditLog`)
- Test: `internal/cli/screenshot_audit_test.go` (extend existing assertions; if golden-string, update them)

**Step 1: Write failing test**

```go
func TestEmitScreenshotAuditLog_IncludesCodeBlockFields(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf)
	defer log.SetDefault(log.Default()) // restore
	log.SetDefault(logger)

	stats := []analyzer.ScreenshotPageStats{{
		PageURL:               "https://x/p",
		VisionEnabled:         true,
		ImagesSeen:            2,
		CodeBlocksSeen:        3,
		PossiblyCovered:       2,
		SuppressedByCodeBlock: 1,
	}}
	emitScreenshotAuditLog(stats)
	out := buf.String()
	assert.Contains(t, out, "code_blocks_seen=3")
	assert.Contains(t, out, "suppressed_by_code_block=1")
}
```

(If the existing test file uses a different log capture pattern, follow that pattern. Use `internal/cli/screenshot_audit_test.go` as the template.)

**Step 2: Verify RED**

Run: `go test ./internal/cli/... -run TestEmitScreenshotAuditLog_IncludesCodeBlockFields -count=1 -v`
Expected: substring assertions fail.

**Step 3: Write minimal implementation**

Extend the format string and arglist:

```go
log.Infof(
	"page=%s vision=%s relevance_batches=%d images_seen=%d code_blocks_seen=%d image_issues=%d missing_screenshots=%d possibly_covered=%d suppressed_by_code_block=%d detection_skipped=%t",
	s.PageURL, visionFlag, s.RelevanceBatches, s.ImagesSeen, s.CodeBlocksSeen,
	s.ImageIssues, s.MissingScreenshots, s.PossiblyCovered, s.SuppressedByCodeBlock, s.DetectionSkipped,
)
```

The format is column-stable and additive — log scrapers that key on the existing fields keep working; the new fields appear in fixed positions.

**Step 4: Verify GREEN**

Run: `go test ./internal/cli/... -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/screenshot_audit.go internal/cli/screenshot_audit_test.go
git commit -m "$(cat <<'EOF'
feat(cli): surface code-block coverage signals in screenshot audit log

- RED: TestEmitScreenshotAuditLog_IncludesCodeBlockFields
- GREEN: add code_blocks_seen and suppressed_by_code_block to the per-page
  audit log line
- Status: cli tests passing, build successful
EOF
)"
```

---

## Task 8: Integration test against the real prompt path

**Files:**
- Modify: `internal/analyzer/screenshot_gaps_integration_test.go` (extend the existing happy-path fixture pattern)

**Step 1: Write failing test**

Add three fixture pages to the existing integration test (or a new test using the same stub-client harness):

```go
func TestDetectScreenshotGaps_NoFalsePositiveOnTerminalOutput(t *testing.T) {
	page := DocPage{
		URL: "https://x/install",
		Content: "# Install\n\nRun:\n\n```bash\nbrew install foo\nfoo --version\n# foo 1.2.3\n```\n\nYou should see the version.\n",
	}
	stub := &screenshotStubClient{ /* echoes the prompt back so we can assert what the model saw */ }
	_, err := DetectScreenshotGaps(context.Background(), stub, []DocPage{page}, 1, nil, nil, nil, nil)
	require.NoError(t, err)
	// The prompt sent to the stub must list the code block and tell the
	// model that terminal output in a nearby bash code block is do-not-flag:
	assert.Contains(t, stub.lastPrompt, `code-1, section "Install"`)
	assert.Contains(t, stub.lastPrompt, "language=bash")
}
```

Two more parallel cases for JSON-response and HTML-preview pages, asserting on the prompt's listed coverage rather than on the model's response (which is stubbed).

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/... -run TestDetectScreenshotGaps_NoFalsePositive -count=1 -v`
Expected: compile or assertion failures.

**Step 3: Write minimal implementation**

The stub-client mechanics may already exist in `screenshot_gaps_integration_test.go`; reuse `lastPrompt` capture if so. Otherwise add a `lastPrompt string` field to the stub.

No production-code changes should be needed for this task — Tasks 1-6 already deliver the wiring. If a test fails, that's a real bug — fix the underlying issue, do not paper over the test.

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/... -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps_integration_test.go
git commit -m "$(cat <<'EOF'
test(analyzer): integration coverage for code-block-suppressed screenshot gaps

- RED: TestDetectScreenshotGaps_NoFalsePositiveOnTerminalOutput plus parallel
  cases for JSON-response and HTML-preview pages
- GREEN: existing wiring already correct; tests pin behavior end-to-end
- Status: analyzer tests passing, build successful
EOF
)"
```

---

## Task 9: Verification plan update

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md` — extend Scenario 5 with code-block-coverage assertions

**Step 1: Edit**

Append to Scenario 5 success criteria:

```markdown
- [ ] On a docs page that contains a terminal-output passage covered by an adjacent ```bash code block, `screenshots.md` does NOT include a missing-screenshot finding for that passage. The audit log for that page reports `code_blocks_seen >= 1` and `suppressed_by_code_block >= 1`.
- [ ] On a docs page whose response shape is documented via prose followed by an adjacent ```json code block, no missing-screenshot finding flags the response shape. Audit log reports `suppressed_by_code_block >= 1` for that page.
- [ ] On a docs page whose UI is described in prose followed by an adjacent ```html / ```jsx code block, no missing-screenshot finding flags the rendered UI. Audit log reports `suppressed_by_code_block >= 1` for that page.
```

**Step 2: Commit**

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "$(cat <<'EOF'
docs(plans): extend Scenario 5 with code-block coverage assertions

- Verify code-block-suppressed terminal output / JSON responses / HTML previews
  do not produce false-positive missing-screenshot findings
EOF
)"
```

---

## Task 10: Final verification

**No new code** — gate the whole change.

**Step 1:** Run the full test suite.
```bash
go test ./... -count=1
```
Expected: all packages PASS.

**Step 2:** Coverage check.
```bash
go test -coverprofile=coverage.out ./internal/analyzer/... && go tool cover -func=coverage.out
```
Expected: ≥90% statement coverage on `internal/analyzer`.

**Step 3:** Lint.
```bash
golangci-lint run
```
Expected: clean.

**Step 4:** Build.
```bash
go build ./...
```
Expected: succeeds.

**Step 5:** Manual smoke test against a real fixture (per Scenario 9 / 5 in the verification plan):

```bash
go build -o /tmp/ftg ./cmd/find-the-gaps
/tmp/ftg analyze --repo ./testdata/fixtures/<known-good> --docs <known-good-docs> --experimental-check-screenshots -v 2>&1 | tee /tmp/ftg-run.log
```

Then:
- Inspect `/tmp/ftg-run.log` for `code_blocks_seen=` audit lines.
- Inspect `<projectDir>/screenshots.md` for false-positive missing-screenshot findings on terminal-output / JSON / HTML passages.
- Compare to the previous fixture run (if available) — expected to see fewer findings in the missing-screenshots section, possibly more in the possibly-covered section.

**If any criterion fails:** stop and ask. Do not paper over.

---

## Rollback notes

The cache key (`URL + content_hash`) is unchanged, so on revert old caches stay usable. The only persisted-on-disk additions are two int fields on `ScreenshotPageStats`; older binaries deserializing newer cache files ignore unknown fields by default in Go's `encoding/json`. No schema bump required either way.

## What this plan does NOT do

- No vision-relevance pass on code blocks. The text model already reads them in the page content; we just give it locality signals.
- No changes to `screenshots.md` user-rendered shape (only the audit log changes).
- No new LLM call — the new signal lands inside the existing detection call's output schema.
- No cache schema version bump.
