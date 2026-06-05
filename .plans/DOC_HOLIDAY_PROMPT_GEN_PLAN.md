# Doc Holiday Prompt Generation — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a default-on `prompts.md` artifact to `ftg analyze` containing ready-to-paste, LLM-authored prompts that drive Doc Holiday (https://doc.holiday) to fix the **stale-documentation** and **missing-documentation** gaps the tool already found.

**Architecture:** A new self-contained `internal/docholiday` package turns the already-computed `[]analyzer.DriftFinding` and `[]analyzer.FeatureEntry` (undocumented features) into work-*units* (pure functions), authors one Doc Holiday prompt per unit via a single typical-tier `LLMClient.Complete` call whose instruction text is an embedded agent-skill markdown file, caches each result in `prompts-cache.json` (SIGINT-resumable, atomic temp+rename, mirroring `internal/cli/drift_cache.go`), and dispatches units through the existing bounded `parallel.Run` pool. A new `internal/reporter/prompts_writer.go` renders the results to `prompts.md`. `ftg analyze` runs the phase after drift detection; `ftg render` re-emits `prompts.md` from the cache with no LLM calls.

**Tech Stack:** Go 1.26+, `//go:embed` (single-file string, per `internal/scanner/ignore/defaults.go`), `internal/parallel.Run`, `internal/analyzer` LLM tiering (`tiering.Typical().Complete`), testify, testscript.

---

## Conventions for the implementing engineer (read first)

- **TDD is mandatory** (CLAUDE.md, non-negotiable): write the failing test, run it, watch it fail for the right reason, write minimal code, watch it pass, refactor, commit. One behavior per cycle.
- **Test layout:** `internal/docholiday/foo.go` → `internal/docholiday/foo_test.go`, same dir, `package docholiday` (white-box) unless a test needs only the public surface.
- **No mocks of running systems.** The only test double allowed is a tiny in-process `Completer` fake (a function/struct implementing one method). That is a boundary stub, not a mock of a live service.
- **Every prompt literal** (static or `fmt.Sprintf`) gets a `// PROMPT:` comment on the line directly above it (CLAUDE.md hard rule). The embedded-skill `var` declarations and the `Complete` call site both count.
- **Commands:** `go test ./internal/docholiday/... -run TestX -v` (single test), `go test ./...` (all), `go build ./...`, `golangci-lint run`, `gofmt -w . && goimports -w .`. Coverage gate ≥90% statements per package: `go test -cover ./internal/docholiday/...`.
- **Commit after each green cycle.** Branch is already `doc-holiday-prompt-gen`; do not touch `main`.
- **Key precedents to imitate** (open them before writing the matching task):
  - `internal/cli/drift_cache.go` — cache file shape, atomic save (`os.CreateTemp` + `os.Rename`), mutex-guarded per-unit persister, completion sentinel.
  - `internal/cli/analyze.go:282-305` — `parallel.Run` dispatch with a mutex-guarded shared accumulator.
  - `internal/reporter/reporter.go:160-238` + `internal/reporter/priority.go` — the Large→Medium→Small bucket loop, `priorityHeading`, `_None found._` convention.
  - `internal/scanner/ignore/defaults.go:3-6` — single-file `//go:embed` string.

---

## Task 0: Package scaffold + the two embedded skills

**Files:**
- Create: `internal/docholiday/skills/fix-stale-docs/SKILL.md`
- Create: `internal/docholiday/skills/document-new-feature/SKILL.md`
- Create: `internal/docholiday/skills.go`
- Test: `internal/docholiday/skills_test.go`

**Step 1: Write the two skill markdown files.**

`internal/docholiday/skills/fix-stale-docs/SKILL.md`:

```markdown
---
name: fix-stale-docs
description: Author a Doc Holiday prompt that fixes documentation which no longer matches the code.
---

You are writing a single instruction (a "prompt") that will be pasted into Doc
Holiday, an AI documentation agent that has full read access to this project's
source code and its documentation files.

You will be given one documentation page and a list of specific claims on that
page that no longer match the current code. Write a prompt that tells Doc
Holiday to correct the page.

Rules for the prompt you write:
- Address Doc Holiday directly in the imperative ("Update the page…",
  "Correct…"). Do not address the human maintainer.
- Name the exact documentation page and each specific inaccuracy. Doc Holiday
  can open the relevant code and docs itself — point it at what to verify, do
  not paste large code or doc excerpts.
- Tell it to confirm the corrected behavior against the actual code before
  rewriting, and to preserve the page's existing structure, tone, and
  formatting.
- Tell it to change only what is inaccurate; leave correct prose untouched.
- Output ONLY the prompt text. No preamble, no surrounding quotes, no
  commentary, no markdown headings.
```

`internal/docholiday/skills/document-new-feature/SKILL.md`:

```markdown
---
name: document-new-feature
description: Author a Doc Holiday prompt that writes a brand-new documentation page for an undocumented feature.
---

You are writing a single instruction (a "prompt") that will be pasted into Doc
Holiday, an AI documentation agent that has full read access to this project's
source code and its documentation files.

You will be given one user-facing feature that currently has no documentation
page, along with the files and symbols that implement it and a short note on
why it matters. Write a prompt that tells Doc Holiday to create a new
documentation page for it.

Rules for the prompt you write:
- Address Doc Holiday directly in the imperative ("Write a new page…").
- Name the feature and point at the implementing files and symbols so Doc
  Holiday can read the real behavior itself. Do not invent API details — tell
  it to derive them from the code.
- Tell it to match the structure, depth, and tone of the project's existing
  documentation pages, and to place the new page where similar pages live.
- Tell it to cover what the feature does, why a reader would use it, and a
  minimal usage example grounded in the actual code.
- Output ONLY the prompt text. No preamble, no surrounding quotes, no
  commentary, no markdown headings.
```

**Step 2: Write the failing test** `internal/docholiday/skills_test.go`:

```go
package docholiday

import "strings"

import "testing"

func TestEmbeddedSkillsArePresent(t *testing.T) {
	if strings.TrimSpace(staleDocsSkillRaw) == "" {
		t.Fatal("staleDocsSkillRaw is empty; go:embed did not load fix-stale-docs/SKILL.md")
	}
	if strings.TrimSpace(newFeatureSkillRaw) == "" {
		t.Fatal("newFeatureSkillRaw is empty; go:embed did not load document-new-feature/SKILL.md")
	}
}

func TestSkillBodyStripsFrontmatter(t *testing.T) {
	body := skillBody(staleDocsSkillRaw)
	if strings.Contains(body, "name:") || strings.Contains(body, "description:") {
		t.Fatalf("frontmatter not stripped from body:\n%s", body)
	}
	if strings.HasPrefix(strings.TrimSpace(body), "---") {
		t.Fatal("body still begins with frontmatter fence")
	}
	if !strings.Contains(body, "Doc Holiday") {
		t.Fatalf("body missing expected instruction text:\n%s", body)
	}
}

func TestSkillBodyWithoutFrontmatterIsUnchanged(t *testing.T) {
	in := "just a body\nwith no frontmatter\n"
	if got := skillBody(in); got != in {
		t.Fatalf("skillBody altered a frontmatter-free string: %q", got)
	}
}
```

**Step 3: Run it to verify it fails.**

Run: `go test ./internal/docholiday/... -run TestSkill -v`
Expected: FAIL — package does not compile (`staleDocsSkillRaw` undefined).

**Step 4: Write minimal implementation** `internal/docholiday/skills.go`:

```go
package docholiday

import (
	_ "embed"
	"strings"
)

// PROMPT: Meta-prompt instructing the LLM how to author a Doc Holiday prompt
// that fixes stale documentation. Source of truth is the agent-skill markdown
// at skills/fix-stale-docs/SKILL.md.
//go:embed skills/fix-stale-docs/SKILL.md
var staleDocsSkillRaw string

// PROMPT: Meta-prompt instructing the LLM how to author a Doc Holiday prompt
// that documents a brand-new feature. Source of truth is the agent-skill
// markdown at skills/document-new-feature/SKILL.md.
//go:embed skills/document-new-feature/SKILL.md
var newFeatureSkillRaw string

// skillBody returns the markdown body of an agent-skill file with its leading
// YAML frontmatter block stripped. A frontmatter block is a "---" line at the
// very start of the file, its content, and a closing "---" line. Input without
// a leading frontmatter fence is returned unchanged.
func skillBody(raw string) string {
	s := strings.TrimLeft(raw, "﻿") // tolerate a UTF-8 BOM
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return raw
	}
	// Find the closing fence after the opening one.
	rest := s[strings.IndexByte(s, '\n')+1:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return raw // malformed; leave as-is rather than nuke content
	}
	after := rest[idx+len("\n---"):]
	return strings.TrimPrefix(strings.TrimLeft(after, "\r"), "\n")
}
```

**Step 5: Run tests to verify they pass.**

Run: `go test ./internal/docholiday/... -run TestSkill -v`
Expected: PASS (all three).

**Step 6: Commit.**

```bash
git add internal/docholiday/skills.go internal/docholiday/skills_test.go internal/docholiday/skills/
git commit -m "feat(docholiday): embed agent-skill meta-prompts via go:embed

- RED: skills_test asserts both SKILL.md embed non-empty and frontmatter strips
- GREEN: skills.go embeds two SKILL.md files + skillBody frontmatter stripper
- Status: 3 tests passing, build successful"
```

---

## Task 1: Core public types

**Files:**
- Create: `internal/docholiday/types.go`
- Test: `internal/docholiday/types_test.go`

**Step 1: Write the failing test** `internal/docholiday/types_test.go`:

```go
package docholiday

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestPriorityRankOrders(t *testing.T) {
	if !(priorityRank(analyzer.PriorityLarge) < priorityRank(analyzer.PriorityMedium) &&
		priorityRank(analyzer.PriorityMedium) < priorityRank(analyzer.PrioritySmall)) {
		t.Fatal("priorityRank must order large < medium < small")
	}
}

func TestMaxPriorityPicksMostSevere(t *testing.T) {
	got := maxPriority([]analyzer.Priority{analyzer.PrioritySmall, analyzer.PriorityLarge, analyzer.PriorityMedium})
	if got != analyzer.PriorityLarge {
		t.Fatalf("want large, got %q", got)
	}
}

func TestMaxPriorityEmptyIsSmall(t *testing.T) {
	if got := maxPriority(nil); got != analyzer.PrioritySmall {
		t.Fatalf("empty should default to small, got %q", got)
	}
}
```

**Step 2: Run it, expect FAIL** (undefined `priorityRank`, `maxPriority`).

Run: `go test ./internal/docholiday/... -run TestPriority -v` and `-run TestMaxPriority -v`

**Step 3: Write minimal implementation** `internal/docholiday/types.go`:

```go
package docholiday

import "github.com/sandgardenhq/find-the-gaps/internal/analyzer"

// Category distinguishes the two kinds of Doc Holiday prompt this package emits.
type Category string

const (
	CategoryStale   Category = "stale"   // fix documentation that no longer matches the code
	CategoryMissing Category = "missing" // write a page for an undocumented feature
)

// Prompt is one ready-to-paste Doc Holiday prompt plus the metadata the
// reporter needs to render it under the right heading and priority bucket.
type Prompt struct {
	Category Category          `json:"category"`
	Heading  string            `json:"heading"`  // e.g. "docs/api.md (+2 more)" or "New page: Frobnicate"
	Note     string            `json:"note"`     // one-line italic note under the heading
	Priority analyzer.Priority `json:"priority"` // bucket the prompt renders under
	Body     string            `json:"body"`     // the LLM-authored prompt text
}

// Input carries the already-computed findings the generator turns into prompts.
// Rationales maps an undocumented feature name to its "why document this"
// blurb (reusing the analyzer.WhyDocument output the CLI already computes);
// missing keys are fine and simply omit the note from that prompt.
type Input struct {
	Drift        []analyzer.DriftFinding
	Undocumented []analyzer.FeatureEntry
	Rationales   map[string]string
}

func priorityRank(p analyzer.Priority) int {
	switch p {
	case analyzer.PriorityLarge:
		return 0
	case analyzer.PriorityMedium:
		return 1
	case analyzer.PrioritySmall:
		return 2
	default:
		return 3
	}
}

// maxPriority returns the most severe priority in ps (large > medium > small).
// An empty slice yields small — the least-severe default.
func maxPriority(ps []analyzer.Priority) analyzer.Priority {
	best := analyzer.PrioritySmall
	bestRank := priorityRank(best)
	for _, p := range ps {
		if r := priorityRank(p); r < bestRank {
			best, bestRank = p, r
		}
	}
	return best
}
```

**Step 4: Run tests, expect PASS.**

**Step 5: Commit.**

```bash
git add internal/docholiday/types.go internal/docholiday/types_test.go
git commit -m "feat(docholiday): core types (Prompt, Category, Input) + priority helpers

- RED: types_test asserts priorityRank ordering and maxPriority selection
- GREEN: types.go defines Prompt/Category/Input + priority rank/max helpers
- Status: tests passing, build successful"
```

---

## Task 2: Stale unit assembly (group by page, chunk, priority rollup)

**Background:** `analyzer.DriftFinding{Feature string, Issues []DriftIssue}` where `DriftIssue{Page, Issue, Priority, PriorityReason}`. We flatten across features into `(feature, issue)` pairs, group by `issue.Page`, then split each page into chunks of ≤ `maxIssuesPerPrompt`. Each chunk becomes one `unit`. A page with `Page == ""` (cross-page issues) groups under one synthetic bucket.

**Files:**
- Create: `internal/docholiday/unit.go`
- Test: `internal/docholiday/unit_test.go`

**Step 1: Write the failing test** `internal/docholiday/unit_test.go`:

```go
package docholiday

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func driftIssue(page, issue string, p analyzer.Priority) analyzer.DriftIssue {
	return analyzer.DriftIssue{Page: page, Issue: issue, Priority: p, PriorityReason: "because"}
}

func TestStaleUnitsGroupByPage(t *testing.T) {
	drift := []analyzer.DriftFinding{
		{Feature: "Auth", Issues: []analyzer.DriftIssue{
			driftIssue("docs/a.md", "x", analyzer.PrioritySmall),
		}},
		{Feature: "Sync", Issues: []analyzer.DriftIssue{
			driftIssue("docs/a.md", "y", analyzer.PriorityLarge),
			driftIssue("docs/b.md", "z", analyzer.PriorityMedium),
		}},
	}
	units := staleUnits(drift, 5)
	if len(units) != 2 {
		t.Fatalf("want 2 page-units, got %d", len(units))
	}
	// docs/a.md unit carries both issues and rolls up to large.
	var a *unit
	for i := range units {
		if units[i].page == "docs/a.md" {
			a = &units[i]
		}
	}
	if a == nil {
		t.Fatal("missing docs/a.md unit")
	}
	if len(a.staleItems) != 2 {
		t.Fatalf("docs/a.md should hold 2 issues, got %d", len(a.staleItems))
	}
	if a.priority != analyzer.PriorityLarge {
		t.Fatalf("docs/a.md priority should roll up to large, got %q", a.priority)
	}
}

func TestStaleUnitsChunkWhenOverCap(t *testing.T) {
	var issues []analyzer.DriftIssue
	for i := 0; i < 7; i++ {
		issues = append(issues, driftIssue("docs/big.md", "issue", analyzer.PrioritySmall))
	}
	drift := []analyzer.DriftFinding{{Feature: "F", Issues: issues}}
	units := staleUnits(drift, 3)
	if len(units) != 3 { // 3 + 3 + 1
		t.Fatalf("7 issues at cap 3 should produce 3 chunks, got %d", len(units))
	}
	for _, u := range units {
		if u.parts != 3 {
			t.Fatalf("each chunk should record parts=3, got %d", u.parts)
		}
		if len(u.staleItems) > 3 {
			t.Fatalf("chunk exceeds cap: %d items", len(u.staleItems))
		}
	}
	if units[0].part != 1 || units[2].part != 3 {
		t.Fatalf("part numbers should be 1..3, got %d..%d", units[0].part, units[2].part)
	}
}

func TestStaleUnitsAreDeterministic(t *testing.T) {
	drift := []analyzer.DriftFinding{
		{Feature: "B", Issues: []analyzer.DriftIssue{driftIssue("docs/z.md", "1", analyzer.PrioritySmall)}},
		{Feature: "A", Issues: []analyzer.DriftIssue{driftIssue("docs/a.md", "2", analyzer.PrioritySmall)}},
	}
	first := staleUnits(drift, 5)
	second := staleUnits(drift, 5)
	if first[0].page != second[0].page || first[1].page != second[1].page {
		t.Fatal("staleUnits must be deterministic across calls")
	}
	if first[0].page != "docs/a.md" {
		t.Fatalf("units should sort by page; first should be docs/a.md, got %q", first[0].page)
	}
}
```

**Step 2: Run it, expect FAIL** (undefined `unit`, `staleUnits`).

**Step 3: Write minimal implementation** `internal/docholiday/unit.go`:

```go
package docholiday

import (
	"sort"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// maxIssuesPerPrompt caps how many stale-doc issues a single Doc Holiday prompt
// addresses before the page is split into "(part K of M)" chunks. Five keeps a
// prompt focused enough for the agent to act on in one pass.
const maxIssuesPerPrompt = 5

// crossPageKey is the synthetic page key for drift issues with no page anchor.
const crossPageKey = ""

// staleItem is one (feature, inaccuracy) pair within a stale-docs unit.
type staleItem struct {
	feature string
	issue   string
}

// unit is one work-item that becomes exactly one Doc Holiday prompt.
type unit struct {
	category Category
	priority analyzer.Priority

	// stale fields
	page       string
	staleItems []staleItem
	part       int // 1-based; 1 when the page was not split
	parts      int // total chunks for this page; 1 when not split

	// missing fields
	feature   analyzer.FeatureEntry
	rationale string
}

// staleUnits flattens drift findings into per-page chunks of at most cap
// issues. Output is sorted by page for determinism; each chunk's priority is
// the most severe priority among its issues.
func staleUnits(drift []analyzer.DriftFinding, cap int) []unit {
	if cap <= 0 {
		cap = maxIssuesPerPrompt
	}
	byPage := map[string][]staleItem{}
	prioByPage := map[string][]analyzer.Priority{}
	var pageOrder []string
	for _, f := range drift {
		for _, iss := range f.Issues {
			if _, seen := byPage[iss.Page]; !seen {
				pageOrder = append(pageOrder, iss.Page)
			}
			byPage[iss.Page] = append(byPage[iss.Page], staleItem{feature: f.Feature, issue: iss.Issue})
			prioByPage[iss.Page] = append(prioByPage[iss.Page], iss.Priority)
		}
	}
	sort.Strings(pageOrder)

	var out []unit
	for _, page := range pageOrder {
		items := byPage[page]
		chunks := chunkStaleItems(items, cap)
		for i, chunk := range chunks {
			// priority of the chunk = max over the chunk's own issues
			var ps []analyzer.Priority
			for j := range chunk {
				// map chunk item back to its priority via position in items
				ps = append(ps, prioByPage[page][i*cap+j])
			}
			out = append(out, unit{
				category:   CategoryStale,
				priority:   maxPriority(ps),
				page:       page,
				staleItems: chunk,
				part:       i + 1,
				parts:      len(chunks),
			})
		}
	}
	return out
}

func chunkStaleItems(items []staleItem, cap int) [][]staleItem {
	var chunks [][]staleItem
	for i := 0; i < len(items); i += cap {
		end := i + cap
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[i:end])
	}
	return chunks
}
```

> **Note for the engineer:** the `prioByPage[page][i*cap+j]` indexing relies on `chunkStaleItems` slicing `items` in the same order priorities were appended — which it does, since both `byPage` and `prioByPage` are appended in lockstep. If you refactor chunking, keep the two slices index-aligned or fold priority into `staleItem`. (Folding priority into `staleItem` is cleaner; if you prefer, add a `priority analyzer.Priority` field to `staleItem`, populate it in the flatten loop, and compute the chunk priority from `chunk` directly. Update the test only if signatures change.)

**Step 4: Run tests, expect PASS.**

**Step 5: Commit** (`feat(docholiday): stale-doc unit assembly with page grouping + chunking`).

---

## Task 3: Missing-feature unit assembly

**Files:**
- Modify: `internal/docholiday/unit.go`
- Test: `internal/docholiday/unit_test.go` (add cases)

**Step 1: Write the failing test** (append to `unit_test.go`):

```go
func TestMissingUnitsOnePerFeature(t *testing.T) {
	feats := []analyzer.FeatureEntry{
		{Feature: analyzer.CodeFeature{Name: "Frobnicate", UserFacing: true}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "Sync"}, Files: []string{"b.go"}},
	}
	rats := map[string]string{"Frobnicate": "users hit this daily"}
	units := missingUnits(feats, rats)
	if len(units) != 2 {
		t.Fatalf("want one unit per feature, got %d", len(units))
	}
	if units[0].feature.Feature.Name != "Frobnicate" || units[0].category != CategoryMissing {
		t.Fatalf("unexpected first unit: %+v", units[0])
	}
	if units[0].rationale != "users hit this daily" {
		t.Fatalf("rationale not threaded: %q", units[0].rationale)
	}
	if units[1].rationale != "" {
		t.Fatalf("missing rationale should be empty, got %q", units[1].rationale)
	}
}

func TestMissingUnitsGetDefaultPriority(t *testing.T) {
	feats := []analyzer.FeatureEntry{{Feature: analyzer.CodeFeature{Name: "X"}}}
	units := missingUnits(feats, nil)
	if units[0].priority != missingDocPriority(feats[0]) {
		t.Fatalf("missing unit priority should come from missingDocPriority")
	}
}
```

**Step 2: Run, expect FAIL.**

**Step 3: Implement** (append to `unit.go`):

```go
// missingDocPriority assigns a priority to an undocumented-feature prompt.
// Undocumented features carry no priority signal in the data model, so we use
// a single deterministic default. This is the one place to enrich later (e.g.
// derive from layer or usage) — keep it a function so callers never inline a
// constant.
func missingDocPriority(_ analyzer.FeatureEntry) analyzer.Priority {
	return analyzer.PriorityLarge
}

// missingUnits builds one unit per undocumented feature, in the input order
// (UndocumentedFeatures already returns a stable insertion order).
func missingUnits(feats []analyzer.FeatureEntry, rationales map[string]string) []unit {
	out := make([]unit, 0, len(feats))
	for _, f := range feats {
		out = append(out, unit{
			category:  CategoryMissing,
			priority:  missingDocPriority(f),
			feature:   f,
			rationale: rationales[f.Feature.Name],
		})
	}
	return out
}
```

**Step 4: Run, expect PASS. Step 5: Commit** (`feat(docholiday): missing-feature unit assembly`).

---

## Task 4: Render a unit's findings + heading/note (pure)

**Files:**
- Create: `internal/docholiday/render.go`
- Test: `internal/docholiday/render_test.go`

These pure functions produce (a) the findings block appended to the skill body for the LLM call, and (b) the `Heading`/`Note` strings the reporter shows. **No `reporter` import** (would cycle).

**Step 1: Write the failing test** `internal/docholiday/render_test.go`:

```go
package docholiday

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestRenderStaleFindingsListsPageAndIssues(t *testing.T) {
	u := unit{
		category: CategoryStale,
		page:     "docs/api.md",
		staleItems: []staleItem{
			{feature: "Auth", issue: "Connect() now needs a ctx"},
			{feature: "Auth", issue: "Token TTL is 1h not 24h"},
		},
	}
	got := renderUnitFindings(u)
	for _, want := range []string{"docs/api.md", "Connect() now needs a ctx", "Token TTL is 1h not 24h", "Auth"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered findings missing %q:\n%s", want, got)
		}
	}
}

func TestRenderMissingFindingsListsFilesAndWhy(t *testing.T) {
	u := unit{
		category:  CategoryMissing,
		feature:   analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "Frobnicate", Description: "frobs things"}, Files: []string{"frob.go"}, Symbols: []string{"Frobnicate"}},
		rationale: "shipped last month",
	}
	got := renderUnitFindings(u)
	for _, want := range []string{"Frobnicate", "frobs things", "frob.go", "shipped last month"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered findings missing %q:\n%s", want, got)
		}
	}
}

func TestStaleHeadingShowsPageAndOverflow(t *testing.T) {
	u := unit{category: CategoryStale, page: "docs/api.md", staleItems: make([]staleItem, 3), part: 1, parts: 1}
	if got := unitHeading(u); !strings.Contains(got, "docs/api.md") || !strings.Contains(got, "+2 more") {
		t.Fatalf("heading should name page and overflow count: %q", got)
	}
}

func TestStaleHeadingShowsPartWhenSplit(t *testing.T) {
	u := unit{category: CategoryStale, page: "docs/api.md", staleItems: make([]staleItem, 3), part: 2, parts: 3}
	if got := unitHeading(u); !strings.Contains(got, "part 2 of 3") {
		t.Fatalf("split heading should carry part suffix: %q", got)
	}
}

func TestMissingHeadingAndNote(t *testing.T) {
	u := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "Frobnicate"}}}
	if got := unitHeading(u); got != "New page: Frobnicate" {
		t.Fatalf("missing heading: %q", got)
	}
	if got := unitNote(u); !strings.Contains(strings.ToLower(got), "no docs page") {
		t.Fatalf("missing note: %q", got)
	}
}
```

**Step 2: Run, expect FAIL.**

**Step 3: Implement** `internal/docholiday/render.go`:

```go
package docholiday

import (
	"fmt"
	"strings"
)

// renderUnitFindings produces the deterministic facts block appended to the
// skill body to form the full LLM prompt. It is the "user message" half of the
// call (analyzer.LLMClient.Complete takes one flat string, so we concatenate).
func renderUnitFindings(u unit) string {
	var sb strings.Builder
	switch u.category {
	case CategoryStale:
		page := u.page
		if page == crossPageKey {
			page = "(cross-cutting; affects multiple or unspecified pages)"
		}
		fmt.Fprintf(&sb, "Documentation page: %s\n\n", page)
		sb.WriteString("Claims on this page that no longer match the code:\n")
		for _, it := range u.staleItems {
			fmt.Fprintf(&sb, "- [%s] %s\n", it.feature, it.issue)
		}
	case CategoryMissing:
		f := u.feature
		fmt.Fprintf(&sb, "Undocumented feature: %s\n", f.Feature.Name)
		if f.Feature.Description != "" {
			fmt.Fprintf(&sb, "Description: %s\n", f.Feature.Description)
		}
		if len(f.Files) > 0 {
			fmt.Fprintf(&sb, "Implemented in:\n")
			for _, file := range f.Files {
				fmt.Fprintf(&sb, "- %s\n", file)
			}
		}
		if len(f.Symbols) > 0 {
			fmt.Fprintf(&sb, "Key symbols: %s\n", strings.Join(f.Symbols, ", "))
		}
		if strings.TrimSpace(u.rationale) != "" {
			fmt.Fprintf(&sb, "Why it matters: %s\n", u.rationale)
		}
	}
	return sb.String()
}

// unitHeading is the human-readable "####" heading shown above the prompt block.
func unitHeading(u unit) string {
	switch u.category {
	case CategoryMissing:
		return "New page: " + u.feature.Feature.Name
	default:
		page := u.page
		if page == crossPageKey {
			page = "Cross-cutting documentation"
		}
		h := page
		if extra := len(u.staleItems) - 1; extra > 0 {
			h += fmt.Sprintf(" (+%d more)", extra)
		}
		if u.parts > 1 {
			h += fmt.Sprintf(" (part %d of %d)", u.part, u.parts)
		}
		return h
	}
}

// unitNote is the one-line italic note rendered under the heading.
func unitNote(u unit) string {
	switch u.category {
	case CategoryMissing:
		return "No docs page covers this user-facing feature."
	default:
		n := len(u.staleItems)
		plural := "issue"
		if n != 1 {
			plural = "issues"
		}
		return fmt.Sprintf("Addresses %d stale-doc %s on this page.", n, plural)
	}
}
```

**Step 4: Run, expect PASS. Step 5: Commit** (`feat(docholiday): pure rendering of unit findings, headings, notes`).

---

## Task 5: Cache-key hashing (pure)

**Files:**
- Create: `internal/docholiday/cachekey.go`
- Test: `internal/docholiday/cachekey_test.go`

The key binds a unit to its exact content **and** the skill version, so editing a SKILL.md invalidates stale cache entries.

**Step 1: Write the failing test** `internal/docholiday/cachekey_test.go`:

```go
package docholiday

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestUnitKeyStableForSameContent(t *testing.T) {
	u := unit{category: CategoryStale, page: "p", staleItems: []staleItem{{"F", "i"}}, part: 1, parts: 1}
	if unitKey(u) != unitKey(u) {
		t.Fatal("unitKey must be stable for identical units")
	}
}

func TestUnitKeyChangesWhenIssueChanges(t *testing.T) {
	a := unit{category: CategoryStale, page: "p", staleItems: []staleItem{{"F", "i1"}}, part: 1, parts: 1}
	b := unit{category: CategoryStale, page: "p", staleItems: []staleItem{{"F", "i2"}}, part: 1, parts: 1}
	if unitKey(a) == unitKey(b) {
		t.Fatal("changing an issue must change the key")
	}
}

func TestUnitKeyChangesWithSkillVersion(t *testing.T) {
	u := unit{category: CategoryStale, page: "p", staleItems: []staleItem{{"F", "i"}}, part: 1, parts: 1}
	k1 := unitKeyWithSkill(u, "skill-v1")
	k2 := unitKeyWithSkill(u, "skill-v2")
	if k1 == k2 {
		t.Fatal("a skill-body change must invalidate the key")
	}
}

func TestUnitKeyMissingFeatureUsesFilesAndSymbols(t *testing.T) {
	a := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "X"}, Files: []string{"a.go"}}}
	b := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "X"}, Files: []string{"b.go"}}}
	if unitKey(a) == unitKey(b) {
		t.Fatal("changing implementing files must change a missing-feature key")
	}
}
```

**Step 2: Run, expect FAIL.**

**Step 3: Implement** `internal/docholiday/cachekey.go`:

```go
package docholiday

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// skillVersion is a short hash of both embedded skill bodies. Any edit to a
// SKILL.md changes it, which flows into every unit key and invalidates cached
// prompts authored under the old instructions.
func skillVersion() string {
	sum := sha256.Sum256([]byte(skillBody(staleDocsSkillRaw) + "\x00" + skillBody(newFeatureSkillRaw)))
	return hex.EncodeToString(sum[:])[:12]
}

// unitKey is the cache key for a unit under the current skill version.
func unitKey(u unit) string { return unitKeyWithSkill(u, skillVersion()) }

// unitKeyWithSkill is unitKey with an explicit skill-version token (test seam).
func unitKeyWithSkill(u unit, skill string) string {
	var b strings.Builder
	b.WriteString(string(u.category))
	b.WriteByte('|')
	b.WriteString(skill)
	b.WriteByte('|')
	switch u.category {
	case CategoryStale:
		b.WriteString(u.page)
		fmt.Fprintf(&b, "|%d/%d|", u.part, u.parts)
		for _, it := range u.staleItems {
			b.WriteString(it.feature)
			b.WriteByte('\x1f')
			b.WriteString(it.issue)
			b.WriteByte('\n')
		}
	case CategoryMissing:
		f := u.feature
		b.WriteString(f.Feature.Name)
		b.WriteByte('|')
		files := append([]string(nil), f.Files...)
		sort.Strings(files)
		b.WriteString(strings.Join(files, ","))
		b.WriteByte('|')
		syms := append([]string(nil), f.Symbols...)
		sort.Strings(syms)
		b.WriteString(strings.Join(syms, ","))
		b.WriteByte('|')
		b.WriteString(u.rationale)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
```

**Step 4: Run, expect PASS. Step 5: Commit** (`feat(docholiday): content+skill-version cache keys for units`).

---

## Task 6: Generate one prompt from one unit (Completer seam)

**Files:**
- Create: `internal/docholiday/generate.go`
- Test: `internal/docholiday/generate_test.go`

**Step 1: Write the failing test** `internal/docholiday/generate_test.go`:

```go
package docholiday

import (
	"context"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// fakeCompleter records the prompt it received and returns a canned body.
type fakeCompleter struct {
	gotPrompt string
	reply     string
	err       error
}

func (f *fakeCompleter) Complete(_ context.Context, prompt string) (string, error) {
	f.gotPrompt = prompt
	return f.reply, f.err
}

func TestGenerateOneStaleUsesStaleSkillAndFindings(t *testing.T) {
	fc := &fakeCompleter{reply: "Update docs/api.md to ..."}
	u := unit{category: CategoryStale, priority: analyzer.PriorityLarge, page: "docs/api.md",
		staleItems: []staleItem{{"Auth", "Connect() needs ctx"}}, part: 1, parts: 1}
	p, err := generateOne(context.Background(), fc, u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fc.gotPrompt, skillBody(staleDocsSkillRaw)) {
		t.Fatal("prompt should embed the stale-docs skill body")
	}
	if !strings.Contains(fc.gotPrompt, "Connect() needs ctx") {
		t.Fatal("prompt should embed the unit findings")
	}
	if p.Body != "Update docs/api.md to ..." {
		t.Fatalf("body should be the completer reply, got %q", p.Body)
	}
	if p.Category != CategoryStale || p.Priority != analyzer.PriorityLarge {
		t.Fatalf("metadata not carried: %+v", p)
	}
	if !strings.Contains(p.Heading, "docs/api.md") {
		t.Fatalf("heading: %q", p.Heading)
	}
}

func TestGenerateOneMissingUsesNewFeatureSkill(t *testing.T) {
	fc := &fakeCompleter{reply: "Write a new page for Frobnicate"}
	u := unit{category: CategoryMissing, priority: analyzer.PriorityLarge,
		feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "Frobnicate"}}}
	p, err := generateOne(context.Background(), fc, u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fc.gotPrompt, skillBody(newFeatureSkillRaw)) {
		t.Fatal("prompt should embed the new-feature skill body")
	}
	if p.Heading != "New page: Frobnicate" {
		t.Fatalf("heading: %q", p.Heading)
	}
}

func TestGenerateOneTrimsBody(t *testing.T) {
	fc := &fakeCompleter{reply: "\n  prompt text \n\n"}
	u := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "X"}}}
	p, _ := generateOne(context.Background(), fc, u)
	if p.Body != "prompt text" {
		t.Fatalf("body should be trimmed, got %q", p.Body)
	}
}
```

**Step 2: Run, expect FAIL.**

**Step 3: Implement** `internal/docholiday/generate.go`:

```go
package docholiday

import (
	"context"
	"fmt"
	"strings"
)

// Completer is the minimal LLM surface this package needs: one flat-string
// completion. *analyzer.BifrostClient (via analyzer.LLMClient) satisfies it;
// the CLI passes tiering.Typical().
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

func skillRawFor(c Category) string {
	if c == CategoryMissing {
		return newFeatureSkillRaw
	}
	return staleDocsSkillRaw
}

// generateOne authors the Doc Holiday prompt for a single unit. The LLM prompt
// is the skill body (instructions) followed by the unit's findings.
func generateOne(ctx context.Context, gen Completer, u unit) (Prompt, error) {
	// PROMPT: Concatenates the embedded agent-skill instructions (how to write
	// a Doc Holiday prompt) with this unit's concrete findings; the model
	// returns the finished Doc Holiday prompt text.
	prompt := fmt.Sprintf("%s\n\n---\n\n%s", skillBody(skillRawFor(u.category)), renderUnitFindings(u))
	body, err := gen.Complete(ctx, prompt)
	if err != nil {
		return Prompt{}, fmt.Errorf("generate %s prompt for %q: %w", u.category, unitHeading(u), err)
	}
	return Prompt{
		Category: u.category,
		Heading:  unitHeading(u),
		Note:     unitNote(u),
		Priority: u.priority,
		Body:     strings.TrimSpace(body),
	}, nil
}
```

**Step 4: Run, expect PASS. Step 5: Commit** (`feat(docholiday): author one prompt per unit via Completer + embedded skill`).

---

## Task 7: Cache file (load / save / atomic / LoadCachedPrompts)

**Files:**
- Create: `internal/docholiday/cache.go`
- Test: `internal/docholiday/cache_test.go`

Mirror `internal/cli/drift_cache.go`: a `prompts-cache.json` holding all entries, atomic temp+rename, entries store the full rendered `Prompt` plus its key.

**Step 1: Write the failing test** `internal/docholiday/cache_test.go`:

```go
package docholiday

import (
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func samplePrompt() Prompt {
	return Prompt{Category: CategoryStale, Heading: "docs/a.md", Note: "n", Priority: analyzer.PriorityLarge, Body: "do the thing"}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	c := newCache(map[string]Prompt{"k1": samplePrompt()})
	if err := c.save(dir); err != nil {
		t.Fatal(err)
	}
	got, ok := loadCache(dir)
	if !ok {
		t.Fatal("expected cache file to load")
	}
	if got.entries["k1"].Body != "do the thing" {
		t.Fatalf("round-trip lost body: %+v", got.entries["k1"])
	}
}

func TestLoadMissingFileReturnsFalse(t *testing.T) {
	if _, ok := loadCache(t.TempDir()); ok {
		t.Fatal("loadCache on empty dir should return ok=false")
	}
}

func TestLoadCachedPromptsSortsDeterministically(t *testing.T) {
	dir := t.TempDir()
	c := newCache(map[string]Prompt{
		"k1": {Category: CategoryMissing, Heading: "New page: Z", Priority: analyzer.PriorityLarge, Body: "b"},
		"k2": {Category: CategoryStale, Heading: "docs/a.md", Priority: analyzer.PrioritySmall, Body: "b"},
		"k3": {Category: CategoryStale, Heading: "docs/a.md", Priority: analyzer.PriorityLarge, Body: "b"},
	})
	if err := c.save(dir); err != nil {
		t.Fatal(err)
	}
	prompts, ok := LoadCachedPrompts(dir)
	if !ok || len(prompts) != 3 {
		t.Fatalf("want 3 prompts, ok=%v n=%d", ok, len(prompts))
	}
	// stale before missing; within stale, large before small
	if prompts[0].Category != CategoryStale || prompts[0].Priority != analyzer.PriorityLarge {
		t.Fatalf("sort order wrong: %+v", prompts[0])
	}
	if prompts[2].Category != CategoryMissing {
		t.Fatalf("missing should sort last: %+v", prompts[2])
	}
}

func TestCacheFileWrittenAtExpectedPath(t *testing.T) {
	dir := t.TempDir()
	if err := newCache(map[string]Prompt{"k": samplePrompt()}).save(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCache(dir); !true {
		_ = err
	}
	if _, err := readFileExists(filepath.Join(dir, cacheFileName)); err != nil {
		t.Fatalf("expected %s on disk: %v", cacheFileName, err)
	}
}
```

> Add the tiny `readFileExists` helper in the test file:
> ```go
> func readFileExists(p string) (struct{}, error) {
> 	_, err := os.Stat(p)
> 	return struct{}{}, err
> }
> ```
> with `import "os"`. (Or inline `os.Stat`; the helper just keeps the test readable.)

**Step 2: Run, expect FAIL.**

**Step 3: Implement** `internal/docholiday/cache.go`:

```go
package docholiday

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

const cacheFileName = "prompts-cache.json"

type cacheEntry struct {
	Key      string            `json:"key"`
	Category Category          `json:"category"`
	Heading  string            `json:"heading"`
	Note     string            `json:"note"`
	Priority analyzer.Priority `json:"priority"`
	Body     string            `json:"body"`
}

type cacheDoc struct {
	Entries []cacheEntry `json:"entries"`
}

// cache is the in-memory live view persisted to prompts-cache.json. It is safe
// for concurrent put() from worker goroutines.
type cache struct {
	mu      sync.Mutex
	entries map[string]Prompt
}

func newCache(seed map[string]Prompt) *cache {
	m := map[string]Prompt{}
	for k, v := range seed {
		m[k] = v
	}
	return &cache{entries: m}
}

func (c *cache) get(key string) (Prompt, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.entries[key]
	return p, ok
}

// put records a freshly generated prompt and atomically re-flushes the whole
// file so a SIGINT mid-run leaves a valid partial cache.
func (c *cache) put(dir, key string, p Prompt) error {
	c.mu.Lock()
	c.entries[key] = p
	c.mu.Unlock()
	return c.save(dir)
}

func (c *cache) save(dir string) error {
	c.mu.Lock()
	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	doc := cacheDoc{Entries: make([]cacheEntry, 0, len(keys))}
	for _, k := range keys {
		p := c.entries[k]
		doc.Entries = append(doc.Entries, cacheEntry{
			Key: k, Category: p.Category, Heading: p.Heading,
			Note: p.Note, Priority: p.Priority, Body: p.Body,
		})
	}
	c.mu.Unlock()

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".prompts-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, cacheFileName))
}

// loadCache reads prompts-cache.json from dir. ok=false when the file is
// absent or unreadable (treated as a cold cache, never an error).
func loadCache(dir string) (*cache, bool) {
	data, err := os.ReadFile(filepath.Join(dir, cacheFileName))
	if err != nil {
		return nil, false
	}
	var doc cacheDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	m := make(map[string]Prompt, len(doc.Entries))
	for _, e := range doc.Entries {
		m[e.Key] = Prompt{Category: e.Category, Heading: e.Heading, Note: e.Note, Priority: e.Priority, Body: e.Body}
	}
	return &cache{entries: m}, true
}

// LoadCachedPrompts returns the cached prompts for a project dir, sorted, for
// `ftg render` to re-emit prompts.md with no LLM calls. ok=false when no cache
// exists (the analyze phase never ran).
func LoadCachedPrompts(dir string) ([]Prompt, bool) {
	c, ok := loadCache(dir)
	if !ok {
		return nil, false
	}
	out := make([]Prompt, 0, len(c.entries))
	for _, p := range c.entries {
		out = append(out, p)
	}
	SortPrompts(out)
	return out, true
}

// SortPrompts orders prompts deterministically: stale before missing, then
// Large→Medium→Small, then by heading.
func SortPrompts(ps []Prompt) {
	catRank := func(c Category) int {
		if c == CategoryMissing {
			return 1
		}
		return 0
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if r := catRank(ps[i].Category) - catRank(ps[j].Category); r != 0 {
			return r < 0
		}
		if r := priorityRank(ps[i].Priority) - priorityRank(ps[j].Priority); r != 0 {
			return r < 0
		}
		return ps[i].Heading < ps[j].Heading
	})
}
```

**Step 4: Run, expect PASS. Step 5: Commit** (`feat(docholiday): SIGINT-safe prompts-cache.json + LoadCachedPrompts`).

---

## Task 8: GeneratePrompts orchestration (parallel + cache + sort)

**Files:**
- Modify: `internal/docholiday/generate.go`
- Test: `internal/docholiday/generate_orchestration_test.go`

**Step 1: Write the failing test** `internal/docholiday/generate_orchestration_test.go`:

```go
package docholiday

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// countingCompleter returns a deterministic reply and counts calls.
type countingCompleter struct{ calls atomic.Int64 }

func (c *countingCompleter) Complete(_ context.Context, prompt string) (string, error) {
	c.calls.Add(1)
	return "PROMPT BODY", nil
}

func sampleInput() Input {
	return Input{
		Drift: []analyzer.DriftFinding{{Feature: "Auth", Issues: []analyzer.DriftIssue{
			{Page: "docs/a.md", Issue: "stale", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
		}}},
		Undocumented: []analyzer.FeatureEntry{{Feature: analyzer.CodeFeature{Name: "Frob"}, Files: []string{"f.go"}}},
	}
}

func TestGeneratePromptsProducesOnePerUnit(t *testing.T) {
	cc := &countingCompleter{}
	got, err := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: t.TempDir(), Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 { // one stale page + one missing feature
		t.Fatalf("want 2 prompts, got %d", len(got))
	}
	if cc.calls.Load() != 2 {
		t.Fatalf("want 2 LLM calls, got %d", cc.calls.Load())
	}
}

func TestGeneratePromptsSecondRunHitsCache(t *testing.T) {
	dir := t.TempDir()
	cc := &countingCompleter{}
	if _, err := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: dir, Workers: 4}); err != nil {
		t.Fatal(err)
	}
	first := cc.calls.Load()
	if _, err := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: dir, Workers: 4}); err != nil {
		t.Fatal(err)
	}
	if cc.calls.Load() != first {
		t.Fatalf("warm re-run should make 0 new calls; was %d now %d", first, cc.calls.Load())
	}
}

func TestGeneratePromptsNoCacheForcesRegen(t *testing.T) {
	dir := t.TempDir()
	cc := &countingCompleter{}
	in := sampleInput()
	_, _ = GeneratePrompts(context.Background(), cc, in, Options{ProjectDir: dir, Workers: 4})
	before := cc.calls.Load()
	_, _ = GeneratePrompts(context.Background(), cc, in, Options{ProjectDir: dir, Workers: 4, NoCache: true})
	if cc.calls.Load() <= before {
		t.Fatal("NoCache must force regeneration")
	}
}

func TestGeneratePromptsResultIsSorted(t *testing.T) {
	cc := &countingCompleter{}
	got, _ := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: t.TempDir(), Workers: 1})
	if got[0].Category != CategoryStale || got[1].Category != CategoryMissing {
		t.Fatalf("results not sorted stale-before-missing: %+v", got)
	}
}
```

**Step 2: Run, expect FAIL** (undefined `GeneratePrompts`, `Options`).

**Step 3: Implement** (append to `internal/docholiday/generate.go`):

```go
import (
	"sync"

	"github.com/sandgardenhq/find-the-gaps/internal/parallel"
)

// Options configures a GeneratePrompts run.
type Options struct {
	ProjectDir string // where prompts-cache.json lives
	Workers    int    // bounded worker pool size (<=0 → serial)
	NoCache    bool   // ignore and overwrite any existing cache
}

// GeneratePrompts authors one Doc Holiday prompt per work-unit derived from in,
// reusing cached prompts when their unit content + skill version are unchanged.
// Results are returned sorted (SortPrompts order). The cache is flushed after
// every fresh unit so a SIGINT leaves a valid partial prompts-cache.json.
func GeneratePrompts(ctx context.Context, gen Completer, in Input, opts Options) ([]Prompt, error) {
	units := append(staleUnits(in.Drift, maxIssuesPerPrompt), missingUnits(in.Undocumented, in.Rationales)...)

	var c *cache
	if !opts.NoCache {
		if loaded, ok := loadCache(opts.ProjectDir); ok {
			c = loaded
		}
	}
	if c == nil {
		c = newCache(nil)
	}

	var (
		mu  sync.Mutex
		out = make([]Prompt, 0, len(units))
	)
	err := parallel.Run(ctx, units, opts.Workers, func(ctx context.Context, u unit) error {
		key := unitKey(u)
		if p, ok := c.get(key); ok {
			mu.Lock()
			out = append(out, p)
			mu.Unlock()
			return nil
		}
		p, err := generateOne(ctx, gen, u)
		if err != nil {
			return err
		}
		if err := c.put(opts.ProjectDir, key, p); err != nil {
			return fmt.Errorf("persist prompts cache: %w", err)
		}
		mu.Lock()
		out = append(out, p)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, err
	}
	SortPrompts(out)
	return out, nil
}
```

> **Cache-staleness note:** because `NoCache` skips *loading* but `put` still overwrites entries by fresh key, a `NoCache` run regenerates every current unit and leaves only current entries (sorted save rewrites the file from the live map, which started empty). This matches drift's behavior. If a unit disappears between runs without `NoCache`, its stale entry lingers in the cache file but is never read (no current unit has its key) and never rendered — acceptable, same as drift.

**Step 4: Run, expect PASS** (including `-race`: `go test -race ./internal/docholiday/...`). Step 5: Commit (`feat(docholiday): GeneratePrompts orchestration — parallel, cached, sorted`).

---

## Task 9: Reporter writer — `prompts.md`

**Files:**
- Create: `internal/reporter/prompts_writer.go`
- Test: `internal/reporter/prompts_writer_test.go`

`reporter` imports `docholiday` for the `Prompt` type (one-way; `docholiday` never imports `reporter`).

**Step 1: Write the failing test** `internal/reporter/prompts_writer_test.go`:

```go
package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/docholiday"
)

func TestBuildPromptsBytesStructure(t *testing.T) {
	prompts := []docholiday.Prompt{
		{Category: docholiday.CategoryStale, Heading: "docs/a.md", Note: "Addresses 1 stale-doc issue on this page.", Priority: analyzer.PriorityLarge, Body: "Update docs/a.md"},
		{Category: docholiday.CategoryMissing, Heading: "New page: Frob", Note: "No docs page covers this user-facing feature.", Priority: analyzer.PriorityLarge, Body: "Write a page for Frob"},
	}
	out := string(BuildPromptsBytes(prompts))
	for _, want := range []string{
		"# Doc Holiday Prompts",
		"doc.holiday",
		"## Fix Stale Documentation",
		"### Large",
		"#### docs/a.md",
		"_Addresses 1 stale-doc issue on this page._",
		"Update docs/a.md",
		"## Document New Features",
		"#### New page: Frob",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestBuildPromptsBytesEmptyCategories(t *testing.T) {
	out := string(BuildPromptsBytes(nil))
	if !strings.Contains(out, "## Fix Stale Documentation") || !strings.Contains(out, "## Document New Features") {
		t.Fatal("both category headers must always render")
	}
	if strings.Count(out, "_None found._") != 2 {
		t.Fatalf("each empty category should render _None found._:\n%s", out)
	}
}

func TestBuildPromptsBytesFenceEscalation(t *testing.T) {
	body := "Here is a code block:\n```go\nfmt.Println()\n```\ndone"
	out := string(BuildPromptsBytes([]docholiday.Prompt{
		{Category: docholiday.CategoryStale, Heading: "h", Note: "n", Priority: analyzer.PrioritySmall, Body: body},
	}))
	if !strings.Contains(out, "````") {
		t.Fatalf("a body containing ``` must be wrapped in a longer fence:\n%s", out)
	}
}

func TestBuildPromptsBytesEmptyBucketsOmitted(t *testing.T) {
	out := string(BuildPromptsBytes([]docholiday.Prompt{
		{Category: docholiday.CategoryStale, Heading: "h", Note: "n", Priority: analyzer.PriorityLarge, Body: "b"},
	}))
	if strings.Contains(out, "### Medium") || strings.Contains(out, "### Small") {
		t.Fatalf("empty priority buckets must be omitted:\n%s", out)
	}
}

func TestWritePromptsCreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := WritePrompts(dir, []docholiday.Prompt{
		{Category: docholiday.CategoryStale, Heading: "h", Note: "n", Priority: analyzer.PriorityLarge, Body: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts.md")); err != nil {
		t.Fatalf("prompts.md not written: %v", err)
	}
}
```

**Step 2: Run, expect FAIL.**

**Step 3: Implement** `internal/reporter/prompts_writer.go`:

```go
package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/docholiday"
)

// WritePrompts renders the Doc Holiday prompts to prompts.md in dir.
func WritePrompts(dir string, prompts []docholiday.Prompt) error {
	return os.WriteFile(filepath.Join(dir, "prompts.md"), BuildPromptsBytes(prompts), 0o644)
}

// BuildPromptsBytes renders prompts.md: a preamble, then a "Fix Stale
// Documentation" section and a "Document New Features" section, each bucketed
// Large→Medium→Small. Empty buckets are omitted; an empty section renders
// "_None found._".
func BuildPromptsBytes(prompts []docholiday.Prompt) []byte {
	var sb strings.Builder
	sb.WriteString("# Doc Holiday Prompts\n\n")
	sb.WriteString("_Generated by Find the Gaps. Paste any block into Doc Holiday (https://doc.holiday)._\n\n")

	writePromptCategory(&sb, "Fix Stale Documentation", prompts, docholiday.CategoryStale)
	writePromptCategory(&sb, "Document New Features", prompts, docholiday.CategoryMissing)

	return []byte(sb.String())
}

func writePromptCategory(sb *strings.Builder, title string, prompts []docholiday.Prompt, cat docholiday.Category) {
	fmt.Fprintf(sb, "## %s\n\n", title)
	var any bool
	for _, p := range []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall} {
		bucket := filterPromptsByCatPriority(prompts, cat, p)
		if len(bucket) == 0 {
			continue
		}
		any = true
		fmt.Fprintf(sb, "### %s\n\n", priorityHeading(p))
		for _, pr := range bucket {
			fmt.Fprintf(sb, "#### %s\n\n", pr.Heading)
			if pr.Note != "" {
				fmt.Fprintf(sb, "_%s_\n\n", pr.Note)
			}
			fence := fenceFor(pr.Body)
			fmt.Fprintf(sb, "%s\n%s\n%s\n\n", fence, pr.Body, fence)
		}
	}
	if !any {
		sb.WriteString("_None found._\n\n")
	}
}

func filterPromptsByCatPriority(prompts []docholiday.Prompt, cat docholiday.Category, p analyzer.Priority) []docholiday.Prompt {
	var out []docholiday.Prompt
	for _, pr := range prompts {
		if pr.Category == cat && pr.Priority == p {
			out = append(out, pr)
		}
	}
	return out
}

// fenceFor returns a backtick fence at least one tick longer than the longest
// run of backticks inside body, so a body containing ``` stays a valid block.
func fenceFor(body string) string {
	longest, run := 0, 0
	for _, r := range body {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := 3
	if longest+1 > n {
		n = longest + 1
	}
	return strings.Repeat("`", n)
}
```

**Step 4: Run, expect PASS. Step 5: Commit** (`feat(reporter): render prompts.md with fence escalation + priority buckets`).

---

## Task 10: Reports-block count helper + CLI wiring in `analyze.go`

**Files:**
- Modify: `internal/cli/analyze.go`
- Test: `internal/cli/analyze_test.go` (or the existing analyze test file) for the count helper; the full-phase behavior is covered by the testscript task.

**Step 1: Write the failing test** for the count helper (add to an existing `_test.go` in `internal/cli`, e.g. a new `prompts_report_test.go`):

```go
package cli

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/docholiday"
)

func TestPromptsCounts(t *testing.T) {
	got := promptsCounts([]docholiday.Prompt{
		{Category: docholiday.CategoryStale, Priority: analyzer.PriorityLarge},
		{Category: docholiday.CategoryStale, Priority: analyzer.PrioritySmall},
		{Category: docholiday.CategoryMissing, Priority: analyzer.PriorityLarge},
	})
	if got != "2 stale · 1 new" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptsCountsEmpty(t *testing.T) {
	if got := promptsCounts(nil); got != "0" {
		t.Fatalf("got %q", got)
	}
}
```

**Step 2: Run, expect FAIL.**

**Step 3: Implement the count helper** (add near the other `*Counts` helpers in `internal/cli`):

```go
// promptsCounts summarizes the prompts.md reports-block annotation.
func promptsCounts(prompts []docholiday.Prompt) string {
	if len(prompts) == 0 {
		return "0"
	}
	var stale, missing int
	for _, p := range prompts {
		if p.Category == docholiday.CategoryMissing {
			missing++
		} else {
			stale++
		}
	}
	return fmt.Sprintf("%d stale · %d new", stale, missing)
}
```

Add the import `"github.com/sandgardenhq/find-the-gaps/internal/docholiday"` to the file that defines it.

**Step 4: Run the helper test, expect PASS. Commit the helper.**

**Step 5: Wire the phase into `analyze.go`.** Insert after the drift block closes (after `analyze.go:601`, before the screenshot block at `:603`). Use the variables confirmed in scope: `ctx`, `tiering`, `projectDir`, `workers`, `noCache`, `driftFindings`, plus the undocumented features + rationales. Note `undocFeatures` and `whyRationales` are currently scoped **inside** the `if !driftSkipped {` block (around `:508`/`:509`); recompute them here from `featureMap` + `docCoveredFeatures` so the phase runs even when drift was cached-complete:

```go
// Doc Holiday prompt generation: author ready-to-paste prompts for the
// stale-docs and missing-docs gaps. Default-on; reuses the typical tier and a
// per-unit prompts-cache.json.
promptUndoc := reporter.UndocumentedFeatures(featureMap, docCoveredFeatures)
promptInput := docholiday.Input{
	Drift:        driftFindings,
	Undocumented: promptUndoc,
	Rationales:   whyRationales, // see note below
}
prompts, err := docholiday.GeneratePrompts(ctx, tiering.Typical(), promptInput, docholiday.Options{
	ProjectDir: projectDir,
	Workers:    workers,
	NoCache:    noCache,
})
if err != nil {
	return fmt.Errorf("generate doc-holiday prompts: %w", err)
}
if err := reporter.WritePrompts(projectDir, prompts); err != nil {
	return fmt.Errorf("write prompts.md: %w", err)
}
```

> **Rationales scope fix:** `whyRationales` is declared inside the `if !driftSkipped {` block today. Hoist its declaration so it is visible at the prompt-gen site (declare `whyRationales := map[string]string{}` before the `if !driftSkipped {` block, and have the block populate the same map instead of shadowing it). If hoisting is awkward, pass `Rationales: nil` for the first version — the missing-doc prompt simply omits the "why it matters" line, and a follow-up commit can thread the map. Prefer hoisting; it is a two-line change.

**Step 6: Add the `prompts.md` reports line.** At `analyze.go:855-866`, mirror the `pdfLine` pattern:

```go
promptsLine := "  " + projectDir + "/prompts.md"
if c := promptsCounts(prompts); c != "" {
	promptsLine += " (" + c + ")"
}
```

Then extend the final `Fprintf` (currently ending `...%s%s\n", ..., pdfLine, extraLine)`) to include `promptsLine` — add one `\n%s` verb directly after the `pdfLine` verb and pass `promptsLine` in the matching argument position. Verify the verb count equals the argument count after editing.

**Step 7: Build + run the full suite.**

Run: `go build ./... && go test ./internal/cli/... -run TestPrompts -v`
Expected: PASS. Then `go test ./...`.

**Step 8: Commit** (`feat(cli): run doc-holiday prompt phase in analyze + reports line`).

---

## Task 11: `ftg render` re-emits `prompts.md` from cache

**Files:**
- Modify: `internal/cli/render.go`
- Test: covered by the testscript task (render path) + a focused unit test if practical.

**Step 1:** In `render.go`, after the existing reporter re-emit calls (near `render.go:131-144`), add a cache-gated re-emit mirroring how screenshots are gated:

```go
if prompts, ok := docholiday.LoadCachedPrompts(projectDir); ok {
	if err := reporter.WritePrompts(projectDir, prompts); err != nil {
		return fmt.Errorf("write prompts: %w", err)
	}
}
```

Add the `docholiday` import. No LLM calls occur — `LoadCachedPrompts` only reads `prompts-cache.json`.

**Step 2:** Build + run: `go build ./... && go test ./internal/cli/...`. Commit (`feat(cli): re-emit prompts.md in ftg render from cache`).

---

## Task 12: End-to-end testscript

**Files:**
- Create: `cmd/ftg/testdata/prompts.txtar` (follow the shape of an existing `*.txtar`; inspect `cmd/ftg/testdata/` and `cmd/ftg/main_test.go` for the harness, env, and any fake-LLM hooks the suite already uses).

**Step 1:** Read an existing analyze-oriented `.txtar` and the test harness in `cmd/ftg/main_test.go` to learn how the suite stubs the LLM and docs ingestion (the suite must already do this offline — find and reuse that mechanism; do **not** introduce a network or real Bifrost call).

**Step 2:** Write a scenario that runs `ftg analyze` against a tiny fixture with at least one drift finding and one undocumented feature, then asserts:
- `exists prompts.md`
- `stdout` contains `prompts.md (` (the reports-block line)
- `grep '# Doc Holiday Prompts' prompts.md`
- a second `ftg render` invocation re-creates `prompts.md` (delete it, render, assert it exists again)

**Step 3:** Run: `go test ./cmd/ftg/... -run TestScripts/prompts -v` (adjust the test name to the harness). Expected: PASS.

**Step 4:** Commit (`test(cli): e2e testscript for prompts.md generation + render`).

---

## Task 13: Verification scenario + docs

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md` (add Scenario 20)
- Modify: `README.md` (document the new `prompts.md` artifact under the artifacts/output section)
- Modify: `CHANGELOG.md` (add an entry)
- Modify: `PROGRESS.md` (per CLAUDE.md task-completion rule)

**Step 1:** Add **Scenario 20: Doc Holiday Prompt Generation** to `.plans/VERIFICATION_PLAN.md`, in the established style. Cover:
- Default-on: a normal `analyze` run writes `prompts.md`; the stdout `reports:` block lists `prompts.md (N stale · M new)`.
- Content: every stale prompt names a real doc page; every "New page:" prompt names a real undocumented user-facing feature; no prompt references a page/feature absent from `mapping.md`/`gaps.md`.
- Caching: a second run logs/produces zero fresh LLM prompt calls (cache hit); `--no-cache` regenerates.
- SIGINT mid-run leaves a non-empty `prompts-cache.json`; resume regenerates only the missing units.
- Parallel/serial byte-identity after sorting (mirror Scenario 15's method).
- `--no-pdf`/site flags are unaffected (prompts.md is standalone).
- `ftg render` re-emits `prompts.md` from the cache with no LLM calls.

**Step 2:** Update `README.md`: under the outputs/"What you get" section, add `prompts.md` — ready-to-paste Doc Holiday prompts for stale + missing docs, default-on. Note Doc Holiday at https://doc.holiday.

**Step 3:** Add a `CHANGELOG.md` entry (e.g. under an Unreleased/next-version heading): "feat: generate Doc Holiday prompts (`prompts.md`) for stale and missing documentation."

**Step 4:** Update `PROGRESS.md` with the task block (timestamp, tests passing, coverage, build/lint status) per the CLAUDE.md format.

**Step 5:** Final gate before declaring done:
```bash
gofmt -w . && goimports -w .
go build ./...
go test ./... -count=1
go test -race ./internal/docholiday/...
go test -cover ./internal/docholiday/... ./internal/reporter/...
golangci-lint run
```
All must be green; `internal/docholiday` and the new reporter code ≥90% statement coverage. Commit (`docs: verification scenario 20 + README/CHANGELOG/PROGRESS for prompts.md`).

---

## Final verification checklist (CLAUDE.md task-completion gate)

- [ ] All tests pass (`go test ./... -count=1`).
- [ ] `-race` clean on `internal/docholiday`.
- [ ] Build succeeds, zero errors.
- [ ] `golangci-lint run` clean.
- [ ] `internal/docholiday` + new reporter code ≥90% statement coverage.
- [ ] Every prompt literal carries a `// PROMPT:` comment (the two embed vars + the `Complete` call site).
- [ ] `docholiday` does **not** import `reporter` (no cycle).
- [ ] `prompts.md` is default-on; appears in the reports block; `ftg render` re-emits it from cache.
- [ ] `PROGRESS.md` updated.
- [ ] Design doc `.plans/DOC_HOLIDAY_PROMPT_GEN_DESIGN.md` and this plan reflect the as-built shape (amend if anything changed).

---

## Notes on decisions deferred to implementation

1. **`whyRationales` hoist vs. nil** (Task 10): prefer hoisting the map so missing-doc prompts carry the "why it matters" line; nil is an acceptable first-pass fallback.
2. **`staleItem` priority field** (Task 2): folding `priority` into `staleItem` is cleaner than the index-aligned parallel slice; either is fine as long as the chunk-priority rollup is correct and tested.
3. **Missing-doc priority** (Task 3): currently a deterministic `PriorityLarge` via `missingDocPriority`. This is the single edit point if a future richer signal (layer, usage) is wanted — do not inline the constant.
4. **No `--no-prompts` flag** (YAGNI): add only if a user asks. The phase rides `--no-cache` and `--workers`.
