# Local Docs Default — Implementation Plan

## Goal

Stop forcing users to think about how their docs are hosted. Make `ftg analyze` work with a single flag (`--repo`) when docs live in the repo, accept a local path when they live elsewhere on disk, and reserve URL crawling for the case where docs really are on a website.

## New CLI Behavior

| Invocation                                         | What happens                                                                               |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `ftg analyze` (no flags)                           | Scan `--repo` (defaults to `.`) for markdown. No docs URL needed.                          |
| `ftg analyze --repo ./foo`                         | Scan `./foo` on disk for markdown.                                                         |
| `ftg analyze --repo ./foo --docs ./docs`           | Scan `./foo` for code, `./docs` on disk for markdown.                                      |
| `ftg analyze --repo ./foo --docs https://x.com/y`  | Scan `./foo` for code, crawl `https://x.com/y` with mdfetch.                               |
| `ftg analyze --repo ./foo --docs https://github.com/owner/repo` | Forge URL → on-disk scan of `--repo` (existing behavior, unchanged).           |

`--docs` accepts a path or URL. Anything that does not start with `http://` or `https://` is treated as a path. `--docs` is optional. `--forge` is removed.

## Files to Change

### `internal/cli/analyze.go`

- **L96** — keep `docsURL` variable but rename to `docs` for clarity.
- **L109** — delete `forgeFlag` declaration.
- **L154** — change call to `forge.Resolve(docs, repoPath)` (drop the third arg). The function now handles the new modes too.
- **L161-L165** — error message refers to "source-control forges" — keep, but the new local-path-missing case needs its own error path (added in `forge.Resolve`).
- **L704** — rename flag: `cmd.Flags().StringVar(&docs, "docs", "", "URL or local path of docs to analyze (default: scan --repo on disk)")`.
- **L705** — delete `MarkFlagRequired("docs-url")`.
- **L720-L721** — delete `--forge` registration.
- **L225** — `log.Infof("crawling %s", docs)` — only reached when `docs` is a URL, so message is still correct.

### `internal/forge/resolve.go`

Replace the body of `Resolve` with a dispatcher that picks one of four modes, in order:

1. **`docs == ""` (no flag)** → on-disk scan of `repoPath`. Synthesized URL format decided by Open Question Q1 below.
2. **`docs` has no `http://` / `https://` scheme** → on-disk scan of that path. `os.Stat` it; return a clear error if missing or not readable. Synthesized URLs use `file://<abs-path>`.
3. **`docs` is a forge URL** → existing forge logic (origin-match, walk repo). Unchanged.
4. **`docs` is any other URL** → return `Result{OnDisk: false}`; caller crawls.

The `Result` struct stays the same. The `forgeFlag` parameter and `allowedForgeFlags` map are deleted.

`Notice` text per mode:
- Mode 1: `"no --docs provided; reading markdown from <repo> on disk."`
- Mode 2: `"reading markdown from <abs-docs-path> on disk."`
- Mode 3: existing message.

### `internal/forge/walk.go`

`Walk` currently takes `host, owner, name` and synthesizes forge URLs. Add a sibling helper or extend the signature so it can synthesize `file://` URLs for modes 1 and 2. Cleanest: a new exported `WalkLocal(root string, urlPrefix string)` that emits `urlPrefix + relPath` per file, and the existing `Walk` calls it with a forge-shaped prefix. Keep the doc-extension filter (`.md`, `.markdown`, `.mdx`, `.rst`, `.adoc`, `.asciidoc`) and the skip list (`.git`, `node_modules`, `vendor`).

### `internal/forge/resolve_test.go` and `internal/forge/walk_test.go`

- Delete `--forge`-flag tests.
- Add tests:
  - `Resolve("", "/repo", ...)` → on-disk, `file://` URLs.
  - `Resolve("./docs", "/repo", ...)` → on-disk on `./docs`.
  - `Resolve("/missing/path", "/repo", ...)` → error names the path.
  - Forge-URL case still works without the `forgeFlag` arg.

### `internal/cli/analyze_forge_test.go`

Drop `--forge`-flag invocations. Add cases for the no-flag and local-path defaults.

### `cmd/ftg/testdata/script/*.txtar`

- Update every existing `analyze ... --docs-url ...` invocation to `--docs`.
- Delete or rewrite `analyze_forge_*.txtar` cases that exercise `--forge`.
- Add new scripts:
  - `analyze_no_docs.txtar` — `ftg analyze --repo .` scans repo's markdown.
  - `analyze_local_docs.txtar` — `ftg analyze --repo . --docs ./docs` scans the directory.
  - `analyze_local_docs_missing.txtar` — passing a non-existent path produces a clear error and non-zero exit.

### `README.md`

Replace every `--docs-url` reference with `--docs`. Add a "Default behavior" sentence in the quickstart: "If you omit `--docs`, ftg scans your repo for markdown and treats those as your docs."

### `action.yml`

The action exposes a `docs-url` input that maps to `--docs-url` internally. Decision (Q3 below): rename input to `docs` to match, or keep `docs-url` as a stable external name and map it to `--docs`.

### `CHANGELOG.md`

Note the breaking flag changes (`--docs-url` → `--docs`, `--forge` removed) under the next release header.

### `.plans/VERIFICATION_PLAN.md`

Update every scenario invocation to use `--docs`. Add to Scenario 9 (or a new scenario) coverage for: no-flag default, local-path mode, missing-path error.

## TDD Order

1. **`internal/forge/walk_test.go`** — write failing test for `WalkLocal` emitting `file://` URLs. Implement.
2. **`internal/forge/resolve_test.go`** — failing test for `Resolve("", repo)`. Implement mode 1.
3. **`internal/forge/resolve_test.go`** — failing test for `Resolve("./docs", repo)`. Implement mode 2.
4. **`internal/forge/resolve_test.go`** — failing test for missing-path error. Implement.
5. **`internal/forge/resolve_test.go`** — delete `--forge`-flag tests, prove forge URL case still passes without that arg.
6. **`internal/cli/analyze.go`** — rename flag, drop `--forge`, drop required marker. Run existing CLI tests, fix breakage.
7. **`cmd/ftg/testdata/script/analyze_no_docs.txtar`** — failing test, then make it green.
8. **`cmd/ftg/testdata/script/analyze_local_docs.txtar`** — failing test, then green.
9. **`cmd/ftg/testdata/script/analyze_local_docs_missing.txtar`** — failing test, then green.
10. README, CHANGELOG, action.yml, VERIFICATION_PLAN updates.
11. Final `go test ./...`, `golangci-lint run`, `go build ./...`.

## Decisions (locked)

1. **No-`--docs` URL synthesis** — always `file://`. No origin auto-detection. Users who want forge-shaped clickable links in reports pass `--docs https://github.com/x/y` explicitly.
2. **Backward-compat** — hard-break. No `--docs-url` alias.
3. **`action.yml` input name** — rename `docs-url` → `docs` in lockstep with the CLI.
4. **Explicit `--docs` wins** — passing `--docs ./local` in a forge-clone repo produces `file://` URLs, not forge-shaped ones.
