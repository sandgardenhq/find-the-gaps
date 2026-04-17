# MDFetch Spider Design

**Date**: 2026-04-17
**Feature**: Docs site ingestion via mdfetch spidering

## Problem

`mdfetch` fetches a single URL at a time and outputs markdown. To ingest a full
documentation site, `find-the-gaps` must own the crawl loop: discover links,
stay in scope, and manage parallelism and caching.

## Package Structure

```
internal/spider/
    spider.go   # orchestrator: queue, worker pool, visited-set
    links.go    # markdown link extraction + same-host filtering
    cache.go    # disk cache: read/write/check by URL
```

The `analyze` command creates a `Spider` with options drawn from CLI flags,
calls `spider.Crawl(startURL)`, and receives a `map[URL]filepath` to pass to
the analysis phase.

The `--cache-dir` flag on `analyze` defaults to `.find-the-gaps/cache/`.

## Cache Layout

```
.find-the-gaps/cache/
    index.json      # URL → {filename, fetchedAt} mapping
    a3f9c2....md
    b71e40....md
```

- Filenames are the hex-encoded SHA-256 of the URL — stable, collision-free.
- `index.json` is the authoritative visited set. The spider loads it on startup
  and skips any URL already present.
- Each successful fetch appends to the index atomically.
- Re-runs are incremental by default. To force a fresh crawl, point
  `--cache-dir` at a new directory. No TTL is implemented in this iteration.

## Concurrency Model

```
Crawl(startURL string, opts Options) (map[string]string, error)
```

- **Coordinator goroutine**: loads `index.json`, seeds the jobs channel with
  `startURL` (if uncached), reads results, extracts links, filters and enqueues
  new jobs, tracks in-flight count, closes jobs channel when queue is empty and
  in-flight reaches zero.
- **N worker goroutines** (default 5, configurable): read a URL from the jobs
  channel, shell out to `mdfetch <url> -o <cachePath>`, send back a result
  struct containing the URL, cache path, extracted links, and any error.
- A `sync.WaitGroup` coordinates shutdown.
- A failed fetch logs a warning and continues — it does not abort the crawl.
- `mdfetch` timeout and retry flags are forwarded from `Options`.

## Link Extraction

`ExtractLinks(markdown string, pageURL *url.URL) []string`

**Parse** — two patterns:
1. Standard markdown links: `\[([^\]]*)\]\((https?://[^)]+)\)`
2. Bare URLs: `https?://\S+`

**Filter**:
- Resolve relative paths against `pageURL` before filtering.
- Keep only links where `link.Host == pageURL.Host`.
- Drop fragment-only links and `mailto:` links.
- Deduplicate within the page before returning.

Returns absolute URL strings. The spider owns cross-page deduplication via the
visited set.

## CLI Flags (on `analyze`)

| Flag | Default | Description |
|---|---|---|
| `--cache-dir` | `.find-the-gaps/cache` | Where to store fetched pages |
| `--workers` | `5` | Parallel mdfetch worker count |

## What This Does Not Cover

- TTL / cache invalidation (future)
- HTML link parsing (future, if markdown parsing misses too many links)
- `--no-cache` flag (future)
- Rate limiting beyond worker pool size (future)
