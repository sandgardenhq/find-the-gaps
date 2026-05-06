# Finding Priority — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. TDD per @CLAUDE.md is mandatory — RED, then GREEN, then REFACTOR. Commit after each task.

**Goal:** Tag every LLM-judged finding (drift, missing screenshots, image issues, possibly-covered) with a `priority` (`large` / `medium` / `small`) and a one-sentence reason; group findings in Markdown and the rendered Hugo site by priority, most important first.

**Architecture:** Priority is folded into existing structured-output prompts (no new LLM calls). A deterministic `page_role` URL-based heuristic feeds the LLM as bias context. Drift gains additive fields in `drift.json`; a new `screenshots.json` is written. Markdown and site templates render `### Large / Medium / Small` sub-buckets; site renders a colored badge per finding.

**Reference design:** `.plans/2026-05-05-finding-priority-design.md`.

**Tech stack:** Go 1.26+, testify, testscript, Hugo templates.

---

## Common conventions

- Every prompt change keeps the existing `// PROMPT:` comment and adds the rubric exactly once via a shared constant — DRY.
- Every test file lives next to its production file (`foo.go` → `foo_test.go`).
- Run `go test ./...` after each task to verify nothing else broke.
- Commit after each task with the message format from CLAUDE.md (TDD-mentioning).

---

## Task 1: Add `Priority` type + URL-based `pageRole` helper

**Files:**
- Modify: `internal/analyzer/types.go` (add type at end of file)
- Create: `internal/analyzer/page_role.go`
- Create: `internal/analyzer/page_role_test.go`

**Step 1 — Write failing tests for `pageRole`:**

```go
// internal/analyzer/page_role_test.go
package analyzer

import "testing"

func TestPageRole(t *testing.T) {
	cases := []struct {
		url, want string
	}{
		{"https://example.com/", "top-nav"},
		{"https://example.com/readme/", "readme"},
		{"https://example.com/docs/quickstart", "quickstart"},
		{"https://example.com/docs/getting-started/", "quickstart"},
		{"https://example.com/docs/", "top-nav"},
		{"https://example.com/docs/api", "top-nav"},
		{"https://example.com/docs/api/auth/", "reference"},
		{"https://example.com/docs/api/auth/oauth/flows/code", "deep"},
		{"not a url", "unknown"},
	}
	for _, c := range cases {
		if got := pageRole(c.url); got != c.want {
			t.Errorf("pageRole(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}
```

**Step 2 — Run test, see RED:**

```
go test ./internal/analyzer/ -run TestPageRole -v
```
Expected: FAIL with "undefined: pageRole".

**Step 3 — Add the type and helper.**

Append to `internal/analyzer/types.go`:

```go
// Priority is the user-impact rating for a finding.
type Priority string

const (
	PriorityLarge  Priority = "large"
	PriorityMedium Priority = "medium"
	PrioritySmall  Priority = "small"
)
```

Create `internal/analyzer/page_role.go`:

```go
package analyzer

import (
	"net/url"
	"strings"
)

// pageRole classifies a docs page URL into one of:
//   "readme" | "quickstart" | "top-nav" | "reference" | "deep" | "unknown"
// purely from the URL string. Used as a prominence hint to the priority-rating
// LLM prompts; never authoritative.
func pageRole(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	low := strings.ToLower(u.Path)
	if strings.Contains(low, "readme") {
		return "readme"
	}
	if strings.Contains(low, "quickstart") ||
		strings.Contains(low, "getting-started") ||
		strings.Contains(low, "getting_started") {
		return "quickstart"
	}
	segs := strings.FieldsFunc(low, func(r rune) bool { return r == '/' })
	switch {
	case len(segs) <= 2:
		return "top-nav"
	case len(segs) >= 5:
		return "deep"
	default:
		return "reference"
	}
}
```

**Step 4 — Run, see GREEN.**

**Step 5 — Commit:**

```bash
git add internal/analyzer/types.go internal/analyzer/page_role.go internal/analyzer/page_role_test.go
git commit -m "feat(analyzer): add Priority type and pageRole helper

- RED: TestPageRole covering readme/quickstart/top-nav/reference/deep/unknown
- GREEN: URL-only heuristic, no IO
- Status: passing, build clean"
```

---

## Task 2: Shared rubric prompt constant

**Files:**
- Create: `internal/analyzer/priority_prompt.go`
- Create: `internal/analyzer/priority_prompt_test.go`

**Step 1 — Write failing test:**

```go
// internal/analyzer/priority_prompt_test.go
package analyzer

import (
	"strings"
	"testing"
)

func TestPriorityRubric(t *testing.T) {
	for _, s := range []string{`"large"`, `"medium"`, `"small"`, "priority_reason", "page_role"} {
		if !strings.Contains(priorityRubric, s) {
			t.Errorf("priorityRubric missing %q", s)
		}
	}
}
```

**Step 2 — RED:** `go test ./internal/analyzer/ -run TestPriorityRubric -v` → undefined.

**Step 3 — Create file:**

```go
package analyzer

// PROMPT: Shared user-impact rubric inserted into every finding-producing
// prompt. The model returns a "priority" enum and a one-sentence
// "priority_reason". A page_role hint is provided by the caller so the model
// can weight findings on quickstart/readme pages higher than deep reference.
const priorityRubric = `Rate user impact for this finding as one of:
- "large": a reader following the docs will fail or be actively misled.
- "medium": a reader will be confused or have to dig elsewhere, but won't outright fail.
- "small": a reader probably won't notice or can shrug it off.

Factor in where the finding lives. The page_role hint can be:
- "readme" or "quickstart": findings here are weighted higher (very visible).
- "top-nav": findings on top-level navigation pages, weighted higher.
- "reference": findings on deeper reference pages, normal weight.
- "deep": findings on very deep pages or appendices, weighted lower.
- "unknown": no signal — judge on the finding alone.

Also produce priority_reason: one sentence explaining the rating.`
```

**Step 4 — GREEN.**

**Step 5 — Commit.**

---

## Task 3: Drift — add Priority to DriftIssue, schema, judge prompt

**Files:**
- Modify: `internal/analyzer/types.go` (DriftIssue struct around line 106)
- Modify: `internal/analyzer/drift.go` (judgeSchema around line 371, judgeFeatureDrift around line 402)
- Modify: `internal/analyzer/drift_test.go` (existing tests need updated schema fixtures)

**Step 1 — Write failing test in `drift_test.go`:**

Add a new test that asserts the parsed `DriftIssue` carries `Priority` and `PriorityReason`:

```go
func TestJudgeResponseParsesPriority(t *testing.T) {
	raw := []byte(`{"issues":[{"page":"https://x/y","issue":"foo","priority":"large","priority_reason":"on quickstart"}]}`)
	var resp judgeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Issues) != 1 {
		t.Fatalf("got %d issues", len(resp.Issues))
	}
	got := resp.Issues[0]
	if got.Priority != PriorityLarge {
		t.Errorf("Priority = %q, want large", got.Priority)
	}
	if got.PriorityReason != "on quickstart" {
		t.Errorf("PriorityReason = %q", got.PriorityReason)
	}
}
```

Also add a test that fails closed when priority is missing:

```go
func TestJudgeResponseRejectsMissingPriority(t *testing.T) {
	// The Go json package will accept missing fields silently, so the
	// validation lives in validateDriftIssues — assert it rejects.
	issues := []DriftIssue{{Page: "p", Issue: "i" /* no priority */}}
	if err := validateDriftIssues(issues); err == nil {
		t.Fatal("expected error for missing priority")
	}
}

func TestJudgeResponseRejectsBogusPriority(t *testing.T) {
	issues := []DriftIssue{{Page: "p", Issue: "i", Priority: "huge", PriorityReason: "x"}}
	if err := validateDriftIssues(issues); err == nil {
		t.Fatal("expected error for bogus priority")
	}
}
```

**Step 2 — RED:** `go test ./internal/analyzer/ -run TestJudgeResponse -v`. Expected: undefined `validateDriftIssues`, plus the parse test fails because `DriftIssue` lacks the fields.

**Step 3 — Edit `internal/analyzer/types.go`:**

Replace the existing `DriftIssue` struct with:

```go
type DriftIssue struct {
	Page           string   `json:"page"`
	Issue          string   `json:"issue"`
	Priority       Priority `json:"priority"`
	PriorityReason string   `json:"priority_reason"`
}
```

Edit `internal/analyzer/drift.go`:

1. Update `judgeSchema` to require `priority` (enum: large/medium/small) and `priority_reason`:

```go
var judgeSchema = JSONSchema{
	Name: "drift_judge_issues",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "issues": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "page":            {"type": "string"},
              "issue":           {"type": "string"},
              "priority":        {"type": "string", "enum": ["large", "medium", "small"]},
              "priority_reason": {"type": "string"}
            },
            "required": ["page", "issue", "priority", "priority_reason"],
            "additionalProperties": false
          }
        }
      },
      "required": ["issues"],
      "additionalProperties": false
    }`),
}
```

2. Add `validateDriftIssues`:

```go
func validateDriftIssues(issues []DriftIssue) error {
	for i, iss := range issues {
		switch iss.Priority {
		case PriorityLarge, PriorityMedium, PrioritySmall:
		default:
			return fmt.Errorf("issue %d: invalid priority %q", i, iss.Priority)
		}
		if strings.TrimSpace(iss.PriorityReason) == "" {
			return fmt.Errorf("issue %d: empty priority_reason", i)
		}
	}
	return nil
}
```

3. In `judgeFeatureDrift`, after `json.Unmarshal(raw, &resp)`, add:

```go
if err := validateDriftIssues(resp.Issues); err != nil {
	return nil, fmt.Errorf("judgeFeatureDrift %q: %w", feature.Name, err)
}
```

4. Update the judge prompt: append the rubric and a page_role list to the existing prompt body. Add a helper at top of `drift.go`:

```go
func pageRoleSummary(pages []string) string {
	var b strings.Builder
	b.WriteString("Page role hints:\n")
	for _, p := range pages {
		fmt.Fprintf(&b, "- %s -> %s\n", p, pageRole(p))
	}
	return b.String()
}
```

In `judgeFeatureDrift`, change the prompt construction to gather the unique pages from observations and inject:

```go
pages := uniqueObservationPages(observations)
prompt := fmt.Sprintf(`%s

%s

%s

If every observation is a false alarm, emit an empty "issues" array.`,
    /* existing body */, pageRoleSummary(pages), priorityRubric)
```

Add `uniqueObservationPages` helper.

**Step 4 — GREEN:** `go test ./internal/analyzer/ -v`. Existing drift tests likely fail because their fixtures don't include `priority`. Update fixtures in `drift_test.go` and `analyze_page_test.go` (search for hard-coded judge JSON) to include `"priority":"medium","priority_reason":"x"` so they parse.

**Step 5 — Commit.**

---

## Task 4: Drift cache — recompute on missing priority

**Files:**
- Modify: `internal/analyzer/drift.go` (cache hit path around line 134)
- Modify: `internal/analyzer/drift_test.go`

**Step 1 — Write failing test:**

```go
func TestDetectDriftCacheMissOnAbsentPriority(t *testing.T) {
	// Cache entry whose Issues lack a Priority must be treated as a miss.
	cached := map[string]CachedDriftEntry{
		"FeatA": {
			Files:  []string{"a.go"},
			Pages:  []string{"https://x/a"},
			Issues: []DriftIssue{{Page: "https://x/a", Issue: "old", /* no priority */}},
		},
	}
	if !cacheNeedsRecompute(cached["FeatA"]) {
		t.Fatal("expected cache miss for missing priority")
	}
}
```

**Step 2 — RED.**

**Step 3 — Add helper in `drift.go`:**

```go
func cacheNeedsRecompute(entry CachedDriftEntry) bool {
	for _, iss := range entry.Issues {
		switch iss.Priority {
		case PriorityLarge, PriorityMedium, PrioritySmall:
			continue
		default:
			return true
		}
	}
	return false
}
```

In the cache-hit branch of `DetectDrift`, before reusing the cached issues, gate on `!cacheNeedsRecompute(c)`. When it returns true, fall through to the fresh investigate+judge path.

**Step 4 — GREEN.**

**Step 5 — Commit.**

---

## Task 5: ScreenshotGap — Priority through detection schema and prompt

**Files:**
- Modify: `internal/analyzer/types.go` (ScreenshotGap struct around line 118)
- Modify: `internal/analyzer/screenshot_gaps.go` (`screenshotResponseItem` ~715, `screenshotGapsSchema` ~735, `buildScreenshotPrompt` ~515, `buildDetectionPromptWithVerdicts` ~586, `detectionPass` ~1006)
- Modify: `internal/analyzer/screenshot_gaps_test.go`

**Step 1 — Failing tests:**

Add tests asserting:
- `screenshotResponseItem` round-trips `priority` and `priority_reason`.
- `validateScreenshotGap` rejects missing/invalid priority.
- The detection prompt contains the rubric and a `page_role:` line for the page.

**Step 2 — RED.**

**Step 3 — Implement:**

1. `ScreenshotGap` (`types.go`):

```go
type ScreenshotGap struct {
	PageURL        string   `json:"page_url"`
	PagePath       string   `json:"page_path"`
	QuotedPassage  string   `json:"quoted_passage"`
	ShouldShow     string   `json:"should_show"`
	SuggestedAlt   string   `json:"suggested_alt"`
	InsertionHint  string   `json:"insertion_hint"`
	Priority       Priority `json:"priority"`
	PriorityReason string   `json:"priority_reason"`
}
```

2. `screenshotResponseItem` gains `Priority` and `PriorityReason` JSON-tagged fields.

3. `screenshotGapsSchema` — add `priority` (enum) and `priority_reason` to the per-item required fields, in BOTH the `gaps` array items AND the `suppressed_by_image` array items.

4. `buildScreenshotPrompt` and `buildDetectionPromptWithVerdicts`: append after the existing body, before "When in doubt, do not flag.":

```go
fmt.Fprintf(&b, "\nPage role hint: %s\n\n%s", pageRole(pageURL), priorityRubric)
```

Update the "Each object must have:" block in both prompts to list `priority` and `priority_reason` as required fields, with the rubric reference.

5. In `detectionPass`, after building each `ScreenshotGap` from `it`, copy over `it.Priority` and `it.PriorityReason`. Add `validateScreenshotGap` and call it for each gap and suppressed item; on failure log and skip the page (fail-open per existing semantics).

**Step 4 — GREEN.** Update any test fixtures that contain hard-coded LLM JSON to include the new fields.

**Step 5 — Commit.**

---

## Task 6: ImageIssue — Priority through relevance schema and prompt

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` (`ImageIssue` ~779, `relevancePassSchema` ~806, `buildRelevancePrompt` ~849, `relevancePass` ~910)
- Modify: `internal/analyzer/screenshot_gaps_test.go`, `screenshot_gaps_relevance_test.go`

**Step 1 — Failing tests:**

- `ImageIssue` carries `Priority`/`PriorityReason` after `json.Unmarshal`.
- `validateImageIssue` rejects missing/invalid priority.
- Relevance prompt contains the rubric and `page_role:` line.

**Step 2 — RED.**

**Step 3 — Implement:**

1. `ImageIssue`:

```go
type ImageIssue struct {
	PageURL         string   `json:"page_url"`
	Index           string   `json:"index"`
	Src             string   `json:"src"`
	Reason          string   `json:"reason"`
	SuggestedAction string   `json:"suggested_action"`
	Priority        Priority `json:"priority"`
	PriorityReason  string   `json:"priority_reason"`
}
```

2. `relevancePassSchema` — image_issues array items gain `priority` (enum) and `priority_reason`, both required.

3. `buildRelevancePrompt` — append `\nPage role hint: %s\n\n%s` and update the "ONLY when matches=false, ALSO emit a corresponding entry in image_issues" block to list `priority`/`priority_reason` as required.

4. In `relevancePass`, after assigning `PageURL` to each issue, also call `validateImageIssue`. On failure, log and skip that issue (fail-open consistency).

**Step 4 — GREEN.** Update fixtures.

**Step 5 — Commit.**

---

## Task 7: Possibly-covered — already covered by Task 5

**Files:**
- Sanity test only: `internal/analyzer/screenshot_gaps_test.go`

The `suppressed_by_image` items reuse `screenshotResponseItem` and `ScreenshotGap`, so they already gained Priority via Task 5. Add one regression test that round-trips a non-empty `suppressed_by_image` array carrying priorities, asserting they survive into `result.PossiblyCovered`.

**Step 1 — Add the regression test.**

**Step 2 — RED:** assertion fails if any wiring is missed.

**Step 3 — Fix any wiring gaps in `detectionPass`.**

**Step 4 — GREEN.**

**Step 5 — Commit.**

---

## Task 8: New `screenshots.json` artifact

**Files:**
- Modify: `internal/cli/analyze.go` (after the existing JSON writes around line 254-378)
- Create: `internal/reporter/screenshots_json.go`
- Create: `internal/reporter/screenshots_json_test.go`

**Step 1 — Failing test:**

```go
// internal/reporter/screenshots_json_test.go
package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestWriteScreenshotsJSON(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{{
			PageURL: "u", QuotedPassage: "q", Priority: analyzer.PriorityLarge, PriorityReason: "r",
		}},
		ImageIssues: []analyzer.ImageIssue{{
			PageURL: "u", Index: "img-1", Priority: analyzer.PriorityMedium, PriorityReason: "r",
		}},
		PossiblyCovered: []analyzer.ScreenshotGap{{
			PageURL: "u", Priority: analyzer.PrioritySmall, PriorityReason: "r",
		}},
	}
	if err := WriteScreenshotsJSON(dir, res); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "screenshots.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MissingGaps     []analyzer.ScreenshotGap `json:"missing_gaps"`
		ImageIssues     []analyzer.ImageIssue    `json:"image_issues"`
		PossiblyCovered []analyzer.ScreenshotGap `json:"possibly_covered"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.MissingGaps[0].Priority != analyzer.PriorityLarge {
		t.Errorf("missing priority lost")
	}
	if got.ImageIssues[0].Priority != analyzer.PriorityMedium {
		t.Errorf("image-issue priority lost")
	}
	if got.PossiblyCovered[0].Priority != analyzer.PrioritySmall {
		t.Errorf("possibly-covered priority lost")
	}
}
```

**Step 2 — RED.**

**Step 3 — Implement `screenshots_json.go`:**

```go
package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

type screenshotsJSON struct {
	MissingGaps     []analyzer.ScreenshotGap `json:"missing_gaps"`
	ImageIssues     []analyzer.ImageIssue    `json:"image_issues"`
	PossiblyCovered []analyzer.ScreenshotGap `json:"possibly_covered"`
}

// WriteScreenshotsJSON persists the screenshot-pass results as a single JSON
// artifact alongside screenshots.md. Stable original order is preserved
// within each list — consumers sort however they want; the Markdown and the
// rendered site impose priority-based ordering at render time.
func WriteScreenshotsJSON(dir string, res analyzer.ScreenshotResult) error {
	out := screenshotsJSON{
		MissingGaps:     res.MissingGaps,
		ImageIssues:     res.ImageIssues,
		PossiblyCovered: res.PossiblyCovered,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "screenshots.json"), b, 0o644)
}
```

**Step 4 — GREEN.**

**Step 5 — Wire into `internal/cli/analyze.go`:** call `reporter.WriteScreenshotsJSON(projectDir, res)` immediately after the existing `reporter.WriteScreenshots` call. Skip when the screenshot pass was skipped (mirror the markdown-skip rule — `WriteScreenshots` is gated on the same condition).

**Step 6 — Commit.**

---

## Task 9: gaps.md — group drift section by priority

**Files:**
- Modify: `internal/reporter/reporter.go` (`WriteGaps` ~138 onward)
- Modify: `internal/reporter/reporter_test.go`

**Step 1 — Failing test:**

Construct a `[]analyzer.DriftFinding` with three issues spanning all priorities. Call `WriteGaps`; assert the rendered file contains, in order: `### Large`, then the large issue text, then `### Medium`, then the medium text, then `### Small`, then the small text. Assert empty buckets are omitted (run with two-priority input).

**Step 2 — RED.**

**Step 3 — Implement:** rewrite the "Stale documentation" block in `WriteGaps`:

```go
sb.WriteString("\n## Stale Documentation\n\n")
flat := flattenDrift(drift) // []DriftIssueWithFeature
if len(flat) == 0 {
	sb.WriteString("_None found._\n")
} else {
	for _, p := range []Priority{PriorityLarge, PriorityMedium, PrioritySmall} {
		bucket := filterByPriority(flat, p)
		if len(bucket) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "### %s\n\n", priorityHeading(p))
		for _, item := range bucket {
			fmt.Fprintf(&sb, "- **%s** — %s — %s\n  _why: %s_\n\n",
				item.Feature, item.Issue.Page, item.Issue.Issue, item.Issue.PriorityReason)
		}
	}
}
```

`priorityHeading` returns "Large" / "Medium" / "Small". `flattenDrift` and `filterByPriority` live in a new helper file `internal/reporter/priority.go` — write tests there too. Stable original order preserved within each bucket (no sort).

**Step 4 — GREEN.**

**Step 5 — Commit.**

---

## Task 10: screenshots.md — group all sections by priority

**Files:**
- Modify: `internal/reporter/reporter.go` (`WriteScreenshots` ~168)
- Modify: `internal/reporter/reporter_test.go`

**Step 1 — Failing test:**

Build a `ScreenshotResult` with mixed priorities across `MissingGaps`, `ImageIssues`, and `PossiblyCovered`. Assert each rendered section now has `### Large` / `### Medium` / `### Small` sub-headers, with empty buckets omitted, ordering preserved within a bucket. Assert the existing per-page grouping is gone (or moved underneath the priority bucket — see implementation).

**Decision:** priority is the OUTER grouping; per-page grouping happens inside each priority bucket. So a "Missing Screenshots" section now looks like:

```
# Missing Screenshots

### Large

#### https://x/quickstart {#anchor}
- Passage: ...

### Medium

#### https://x/api/auth
- Passage: ...
```

**Step 2 — RED.**

**Step 3 — Implement:** factor a helper `groupByPriorityAndPage[T]` over a generic slice that exposes `Priority()` and `PageURL()` (or pass two getters as funcs to keep it Go-1.x friendly without generics). Apply it to each of the three sections. Reason line shows under the passage:

```
  _why (large): on quickstart_
```

**Step 4 — GREEN.**

**Step 5 — Commit.**

---

## Task 11: Hugo site — badge styling and grouping

**Files:**
- Modify: `internal/site/assets/templates/screenshot_page.md.tmpl` (and any `gaps_page` equivalent — confirm filename when implementing)
- Modify: any custom CSS file under `internal/site/assets/theme/` (search for the existing custom CSS layer)
- Modify or create: a golden-file test for site rendering under `internal/site/`

**Step 1 — Failing test:**

Add a test that exercises the existing site-render path with a fixture containing one finding per priority. Assert the rendered HTML contains:
- `<span class="ftg-priority ftg-priority-large">large</span>`
- `<span class="ftg-priority ftg-priority-medium">medium</span>`
- `<span class="ftg-priority ftg-priority-small">small</span>`
- The headings appear in `Large → Medium → Small` order.

If no site golden test currently exists, create one keyed off `internal/site/assets/templates/screenshot_page.md.tmpl`'s rendered output. Use whatever fixture style adjacent tests use.

**Step 2 — RED.**

**Step 3 — Implement:**

- Update the screenshot template to iterate by priority bucket then by page; emit a badge `<span>` next to each finding's passage.
- Update the gaps-page template to do the same for the drift section.
- Append CSS to the existing custom CSS file:

```css
.ftg-priority {
  display: inline-block;
  font-size: 0.75rem;
  padding: 0.1rem 0.45rem;
  border-radius: 0.25rem;
  margin-right: 0.4rem;
  text-transform: uppercase;
  font-weight: 600;
}
.ftg-priority-large  { background: #c0282d; color: #fff; }
.ftg-priority-medium { background: #d99100; color: #fff; }
.ftg-priority-small  { background: #5d6770; color: #fff; }
```

**Step 4 — GREEN.**

**Step 5 — Commit.**

---

## Task 12: Stdout reports block — per-priority counts

**Files:**
- Modify: `internal/cli/analyze.go` (~line 520, the `reports:` block)
- Modify: a corresponding testscript fixture under `cmd/find-the-gaps/testdata/` if one exists

**Step 1 — Failing test:**

Either add a small helper in `internal/cli/` that formats the line and unit-test it, or extend an existing testscript that captures stdout. Assert that for a fixture with 3L/5M/4S drift issues, the line contains `drift.json (12 issues: 3L · 5M · 4S)`.

**Step 2 — RED.**

**Step 3 — Implement:** add a small `formatPriorityCounts(items []analyzer.DriftIssue)` helper (and overload for `ScreenshotGap` and `ImageIssue`) that returns `"NL · NM · NS"`. Update the `Sprintf` near line 520 to compose the per-file count strings.

**Step 4 — GREEN.**

**Step 5 — Commit.**

---

## Task 13: Verification plan updates

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`

**Step 1 — Edit Scenario 3 (drift detection) success criteria:**
- Add: "Each drift finding in `gaps.md` and `drift.json` carries a `priority` (large/medium/small) and a non-empty `priority_reason`."
- Add: "Findings in `gaps.md`'s Stale Documentation section appear in `Large → Medium → Small` order; empty buckets are omitted."

**Step 2 — Edit Scenario 5 (screenshots) success criteria:**
- Add: "Each missing-screenshot, image-issue, and possibly-covered entry in `screenshots.md` and `screenshots.json` carries a `priority` and `priority_reason`."
- Add: "Sections in `screenshots.md` are sub-grouped by priority in `Large → Medium → Small` order."

**Step 3 — Add new Scenario 14:**

```
### Scenario 14: Priority Calibration Smoke Test

**Context:** The same fixture as Scenario 9.

**Steps:**
1. Run `find-the-gaps analyze --repo <path> --docs-url <url>`.
2. Inspect the priority distribution across `drift.json` and `screenshots.json`.

**Success Criteria:**
- [ ] No priority bucket holds >80% of findings (sanity check that the rubric isn't degenerating to a single bucket).
- [ ] At least one finding has `priority: "large"` (the rubric is not collapsing everything to medium/small).
```

**Step 4 — Commit.**

---

## Final verification

After all tasks:

```bash
go test ./... -count=1
go vet ./...
golangci-lint run
go build ./...
```

All four must succeed. Coverage stays ≥90% per `go test -cover ./...`.

Update `PROGRESS.md` summarizing the work per CLAUDE.md rule #8.

Open a PR using a merge commit; reference this plan in the description.
