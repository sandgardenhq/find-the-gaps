# LLM Analysis Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Analyze crawled documentation pages through an LLM to extract per-page summaries and features, synthesize a product description, map features to code symbols, and produce `mapping.md` + `gaps.md` reports.

**Architecture:** Two new packages: `internal/analyzer` (all LLM calls, behind a `LLMClient` interface) and `internal/reporter` (writes output files). `internal/spider/cache.go` is extended so `index.json` persists analysis results alongside crawl metadata. The `analyze` CLI command (`internal/cli/analyze.go`) orchestrates all stages after crawling. Unit tests use a `fakeClient`; integration tests (tagged `//go:build integration`) use the real Bifrost client.

**Tech Stack:** Go 1.26+, `github.com/maximhq/bifrost/core` + `github.com/maximhq/bifrost/core/schemas` (LLM gateway), `encoding/json` for structured prompt responses, standard `testing` package, `//go:build integration` build tag for real-LLM tests.

---

## Reference: Bifrost SDK

Install:
```bash
go get github.com/maximhq/bifrost/core
go get github.com/maximhq/bifrost/core/schemas
```

Initialize and call:
```go
import (
    "github.com/maximhq/bifrost/core"
    "github.com/maximhq/bifrost/core/schemas"
)

client, err := bifrost.Init(ctx, schemas.BifrostConfig{Account: &myAccount{}})
// myAccount implements GetConfiguredProviders, GetKeysForProvider, GetConfigForProvider

response, err := client.ChatCompletionRequest(ctx, &schemas.BifrostChatRequest{
    Provider: schemas.Anthropic,
    Model:    "claude-3-5-sonnet-20241022",
    Input:    []schemas.ChatMessage{{
        Role:    schemas.ChatMessageRoleUser,
        Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("your prompt")},
    }},
})
text := *response.Choices[0].Message.Content.ContentStr
```

Env var: `ANTHROPIC_API_KEY` (read in `myAccount.GetKeysForProvider`).

---

## Task 1: Install Bifrost SDK + data types + LLMClient interface

**Files:**
- Create: `internal/analyzer/types.go`
- Create: `internal/analyzer/client.go`
- Create: `internal/analyzer/testhelpers_test.go`
- Create: `internal/analyzer/analyzer_test.go`
- Modify: `go.mod` / `go.sum` (via `go get`)

**Step 1: Install SDK**

```bash
cd /Users/brittcrawford/workspace/find-the-gaps/.worktrees/feat-llm-analysis
go get github.com/maximhq/bifrost/core
go get github.com/maximhq/bifrost/core/schemas
go mod tidy
```

Expected: `go.mod` updated with `github.com/maximhq/bifrost` entries.

**Step 2: Write the failing test**

Create `internal/analyzer/analyzer_test.go`:

```go
package analyzer_test

import (
    "testing"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestTypes_PageAnalysis(t *testing.T) {
    pa := analyzer.PageAnalysis{
        URL:      "https://docs.example.com/install",
        Summary:  "Covers installation.",
        Features: []string{"Homebrew install", "go install"},
    }
    if pa.URL == "" {
        t.Fatal("URL must not be empty")
    }
    if len(pa.Features) != 2 {
        t.Fatalf("expected 2 features, got %d", len(pa.Features))
    }
}

func TestTypes_ProductSummary(t *testing.T) {
    ps := analyzer.ProductSummary{
        Description: "A CLI tool for finding doc gaps.",
        Features:    []string{"gap analysis", "doctor command"},
    }
    if len(ps.Features) == 0 {
        t.Fatal("features must not be empty")
    }
}

func TestTypes_FeatureMap(t *testing.T) {
    fm := analyzer.FeatureMap{
        {Feature: "gap analysis", Files: []string{"internal/analyzer/analyzer.go"}, Symbols: []string{"AnalyzePage"}},
    }
    if len(fm) != 1 {
        t.Fatalf("expected 1 entry, got %d", len(fm))
    }
}

func TestLLMClient_FakeImplementsInterface(t *testing.T) {
    var _ analyzer.LLMClient = &fakeClient{}
}
```

Also create `internal/analyzer/testhelpers_test.go`:

```go
package analyzer_test

import "context"

// fakeClient is a test double for analyzer.LLMClient.
type fakeClient struct {
    responses []string // popped in order; last entry repeated when exhausted
    callCount int
    forcedErr error
}

func (f *fakeClient) Complete(_ context.Context, _ string) (string, error) {
    if f.forcedErr != nil {
        return "", f.forcedErr
    }
    if len(f.responses) == 0 {
        return "", nil
    }
    idx := f.callCount
    if idx >= len(f.responses) {
        idx = len(f.responses) - 1
    }
    f.callCount++
    return f.responses[idx], nil
}
```

**Step 3: Run — expect FAIL**

```bash
go test ./internal/analyzer/...
```

Expected: `cannot find package "github.com/sandgardenhq/find-the-gaps/internal/analyzer"`.

**Step 4: Create `internal/analyzer/types.go`**

```go
package analyzer

// PageAnalysis is the LLM-extracted summary and feature list for one documentation page.
type PageAnalysis struct {
    URL      string
    Summary  string
    Features []string
}

// ProductSummary is the synthesized product description and deduplicated feature list.
type ProductSummary struct {
    Description string
    Features    []string
}

// FeatureEntry maps one product feature to the code files and symbols that implement it.
type FeatureEntry struct {
    Feature string
    Files   []string
    Symbols []string
}

// FeatureMap is the complete feature-to-code mapping for a project.
type FeatureMap []FeatureEntry
```

**Step 5: Create `internal/analyzer/client.go`**

```go
package analyzer

import "context"

// LLMClient sends a prompt and returns the completion text.
// The real implementation wraps the Bifrost SDK; unit tests use a fake.
type LLMClient interface {
    Complete(ctx context.Context, prompt string) (string, error)
}
```

**Step 6: Run — expect PASS**

```bash
go test ./internal/analyzer/...
```

Expected: all 4 tests pass.

**Step 7: Commit**

```bash
git add internal/analyzer/ go.mod go.sum
git commit -m "feat(analyzer): data types + LLMClient interface + Bifrost SDK

- RED: TestTypes_PageAnalysis, TestTypes_ProductSummary, TestTypes_FeatureMap, TestLLMClient_FakeImplementsInterface
- GREEN: types.go, client.go, Bifrost added to go.mod
- Status: 4 tests passing, build successful"
```

---

## Task 2: AnalyzePage

**Files:**
- Create: `internal/analyzer/analyze_page.go`
- Create: `internal/analyzer/analyze_page_test.go`

**Step 1: Write the failing test**

Create `internal/analyzer/analyze_page_test.go`:

```go
package analyzer_test

import (
    "context"
    "errors"
    "testing"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestAnalyzePage_ExtractsSummaryAndFeatures(t *testing.T) {
    c := &fakeClient{responses: []string{
        `{"summary":"Covers Homebrew install.","features":["Homebrew install","go install"]}`,
    }}

    got, err := analyzer.AnalyzePage(context.Background(), c, "https://docs.example.com/install", "# Install\nUse brew.")
    if err != nil {
        t.Fatal(err)
    }
    if got.URL != "https://docs.example.com/install" {
        t.Errorf("URL: got %q", got.URL)
    }
    if got.Summary != "Covers Homebrew install." {
        t.Errorf("Summary: got %q", got.Summary)
    }
    if len(got.Features) != 2 || got.Features[0] != "Homebrew install" {
        t.Errorf("Features: got %v", got.Features)
    }
}

func TestAnalyzePage_EmptyFeatures_OK(t *testing.T) {
    c := &fakeClient{responses: []string{`{"summary":"A page.","features":[]}`}}
    got, err := analyzer.AnalyzePage(context.Background(), c, "https://example.com", "content")
    if err != nil {
        t.Fatal(err)
    }
    if len(got.Features) != 0 {
        t.Errorf("expected empty features, got %v", got.Features)
    }
}

func TestAnalyzePage_ClientError_Propagates(t *testing.T) {
    c := &fakeClient{forcedErr: errors.New("timeout")}
    _, err := analyzer.AnalyzePage(context.Background(), c, "https://example.com", "content")
    if err == nil {
        t.Fatal("expected error")
    }
}

func TestAnalyzePage_InvalidJSON_ReturnsError(t *testing.T) {
    c := &fakeClient{responses: []string{"not json"}}
    _, err := analyzer.AnalyzePage(context.Background(), c, "https://example.com", "content")
    if err == nil {
        t.Fatal("expected error for invalid JSON")
    }
}
```

**Step 2: Run — expect FAIL**

```bash
go test ./internal/analyzer/...
```

Expected: `undefined: analyzer.AnalyzePage`.

**Step 3: Create `internal/analyzer/analyze_page.go`**

```go
package analyzer

import (
    "context"
    "encoding/json"
    "fmt"
)

type analyzePageResponse struct {
    Summary  string   `json:"summary"`
    Features []string `json:"features"`
}

// AnalyzePage sends doc page content to the LLM and returns a summary and feature list.
func AnalyzePage(ctx context.Context, client LLMClient, pageURL, content string) (PageAnalysis, error) {
    // PROMPT: Summarizes a single documentation page and extracts the product features or capabilities described on it. Responds with JSON only.
    prompt := fmt.Sprintf(`You are analyzing a documentation page for a software product.

URL: %s

Content:
%s

Return a JSON object with exactly these fields:
- "summary": a 1-2 sentence description of what this page covers
- "features": a list of product features or capabilities described on this page (short noun phrases, max 8 words each)

Respond with only the JSON object. No markdown code fences. No prose.`, pageURL, content)

    raw, err := client.Complete(ctx, prompt)
    if err != nil {
        return PageAnalysis{}, fmt.Errorf("AnalyzePage %s: %w", pageURL, err)
    }

    var resp analyzePageResponse
    if err := json.Unmarshal([]byte(raw), &resp); err != nil {
        return PageAnalysis{}, fmt.Errorf("AnalyzePage %s: invalid JSON response: %w", pageURL, err)
    }

    if resp.Features == nil {
        resp.Features = []string{}
    }

    return PageAnalysis{
        URL:      pageURL,
        Summary:  resp.Summary,
        Features: resp.Features,
    }, nil
}
```

**Step 4: Run — expect PASS**

```bash
go test ./internal/analyzer/...
```

Expected: all 8 tests pass.

**Step 5: Commit**

```bash
git add internal/analyzer/analyze_page.go internal/analyzer/analyze_page_test.go
git commit -m "feat(analyzer): AnalyzePage with PROMPT comment

- RED: 4 TestAnalyzePage_* tests
- GREEN: analyze_page.go with // PROMPT: comment above LLM prompt
- Status: 8 tests passing, build successful"
```

---

## Task 3: SynthesizeProduct

**Files:**
- Create: `internal/analyzer/synthesize.go`
- Create: `internal/analyzer/synthesize_test.go`

**Step 1: Write the failing test**

Create `internal/analyzer/synthesize_test.go`:

```go
package analyzer_test

import (
    "context"
    "errors"
    "testing"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestSynthesizeProduct_ReturnsDescriptionAndFeatures(t *testing.T) {
    c := &fakeClient{responses: []string{
        `{"description":"A CLI for doc gap detection.","features":["gap analysis","doctor command","Homebrew install"]}`,
    }}

    pages := []analyzer.PageAnalysis{
        {URL: "https://example.com/install", Summary: "Covers install.", Features: []string{"Homebrew install"}},
        {URL: "https://example.com/usage", Summary: "Covers usage.", Features: []string{"gap analysis", "doctor command"}},
    }

    got, err := analyzer.SynthesizeProduct(context.Background(), c, pages)
    if err != nil {
        t.Fatal(err)
    }
    if got.Description == "" {
        t.Error("Description must not be empty")
    }
    if len(got.Features) == 0 {
        t.Error("Features must not be empty")
    }
}

func TestSynthesizeProduct_SinglePage_OK(t *testing.T) {
    c := &fakeClient{responses: []string{`{"description":"One page product.","features":["one feature"]}`}}
    pages := []analyzer.PageAnalysis{{URL: "https://example.com", Summary: "One page.", Features: []string{"one feature"}}}
    _, err := analyzer.SynthesizeProduct(context.Background(), c, pages)
    if err != nil {
        t.Fatal(err)
    }
}

func TestSynthesizeProduct_ClientError_Propagates(t *testing.T) {
    c := &fakeClient{forcedErr: errors.New("network down")}
    _, err := analyzer.SynthesizeProduct(context.Background(), c, []analyzer.PageAnalysis{
        {URL: "https://example.com", Summary: "page.", Features: nil},
    })
    if err == nil {
        t.Fatal("expected error")
    }
}

func TestSynthesizeProduct_InvalidJSON_ReturnsError(t *testing.T) {
    c := &fakeClient{responses: []string{"oops"}}
    _, err := analyzer.SynthesizeProduct(context.Background(), c, []analyzer.PageAnalysis{
        {URL: "https://example.com", Summary: "page.", Features: nil},
    })
    if err == nil {
        t.Fatal("expected error for invalid JSON")
    }
}
```

**Step 2: Run — expect FAIL**

```bash
go test ./internal/analyzer/...
```

Expected: `undefined: analyzer.SynthesizeProduct`.

**Step 3: Create `internal/analyzer/synthesize.go`**

```go
package analyzer

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
)

type synthesizeResponse struct {
    Description string   `json:"description"`
    Features    []string `json:"features"`
}

// SynthesizeProduct combines all per-page analyses into a product summary and
// a deduplicated feature list.
func SynthesizeProduct(ctx context.Context, client LLMClient, pages []PageAnalysis) (ProductSummary, error) {
    var sb strings.Builder
    for _, p := range pages {
        fmt.Fprintf(&sb, "URL: %s\nSummary: %s\nFeatures: %s\n\n",
            p.URL, p.Summary, strings.Join(p.Features, ", "))
    }

    // PROMPT: Synthesizes a product-level description and a deduplicated feature list from all documentation page summaries. Responds with JSON only.
    prompt := fmt.Sprintf(`You are analyzing documentation for a software product.

Here are summaries and features extracted from individual documentation pages:

%s
Based on the above, return a JSON object with exactly these fields:
- "description": a 2-3 sentence summary of what this product is and what it does
- "features": a deduplicated, sorted list of all product features and capabilities (short noun phrases, max 8 words each)

Respond with only the JSON object. No markdown code fences. No prose.`, sb.String())

    raw, err := client.Complete(ctx, prompt)
    if err != nil {
        return ProductSummary{}, fmt.Errorf("SynthesizeProduct: %w", err)
    }

    var resp synthesizeResponse
    if err := json.Unmarshal([]byte(raw), &resp); err != nil {
        return ProductSummary{}, fmt.Errorf("SynthesizeProduct: invalid JSON response: %w", err)
    }

    if resp.Features == nil {
        resp.Features = []string{}
    }

    return ProductSummary{Description: resp.Description, Features: resp.Features}, nil
}
```

**Step 4: Run — expect PASS**

```bash
go test ./internal/analyzer/...
```

Expected: all 12 tests pass.

**Step 5: Commit**

```bash
git add internal/analyzer/synthesize.go internal/analyzer/synthesize_test.go
git commit -m "feat(analyzer): SynthesizeProduct with PROMPT comment

- RED: 4 TestSynthesizeProduct_* tests
- GREEN: synthesize.go with // PROMPT: comment
- Status: 12 tests passing, build successful"
```

---

## Task 4: MapFeaturesToCode

**Files:**
- Create: `internal/analyzer/mapper.go`
- Create: `internal/analyzer/mapper_test.go`

The mapper takes a feature list and a `*scanner.ProjectScan` (the codebase). It builds a compact symbol list from the scan and sends it to the LLM with the features, asking for a JSON mapping.

**Step 1: Write the failing test**

Create `internal/analyzer/mapper_test.go`:

```go
package analyzer_test

import (
    "context"
    "errors"
    "testing"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
    "github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestMapFeaturesToCode_ReturnsMappings(t *testing.T) {
    c := &fakeClient{responses: []string{
        `[{"feature":"gap analysis","files":["internal/analyzer/analyzer.go"],"symbols":["AnalyzePage"]},{"feature":"doctor command","files":["internal/cli/doctor.go"],"symbols":["RunDoctor"]}]`,
    }}

    scan := &scanner.ProjectScan{
        Files: []scanner.ScannedFile{
            {Path: "internal/analyzer/analyzer.go", Language: "go", Symbols: []scanner.Symbol{{Name: "AnalyzePage"}}},
            {Path: "internal/cli/doctor.go", Language: "go", Symbols: []scanner.Symbol{{Name: "RunDoctor"}}},
        },
    }

    features := []string{"gap analysis", "doctor command"}
    got, err := analyzer.MapFeaturesToCode(context.Background(), c, features, scan)
    if err != nil {
        t.Fatal(err)
    }
    if len(got) != 2 {
        t.Fatalf("expected 2 entries, got %d", len(got))
    }
    if got[0].Feature != "gap analysis" {
        t.Errorf("Feature[0]: got %q", got[0].Feature)
    }
    if len(got[0].Files) == 0 {
        t.Error("Files must not be empty for gap analysis")
    }
}

func TestMapFeaturesToCode_EmptyFeatures_ReturnsEmpty(t *testing.T) {
    c := &fakeClient{responses: []string{`[]`}}
    got, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{}, &scanner.ProjectScan{})
    if err != nil {
        t.Fatal(err)
    }
    if len(got) != 0 {
        t.Errorf("expected empty map, got %v", got)
    }
}

func TestMapFeaturesToCode_ClientError_Propagates(t *testing.T) {
    c := &fakeClient{forcedErr: errors.New("llm down")}
    _, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"f1"}, &scanner.ProjectScan{})
    if err == nil {
        t.Fatal("expected error")
    }
}

func TestMapFeaturesToCode_InvalidJSON_ReturnsError(t *testing.T) {
    c := &fakeClient{responses: []string{"not json"}}
    _, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"f1"}, &scanner.ProjectScan{})
    if err == nil {
        t.Fatal("expected error for invalid JSON")
    }
}
```

**Step 2: Run — expect FAIL**

```bash
go test ./internal/analyzer/...
```

Expected: `undefined: analyzer.MapFeaturesToCode`.

**Step 3: Create `internal/analyzer/mapper.go`**

```go
package analyzer

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

type mapEntry struct {
    Feature string   `json:"feature"`
    Files   []string `json:"files"`
    Symbols []string `json:"symbols"`
}

// MapFeaturesToCode maps a list of product features to code files and symbols in scan.
func MapFeaturesToCode(ctx context.Context, client LLMClient, features []string, scan *scanner.ProjectScan) (FeatureMap, error) {
    if len(features) == 0 {
        return FeatureMap{}, nil
    }

    // Build a compact symbol index: "path: Symbol1, Symbol2"
    var symLines []string
    for _, f := range scan.Files {
        if len(f.Symbols) == 0 {
            continue
        }
        names := make([]string, len(f.Symbols))
        for i, s := range f.Symbols {
            names[i] = s.Name
        }
        symLines = append(symLines, fmt.Sprintf("%s: %s", f.Path, strings.Join(names, ", ")))
    }

    featuresJSON, _ := json.Marshal(features)
    symbolsText := strings.Join(symLines, "\n")

    // PROMPT: Maps product features to the code files and symbols most likely to implement them. Returns a JSON array only.
    prompt := fmt.Sprintf(`You are mapping product features to their code implementations.

Product features:
%s

Code symbols (format: "file/path: Symbol1, Symbol2"):
%s

For each feature, identify which code files and exported symbols are most relevant to implementing it.
Return a JSON array where each element has:
- "feature": the feature name exactly as provided
- "files": list of relevant file paths (empty array if none)
- "symbols": list of relevant exported symbol names (empty array if none)

Respond with only the JSON array. No markdown code fences. No prose.`, string(featuresJSON), symbolsText)

    raw, err := client.Complete(ctx, prompt)
    if err != nil {
        return nil, fmt.Errorf("MapFeaturesToCode: %w", err)
    }

    var entries []mapEntry
    if err := json.Unmarshal([]byte(raw), &entries); err != nil {
        return nil, fmt.Errorf("MapFeaturesToCode: invalid JSON response: %w", err)
    }

    out := make(FeatureMap, len(entries))
    for i, e := range entries {
        if e.Files == nil {
            e.Files = []string{}
        }
        if e.Symbols == nil {
            e.Symbols = []string{}
        }
        out[i] = FeatureEntry{Feature: e.Feature, Files: e.Files, Symbols: e.Symbols}
    }
    return out, nil
}
```

**Step 4: Run — expect PASS**

```bash
go test ./internal/analyzer/...
```

Expected: all 16 tests pass.

**Step 5: Commit**

```bash
git add internal/analyzer/mapper.go internal/analyzer/mapper_test.go
git commit -m "feat(analyzer): MapFeaturesToCode with PROMPT comment

- RED: 4 TestMapFeaturesToCode_* tests
- GREEN: mapper.go with // PROMPT: comment
- Status: 16 tests passing, build successful"
```

---

## Task 5: Extend spider.Index with analysis fields

`index.json` must persist per-page summaries and features alongside crawl metadata, plus top-level `product_summary` and `all_features`. This changes the JSON schema.

**Current schema** (`internal/spider/cache.go`):
```json
{ "https://url": {"filename": "abc.md", "fetched_at": "..."} }
```

**New schema**:
```json
{
  "pages": {
    "https://url": {"filename":"abc.md","fetched_at":"...","summary":"...","features":["..."]}
  },
  "product_summary": "...",
  "all_features": ["..."]
}
```

**Files:**
- Modify: `internal/spider/cache.go`
- Modify: `internal/spider/cache_test.go`

**Step 1: Write the failing test**

Add to `internal/spider/cache_test.go` (append after existing tests):

```go
func TestIndex_RecordAnalysis_PersistsAndLoads(t *testing.T) {
    dir := t.TempDir()
    idx, err := LoadIndex(dir)
    if err != nil {
        t.Fatal(err)
    }

    if err := idx.Record("https://example.com", "abc.md"); err != nil {
        t.Fatal(err)
    }
    if err := idx.RecordAnalysis("https://example.com", "Covers install.", []string{"Homebrew install"}); err != nil {
        t.Fatal(err)
    }

    // Reload and verify
    idx2, err := LoadIndex(dir)
    if err != nil {
        t.Fatal(err)
    }

    summary, features, ok := idx2.Analysis("https://example.com")
    if !ok {
        t.Fatal("expected analysis to be found")
    }
    if summary != "Covers install." {
        t.Errorf("Summary: got %q", summary)
    }
    if len(features) != 1 || features[0] != "Homebrew install" {
        t.Errorf("Features: got %v", features)
    }
}

func TestIndex_SetProductSummary_PersistsAndLoads(t *testing.T) {
    dir := t.TempDir()
    idx, err := LoadIndex(dir)
    if err != nil {
        t.Fatal(err)
    }

    if err := idx.SetProductSummary("A CLI tool.", []string{"gap analysis", "doctor"}); err != nil {
        t.Fatal(err)
    }

    idx2, err := LoadIndex(dir)
    if err != nil {
        t.Fatal(err)
    }

    if idx2.ProductSummary != "A CLI tool." {
        t.Errorf("ProductSummary: got %q", idx2.ProductSummary)
    }
    if len(idx2.AllFeatures) != 2 {
        t.Errorf("AllFeatures: got %v", idx2.AllFeatures)
    }
}
```

**Step 2: Run — expect FAIL**

```bash
go test ./internal/spider/...
```

Expected: `idx.RecordAnalysis undefined`, `idx.Analysis undefined`, `idx.SetProductSummary undefined`.

**Step 3: Rewrite `internal/spider/cache.go`**

Replace the file with:

```go
package spider

import (
    "crypto/sha256"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "time"
)

// URLToFilename returns a stable, collision-resistant filename for rawURL.
func URLToFilename(rawURL string) string {
    sum := sha256.Sum256([]byte(rawURL))
    return fmt.Sprintf("%x.md", sum)
}

type indexEntry struct {
    Filename  string    `json:"filename"`
    FetchedAt time.Time `json:"fetched_at"`
    Summary   string    `json:"summary,omitempty"`
    Features  []string  `json:"features,omitempty"`
}

type indexJSON struct {
    Pages          map[string]indexEntry `json:"pages"`
    ProductSummary string                `json:"product_summary,omitempty"`
    AllFeatures    []string              `json:"all_features,omitempty"`
}

// Index is an in-memory view of index.json backed by a cache directory.
type Index struct {
    dir            string
    entries        map[string]indexEntry
    ProductSummary string
    AllFeatures    []string
}

// LoadIndex reads index.json from dir, or returns an empty index if the file
// does not exist. It creates dir if it does not exist.
func LoadIndex(dir string) (*Index, error) {
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return nil, err
    }
    idx := &Index{dir: dir, entries: make(map[string]indexEntry)}
    data, err := os.ReadFile(filepath.Join(dir, "index.json"))
    if errors.Is(err, os.ErrNotExist) {
        return idx, nil
    }
    if err != nil {
        return nil, err
    }
    var raw indexJSON
    if err := json.Unmarshal(data, &raw); err != nil {
        return nil, err
    }
    if raw.Pages != nil {
        idx.entries = raw.Pages
    }
    idx.ProductSummary = raw.ProductSummary
    idx.AllFeatures = raw.AllFeatures
    return idx, nil
}

// Has reports whether rawURL is already recorded in the index.
func (idx *Index) Has(rawURL string) bool {
    _, ok := idx.entries[rawURL]
    return ok
}

// Record adds rawURL to the index with the given filename and saves index.json.
func (idx *Index) Record(rawURL, filename string) error {
    e := idx.entries[rawURL]
    e.Filename = filename
    e.FetchedAt = time.Now()
    idx.entries[rawURL] = e
    return idx.save()
}

// RecordAnalysis stores the LLM-produced summary and features for rawURL.
func (idx *Index) RecordAnalysis(rawURL, summary string, features []string) error {
    e := idx.entries[rawURL]
    e.Summary = summary
    e.Features = features
    idx.entries[rawURL] = e
    return idx.save()
}

// Analysis returns the cached summary and features for rawURL, if present.
func (idx *Index) Analysis(rawURL string) (summary string, features []string, ok bool) {
    e, found := idx.entries[rawURL]
    if !found || e.Summary == "" {
        return "", nil, false
    }
    return e.Summary, e.Features, true
}

// SetProductSummary stores the product-level summary and feature list.
func (idx *Index) SetProductSummary(description string, features []string) error {
    idx.ProductSummary = description
    idx.AllFeatures = features
    return idx.save()
}

// FilePath returns the absolute cache file path for rawURL, if present.
func (idx *Index) FilePath(rawURL string) (string, bool) {
    e, ok := idx.entries[rawURL]
    if !ok {
        return "", false
    }
    return filepath.Join(idx.dir, e.Filename), true
}

// All returns a map of every cached URL to its absolute file path.
func (idx *Index) All() map[string]string {
    out := make(map[string]string, len(idx.entries))
    for u, e := range idx.entries {
        out[u] = filepath.Join(idx.dir, e.Filename)
    }
    return out
}

func (idx *Index) save() error {
    raw := indexJSON{
        Pages:          idx.entries,
        ProductSummary: idx.ProductSummary,
        AllFeatures:    idx.AllFeatures,
    }
    data, err := json.MarshalIndent(raw, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(idx.dir, "index.json"), data, 0o644)
}
```

**Step 4: Run — expect PASS**

```bash
go test ./internal/spider/...
```

All existing tests plus the two new tests must pass. The JSON schema change is backward-incompatible with old `index.json` files — that is acceptable since this feature is not yet released.

**Step 5: Commit**

```bash
git add internal/spider/cache.go internal/spider/cache_test.go
git commit -m "feat(spider): extend Index with per-page analysis + product summary

- RED: TestIndex_RecordAnalysis_PersistsAndLoads, TestIndex_SetProductSummary_PersistsAndLoads
- GREEN: cache.go with RecordAnalysis, Analysis, SetProductSummary; new JSON schema
- Status: all spider tests passing, build successful"
```

---

## Task 6: internal/reporter

**Files:**
- Create: `internal/reporter/reporter.go`
- Create: `internal/reporter/reporter_test.go`

Produces two files in a given output directory:

- **`mapping.md`** — product summary + feature-to-code table
- **`gaps.md`** — undocumented code + stale doc references

**Step 1: Write the failing test**

Create `internal/reporter/reporter_test.go`:

```go
package reporter_test

import (
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
    "github.com/sandgardenhq/find-the-gaps/internal/reporter"
    "github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestWriteMapping_CreatesFile(t *testing.T) {
    dir := t.TempDir()

    summary := analyzer.ProductSummary{
        Description: "A CLI tool for finding doc gaps.",
        Features:    []string{"gap analysis", "doctor command"},
    }
    mapping := analyzer.FeatureMap{
        {Feature: "gap analysis", Files: []string{"internal/analyzer/analyzer.go"}, Symbols: []string{"AnalyzePage"}},
        {Feature: "doctor command", Files: []string{"internal/cli/doctor.go"}, Symbols: []string{}},
    }
    pages := []analyzer.PageAnalysis{
        {URL: "https://docs.example.com/gap", Summary: "Covers gap analysis.", Features: []string{"gap analysis"}},
    }

    if err := reporter.WriteMapping(dir, summary, mapping, pages); err != nil {
        t.Fatal(err)
    }

    data, err := os.ReadFile(filepath.Join(dir, "mapping.md"))
    if err != nil {
        t.Fatal(err)
    }
    content := string(data)
    if !strings.Contains(content, "gap analysis") {
        t.Error("mapping.md must mention 'gap analysis'")
    }
    if !strings.Contains(content, "A CLI tool") {
        t.Error("mapping.md must include product summary")
    }
    if !strings.Contains(content, "internal/analyzer/analyzer.go") {
        t.Error("mapping.md must include file paths")
    }
}

func TestWriteGaps_CreatesFile(t *testing.T) {
    dir := t.TempDir()

    scan := &scanner.ProjectScan{
        Files: []scanner.ScannedFile{
            {Path: "internal/foo/bar.go", Symbols: []scanner.Symbol{{Name: "Undocumented", Kind: scanner.KindFunc}}},
        },
    }
    mapping := analyzer.FeatureMap{} // no features map to Undocumented
    allFeatures := []string{"gap analysis"}

    if err := reporter.WriteGaps(dir, scan, mapping, allFeatures); err != nil {
        t.Fatal(err)
    }

    data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
    if err != nil {
        t.Fatal(err)
    }
    if len(data) == 0 {
        t.Error("gaps.md must not be empty")
    }
}

func TestWriteMapping_EmptyMapping_Succeeds(t *testing.T) {
    dir := t.TempDir()
    err := reporter.WriteMapping(dir,
        analyzer.ProductSummary{Description: "Product.", Features: []string{}},
        analyzer.FeatureMap{},
        []analyzer.PageAnalysis{},
    )
    if err != nil {
        t.Fatal(err)
    }
}
```

**Step 2: Run — expect FAIL**

```bash
go test ./internal/reporter/...
```

Expected: `cannot find package "github.com/sandgardenhq/find-the-gaps/internal/reporter"`.

**Step 3: Create `internal/reporter/reporter.go`**

```go
package reporter

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
    "github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// WriteMapping writes mapping.md to dir.
func WriteMapping(dir string, summary analyzer.ProductSummary, mapping analyzer.FeatureMap, pages []analyzer.PageAnalysis) error {
    var sb strings.Builder

    sb.WriteString("# Feature Map\n\n")
    sb.WriteString("## Product Summary\n\n")
    sb.WriteString(summary.Description)
    sb.WriteString("\n\n")

    sb.WriteString("## Features\n\n")
    for _, entry := range mapping {
        fmt.Fprintf(&sb, "### %s\n", entry.Feature)
        if len(entry.Files) > 0 {
            fmt.Fprintf(&sb, "- **Implemented in:** %s\n", strings.Join(entry.Files, ", "))
        }
        if len(entry.Symbols) > 0 {
            fmt.Fprintf(&sb, "- **Symbols:** %s\n", strings.Join(entry.Symbols, ", "))
        }
        // Find doc pages that mention this feature
        for _, p := range pages {
            for _, f := range p.Features {
                if f == entry.Feature {
                    fmt.Fprintf(&sb, "- **Documented on:** %s\n", p.URL)
                    break
                }
            }
        }
        sb.WriteString("\n")
    }

    return os.WriteFile(filepath.Join(dir, "mapping.md"), []byte(sb.String()), 0o644)
}

// WriteGaps writes gaps.md to dir.
// It identifies exported symbols with no feature mapping and features with no code mapping.
func WriteGaps(dir string, scan *scanner.ProjectScan, mapping analyzer.FeatureMap, allDocFeatures []string) error {
    // Build set of symbols that appear in any feature mapping
    mappedSymbols := make(map[string]bool)
    for _, entry := range mapping {
        for _, sym := range entry.Symbols {
            mappedSymbols[sym] = true
        }
    }

    // Build set of features that have at least one file mapped
    mappedFeatures := make(map[string]bool)
    for _, entry := range mapping {
        if len(entry.Files) > 0 {
            mappedFeatures[entry.Feature] = true
        }
    }

    var sb strings.Builder
    sb.WriteString("# Gaps Found\n\n")

    // Undocumented code: exported symbols not in any feature mapping
    sb.WriteString("## Undocumented Code\n\n")
    found := false
    for _, f := range scan.Files {
        for _, sym := range f.Symbols {
            if sym.Kind != scanner.KindFunc && sym.Kind != scanner.KindType && sym.Kind != scanner.KindInterface {
                continue
            }
            if isExported(sym.Name) && !mappedSymbols[sym.Name] {
                fmt.Fprintf(&sb, "- `%s` in `%s` — no documentation page covers this symbol\n", sym.Name, f.Path)
                found = true
            }
        }
    }
    if !found {
        sb.WriteString("_None found._\n")
    }

    // Unmapped features: doc features with no code match
    sb.WriteString("\n## Unmapped Features\n\n")
    found = false
    for _, feat := range allDocFeatures {
        if !mappedFeatures[feat] {
            fmt.Fprintf(&sb, "- \"%s\" mentioned in docs — no code match found\n", feat)
            found = true
        }
    }
    if !found {
        sb.WriteString("_None found._\n")
    }

    return os.WriteFile(filepath.Join(dir, "gaps.md"), []byte(sb.String()), 0o644)
}

func isExported(name string) bool {
    if len(name) == 0 {
        return false
    }
    return name[0] >= 'A' && name[0] <= 'Z'
}
```

**Step 4: Run — expect PASS**

```bash
go test ./internal/reporter/...
```

Expected: 3 tests pass.

**Step 5: Check coverage**

```bash
go test -cover ./internal/reporter/...
```

Expected: ≥90% statement coverage.

**Step 6: Commit**

```bash
git add internal/reporter/
git commit -m "feat(reporter): WriteMapping and WriteGaps

- RED: TestWriteMapping_CreatesFile, TestWriteGaps_CreatesFile, TestWriteMapping_EmptyMapping_Succeeds
- GREEN: reporter.go with WriteMapping, WriteGaps
- Status: 3 tests passing, build successful"
```

---

## Task 7: Wire analyzer into `analyze` CLI

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `cmd/find-the-gaps/testdata/script/analyze_stub.txtar`

The full pipeline after crawling:
1. Load the spider `Index` for the docs cache dir.
2. For each page URL + file path, call `AnalyzePage`. Skip pages where `idx.Analysis(url)` returns `ok == true` (cache hit). Record results in the index.
3. Call `SynthesizeProduct` with all analyses.
4. Store product summary in the index.
5. Call `MapFeaturesToCode` with `summary.Features` and the `scan`.
6. Call `reporter.WriteMapping` and `reporter.WriteGaps`.
7. Print summary line to stdout.

The `LLMClient` is constructed from a `BifrostClient` (Task 8). For now, the CLI will return a clear error if no LLM client can be constructed (missing API key), so tests can pass without a real key.

**Step 1: Write the failing test**

The existing `analyze_stub.txtar` tests the no-docs-url path. Add a new txtar for the case where `--docs-url` is provided but LLM credentials are missing. This is an integration concern; for unit-level coverage, update the test in `cmd/find-the-gaps/root_test.go`.

Add to `cmd/find-the-gaps/root_test.go` (or find the relevant test file):

```go
func TestAnalyze_PrintsFilesAndPages(t *testing.T) {
    // This test uses a fake spider output; it does not test LLM calls.
    // Full integration is covered by the VERIFICATION_PLAN.
}
```

Actually the CLI wiring is best tested via the existing txtar mechanism. Update `analyze_stub.txtar`:

```
# analyze subcommand scans an empty repo and prints a summary.
mkdir repo
exec find-the-gaps analyze --repo repo --cache-dir $WORK/cache
stdout 'scanned 0 files'
```

This test already passes. The new behavior (LLM analysis) only runs when `--docs-url` is provided AND a `LLMClient` can be constructed. Guard it so that missing `ANTHROPIC_API_KEY` returns a descriptive error rather than a panic.

**Step 2: Modify `internal/cli/analyze.go`**

```go
package cli

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
    "github.com/sandgardenhq/find-the-gaps/internal/reporter"
    "github.com/sandgardenhq/find-the-gaps/internal/scanner"
    "github.com/sandgardenhq/find-the-gaps/internal/spider"
    "github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
    var (
        docsURL  string
        repoPath string
        cacheDir string
        workers  int
        noCache  bool
    )

    cmd := &cobra.Command{
        Use:   "analyze",
        Short: "Analyze a codebase against its documentation site for gaps.",
        RunE: func(cmd *cobra.Command, _ []string) error {
            ctx := context.Background()

            projectName := filepath.Base(filepath.Clean(repoPath))
            projectDir := filepath.Join(cacheDir, projectName)

            scanOpts := scanner.Options{
                CacheDir: filepath.Join(projectDir, "scan"),
                NoCache:  noCache,
            }
            scan, err := scanner.Scan(repoPath, scanOpts)
            if err != nil {
                return fmt.Errorf("scan failed: %w", err)
            }

            if docsURL == "" {
                _, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files\n", len(scan.Files))
                return nil
            }

            docsDir := filepath.Join(projectDir, "docs")
            spiderOpts := spider.Options{
                CacheDir: docsDir,
                Workers:  workers,
            }
            pages, err := spider.Crawl(docsURL, spiderOpts, spider.MdfetchFetcher(spiderOpts))
            if err != nil {
                return fmt.Errorf("crawl failed: %w", err)
            }

            llmClient, err := newBifrostClient()
            if err != nil {
                return fmt.Errorf("LLM client: %w", err)
            }

            idx, err := spider.LoadIndex(docsDir)
            if err != nil {
                return fmt.Errorf("load index: %w", err)
            }

            // Analyze each page; skip cached results.
            var analyses []analyzer.PageAnalysis
            for url, filePath := range pages {
                if summary, features, ok := idx.Analysis(url); ok {
                    analyses = append(analyses, analyzer.PageAnalysis{
                        URL:      url,
                        Summary:  summary,
                        Features: features,
                    })
                    continue
                }
                content, readErr := os.ReadFile(filePath)
                if readErr != nil {
                    continue
                }
                pa, analyzeErr := analyzer.AnalyzePage(ctx, llmClient, url, string(content))
                if analyzeErr != nil {
                    _, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: AnalyzePage %s: %v\n", url, analyzeErr)
                    continue
                }
                if recErr := idx.RecordAnalysis(url, pa.Summary, pa.Features); recErr != nil {
                    return fmt.Errorf("record analysis: %w", recErr)
                }
                analyses = append(analyses, pa)
            }

            if len(analyses) == 0 {
                _, _ = fmt.Fprintf(cmd.OutOrStdout(), "scanned %d files, fetched %d pages, 0 pages analyzed\n",
                    len(scan.Files), len(pages))
                return nil
            }

            productSummary, err := analyzer.SynthesizeProduct(ctx, llmClient, analyses)
            if err != nil {
                return fmt.Errorf("synthesize: %w", err)
            }
            if err := idx.SetProductSummary(productSummary.Description, productSummary.Features); err != nil {
                return fmt.Errorf("save product summary: %w", err)
            }

            featureMap, err := analyzer.MapFeaturesToCode(ctx, llmClient, productSummary.Features, scan)
            if err != nil {
                return fmt.Errorf("map features: %w", err)
            }

            if err := reporter.WriteMapping(projectDir, productSummary, featureMap, analyses); err != nil {
                return fmt.Errorf("write mapping: %w", err)
            }
            if err := reporter.WriteGaps(projectDir, scan, featureMap, productSummary.Features); err != nil {
                return fmt.Errorf("write gaps: %w", err)
            }

            _, _ = fmt.Fprintf(cmd.OutOrStdout(),
                "scanned %d files, fetched %d pages, %d features mapped\nreports: %s/mapping.md, %s/gaps.md\n",
                len(scan.Files), len(pages), len(featureMap), projectDir, projectDir)

            return nil
        },
    }

    cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository to analyze")
    cmd.Flags().StringVar(&cacheDir, "cache-dir", ".find-the-gaps", "base directory for all cached results")
    cmd.Flags().BoolVar(&noCache, "no-cache", false, "force full re-scan, ignoring any cached results")
    cmd.Flags().StringVar(&docsURL, "docs-url", "", "URL of the documentation site to analyze")
    cmd.Flags().IntVar(&workers, "workers", 5, "number of parallel mdfetch workers")

    return cmd
}

// newBifrostClient is declared here; implemented in bifrost_client.go (Task 8).
// Returns an error if required env vars are missing.
func newBifrostClient() (analyzer.LLMClient, error) {
    return newRealBifrostClient()
}
```

Note: `newRealBifrostClient()` will be defined in Task 8. For now, create a stub that returns an error.

**Step 3: Create stub `internal/cli/bifrost_client.go`**

```go
package cli

import (
    "errors"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// newRealBifrostClient constructs the Bifrost-backed LLM client.
// Returns an error if ANTHROPIC_API_KEY is not set.
// Full implementation added in Task 8.
func newRealBifrostClient() (analyzer.LLMClient, error) {
    return nil, errors.New("LLM client not yet implemented — see Task 8")
}
```

**Step 4: Run all tests — expect PASS**

```bash
go test ./...
```

The existing `analyze_stub.txtar` tests the no-docs-url path and must still pass. The LLM path is gated behind `--docs-url`, so no real LLM calls happen in tests.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/bifrost_client.go
git commit -m "feat(cli): wire analyzer pipeline into analyze command

- RED: existing analyze_stub.txtar still green, no LLM called without --docs-url
- GREEN: analyze.go orchestrates AnalyzePage→SynthesizeProduct→MapFeaturesToCode→reports
- Status: all tests passing, build successful"
```

---

## Task 8: BifrostClient real implementation

**Files:**
- Replace: `internal/cli/bifrost_client.go` (replaces stub from Task 7)
- Create: `internal/analyzer/bifrost_client.go`
- Create: `internal/analyzer/bifrost_client_integration_test.go` (build tag: `integration`)

The real `BifrostClient` wraps the Bifrost Go SDK. Unit tests don't exercise it (they use `fakeClient`). Integration tests require `ANTHROPIC_API_KEY` in the environment and are skipped in normal `go test ./...`.

**Step 1: Write the integration test (RED — skipped in CI)**

Create `internal/analyzer/bifrost_client_integration_test.go`:

```go
//go:build integration

package analyzer_test

import (
    "context"
    "os"
    "testing"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestBifrostClient_RealCompletion(t *testing.T) {
    key := os.Getenv("ANTHROPIC_API_KEY")
    if key == "" {
        t.Skip("ANTHROPIC_API_KEY not set")
    }

    client, err := analyzer.NewBifrostClient(key)
    if err != nil {
        t.Fatal(err)
    }

    resp, err := client.Complete(context.Background(), "Reply with the single word: pong")
    if err != nil {
        t.Fatal(err)
    }
    if resp == "" {
        t.Error("expected non-empty response")
    }
    t.Logf("Response: %s", resp)
}
```

Run normally (should produce 0 test results, not a failure):
```bash
go test ./internal/analyzer/...
```

Run with tag (requires real API key):
```bash
ANTHROPIC_API_KEY=your_key go test -tags integration ./internal/analyzer/...
```

**Step 2: Create `internal/analyzer/bifrost_client.go`**

```go
package analyzer

import (
    "context"
    "fmt"
    "os"

    "github.com/maximhq/bifrost/core"
    "github.com/maximhq/bifrost/core/schemas"
)

// BifrostClient implements LLMClient using the Bifrost Go SDK.
type BifrostClient struct {
    client   *bifrost.Bifrost
    provider schemas.ModelProvider
    model    string
}

type bifrostAccount struct {
    provider schemas.ModelProvider
    apiKey   string
}

func (a *bifrostAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
    return []schemas.ModelProvider{a.provider}, nil
}

func (a *bifrostAccount) GetKeysForProvider(_ *context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
    if provider == a.provider {
        return []schemas.Key{{Value: a.apiKey, Weight: 1.0}}, nil
    }
    return nil, fmt.Errorf("unsupported provider: %s", provider)
}

func (a *bifrostAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
    if provider == a.provider {
        return &schemas.ProviderConfig{
            NetworkConfig:            schemas.DefaultNetworkConfig,
            ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
        }, nil
    }
    return nil, fmt.Errorf("unsupported provider: %s", provider)
}

// NewBifrostClient creates a BifrostClient using Anthropic as the provider.
// apiKey is the Anthropic API key.
func NewBifrostClient(apiKey string) (*BifrostClient, error) {
    account := &bifrostAccount{
        provider: schemas.Anthropic,
        apiKey:   apiKey,
    }
    client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: account})
    if err != nil {
        return nil, fmt.Errorf("bifrost init: %w", err)
    }
    return &BifrostClient{
        client:   client,
        provider: schemas.Anthropic,
        model:    "claude-3-5-sonnet-20241022",
    }, nil
}

// NewBifrostClientFromEnv creates a BifrostClient reading ANTHROPIC_API_KEY from the environment.
func NewBifrostClientFromEnv() (*BifrostClient, error) {
    key := os.Getenv("ANTHROPIC_API_KEY")
    if key == "" {
        return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
    }
    return NewBifrostClient(key)
}

// Complete sends a user prompt and returns the first completion text.
func (c *BifrostClient) Complete(ctx context.Context, prompt string) (string, error) {
    resp, err := c.client.ChatCompletionRequest(ctx, &schemas.BifrostChatRequest{
        Provider: c.provider,
        Model:    c.model,
        Input: []schemas.ChatMessage{
            {
                Role:    schemas.ChatMessageRoleUser,
                Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr(prompt)},
            },
        },
    })
    if err != nil {
        return "", fmt.Errorf("bifrost completion: %w", err)
    }
    if len(resp.Choices) == 0 {
        return "", fmt.Errorf("bifrost completion: no choices returned")
    }
    content := resp.Choices[0].Message.Content
    if content == nil || content.ContentStr == nil {
        return "", fmt.Errorf("bifrost completion: nil content")
    }
    return *content.ContentStr, nil
}
```

**Step 3: Update `internal/cli/bifrost_client.go`**

Replace the stub:

```go
package cli

import (
    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// newRealBifrostClient constructs the Bifrost-backed LLM client from ANTHROPIC_API_KEY.
func newRealBifrostClient() (analyzer.LLMClient, error) {
    return analyzer.NewBifrostClientFromEnv()
}
```

**Step 4: Run all tests — expect PASS**

```bash
go test ./...
```

Normal test run must pass (integration test is build-tag-gated and skipped). The `analyze_stub.txtar` and all unit tests pass.

**Step 5: Verify build**

```bash
go build ./...
```

Expected: success, no errors.

**Step 6: Check coverage**

```bash
go test -cover ./internal/analyzer/... ./internal/reporter/... ./internal/spider/...
```

Expected: ≥90% statement coverage per package.

**Step 7: Run linter**

```bash
golangci-lint run
```

Fix any reported issues before committing.

**Step 8: Commit**

```bash
git add internal/analyzer/bifrost_client.go internal/analyzer/bifrost_client_integration_test.go internal/cli/bifrost_client.go
git commit -m "feat(analyzer): BifrostClient wrapping Bifrost Go SDK

- RED: TestBifrostClient_RealCompletion (integration tag, skipped in normal run)
- GREEN: bifrost_client.go with NewBifrostClient, NewBifrostClientFromEnv, Complete
- Status: all unit tests passing, build successful"
```

---

## Final checks before PR

Run the full suite and verify everything is clean:

```bash
go test ./...
go build ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
golangci-lint run
gofmt -w . && goimports -w .
```

All packages must meet ≥90% statement coverage. Then open a PR against `main` using a merge commit.
