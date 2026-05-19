# Dead Link Check — Design

**Status**: Design, ready for implementation plan
**Date**: 2026-05-19

## Problem

Docs sites accumulate dead links: pages that 404, third-party SDK references that move, GitHub repos that get renamed, blog posts that get pulled. Find the Gaps already surfaces semantic gaps (drift, undocumented surface, missing screenshots) but says nothing about whether the links a maintainer ships in their docs actually resolve. A mechanical link check closes that gap.

## Scope

The pass runs as part of `ftg analyze` by default. It probes every link discovered while crawling the docs site — both **same-host** (intra-docs nav) and **outbound** (third-party references). It reports broken links, links that require auth, and links that work but resolve via redirect. Opt out with `--no-link-check`.

Out of scope for v1:

- Fragment-anchor validation (`#section-name` does not exist on the target page).
- GitHub Action issue-body integration of dead-link findings.
- Honoring `Retry-After` response headers.

## Surface

A new top-level artifact set, parallel to drift and screenshots:

- `<projectDir>/links.md`
- `<projectDir>/links.json`
- `<projectDir>/site/links/` (rendered by Hugo / Hextra)
- A "Dead Links" section in `<projectDir>/report.pdf`
- A new line in the stdout `reports:` block: `links.md (12 broken · 3 auth · 47 redirected)` or `links.md (skipped)` under `--no-link-check`.

## Architecture

### Package layout

```
internal/linkcheck/
    checker.go      // HTTP probing
    extract.go      // un-filtered link extraction over cached markdown
    cache.go        // persistent <projectDir>/links-cache.json
    linkcheck.go    // orchestration + per-host throttle
    *_test.go
internal/reporter/
    links_writer.go     // links.md
    links_json.go       // links.json
internal/site/
    (new content type wired into existing site builder)
internal/pdf/
    (new section in the existing renderer)
```

### Lifecycle inside `ftg analyze`

1. Spider crawl finishes (unchanged).
2. **Link-check phase** runs:
   1. Walk every cached page; call `linkcheck.Extract` (no host filter) to collect every link.
   2. Dedupe globally, build a `map[url]→[]pageURL`.
   3. Load `links-cache.json`; partition into cached (skip) and uncached (probe).
   4. Probe uncached URLs in parallel under the bounded worker pool. A per-host concurrency cap of 4 throttles requests to any single host, even when the global `--workers` is higher.
   5. Persist `links-cache.json` incrementally (every N completions and on SIGINT via a deferred flush).
3. Reporter writes `links.md` + `links.json`.
4. Site builder renders `/links/`.
5. PDF renderer adds the "Dead Links" section.
6. Stdout `reports:` block emits the new line.

### Checker

```go
type Bucket int

const (
    BucketOK Bucket = iota
    BucketBroken
    BucketAuth
    BucketRedirected
)

type Result struct {
    URL         string
    FinalURL    string    // populated when redirected
    StatusChain []int     // [301, 200] or [503] or [] for network errors
    ErrorType   string    // http_404, http_5xx, timeout, dns, tls,
                          // connection_refused, redirect_loop, ""
    Detail      string    // short human-readable reason
    Bucket      Bucket
    CheckedAt   time.Time
}

type Checker interface {
    Check(ctx context.Context, url string) Result
}
```

Production `Checker` uses `net/http.Client` with:

- **Method**: HEAD first, GET fallback on 405 / 501 / connection-close.
- **Retry**: once on timeout / 5xx / connection error, 1s backoff. Never retry 4xx.
- **Timeout**: 10s per request.
- **User-Agent**: `find-the-gaps/<version> (+https://github.com/sandgardenhq/find-the-gaps)`.
- **Redirects**: follow up to 10 hops; record full status chain.
- **TLS**: standard verification. Cert errors → `ErrorType: tls`, `BucketBroken`.

Per-host throttling is **not** the Checker's concern — the orchestrator owns a `map[host]chan struct{}` of semaphores, so the Checker stays trivially unit-testable.

### Classification

| Outcome | Bucket | Notes |
| --- | --- | --- |
| 2xx, no redirect | `OK` | No finding emitted. |
| 3xx → 2xx, `final_url == url` after normalization | `OK` | No finding. |
| 3xx → 2xx, `final_url != url` | `Redirected` | Records full status chain + final URL. |
| 401, 403 | `Auth` | Called out separately so maintainers know to check manually. |
| 404, 410, other 4xx | `Broken` | `error_type: http_4xx` (specific subtype where useful, e.g. `http_404`). |
| 5xx after retry | `Broken` | `error_type: http_5xx`. |
| Timeout after retry | `Broken` | `error_type: timeout`. |
| DNS / connection refused | `Broken` | `error_type: dns` / `connection_refused`. |
| TLS error | `Broken` | `error_type: tls`. |
| Redirect loop / hop cap exceeded | `Broken` | `error_type: redirect_loop`. |

### Cache

`<projectDir>/links-cache.json` — `map[url]Result`. **No TTL**: cache entries persist across runs indefinitely. Invalidated only by `--no-cache`, which skips both the load and the persist. Incremental flush at SIGINT mirrors the screenshots-cache pattern, so partial runs resume cleanly.

### Extraction

`spider.ExtractLinks` keeps its same-host filter (drift pipeline still wants only intra-docs links). A new sibling function `linkcheck.Extract` accepts the same markdown + page URL and returns every absolute URL it finds, regardless of host.

Skipped during extraction (not reported as errors):

- `mailto:`, `tel:`, `javascript:`, `data:` URIs.
- Localhost and RFC1918 (`10/8`, `172.16/12`, `192.168/16`) URLs.
- Pure fragment refs (`#anchor` only — already handled by the existing extractor logic).

Fragments are stripped before dedupe: `https://x/y#a` and `https://x/y#b` collapse to `https://x/y`.

## Report shape

### `links.json`

```json
{
  "broken": [
    {
      "url": "https://gone.example.com/old",
      "error_type": "http_404",
      "detail": "HTTP 404 Not Found",
      "status_chain": [404],
      "pages": [
        "https://docs.example.com/a/",
        "https://docs.example.com/b/"
      ]
    }
  ],
  "auth_required": [
    {
      "url": "https://private.example.com/x",
      "status_chain": [401],
      "detail": "HTTP 401 Unauthorized",
      "pages": ["..."]
    }
  ],
  "redirected": [
    {
      "url": "https://old/x",
      "final_url": "https://new/x",
      "status_chain": [301, 200],
      "pages": ["..."]
    }
  ]
}
```

No `priority` / `priority_reason` fields — dead-link findings are deliberately un-prioritized. Within each bucket, entries are sorted by `len(pages)` desc, tiebreak alphabetic by URL.

### `links.md`

Three sections in this order, each rendered iff non-empty: `## Broken`, `## Auth Required`, `## Redirected`. One block per finding:

```markdown
### https://gone.example.com/old

**Reason:** HTTP 404 Not Found

**Pages:**
- /docs/intro/
- /docs/getting-started/
```

Same-host pages render as site-relative paths; external pages render as full URLs.

### Site

A new Hextra page at `/links/` mirrors the markdown structure. Each bucket is its own section with a count badge in the heading (`## Broken (12)`). Site-internal page refs link to the corresponding `/docs/...` paths within the rendered site. No card-stripe color treatment (no priority gradient).

### PDF

A new "Dead Links" section, placed after "Screenshots". Three buckets rendered as cards, flat — no L/M/S sub-headings, no colored left stripes. Cover stat-card row stays at three (features / gaps / screenshot issues); dead-link count appears in the section header itself rather than on the cover.

### Stdout

```
reports:
  ...
  links.md (12 broken · 3 auth · 47 redirected)
```

or under `--no-link-check`:

```
  links.md (skipped)
```

## Testing

- `internal/linkcheck/checker_test.go` — table-driven against `httptest.NewServer`. Every status code path: 200, 301→200, 301→404, 401, 403, 404, 410, 500, 503, timeout, TLS error, connection refused, redirect loop.
- `internal/linkcheck/extract_test.go` — coverage shape mirrors `spider.ExtractLinks_test.go`, plus outbound URL cases and the skip rules (mailto, localhost, etc.).
- `internal/linkcheck/cache_test.go` — round-trip, partial-flush, SIGINT recovery, `--no-cache` skip.
- `internal/linkcheck/linkcheck_test.go` — orchestration: per-host throttle is honored, page-list aggregation is correct, sort order is stable.
- `internal/reporter/links_writer_test.go`, `links_json_test.go` — golden-file tests; empty buckets are omitted; sort order verified.
- `internal/site/...` and `internal/pdf/...` — extend the existing build-integration tests to exercise the new content type and PDF section.

No mocks. The Checker is dependency-injected with an `*http.Client`, so we test against real local HTTP via `httptest`, not a stubbed client.

## Verification

New **Scenario 19** in `.plans/VERIFICATION_PLAN.md`:

1. Run analyze against the Scenario 9 fixture, with a seed page that links to (a) a known-404 outbound URL, (b) a known auth-walled URL (e.g. a private repo), and (c) a known 301-redirecting URL.
2. Confirm `links.md`, `links.json`, `<projectDir>/site/links/index.html`, and the "Dead Links" section in `report.pdf` all exist and contain the seeded URLs in the right buckets.
3. Re-run; confirm no fresh HTTP calls (verified via `-v`) and that the artifact is byte-identical to the first run.
4. Re-run with `--no-cache`; confirm every URL is re-probed.
5. Re-run with `--no-link-check`; confirm stdout shows `links.md (skipped)` and that no artifacts are written.
6. SIGINT mid-phase; confirm `links-cache.json` contains partial results and the next run completes only the un-probed URLs.

## Out of scope (v2 candidates)

- Fragment-anchor validation against rendered HTML.
- GitHub Action issue body inclusion of dead-link summary.
- `Retry-After` header honoring on rate-limit responses.
- Per-link priority via LLM ("links from the landing page matter more").
