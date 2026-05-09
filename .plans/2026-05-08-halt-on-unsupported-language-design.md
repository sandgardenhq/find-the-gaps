# Halt on Unsupported-Language Repo

**Date:** 2026-05-08
**Status:** Designed, ready for implementation plan

## Problem

`ftg analyze` will happily run its full pipeline — docs ingestion via `mdfetch`, LLM-driven feature mapping, drift detection, screenshot detection — against a repository that contains zero supported source code. The result is a set of empty or near-empty reports produced after spending tokens on nothing.

## Decision

When `analyze` is pointed at a repo whose scan produces no symbols from any of the 13 dedicated language extractors, halt before any docs ingestion or LLM work and tell the user what we did find.

## Behavior

**Trigger.** After `scanner.Scan()` returns, `internal/cli/analyze.go` filters `scan.Languages` and removes the `"Generic"` entry. If the resulting set is empty, analyze halts.

**Strict definition.** "No supported language" means: zero files matched any dedicated extractor (Go, Python, TypeScript, Rust, Java, C#, Kotlin, Swift, Scala, PHP, Ruby, C, C++). A repo with only Generic-text files (markdown, JSON, YAML, etc.) triggers the halt.

**Side effects on halt.** The scan still writes `<projectDir>/project.md` and saves the scan cache. The directory serves as evidence of what was inspected; on re-run, the cached scan satisfies the language check without re-walking. No `mapping.md`, `gaps.md`, `screenshots.md`, or `site/` is written. `mdfetch` is not called.

**Output.** Stderr message, exit code 1:

```
Error: no supported programming languages detected in <repo>.

Find the Gaps walked 412 files but found no Go, Python, TypeScript, Rust,
Java, C#, Kotlin, Swift, Scala, PHP, Ruby, C, or C++ source.

If your repo uses an unsupported language, please open an issue:
https://github.com/sandgardenhq/find-the-gaps/issues
```

The file count comes from `ignore.Stats.Scanned` already returned by `Scan()`. The language list is generated from the `lang` registry so it stays in sync if a new extractor is added.

**Out of scope.**
- No `--allow-unsupported` flag.
- No new exit code; standard `1`.
- No interaction with `--no-cache`; same logic on cache miss.
- No pre-scan extension peek; the post-scan check is the single source of truth.

## Implementation

A small helper in `internal/cli/analyze.go`:

```go
func supportedLanguages(scan *scanner.ProjectScan) []string {
    out := make([]string, 0, len(scan.Languages))
    for _, l := range scan.Languages {
        if l != "Generic" {
            out = append(out, l)
        }
    }
    return out
}
```

Called immediately after the scan returns, before docs-ingestion / LLM-tier setup. On empty result: write the message to `cmd.ErrOrStderr()`, return a non-`nil` error so Cobra propagates exit 1.

A second helper enumerates the `lang` registry's `Language()` values for the message body so adding a new extractor automatically updates the error text.

## Tests (TDD, per CLAUDE.md)

1. **`internal/cli/analyze_unsupported_lang_test.go`** — unit:
   - Markdown-and-JSON-only fixture: assert exit error, stderr contains file count and language list, no `mapping.md` / `gaps.md` written, `project.md` *was* written.
   - One-`main.go`-plus-markdown fixture: assert the language check passes (extend an existing test pattern that injects a fake LLM client, or stop short of LLM via the helper).
2. **`cmd/ftg/testdata/analyze_unsupported.txtar`** — `testscript` end-to-end against a markdown-only fixture; assert exit code 1 and stderr substring match.

## Verification

Add **Scenario 17: Unsupported-Language Repo** to `.plans/VERIFICATION_PLAN.md`:
- Fixture: tiny markdown-only repo.
- Success: exit 1, error message names the file count and the supported-language list, `mdfetch` was not called (verifiable via `-v`), no LLM calls observed.

## Files Touched

- `internal/cli/analyze.go` — add helper + check + error.
- `internal/cli/analyze_unsupported_lang_test.go` — new.
- `cmd/ftg/testdata/analyze_unsupported.txtar` — new.
- `.plans/VERIFICATION_PLAN.md` — append Scenario 17.
