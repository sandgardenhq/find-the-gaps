# Missing Screenshots Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a detector that scans every fetched docs page, finds passages describing user-facing moments that lack a nearby screenshot, and emits actionable findings (passage, what to show, alt text, insertion hint) grouped under a new "Missing Screenshots" section of the report.

**Architecture:** New sibling module `internal/analyzer/screenshot_gaps.go` runs one LLM call per page. Markdown image positions are pre-computed in Go (deterministic), then passed into the prompt so the model can apply the locality rule (same section or within 3 paragraphs). Findings are merged into the existing `gaps.md` report. A new `--skip-screenshot-check` flag (default off) disables the pass.

**Tech Stack:** Go 1.26+, `testing` stdlib + `testify`, `cobra` for the CLI flag, existing `LLMClient` abstraction.

**Reference:** Design at `.plans/2026-04-23-missing-screenshots-design.md`.

---

## Conventions

- **TDD**: every task is RED → verify RED → GREEN → verify GREEN → REFACTOR → commit. No production code before a failing test.
- **Commit message format**: `type(scope): brief description` with a body describing RED/GREEN per CLAUDE.md.
- **Coverage**: ≥90% statement coverage on new files. Verify with `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`.
- **Format**: `gofmt -w . && goimports -w .` before every commit.
- **Lint**: `golangci-lint run` must pass.
- **Progress**: update `PROGRESS.md` after each task completes.

All file paths in this plan are relative to the repo root: `/Users/brittcrawford/conductor/workspaces/find-the-gaps/conakry/`.

---

## Task 1: Add `ScreenshotGap` type

**Files:**
- Modify: `internal/analyzer/types.go` (append new types at end of file)
- Modify: `internal/analyzer/types_test.go`

**Step 1: Write the failing test**

In `internal/analyzer/types_test.go`, add:

```go
func TestScreenshotGap_ZeroValue(t *testing.T) {
	var g ScreenshotGap
	assert.Equal(t, "", g.PageURL)
	assert.Equal(t, "", g.PagePath)
	assert.Equal(t, "", g.QuotedPassage)
	assert.Equal(t, "", g.ShouldShow)
	assert.Equal(t, "", g.SuggestedAlt)
	assert.Equal(t, "", g.InsertionHint)
}

func TestScreenshotGap_JSONRoundTrip(t *testing.T) {
	in := ScreenshotGap{
		PageURL:       "https://example.com/quickstart",
		PagePath:      "/cache/quickstart.md",
		QuotedPassage: "Click Save to continue.",
		ShouldShow:    "The Save button highlighted in the settings panel.",
		SuggestedAlt:  "Settings panel with Save button highlighted.",
		InsertionHint: "after the paragraph ending '...to continue.'",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out ScreenshotGap
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}
```

Add imports at the top of the test file if missing: `"encoding/json"`, `"github.com/stretchr/testify/assert"`, `"github.com/stretchr/testify/require"`.

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/ -run TestScreenshotGap -v`
Expected: compile error `undefined: ScreenshotGap`.

**Step 3: Minimal implementation**

Append to `internal/analyzer/types.go`:

```go
// ScreenshotGap is one place in a docs page where a screenshot should exist but does not.
type ScreenshotGap struct {
	PageURL       string `json:"page_url"`
	PagePath      string `json:"page_path"`
	QuotedPassage string `json:"quoted_passage"`
	ShouldShow    string `json:"should_show"`
	SuggestedAlt  string `json:"suggested_alt"`
	InsertionHint string `json:"insertion_hint"`
}
```

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/ -run TestScreenshotGap -v`
Expected: PASS.

**Step 5: Commit**

```bash
gofmt -w internal/analyzer/types.go internal/analyzer/types_test.go
git add internal/analyzer/types.go internal/analyzer/types_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add ScreenshotGap type

- RED: TestScreenshotGap_ZeroValue, TestScreenshotGap_JSONRoundTrip
- GREEN: add ScreenshotGap struct with JSON tags in types.go
- Status: tests pass, build clean

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Image-position parser — simple `![alt](src)` case

**Files:**
- Create: `internal/analyzer/screenshot_gaps.go`
- Create: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Write the failing test**

In `internal/analyzer/screenshot_gaps_test.go`:

```go
package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractImages_MarkdownSyntax(t *testing.T) {
	md := "# Title\n\nIntro paragraph.\n\n![Dashboard](dashboard.png)\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 2},
	}, got)
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/ -run TestExtractImages -v`
Expected: compile error — `extractImages`, `imageRef` undefined.

**Step 3: Minimal implementation**

In `internal/analyzer/screenshot_gaps.go`:

```go
package analyzer

import (
	"regexp"
	"strings"
)

// imageRef is one image occurrence on a docs page.
type imageRef struct {
	AltText        string
	Src            string
	SectionHeading string // most recent "# ..." or "## ..." heading above this image; "" if none
	ParagraphIndex int    // 0-based index of the paragraph block containing this image
}

var markdownImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// extractImages returns all image references in the markdown, annotated with their
// containing section heading and paragraph index. Paragraphs are separated by blank lines.
func extractImages(md string) []imageRef {
	var refs []imageRef
	paragraphs := strings.Split(md, "\n\n")
	currentHeading := ""
	for pIdx, block := range paragraphs {
		// Track the most recent heading.
		for _, line := range strings.Split(block, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				h := strings.TrimLeft(trimmed, "#")
				currentHeading = strings.TrimSpace(h)
			}
		}
		// Find markdown images in this block.
		for _, m := range markdownImageRe.FindAllStringSubmatch(block, -1) {
			refs = append(refs, imageRef{
				AltText:        m[1],
				Src:            m[2],
				SectionHeading: currentHeading,
				ParagraphIndex: pIdx,
			})
		}
	}
	return refs
}
```

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/ -run TestExtractImages -v`
Expected: PASS.

**Step 5: Commit**

```bash
gofmt -w internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add markdown image-position parser

- RED: TestExtractImages_MarkdownSyntax
- GREEN: extractImages scans paragraphs, tracks heading context
- Status: tests pass

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Image-position parser — `<img>` tag case

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Modify: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Write the failing test**

Append to `screenshot_gaps_test.go`:

```go
func TestExtractImages_HTMLSyntax(t *testing.T) {
	md := "# Title\n\n<img src=\"dashboard.png\" alt=\"Dashboard\">\n\nNext paragraph.\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 1},
	}, got)
}

func TestExtractImages_HTMLSyntax_AttrsReversed(t *testing.T) {
	md := "# Title\n\n<img alt=\"Dashboard\" src=\"dashboard.png\">\n"
	got := extractImages(md)
	assert.Equal(t, []imageRef{
		{AltText: "Dashboard", Src: "dashboard.png", SectionHeading: "Title", ParagraphIndex: 1},
	}, got)
}

func TestExtractImages_MixedSyntaxes(t *testing.T) {
	md := "# Title\n\n![One](a.png)\n\n<img src=\"b.png\" alt=\"Two\">\n"
	got := extractImages(md)
	assert.Len(t, got, 2)
	assert.Equal(t, "a.png", got[0].Src)
	assert.Equal(t, "b.png", got[1].Src)
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/ -run TestExtractImages -v`
Expected: FAIL — HTML images are not detected.

**Step 3: Minimal implementation**

Add to `screenshot_gaps.go`:

```go
var htmlImgRe = regexp.MustCompile(`(?i)<img\s+([^>]+?)>`)
var htmlAttrSrcRe = regexp.MustCompile(`(?i)\bsrc\s*=\s*"([^"]*)"`)
var htmlAttrAltRe = regexp.MustCompile(`(?i)\balt\s*=\s*"([^"]*)"`)
```

Modify `extractImages` to also scan each block for `<img>` tags:

```go
for _, m := range htmlImgRe.FindAllStringSubmatch(block, -1) {
	attrs := m[1]
	src := ""
	alt := ""
	if mm := htmlAttrSrcRe.FindStringSubmatch(attrs); mm != nil {
		src = mm[1]
	}
	if mm := htmlAttrAltRe.FindStringSubmatch(attrs); mm != nil {
		alt = mm[1]
	}
	refs = append(refs, imageRef{
		AltText:        alt,
		Src:            src,
		SectionHeading: currentHeading,
		ParagraphIndex: pIdx,
	})
}
```

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/ -run TestExtractImages -v`
Expected: PASS for all three new tests and the original.

**Step 5: Commit**

```bash
gofmt -w internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): parse <img> tags in image-position parser

- RED: TestExtractImages_HTMLSyntax, attrs-reversed, mixed-syntaxes
- GREEN: add htmlImgRe plus src/alt attr regexes, scan each block
- Status: 4 parser tests pass

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Coverage map builder

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Modify: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Write the failing test**

Append to `screenshot_gaps_test.go`:

```go
func TestBuildCoverageMap_GroupsBySection(t *testing.T) {
	refs := []imageRef{
		{Src: "a.png", SectionHeading: "Intro", ParagraphIndex: 1},
		{Src: "b.png", SectionHeading: "Intro", ParagraphIndex: 3},
		{Src: "c.png", SectionHeading: "Setup", ParagraphIndex: 7},
	}
	m := buildCoverageMap(refs)
	assert.Equal(t, []imageRef{refs[0], refs[1]}, m["Intro"])
	assert.Equal(t, []imageRef{refs[2]}, m["Setup"])
}

func TestBuildCoverageMap_EmptyInput(t *testing.T) {
	assert.Empty(t, buildCoverageMap(nil))
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/ -run TestBuildCoverageMap -v`
Expected: compile error — `buildCoverageMap` undefined.

**Step 3: Minimal implementation**

Add to `screenshot_gaps.go`:

```go
// buildCoverageMap groups image references by their containing section heading.
// Passed into the prompt so the LLM can apply the locality rule.
func buildCoverageMap(refs []imageRef) map[string][]imageRef {
	if len(refs) == 0 {
		return map[string][]imageRef{}
	}
	out := make(map[string][]imageRef)
	for _, r := range refs {
		out[r.SectionHeading] = append(out[r.SectionHeading], r)
	}
	return out
}
```

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/ -run TestBuildCoverageMap -v`
Expected: PASS.

**Step 5: Commit**

```bash
gofmt -w internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add coverage map builder for image refs

- RED: TestBuildCoverageMap_GroupsBySection, EmptyInput
- GREEN: buildCoverageMap groups refs by section heading
- Status: tests pass

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Prompt builder

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Modify: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Write the failing test**

Append to `screenshot_gaps_test.go`:

```go
func TestBuildScreenshotPrompt_IncludesPageContentAndCoverageMap(t *testing.T) {
	pageURL := "https://example.com/quickstart"
	content := "# Quickstart\n\nRun the command and see the output.\n"
	coverage := map[string][]imageRef{
		"Quickstart": {{Src: "hero.png", AltText: "Hero", SectionHeading: "Quickstart", ParagraphIndex: 0}},
	}
	got := buildScreenshotPrompt(pageURL, content, coverage)
	assert.Contains(t, got, pageURL)
	assert.Contains(t, got, content)
	assert.Contains(t, got, "hero.png")
	assert.Contains(t, got, "quoted_passage")
	assert.Contains(t, got, "should_show")
	assert.Contains(t, got, "suggested_alt")
	assert.Contains(t, got, "insertion_hint")
	assert.Contains(t, got, "same section")
	assert.Contains(t, got, "3 paragraphs")
}

func TestBuildScreenshotPrompt_EmptyCoverage(t *testing.T) {
	got := buildScreenshotPrompt("https://example.com/x", "# X\n\nHello.\n", nil)
	assert.Contains(t, got, "No existing images")
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/ -run TestBuildScreenshotPrompt -v`
Expected: compile error — `buildScreenshotPrompt` undefined.

**Step 3: Minimal implementation**

Add to `screenshot_gaps.go`:

```go
import (
	// ... existing imports ...
	"fmt"
	"sort"
)

// buildScreenshotPrompt assembles the LLM prompt for one docs page.
func buildScreenshotPrompt(pageURL, content string, coverage map[string][]imageRef) string {
	var coverageSummary string
	if len(coverage) == 0 {
		coverageSummary = "No existing images on this page."
	} else {
		sections := make([]string, 0, len(coverage))
		for s := range coverage {
			sections = append(sections, s)
		}
		sort.Strings(sections)
		var lines []string
		for _, s := range sections {
			heading := s
			if heading == "" {
				heading = "(no heading)"
			}
			for _, r := range coverage[s] {
				lines = append(lines, fmt.Sprintf("- section %q, paragraph %d: src=%q alt=%q",
					heading, r.ParagraphIndex, r.Src, r.AltText))
			}
		}
		coverageSummary = strings.Join(lines, "\n")
	}

	// PROMPT: Identifies passages in a documentation page that describe user-facing UI moments (web, app, terminal) and should have a screenshot nearby but do not. Applies a locality rule: a passage is already covered if an image appears in the same section heading or within 3 paragraphs before/after. Returns a JSON array; empty if nothing needs a screenshot.
	return fmt.Sprintf(`You are reviewing a documentation page to identify places where a screenshot would materially help the reader, but none is present nearby.

URL: %s

Existing images on this page (if any):
%s

Page content:
%s

A passage is ALREADY COVERED (do not flag it) if an existing image on this page appears in the same section heading as the passage, OR within 3 paragraphs before/after the passage.

Only flag passages that describe a concrete user-facing moment the reader would benefit from seeing: a web UI, an app screen, a terminal session with visible output, a dialog, a dashboard, a button or form the user interacts with.

Do NOT flag:
- Pure reference material (API signatures, type tables, option lists).
- Abstract prose with no concrete UI moment.
- Passages already covered by a nearby image per the locality rule above.

For each remaining gap, return an object with these fields:
- "quoted_passage": the exact verbatim quote from the page that describes the UI moment. Do not paraphrase.
- "should_show": a concrete description of what the screenshot should depict. Be specific: name the visible elements, values, buttons, states. Not "a screenshot of the feature".
- "suggested_alt": alt text / caption for the screenshot, under 100 characters.
- "insertion_hint": where to paste the image, referencing existing prose. Example: "after the paragraph ending '…click Save.'" Do not use line numbers.

Return a JSON array of these objects. Return [] if nothing needs a screenshot.
Respond with only the JSON array. No markdown code fences. No prose.`, pageURL, coverageSummary, content)
}
```

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/ -run TestBuildScreenshotPrompt -v`
Expected: PASS.

**Step 5: Commit**

```bash
gofmt -w internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add screenshot-gaps prompt builder

- RED: TestBuildScreenshotPrompt covers page content, coverage map, empty coverage
- GREEN: buildScreenshotPrompt renders URL + coverage summary + page + structured instructions
- PROMPT marker per project convention

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Response parser

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Modify: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Write the failing test**

Append to `screenshot_gaps_test.go`:

```go
func TestParseScreenshotResponse_Valid(t *testing.T) {
	raw := `[{"quoted_passage":"Click Save.","should_show":"Save button highlighted.","suggested_alt":"Save button","insertion_hint":"after the sentence 'Click Save.'"}]`
	got, err := parseScreenshotResponse(raw)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Click Save.", got[0].QuotedPassage)
	assert.Equal(t, "Save button highlighted.", got[0].ShouldShow)
	assert.Equal(t, "Save button", got[0].SuggestedAlt)
	assert.Equal(t, "after the sentence 'Click Save.'", got[0].InsertionHint)
}

func TestParseScreenshotResponse_EmptyArray(t *testing.T) {
	got, err := parseScreenshotResponse("[]")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseScreenshotResponse_WithPreamble(t *testing.T) {
	raw := "Here is the JSON: []"
	got, err := parseScreenshotResponse(raw)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseScreenshotResponse_Malformed(t *testing.T) {
	_, err := parseScreenshotResponse("not json")
	require.Error(t, err)
}
```

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/ -run TestParseScreenshotResponse -v`
Expected: compile error — `parseScreenshotResponse` undefined.

**Step 3: Minimal implementation**

Add to `screenshot_gaps.go`:

```go
import (
	// ... existing ...
	"encoding/json"
)

type screenshotResponseItem struct {
	QuotedPassage string `json:"quoted_passage"`
	ShouldShow    string `json:"should_show"`
	SuggestedAlt  string `json:"suggested_alt"`
	InsertionHint string `json:"insertion_hint"`
}

// parseScreenshotResponse extracts a JSON array from raw LLM output and returns
// parsed items. Reuses extractJSONArray (from drift.go) for preamble tolerance.
func parseScreenshotResponse(raw string) ([]screenshotResponseItem, error) {
	arr := extractJSONArray(raw)
	var items []screenshotResponseItem
	if err := json.Unmarshal([]byte(arr), &items); err != nil {
		return nil, fmt.Errorf("invalid screenshot-gap JSON: %w (raw: %q)", err, raw)
	}
	return items, nil
}
```

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/ -run TestParseScreenshotResponse -v`
Expected: PASS.

**Step 5: Commit**

```bash
gofmt -w internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add screenshot-gaps response parser

- RED: TestParseScreenshotResponse covers valid, empty, preamble, malformed
- GREEN: parseScreenshotResponse reuses extractJSONArray from drift.go
- Status: 4 parser tests pass

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Orchestrator — DocPage, ScreenshotProgressFunc, DetectScreenshotGaps

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Modify: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Write the failing tests**

Append to `screenshot_gaps_test.go`:

```go
// fakeLLMClient collects calls and returns canned responses per call index.
type fakeLLMClient struct {
	responses []string
	errs      []error
	prompts   []string
}

func (f *fakeLLMClient) Complete(_ context.Context, prompt string) (string, error) {
	i := len(f.prompts)
	f.prompts = append(f.prompts, prompt)
	if i < len(f.errs) && f.errs[i] != nil {
		return "", f.errs[i]
	}
	if i < len(f.responses) {
		return f.responses[i], nil
	}
	return "[]", nil
}

func TestDetectScreenshotGaps_NoPages(t *testing.T) {
	client := &fakeLLMClient{}
	gaps, err := DetectScreenshotGaps(context.Background(), client, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, gaps)
	assert.Empty(t, client.prompts)
}

func TestDetectScreenshotGaps_SinglePage_Findings(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{
			`[{"quoted_passage":"Run the command.","should_show":"Terminal showing output.","suggested_alt":"Terminal","insertion_hint":"after the command block"}]`,
		},
	}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n\nRun the command.\n"},
	}
	gaps, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)
	require.Len(t, gaps, 1)
	assert.Equal(t, "https://example.com/a", gaps[0].PageURL)
	assert.Equal(t, "/tmp/a.md", gaps[0].PagePath)
	assert.Equal(t, "Run the command.", gaps[0].QuotedPassage)
}

func TestDetectScreenshotGaps_ParseErrorIsolatesPage(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{"not json", "[]"},
	}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n"},
		{URL: "https://example.com/b", Path: "/tmp/b.md", Content: "# B\n"},
	}
	gaps, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err) // parse errors log-and-continue
	assert.Empty(t, gaps)
	assert.Len(t, client.prompts, 2) // both pages were attempted
}

func TestDetectScreenshotGaps_Progress(t *testing.T) {
	client := &fakeLLMClient{}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n"},
		{URL: "https://example.com/b", Path: "/tmp/b.md", Content: "# B\n"},
	}
	var calls []struct {
		done, total int
		page        string
	}
	progress := func(done, total int, page string) {
		calls = append(calls, struct {
			done, total int
			page        string
		}{done, total, page})
	}
	_, err := DetectScreenshotGaps(context.Background(), client, pages, progress)
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, 1, calls[0].done)
	assert.Equal(t, 2, calls[0].total)
	assert.Equal(t, "https://example.com/a", calls[0].page)
	assert.Equal(t, 2, calls[1].done)
}

func TestDetectScreenshotGaps_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeLLMClient{errs: []error{context.Canceled}}
	pages := []DocPage{{URL: "https://x", Path: "/x", Content: "# x\n"}}
	_, err := DetectScreenshotGaps(ctx, client, pages, nil)
	require.Error(t, err)
}
```

Add `"context"` to the test file imports.

**Step 2: Verify RED**

Run: `go test ./internal/analyzer/ -run TestDetectScreenshotGaps -v`
Expected: compile error — `DocPage`, `DetectScreenshotGaps` undefined.

**Step 3: Minimal implementation**

Add to `screenshot_gaps.go`:

```go
import (
	// ... existing ...
	"context"
	"github.com/charmbracelet/log"
)

// DocPage is one fetched documentation page.
type DocPage struct {
	URL     string
	Path    string
	Content string
}

// ScreenshotProgressFunc is called after each page completes. done/total express
// progress counts. currentPage is the URL of the page just processed.
type ScreenshotProgressFunc func(done, total int, currentPage string)

// DetectScreenshotGaps iterates pages sequentially, issues one LLM call per page,
// and returns all findings. Per-page parse failures are logged and skipped
// (fail-open); context / network errors are returned immediately.
func DetectScreenshotGaps(
	ctx context.Context,
	client LLMClient,
	pages []DocPage,
	progress ScreenshotProgressFunc,
) ([]ScreenshotGap, error) {
	var gaps []ScreenshotGap
	total := len(pages)
	for i, page := range pages {
		refs := extractImages(page.Content)
		coverage := buildCoverageMap(refs)
		prompt := buildScreenshotPrompt(page.URL, page.Content, coverage)
		raw, err := client.Complete(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("DetectScreenshotGaps %s: %w", page.URL, err)
		}
		items, err := parseScreenshotResponse(raw)
		if err != nil {
			log.Warnf("screenshot-gaps: skipping %s: %v", page.URL, err)
			if progress != nil {
				progress(i+1, total, page.URL)
			}
			continue
		}
		for _, it := range items {
			gaps = append(gaps, ScreenshotGap{
				PageURL:       page.URL,
				PagePath:      page.Path,
				QuotedPassage: it.QuotedPassage,
				ShouldShow:    it.ShouldShow,
				SuggestedAlt:  it.SuggestedAlt,
				InsertionHint: it.InsertionHint,
			})
		}
		if progress != nil {
			progress(i+1, total, page.URL)
		}
	}
	return gaps, nil
}
```

**Step 4: Verify GREEN**

Run: `go test ./internal/analyzer/ -run TestDetectScreenshotGaps -v`
Expected: all five tests PASS.

Also run: `go test ./internal/analyzer/ -v` to confirm no regressions in existing tests.

**Step 5: Verify coverage**

Run: `go test -coverprofile=coverage.out ./internal/analyzer/ && go tool cover -func=coverage.out | grep screenshot_gaps`
Expected: ≥90% per function.

**Step 6: Commit**

```bash
gofmt -w internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add DetectScreenshotGaps orchestrator

- RED: 5 tests — no pages, single page findings, parse-error isolation, progress callback, context cancel
- GREEN: DocPage + ScreenshotProgressFunc + DetectScreenshotGaps, sequential LLM call per page, parse errors log-and-continue
- Coverage: ≥90% on screenshot_gaps.go

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Reporter — "Missing Screenshots" section

**Files:**
- Modify: `internal/reporter/reporter.go`
- Modify: `internal/reporter/reporter_test.go`

**Step 1: Write the failing tests**

Append to `internal/reporter/reporter_test.go`:

```go
func TestWriteGaps_MissingScreenshotsSection(t *testing.T) {
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
	require.NoError(t, WriteGaps(dir, nil, nil, nil, gaps))
	body, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, "## Missing Screenshots")
	// Grouped per page, in order of first occurrence.
	assert.Regexp(t, `### https://example.com/quickstart[\s\S]*### https://example.com/setup`, s)
	// Each gap shows its four fields.
	assert.Contains(t, s, "Run the command and see the output.")
	assert.Contains(t, s, "Terminal showing the analyze summary")
	assert.Contains(t, s, "Terminal output of find-the-gaps analyze")
	assert.Contains(t, s, "after the paragraph ending '...see the output.'")
}

func TestWriteGaps_MissingScreenshotsEmpty_OmitsSection(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteGaps(dir, nil, nil, nil, nil))
	body, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(body), "## Missing Screenshots")
}
```

Add imports if missing: `"github.com/stretchr/testify/assert"`, `"github.com/stretchr/testify/require"`, `"os"`, `"path/filepath"`.

**Step 2: Verify RED**

Run: `go test ./internal/reporter/ -v`
Expected: compile error — `WriteGaps` signature mismatch (now takes 5 args).

**Step 3: Minimal implementation**

Change `WriteGaps` signature in `internal/reporter/reporter.go`:

```go
func WriteGaps(
	dir string,
	mapping analyzer.FeatureMap,
	allDocFeatures []string,
	drift []analyzer.DriftFinding,
	screenshotGaps []analyzer.ScreenshotGap,
) error {
```

Before the final `return os.WriteFile(...)`, add:

```go
// Missing screenshots — omitted when there are none.
if len(screenshotGaps) > 0 {
	sb.WriteString("\n## Missing Screenshots\n\n")
	// Preserve first-occurrence order of pages.
	seen := map[string]bool{}
	var order []string
	byPage := map[string][]analyzer.ScreenshotGap{}
	for _, g := range screenshotGaps {
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
}
```

**Step 4: Update existing callers to pass the new arg**

Find every call site of `WriteGaps` across the repo:

```bash
grep -rn "reporter.WriteGaps(" --include="*.go"
```

Expected hits (from current codebase): `internal/cli/analyze.go` (two calls). Add `nil` as the fifth argument to both for now — the real value gets wired in Task 11.

**Step 5: Verify GREEN**

Run: `go test ./internal/reporter/ -v`
Expected: both new tests PASS.

Run: `go build ./...`
Expected: clean build (all callers updated).

Run: `go test ./...`
Expected: all prior tests still pass.

**Step 6: Commit**

```bash
gofmt -w internal/reporter/reporter.go internal/reporter/reporter_test.go internal/cli/analyze.go
goimports -w internal/reporter/ internal/cli/
git add internal/reporter/reporter.go internal/reporter/reporter_test.go internal/cli/analyze.go
git commit -m "$(cat <<'EOF'
feat(reporter): add Missing Screenshots section to gaps.md

- RED: TestWriteGaps_MissingScreenshotsSection, TestWriteGaps_MissingScreenshotsEmpty_OmitsSection
- GREEN: WriteGaps gains screenshotGaps param, renders grouped-by-page section when non-empty; callers updated to pass nil
- Status: all tests green, build clean

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: CLI — `--skip-screenshot-check` flag

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `internal/cli/analyze_test.go`

**Step 1: Write the failing test**

Append to `internal/cli/analyze_test.go`:

```go
func TestAnalyzeCmd_HasSkipScreenshotCheckFlag(t *testing.T) {
	cmd := newAnalyzeCmd()
	f := cmd.Flags().Lookup("skip-screenshot-check")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
	assert.Contains(t, f.Usage, "screenshot")
}
```

**Step 2: Verify RED**

Run: `go test ./internal/cli/ -run TestAnalyzeCmd_HasSkipScreenshotCheckFlag -v`
Expected: FAIL — flag not registered.

**Step 3: Minimal implementation**

In `internal/cli/analyze.go`:

1. Add variable in `newAnalyzeCmd`: `var skipScreenshotCheck bool`
2. Register the flag near the other flags:
   ```go
   cmd.Flags().BoolVar(&skipScreenshotCheck, "skip-screenshot-check", false,
       "skip the missing-screenshot detection pass")
   ```
3. Do **not** wire it into the pipeline yet — that happens in Task 10. The flag just needs to exist.

**Step 4: Verify GREEN**

Run: `go test ./internal/cli/ -run TestAnalyzeCmd_HasSkipScreenshotCheckFlag -v`
Expected: PASS.

**Step 5: Commit**

```bash
gofmt -w internal/cli/analyze.go internal/cli/analyze_test.go
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "$(cat <<'EOF'
feat(cli): add --skip-screenshot-check flag to analyze

- RED: TestAnalyzeCmd_HasSkipScreenshotCheckFlag
- GREEN: register BoolVar with default false; not yet wired into pipeline
- Status: flag registered, existing behavior unchanged

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: CLI — wire `DetectScreenshotGaps` into `analyze`

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `internal/cli/analyze_test.go`

**Step 1: Write the failing test**

Append to `internal/cli/analyze_test.go`. This test exercises the full analyze flow with stubs for LLM and spider inputs, asserting that screenshot gaps are fed into `WriteGaps`. Because `analyze.go` wires real network paths, the cleanest RED test here is an end-to-end testscript, not a unit test.

Instead of a unit test, create a testscript fixture. Create: `cmd/find-the-gaps/testdata/skip_screenshot_check.txtar`:

```
# Verifies --skip-screenshot-check is accepted and omits the Missing Screenshots
# section from the report. No LLM is involved because docs-url is blank.
exec find-the-gaps analyze --repo . --skip-screenshot-check
! stderr .
```

And a companion test `cmd/find-the-gaps/testdata/default_screenshot_check.txtar`:

```
# Verifies analyze accepts the default (screenshot check enabled) without error.
exec find-the-gaps analyze --repo .
! stderr .
```

Hook these into the existing testscript runner in `cmd/find-the-gaps/main_test.go` (it should already pick up all `.txtar` files under `testdata/`).

**Step 2: Verify RED**

Run: `go test ./cmd/find-the-gaps/ -v`
Expected: the new `skip_screenshot_check` test fails if the flag produces unexpected stderr, OR both pass trivially if the analyze command bails early on empty `--docs-url`. If both already pass (because analyze short-circuits), proceed to Step 3 — the pipeline-integration behavior needs a different verification.

In that case, add a unit-level test that asserts the handler constructs the right gap slice. Add a new test-only export in `internal/analyzer/export_test.go`:

```go
// DetectScreenshotGapsForTest is re-exported for CLI integration tests.
var DetectScreenshotGapsForTest = DetectScreenshotGaps
```

Then write a CLI test that, via a new seam, captures the screenshotGaps argument passed to `reporter.WriteGaps`. If refactoring `analyze.go` to accept an injectable detector is too invasive, skip the unit test here and rely entirely on the testscript in Task 12.

**Step 3: Minimal implementation**

In `internal/cli/analyze.go`, after `driftFindings` is computed and **before** the final `reporter.WriteGaps(...)` call (line ~336), add:

```go
var screenshotGaps []analyzer.ScreenshotGap
if !skipScreenshotCheck {
	log.Infof("detecting missing screenshots...")
	var docPages []analyzer.DocPage
	for url, filePath := range pages {
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			log.Warnf("skip page %s: %v", url, readErr)
			continue
		}
		docPages = append(docPages, analyzer.DocPage{
			URL:     url,
			Path:    filePath,
			Content: string(data),
		})
	}
	progress := func(done, total int, page string) {
		log.Infof("  [%d/%d] %s", done, total, page)
	}
	screenshotGaps, err = analyzer.DetectScreenshotGaps(ctx, llmClient, docPages, progress)
	if err != nil {
		return fmt.Errorf("detect screenshots: %w", err)
	}
	log.Debugf("screenshot-gap detection complete: %d gaps", len(screenshotGaps))
}
```

Update both `WriteGaps` call sites to pass `screenshotGaps` (replacing the `nil` placeholder added in Task 8). That includes the incremental `driftOnFinding` callback — pass `nil` there for screenshot gaps (incremental drift persistence predates this feature).

**Step 4: Verify GREEN**

Run:
- `go build ./...`
- `go test ./...`

Expected: all green.

**Step 5: Commit**

```bash
gofmt -w internal/cli/analyze.go internal/cli/analyze_test.go
git add internal/cli/analyze.go internal/cli/analyze_test.go cmd/find-the-gaps/testdata/*.txtar
git commit -m "$(cat <<'EOF'
feat(cli): wire DetectScreenshotGaps into analyze pipeline

- RED: testscript fixtures for default and --skip-screenshot-check flows
- GREEN: analyze reads every fetched page, calls DetectScreenshotGaps when flag is unset, passes results to WriteGaps
- Progress logged per-page; errors surface via %w wrap

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Update `VERIFICATION_PLAN.md` with new scenario

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`

**Step 1: Append the scenario**

Add a new scenario section after Scenario 4:

```markdown
### Scenario: Detect Missing Screenshots

**Context**: Known-good fixture + docs site, but a page describes a UI moment with no nearby image.

**Steps**:
1. Run `find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url https://<docs>`.
2. Inspect `gaps.md`.
3. Re-run with `--skip-screenshot-check`.
4. Re-inspect `gaps.md`.

**Success Criteria**:
- [ ] First run's `gaps.md` contains a `## Missing Screenshots` section.
- [ ] At least one gap is listed for the known UI passage, with all four fields populated: passage, should-show, alt text, insertion hint.
- [ ] Second run's `gaps.md` contains NO `## Missing Screenshots` section.
- [ ] Exit code behavior matches the findings count (consistent with existing drift exit semantics).

**If Blocked**: If the section renders on the skip run, the flag is not wired correctly. Stop and ask.
```

Renumber subsequent scenarios if the doc uses numeric ordering.

**Step 2: Commit**

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "$(cat <<'EOF'
docs(plans): add missing-screenshots verification scenario

- Real-system verification: default run emits section, --skip-screenshot-check omits it
- Matches design doc (.plans/2026-04-23-missing-screenshots-design.md)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Full build & coverage check

**Step 1:** Run the full test suite.

```bash
go test -count=1 ./...
```

Expected: all green.

**Step 2:** Coverage report.

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | grep -E "(screenshot_gaps|reporter.go)"
```

Expected: ≥90% statement coverage on `screenshot_gaps.go` and on the new reporter additions.

**Step 3:** Lint.

```bash
golangci-lint run
```

Expected: clean.

**Step 4:** Format.

```bash
gofmt -l . | tee /dev/stderr | test ! -s
goimports -l . | tee /dev/stderr | test ! -s
```

Expected: no files listed.

**Step 5:** Update `PROGRESS.md` with final summary per CLAUDE.md format:

```markdown
## Task: Missing Screenshots Detection - COMPLETE
- Started: 2026-04-23
- Tests: <N> passing, 0 failing
- Coverage: Statements: <X>%
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: <timestamp>
- Notes: implements .plans/2026-04-23-missing-screenshots-design.md
```

**Step 6:** Commit progress.

```bash
git add PROGRESS.md
git commit -m "$(cat <<'EOF'
chore: record completion of missing-screenshots feature

- All tests green, coverage above threshold, lint clean.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

**Step 7:** Open PR.

```bash
git push -u origin britt/missing-screenshots
gh pr create --base main --title "feat: detect missing screenshots in docs" --body "$(cat <<'EOF'
## Summary
- New detector identifies passages in docs that describe user-facing moments lacking nearby screenshots.
- Each finding includes passage, what the screenshot should show, alt text, and insertion hint.
- `--skip-screenshot-check` flag disables the pass (default off).
- New "Missing Screenshots" section in `gaps.md`.

## Design
See `.plans/2026-04-23-missing-screenshots-design.md` (merged in this branch).

## Test plan
- [ ] `go test ./...` passes locally
- [ ] `golangci-lint run` clean
- [ ] Coverage ≥90% on new files
- [ ] Manual: `find-the-gaps analyze` on fixture with known UI passage produces a Missing Screenshots section
- [ ] Manual: same run with `--skip-screenshot-check` omits the section
EOF
)"
```

---

## Post-Implementation Checklist

Per CLAUDE.md, a task is complete only when all of these are true:

- [ ] All tests pass (`go test -count=1 ./...`).
- [ ] Build succeeds (`go build ./...`).
- [ ] No linter errors (`golangci-lint run`).
- [ ] ≥90% statement coverage on new files.
- [ ] `PROGRESS.md` updated.
- [ ] Frequent, TDD-shaped commits.
- [ ] PR opened against `main` (merge commit style per CLAUDE.md).
