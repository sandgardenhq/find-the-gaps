# LLM Analysis Design

## Goal

Read every fetched documentation page through an LLM to extract summaries and
product features, then map those features to code symbols so the tool can
surface both undocumented code and stale/unmapped documentation.

## Pipeline

```
1. Scan      internal/scanner   → ProjectScan (symbols, imports, graph)
2. Crawl     internal/spider    → markdown pages on disk + index.json
3. Analyze   internal/analyzer  → per-page summaries + features, product summary,
                                  feature→code map
4. Report    internal/reporter  → mapping.md + gaps.md + stdout summary
```

## Data Model

`index.json` is the persistent knowledge store. Each entry grows to hold
LLM-produced fields alongside the file path it already tracks:

```json
{
  "pages": {
    "https://docs.example.com/installation": {
      "file": "abc123.md",
      "summary": "Covers installing via Homebrew and go install.",
      "features": ["Homebrew install", "go install", "ripgrep dependency"]
    }
  },
  "product_summary": "Find the Gaps is a CLI tool that...",
  "all_features": ["Homebrew install", "gap analysis", "doctor command"]
}
```

Pages that already have a `summary` are skipped on subsequent runs (cache-aware).
`--no-cache` forces re-analysis of all pages.

## Package: `internal/analyzer`

Owns all LLM interaction. Three public functions:

```go
type LLMClient interface {
    Complete(ctx context.Context, prompt string) (string, error)
}

// AnalyzePage summarizes one doc page and extracts features from it.
func AnalyzePage(ctx context.Context, client LLMClient, content string) (PageAnalysis, error)

// SynthesizeProduct combines all page analyses into a product summary
// and a deduplicated feature list.
func SynthesizeProduct(ctx context.Context, client LLMClient, pages []PageAnalysis) (ProductSummary, error)

// MapFeaturesToCode maps the feature list to code symbols and files.
func MapFeaturesToCode(ctx context.Context, client LLMClient, features []string, scan *scanner.ProjectScan) (FeatureMap, error)
```

The real Bifrost SDK implementation wraps `LLMClient`. Unit tests use a fake
client with scripted responses. Integration tests (tagged `//go:build integration`)
use a real Bifrost key from the environment and are skipped in normal `go test ./...`.

## Error Handling

- `AnalyzePage` failure: non-fatal — logs warning, leaves summary/features empty,
  continues with remaining pages.
- If more than half of pages fail analysis, the run errors out with a clear message.
- `SynthesizeProduct` and `MapFeaturesToCode` failures: fatal — the run errors out.

## Package: `internal/reporter`

Writes two files to `<cache-dir>/<project>/`:

**`mapping.md`** — full feature→code map:
```markdown
# Find the Gaps — Feature Map

## Product Summary
...

## Features

### Homebrew install
- **Documented on:** docs/installation.md
- **Implemented in:** `cmd/find-the-gaps/main.go`
```

**`gaps.md`** — actionable gap list:
```markdown
# Gaps Found

## Undocumented Code
- `internal/scanner.Scan()` — no doc page covers this function

## Stale Docs
- docs/config.md mentions `--verbose` flag — not found in codebase

## Unmapped Features
- "Plugin system" mentioned on docs/roadmap.md — no code match found
```

Stdout summary at the end of `analyze`:
```
3 undocumented symbols, 1 stale reference, 2 unmapped features
```

## Bifrost Integration

Use the [Bifrost Go SDK](https://docs.getbifrost.ai/quickstart/go-sdk/setting-up)
as the `LLMClient` implementation. API key read from the environment variable the
SDK expects. The SDK is added to `go.mod` as part of implementation.
