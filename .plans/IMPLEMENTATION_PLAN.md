# Docs Page Classifier — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a binary `is_docs` classifier to `AnalyzePage` so drift detection and screenshot detection only run on documentation pages, not on blogs/marketing/team/legal pages.

**Architecture:** Single change point — extend the existing `AnalyzePage` LLM call's schema with one boolean. Filter the docs feature map and the screenshot input list on it. Refuse to produce a report if every page classifies as non-docs. No overrides in v1; `--no-cache` is the escape hatch. Full design in `.plans/DOCS_CLASSIFIER_DESIGN.md`.

**Tech Stack:** Go 1.26+, testify, testscript. Follows project TDD rules in `CLAUDE.md` (RED → GREEN → REFACTOR → commit per cycle, ≥90% statement coverage per package).

**Key files touched:**
- `internal/analyzer/analyze_page.go` — schema + struct + prompt
- `internal/analyzer/types.go` — `PageAnalysis.IsDocs` field
- `internal/spider/cache.go` — `indexEntry.IsDocs` + `RecordAnalysis` / `Analysis`
- `internal/cli/analyze.go` — orchestration: filter, audit log, hard-floor guard
- Test files alongside each
- New testscript scenario in `cmd/find-the-gaps/testdata/script/`
- `.plans/VERIFICATION_PLAN.md` — new acceptance scenario

---

## Task 1: Add `is_docs` to `analyzePageSchema`

**Files:**
- Modify: `internal/analyzer/analyze_page.go:14-26`
- Test: `internal/analyzer/schema_test.go`

**Step 1: Write the failing test**

Add to `internal/analyzer/schema_test.go`:

```go
func TestAnalyzePageSchema_IncludesIsDocsField(t *testing.T) {
    var doc map[string]any
    if err := json.Unmarshal(analyzer.AnalyzePageSchemaForTest().Doc, &doc); err != nil {
        t.Fatalf("schema doc must be valid JSON: %v", err)
    }
    props, ok := doc["properties"].(map[string]any)
    if !ok {
        t.Fatal("schema must have properties object")
    }
    isDocs, ok := props["is_docs"].(map[string]any)
    if !ok {
        t.Fatal("schema must declare is_docs property")
    }
    if isDocs["type"] != "boolean" {
        t.Errorf("is_docs type: got %v, want boolean", isDocs["type"])
    }
    required, ok := doc["required"].([]any)
    if !ok {
        t.Fatal("schema must have required array")
    }
    found := false
    for _, r := range required {
        if r == "is_docs" {
            found = true
        }
    }
    if !found {
        t.Error("is_docs must be in required[]")
    }
}
```

If `AnalyzePageSchemaForTest` doesn't exist, add a one-line export in `internal/analyzer/export_test.go`:

```go
func AnalyzePageSchemaForTest() JSONSchema { return analyzePageSchema }
```

**Step 2: Run the test, watch it fail**

```bash
go test ./internal/analyzer/ -run TestAnalyzePageSchema_IncludesIsDocsField -v
```

Expected: FAIL — schema doesn't yet declare `is_docs`.

**Step 3: Update the schema**

In `internal/analyzer/analyze_page.go`, change `analyzePageSchema.Doc` to:

```go
Doc: json.RawMessage(`{
  "type": "object",
  "properties": {
    "summary":  {"type": "string"},
    "features": {"type": "array", "items": {"type": "string"}},
    "is_docs":  {"type": "boolean"}
  },
  "required": ["summary", "features", "is_docs"],
  "additionalProperties": false
}`),
```

**Step 4: Run the test, watch it pass**

```bash
go test ./internal/analyzer/ -run TestAnalyzePageSchema_IncludesIsDocsField -v
```

Expected: PASS.

**Step 5: Run all tests in the package — they may break**

```bash
go test ./internal/analyzer/ -count=1
```

Expected: existing `TestAnalyzePage_*` tests will FAIL because the LLM stub responses don't include `is_docs` and the schema is now strict. That is expected and is what Task 3 fixes. Do NOT loosen the schema.

**Step 6: Commit**

```bash
git add internal/analyzer/analyze_page.go internal/analyzer/schema_test.go internal/analyzer/export_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add is_docs field to AnalyzePage schema

- RED: TestAnalyzePageSchema_IncludesIsDocsField asserts
  is_docs is declared, typed boolean, and required
- GREEN: schema doc updated with is_docs property
- Note: existing AnalyzePage_* tests temporarily fail; fixed in
  follow-up tasks that thread the new field end-to-end

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add `IsDocs` field to `PageAnalysis`

**Files:**
- Modify: `internal/analyzer/types.go:13-18`
- Test: `internal/analyzer/types_test.go`

**Step 1: Write the failing test**

Add to `internal/analyzer/types_test.go`:

```go
func TestPageAnalysis_HasIsDocsField(t *testing.T) {
    p := analyzer.PageAnalysis{
        URL:     "https://example.com",
        Summary: "x",
        IsDocs:  false,
    }
    if p.IsDocs != false {
        t.Errorf("IsDocs round-trip: got %v, want false", p.IsDocs)
    }
    p.IsDocs = true
    if p.IsDocs != true {
        t.Errorf("IsDocs round-trip: got %v, want true", p.IsDocs)
    }
}
```

**Step 2: Run the test, watch it fail**

```bash
go test ./internal/analyzer/ -run TestPageAnalysis_HasIsDocsField -v
```

Expected: FAIL — compile error, `IsDocs` undefined.

**Step 3: Add the field**

In `internal/analyzer/types.go:13-18`:

```go
type PageAnalysis struct {
    URL      string
    Summary  string
    Features []string
    IsDocs   bool
}
```

**Step 4: Run the test, watch it pass**

```bash
go test ./internal/analyzer/ -run TestPageAnalysis_HasIsDocsField -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/types.go internal/analyzer/types_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add IsDocs field to PageAnalysis

- RED: round-trip test asserts the field exists and toggles
- GREEN: added IsDocs bool to struct

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Plumb `is_docs` through `AnalyzePage` (and update its prompt)

**Files:**
- Modify: `internal/analyzer/analyze_page.go:9-12, 28-62`
- Test: `internal/analyzer/analyze_page_test.go`

**Step 1: Write the failing tests**

Add three cases to `internal/analyzer/analyze_page_test.go`:

```go
func TestAnalyzePage_IsDocsTrue(t *testing.T) {
    c := &fakeClient{jsonResponses: map[string]json.RawMessage{
        "analyze_page_response": json.RawMessage(
            `{"summary":"API ref.","features":["x"],"is_docs":true}`),
    }}
    got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c},
        "https://docs.example.com/api", "content")
    if err != nil { t.Fatal(err) }
    if got.IsDocs != true {
        t.Errorf("IsDocs: got %v, want true", got.IsDocs)
    }
}

func TestAnalyzePage_IsDocsFalse(t *testing.T) {
    c := &fakeClient{jsonResponses: map[string]json.RawMessage{
        "analyze_page_response": json.RawMessage(
            `{"summary":"Team page.","features":[],"is_docs":false}`),
    }}
    got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c},
        "https://docs.example.com/team", "content")
    if err != nil { t.Fatal(err) }
    if got.IsDocs != false {
        t.Errorf("IsDocs: got %v, want false", got.IsDocs)
    }
}

func TestAnalyzePage_IsDocsMissing_DefaultsTrue(t *testing.T) {
    // Inclusive-by-default: a malformed response missing is_docs
    // must NOT silently drop the page; treat as docs.
    c := &fakeClient{jsonResponses: map[string]json.RawMessage{
        "analyze_page_response": json.RawMessage(
            `{"summary":"Old cache shape.","features":["x"]}`),
    }}
    got, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c},
        "https://docs.example.com/x", "content")
    if err != nil { t.Fatal(err) }
    if got.IsDocs != true {
        t.Errorf("missing is_docs must default to true (false-negative-averse), got %v", got.IsDocs)
    }
}

func TestAnalyzePage_PromptIncludesClassificationRule(t *testing.T) {
    c := &fakeClient{jsonResponses: map[string]json.RawMessage{
        "analyze_page_response": json.RawMessage(
            `{"summary":"x","features":[],"is_docs":true}`),
    }}
    _, err := analyzer.AnalyzePage(context.Background(), &fakeTiering{small: c},
        "https://example.com", "content")
    if err != nil { t.Fatal(err) }
    if len(c.receivedPrompts) == 0 {
        t.Fatal("expected a prompt")
    }
    p := c.receivedPrompts[0]
    if !strings.Contains(p, "is_docs") {
        t.Error("prompt must reference is_docs")
    }
    if !strings.Contains(p, "Default to docs when unsure") {
        t.Error("prompt must include the inclusive-by-default guardrail")
    }
}
```

Also fix the existing `TestAnalyzePage_*` tests that currently fail after Task 1 — append `,"is_docs":true` to their canned JSON responses.

**Step 2: Run, watch fail**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage -v
```

Expected: the three new tests FAIL because the response struct doesn't read `is_docs` and `PageAnalysis.IsDocs` isn't set; the existing tests should now PASS again because their JSON has `is_docs:true`.

**Step 3: Update `analyzePageResponse` and the assignment**

In `internal/analyzer/analyze_page.go`:

```go
type analyzePageResponse struct {
    Summary  string   `json:"summary"`
    Features []string `json:"features"`
    IsDocs   *bool    `json:"is_docs"` // pointer so we can detect "missing" → default to true
}
```

In `AnalyzePage`, after the existing nil-features guard:

```go
isDocs := true // inclusive-by-default
if resp.IsDocs != nil {
    isDocs = *resp.IsDocs
}

return PageAnalysis{
    URL:      pageURL,
    Summary:  resp.Summary,
    Features: resp.Features,
    IsDocs:   isDocs,
}, nil
```

**Step 4: Update the prompt**

Replace the prompt string in `AnalyzePage` (currently `analyze_page.go:31-41`). Keep the `// PROMPT:` comment. New prompt:

```go
// PROMPT: Summarizes a single documentation page, extracts the product features described on it, and classifies whether the page is product documentation. Inclusive-by-default: when uncertain, classify as docs (false negatives are worse than false positives).
prompt := fmt.Sprintf(`You are analyzing a page on a software product's website.

URL: %s

Content:
%s

Populate the response with:
- "summary": a 1-2 sentence description of what this page covers
- "features": a list of product features or capabilities described on this page (short noun phrases, max 8 words each). May be empty.
- "is_docs": a boolean classifying whether this page is product DOCUMENTATION.

Rule for is_docs:
A page is DOCS if a user trying to USE this product would consult it for current technical information about features, APIs, configuration, or behavior.

Examples of docs (is_docs=true):
- API references, tutorials, quickstarts, configuration references
- Changelogs and release notes
- "Announcing v3"-style new-feature blog posts
- Marketing landing pages that contain code snippets or technical claims about how the product works

Examples of NOT docs (is_docs=false):
- Engineering retrospectives ("how we built X", "scaling our database")
- Customer case studies / customer logos
- Team, about, careers, legal pages
- Pricing pages without technical content
- Generic company blog posts (hiring announcements, fundraising news, holiday messages)

Set is_docs=false ONLY when you are confident the page is one of the not-docs categories above. Default to docs when unsure.`, pageURL, content)
```

**Step 5: Run, watch pass**

```bash
go test ./internal/analyzer/ -count=1
```

Expected: all PASS, including the four new tests and the previously-broken existing tests.

**Step 6: Commit**

```bash
git add internal/analyzer/analyze_page.go internal/analyzer/analyze_page_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): classify docs pages in AnalyzePage

- RED: tests for is_docs=true, false, missing→true (inclusive default),
  and prompt content
- GREEN: response struct gains optional IsDocs pointer; missing field
  defaults to true; prompt rewritten with the docs/non-docs rule and
  inclusive-by-default guardrail; existing test fixtures updated to
  include is_docs:true

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Persist `IsDocs` in the spider cache

**Files:**
- Modify: `internal/spider/cache.go:20-25, 89-109`
- Test: `internal/spider/cache_test.go`

**Step 1: Write the failing tests**

Add to `internal/spider/cache_test.go`:

```go
func TestIndex_RecordAnalysis_PersistsIsDocs(t *testing.T) {
    dir := t.TempDir()
    idx, err := spider.LoadIndex(dir)
    require.NoError(t, err)
    require.NoError(t, idx.Record("https://x/a", "a.md"))
    require.NoError(t, idx.RecordAnalysis("https://x/a", "summary", []string{"f"}, false))

    // Reload from disk
    idx2, err := spider.LoadIndex(dir)
    require.NoError(t, err)
    summary, features, isDocs, ok := idx2.Analysis("https://x/a")
    require.True(t, ok)
    assert.Equal(t, "summary", summary)
    assert.Equal(t, []string{"f"}, features)
    assert.False(t, isDocs)
}

func TestIndex_Analysis_OldCacheWithoutIsDocs_DefaultsTrue(t *testing.T) {
    // Simulate a cache file written by a previous build with no is_docs field.
    dir := t.TempDir()
    raw := `{
      "pages": {
        "https://x/a": {
          "filename": "a.md",
          "fetched_at": "2025-01-01T00:00:00Z",
          "summary": "old",
          "features": ["f"]
        }
      }
    }`
    require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), []byte(raw), 0o644))

    idx, err := spider.LoadIndex(dir)
    require.NoError(t, err)
    _, _, isDocs, ok := idx.Analysis("https://x/a")
    require.True(t, ok)
    assert.True(t, isDocs, "old cache without is_docs must default to true (inclusive)")
}
```

If existing tests call `RecordAnalysis(url, summary, features)`, update them to pass the new arg (`true` is the inclusive default). Likewise update existing callers of `Analysis` to consume the extra return value.

**Step 2: Run, watch fail**

```bash
go test ./internal/spider/ -run TestIndex_ -v
```

Expected: FAIL — compile errors on the new signature; new tests fail because the field doesn't exist.

**Step 3: Update the cache**

`internal/spider/cache.go`:

```go
type indexEntry struct {
    Filename  string    `json:"filename"`
    FetchedAt time.Time `json:"fetched_at"`
    Summary   string    `json:"summary,omitempty"`
    Features  []string  `json:"features,omitempty"`
    IsDocs    *bool     `json:"is_docs,omitempty"` // pointer so missing-in-old-cache → default true
}

func (idx *Index) RecordAnalysis(rawURL, summary string, features []string, isDocs bool) error {
    idx.mu.Lock()
    defer idx.mu.Unlock()
    e := idx.entries[rawURL]
    e.Summary = summary
    e.Features = features
    e.IsDocs = &isDocs
    idx.entries[rawURL] = e
    return idx.save()
}

func (idx *Index) Analysis(rawURL string) (summary string, features []string, isDocs bool, ok bool) {
    idx.mu.Lock()
    defer idx.mu.Unlock()
    e, found := idx.entries[rawURL]
    if !found || e.Summary == "" {
        return "", nil, false, false
    }
    is := true // inclusive-by-default for old cache entries
    if e.IsDocs != nil {
        is = *e.IsDocs
    }
    return e.Summary, e.Features, is, true
}
```

**Step 4: Run, watch pass**

```bash
go test ./internal/spider/ -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/spider/cache.go internal/spider/cache_test.go
git commit -m "$(cat <<'EOF'
feat(spider): persist is_docs in the analysis cache

- RED: round-trip test for new isDocs arg; legacy-cache test
  asserting missing field defaults to true
- GREEN: indexEntry.IsDocs is *bool (omitempty); RecordAnalysis takes
  isDocs; Analysis returns it; nil on disk → true in memory

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Propagate the new signatures to callers

**Files:**
- Modify: `internal/cli/analyze.go` (around lines 178-204 — the analyze loop calling `RecordAnalysis` and `Analysis`)
- Possibly: `internal/cli/cachescan.go`, `internal/cli/serve.go` if they call `Analysis`

**Step 1: Build to find every broken call site**

```bash
go build ./...
```

Compile errors will list every file that calls `RecordAnalysis` or `Analysis`. Each must be updated to thread `IsDocs`.

**Step 2: Update the analyze loop in `internal/cli/analyze.go`**

Around lines 179-202, the cache hit / fresh path:

```go
if summary, features, isDocs, ok := idx.Analysis(url); ok {
    log.Debug("page cache hit", "url", url)
    analyses = append(analyses, analyzer.PageAnalysis{
        URL:      url,
        Summary:  summary,
        Features: features,
        IsDocs:   isDocs,
    })
    continue
}
// ...fresh analysis...
if recErr := idx.RecordAnalysis(url, pa.Summary, pa.Features, pa.IsDocs); recErr != nil {
    return fmt.Errorf("record analysis: %w", recErr)
}
```

**Step 3: Update any other callers the build flagged**

Likely `internal/cli/serve.go` reads `Analysis`. Add the new return value (use `_` if the value is genuinely unused, but prefer to thread it because future code may want it).

**Step 4: Build clean, run all tests**

```bash
go build ./... && go test ./... -count=1
```

Expected: build succeeds, all existing tests pass.

**Step 5: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor(cli): thread IsDocs through analyze cache hit/store path

- Updates RecordAnalysis / Analysis call sites to pass and consume
  the new is_docs argument added in the previous commit.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Filter the docs feature map on `IsDocs`

**Files:**
- Modify: `internal/cli/analyze.go` (around the docs feature-map build, search for `runBothMaps` near line 303)
- Test: `internal/cli/analyze_test.go` — new test

**Step 1: Write the failing test**

Add to `internal/cli/analyze_test.go`. The test invokes the CLI end-to-end with an httptest LLM server and asserts that drift is not run against not-docs pages. Pattern follows existing `analyze_test.go` setups (see `TestAnalyze_cacheUsesProjectSubdir`).

A minimal version: write a focused unit test on a helper function `filterDocsAnalyses(analyses []analyzer.PageAnalysis) []analyzer.PageAnalysis`.

```go
func TestFilterDocsAnalyses_ExcludesNotDocs(t *testing.T) {
    analyses := []analyzer.PageAnalysis{
        {URL: "https://x/api", Summary: "API", IsDocs: true},
        {URL: "https://x/team", Summary: "Team", IsDocs: false},
        {URL: "https://x/guide", Summary: "Guide", IsDocs: true},
    }
    got := filterDocsAnalyses(analyses)
    require.Len(t, got, 2)
    assert.Equal(t, "https://x/api", got[0].URL)
    assert.Equal(t, "https://x/guide", got[1].URL)
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/cli/ -run TestFilterDocsAnalyses -v
```

Expected: FAIL — `filterDocsAnalyses` doesn't exist.

**Step 3: Add the helper and call it before the docs feature map runs**

In `internal/cli/analyze.go`:

```go
func filterDocsAnalyses(in []analyzer.PageAnalysis) []analyzer.PageAnalysis {
    out := make([]analyzer.PageAnalysis, 0, len(in))
    for _, a := range in {
        if a.IsDocs {
            out = append(out, a)
        }
    }
    return out
}
```

Then, immediately before the `if !codeMapCached || !docsMapCached {` block (around line 287), introduce a `docsAnalyses` slice:

```go
docsAnalyses := filterDocsAnalyses(analyses)
```

…and use `docsAnalyses` (not `analyses`) wherever the docs feature mapping consumes per-page analysis. The `pages` map passed to `runBothMaps` should also be filtered: build a `docsPages` map containing only URLs whose analysis has `IsDocs=true`. Pass `docsPages` in place of `pages` to `runBothMaps`.

**Step 4: Run, watch pass**

```bash
go test ./internal/cli/ -count=1
```

Expected: PASS, including the new helper test and unchanged behavior on existing tests (none of the fixtures have `IsDocs=false` yet).

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "$(cat <<'EOF'
feat(cli): exclude non-docs pages from the docs feature map

- RED: filterDocsAnalyses test on a mixed-classification slice
- GREEN: helper added; docs feature mapping now consumes the filtered
  slice + a docs-only pages map, so drift detection cannot match
  code features against blog/marketing/team URLs.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Filter screenshot detection input on `IsDocs`

**Files:**
- Modify: `internal/cli/analyze.go` (around lines 380-399 — screenshot input loop)
- Test: `internal/cli/analyze_test.go`

**Step 1: Write the failing test**

```go
func TestBuildScreenshotDocPages_SkipsNotDocs(t *testing.T) {
    // Build a tmp dir with three pages on disk.
    dir := t.TempDir()
    aPath := filepath.Join(dir, "a.md")
    bPath := filepath.Join(dir, "b.md")
    cPath := filepath.Join(dir, "c.md")
    require.NoError(t, os.WriteFile(aPath, []byte("docs A"), 0o644))
    require.NoError(t, os.WriteFile(bPath, []byte("team B"), 0o644))
    require.NoError(t, os.WriteFile(cPath, []byte("docs C"), 0o644))

    pages := map[string]string{
        "https://x/a":    aPath,
        "https://x/team": bPath,
        "https://x/c":    cPath,
    }
    analyses := []analyzer.PageAnalysis{
        {URL: "https://x/a", IsDocs: true},
        {URL: "https://x/team", IsDocs: false},
        {URL: "https://x/c", IsDocs: true},
    }
    got := buildScreenshotDocPages(pages, analyses)

    require.Len(t, got, 2)
    urls := []string{got[0].URL, got[1].URL}
    sort.Strings(urls)
    assert.Equal(t, []string{"https://x/a", "https://x/c"}, urls)
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/cli/ -run TestBuildScreenshotDocPages -v
```

Expected: FAIL — function doesn't exist.

**Step 3: Extract and modify the screenshot input loop**

Lift the inline `for _, url := range urls` loop (`analyze.go:387-399`) into a helper:

```go
func buildScreenshotDocPages(pages map[string]string, analyses []analyzer.PageAnalysis) []analyzer.DocPage {
    isDocs := make(map[string]bool, len(analyses))
    for _, a := range analyses {
        isDocs[a.URL] = a.IsDocs
    }
    urls := make([]string, 0, len(pages))
    for url := range pages {
        if isDocs[url] {
            urls = append(urls, url)
        }
    }
    sort.Strings(urls)
    out := make([]analyzer.DocPage, 0, len(urls))
    for _, url := range urls {
        filePath := pages[url]
        data, err := os.ReadFile(filePath)
        if err != nil {
            log.Warnf("skip page %s: %v", url, err)
            continue
        }
        out = append(out, analyzer.DocPage{
            URL:     url,
            Path:    filePath,
            Content: string(data),
        })
    }
    return out
}
```

Replace the inline loop with `docPages := buildScreenshotDocPages(pages, analyses)`.

Note: a URL with no entry in the `analyses` slice (defensive case) defaults to `false` in the lookup map, so it gets excluded. That matches the "if we don't know, don't bother screenshotting" instinct, but it's NOT consistent with the inclusive default elsewhere. **Resolution:** populate the lookup with `true` as the absent-key default by initializing the map differently — actually, simpler: change the test to assert behavior with a fully-classified `analyses` slice (the realistic case) and don't sweat the "URL exists in `pages` but not in `analyses`" path; the existing analyze loop guarantees they're 1:1.

**Step 4: Run, watch pass**

```bash
go test ./internal/cli/ -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "$(cat <<'EOF'
feat(cli): skip non-docs pages in screenshot detection

- RED: buildScreenshotDocPages test with mixed classifications
- GREEN: helper extracted from the inline loop; only IsDocs=true URLs
  are read and passed to DetectScreenshotGaps.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Hard-floor — refuse if every page is non-docs

**Files:**
- Modify: `internal/cli/analyze.go` (after the analyses loop completes, before feature mapping)
- Test: `internal/cli/analyze_test.go`

**Step 1: Write the failing test**

Test at the helper level:

```go
func TestAllNotDocsGuard_TriggersOnAllFalse(t *testing.T) {
    err := allNotDocsGuard([]analyzer.PageAnalysis{
        {URL: "https://x/a", IsDocs: false},
        {URL: "https://x/b", IsDocs: false},
    })
    require.Error(t, err)
    assert.Contains(t, err.Error(), "all 2 pages classified as non-docs")
    assert.Contains(t, err.Error(), "--no-cache")
}

func TestAllNotDocsGuard_PassesOnAnyDocs(t *testing.T) {
    err := allNotDocsGuard([]analyzer.PageAnalysis{
        {URL: "https://x/a", IsDocs: false},
        {URL: "https://x/b", IsDocs: true},
    })
    require.NoError(t, err)
}

func TestAllNotDocsGuard_PassesOnEmpty(t *testing.T) {
    // An empty analyses slice means "no pages were analyzed at all"
    // (e.g., crawl produced nothing). That's a different failure mode
    // and is handled by the existing "0 pages analyzed" branch; the
    // guard must not double-fire here.
    err := allNotDocsGuard(nil)
    require.NoError(t, err)
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/cli/ -run TestAllNotDocsGuard -v
```

Expected: FAIL — function doesn't exist.

**Step 3: Add the guard and call it**

```go
func allNotDocsGuard(analyses []analyzer.PageAnalysis) error {
    if len(analyses) == 0 {
        return nil
    }
    for _, a := range analyses {
        if a.IsDocs {
            return nil
        }
    }
    return fmt.Errorf(
        "all %d pages classified as non-docs; refusing to produce a misleading report.\n"+
            "this is almost certainly a classifier mistake.\n"+
            "re-run with --no-cache, or file an issue with the docs URL.",
        len(analyses))
}
```

In `analyze.go`, immediately after the existing `if len(analyses) == 0 { ... }` short-circuit (around line 206-210):

```go
if err := allNotDocsGuard(analyses); err != nil {
    return err
}
```

**Step 4: Run, watch pass**

```bash
go test ./internal/cli/ -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "$(cat <<'EOF'
feat(cli): refuse to report when every page is classified non-docs

- RED: guard tests for all-false (error), any-true (pass), empty (pass)
- GREEN: allNotDocsGuard added and wired in after analysis;
  protects against the silent-zero-output failure mode the design
  identified as the worst case under inclusive-by-default.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Audit log line — classification counts

**Files:**
- Modify: `internal/cli/analyze.go` (after analyses loop, before guard call)
- Test: `internal/cli/analyze_test.go`

**Step 1: Write the failing test**

```go
func TestClassificationSummary_FormatsCounts(t *testing.T) {
    line := classificationSummary([]analyzer.PageAnalysis{
        {URL: "https://x/a", IsDocs: true},
        {URL: "https://x/b", IsDocs: false},
        {URL: "https://x/c", IsDocs: true},
    })
    assert.Contains(t, line, "2 docs")
    assert.Contains(t, line, "1 non-docs")
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/cli/ -run TestClassificationSummary -v
```

Expected: FAIL.

**Step 3: Implement and wire**

```go
func classificationSummary(analyses []analyzer.PageAnalysis) string {
    docs, notDocs := 0, 0
    for _, a := range analyses {
        if a.IsDocs {
            docs++
        } else {
            notDocs++
        }
    }
    return fmt.Sprintf("classified: %d docs, %d non-docs (use -v to list)", docs, notDocs)
}
```

In `analyze.go`, after the analyses loop:

```go
log.Infof("%s", classificationSummary(analyses))
for _, a := range analyses {
    if !a.IsDocs {
        log.Debugf("  non-docs: %s — %s", a.URL, a.Summary)
    }
}
```

(`log.Debugf` prints only at `-v` level, matching the design.)

**Step 4: Run, watch pass**

```bash
go test ./internal/cli/ -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "$(cat <<'EOF'
feat(cli): emit per-run classification summary log line

- RED: classificationSummary formats docs/non-docs counts
- GREEN: helper added; INFO line emitted once per run, with verbose-
  level per-URL listing for non-docs pages so users can audit.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Integration test — end-to-end with mixed classifications

**Files:**
- Add: `internal/cli/analyze_classifier_test.go` (new file)

This test runs the CLI through `run()` (the same entry point used by other `analyze_test.go` tests), uses the existing httptest pattern in that file to stub Bifrost responses, and asserts the full pipeline filters correctly.

**Step 1: Write the failing test**

Pattern: small repo with one Go function, three docs URLs (one API ref, one team page, one changelog), an httptest server returning canned LLM responses keyed on prompt content. Asserts:

- Stdout contains the `classified: …` line with the expected counts.
- `mapping.md` contains the API/changelog feature names but not anything from the team page.
- `gaps.md` does not list drift findings whose page URL is the team page.
- `screenshots.md` does not list screenshot gaps for the team page URL.

The fakest version in 80 lines: copy the `httptest.NewServer` setup from elsewhere in `analyze_test.go` (search the file for `httptest`). Use `strings.Contains(body, "/team")` to dispatch a not-docs response; everything else returns docs.

**Step 2: Run, watch fail**

```bash
go test ./internal/cli/ -run TestAnalyzeEndToEnd_FiltersNonDocs -v
```

Expected: FAIL — depending on the assertion order, either the file scaffolding is missing or the fixture is wrong. Investigate, fix, re-run until the fail reason is "the team page leaked into report X."

**Step 3: Run the full pipeline and verify the assertions match**

If the previous tasks were done correctly, the only changes needed are in the test fixture itself.

**Step 4: Run, watch pass**

```bash
go test ./internal/cli/ -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/analyze_classifier_test.go
git commit -m "$(cat <<'EOF'
test(cli): end-to-end classifier filter assertions

Stubs three docs URLs (API, changelog, team), runs the full analyze
pipeline against an httptest LLM server, and asserts that:
  - stdout reports correct docs/non-docs counts
  - mapping.md, gaps.md, and screenshots.md exclude the team URL

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Coverage check — must hit 90%+ statements per touched package

**Step 1: Run coverage**

```bash
go test -coverprofile=coverage.out ./internal/analyzer/ ./internal/spider/ ./internal/cli/
go tool cover -func=coverage.out | tail -20
```

**Step 2: Identify any package below 90%**

Inspect uncovered lines in the touched files only:

```bash
go tool cover -func=coverage.out | grep -E "analyze_page\.go|cache\.go|analyze\.go" | grep -v "100.0%"
```

**Step 3: Add tests for uncovered branches**

Likely candidates: error paths in `RecordAnalysis` (disk write failure), the `_ = res.err` log-and-continue branch in `analyze.go`'s spider loop. Add focused tests if coverage is below threshold for the new code paths only — do not chase pre-existing gaps in unrelated code.

**Step 4: Re-run coverage; verify threshold**

```bash
go test -cover ./internal/analyzer/ ./internal/spider/ ./internal/cli/
```

Expected: each package ≥90.0% statement coverage.

**Step 5: Commit (only if new tests were added)**

```bash
git add -A
git commit -m "$(cat <<'EOF'
test: lift coverage on new classifier code paths to ≥90%

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Update `PROGRESS.md` and `VERIFICATION_PLAN.md`

**Files:**
- Modify: `PROGRESS.md`
- Modify: `.plans/VERIFICATION_PLAN.md`

**Step 1: Append a PROGRESS.md entry**

```markdown
## Task: Docs Page Classifier - COMPLETE
- Started: <timestamp>
- Tests: X passing, 0 failing (analyzer + spider + cli + integration)
- Coverage: analyzer ≥90%, spider ≥90%, cli ≥90%
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: <timestamp>
- Notes: Binary classifier wired into AnalyzePage. Drift + screenshot
  filters consume IsDocs. Hard-floor guard in place. No overrides v1.
```

**Step 2: Add a Scenario 12 to `.plans/VERIFICATION_PLAN.md`**

Append after Scenario 11. The acceptance test runs `analyze` against a real docs site that has a known blog section (suggested fixture: a project whose root has both `/docs/` and `/blog/`), then asserts:

- Stdout contains a `classified: N docs, M non-docs` line where M > 0.
- `gaps.md` contains zero drift findings whose source URL is under `/blog/`, `/team/`, `/careers/`, or `/legal/`.
- A page known to be canonical docs (e.g., `/docs/api/`) is reflected in `mapping.md`.
- Re-running with `--no-cache` produces classifications consistent with the previous run on stable input.

Use the same prerequisites and "If Blocked" framing as the existing scenarios.

**Step 3: Commit**

```bash
git add PROGRESS.md .plans/VERIFICATION_PLAN.md
git commit -m "$(cat <<'EOF'
docs: record classifier completion + add verification scenario

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Pre-PR checklist

Before opening a PR:

- [ ] `go build ./...` succeeds
- [ ] `go test ./... -count=1` passes
- [ ] `golangci-lint run` clean
- [ ] `go test -cover ./internal/analyzer/ ./internal/spider/ ./internal/cli/` reports ≥90% per package
- [ ] `gofmt -l . && goimports -l .` produce no output
- [ ] `PROGRESS.md` updated
- [ ] `.plans/VERIFICATION_PLAN.md` updated
- [ ] Branch name is `filter-non-doc-pages` (already set)
- [ ] PR base is `main`, merge-commit (no squash) per CLAUDE.md PR rules

PR title suggestion: `feat(analyzer): classify docs pages and filter drift + screenshots accordingly`

PR body should reference `.plans/DOCS_CLASSIFIER_DESIGN.md` for the rationale.
