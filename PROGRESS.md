# Progress

## Task: Scaffold CLI skeleton — COMPLETE

- Started: 2026-04-17
- Module path: `github.com/sandgardenhq/find-the-gaps`
- Go version: 1.26.1
- Created:
  - `cmd/find-the-gaps/main.go` + `main_test.go` (testscript driver)
  - `cmd/find-the-gaps/testdata/script/{help,version,doctor_stub,analyze_stub}.txtar`
  - `internal/cli/{root,doctor,analyze}.go` + `root_test.go`
  - `Makefile` with test/build/lint/cover/fmt/tidy targets
- TDD cycle: wrote failing testscript tests first (compile error + behavioral), then implemented minimal Cobra root + `doctor` / `analyze` stubs that return "not yet implemented" errors.
- Tests: 7 passing, 0 failing
- Coverage: 100.0% of statements (both packages)
- Build: Successful (`go build ./...`)
- Linting: Clean (`golangci-lint run`)
- Notes: `Execute()` was refactored to return `int` rather than call `os.Exit` directly, enabling unit-testability of the error-handling path. `SilenceErrors: true` on the root cobra command so `run` owns stderr formatting.
- Completed: 2026-04-17

## Task: Implement `doctor` subcommand — COMPLETE

- Started: 2026-04-17
- Goal: Replace the `doctor` stub with a real preflight check for `ripgrep` and `mdfetch` that exits 0 when both are available on `$PATH` and 1 otherwise, printing install hints on failure.
- TDD cycle:
  - **RED**: `internal/doctor/doctor_test.go` — 5 table-ish tests using hermetic `t.TempDir()` + `t.Setenv("PATH", …)` with fake shell-script binaries (`writeFakeBin`, `writeFailingBin`): AllPresent, MdfetchMissing, RgMissing, BothMissing, VersionCommandFails. `cmd/find-the-gaps/testdata/script/doctor_ok.txtar` for end-to-end invocation via the compiled binary.
  - **GREEN**: `internal/doctor/doctor.go` — `Tool` struct + `RequiredTools` list, `Run` shells out to each tool's `--version` via `exec.CommandContext`, prints found tools to stdout and missing/broken tools with install hints to stderr, returns 0/1. `internal/cli/doctor.go` RunE calls `doctor.Run` and propagates a non-zero code via `&ExitCodeError{Code: code}`.
  - Added `ExitCodeError` + `errorToExitCode` in `internal/cli/root.go` so subcommands can set a specific exit code without Cobra printing the error twice. `internal/cli/doctor_test.go` exercises both success and failure paths through the full `run()` entry point.
- Tests: 14 passing, 0 failing (doctor package + cli package + cmd testscripts)
- Coverage: 100.0% of statements across all three packages
- Build: Successful (`go build ./...`)
- Linting: Clean (`golangci-lint run`, 0 issues)
- Notes: Dropped `doctor_missing_*.txtar` testscripts — they are not hermetic because real `rg` on the dev machine shadows the stub `$WORK/bin`. The unit tests in `internal/doctor` cover the missing-binary paths with a fully isolated `PATH`.
- Completed: 2026-04-17

## Task 1–7: spider package foundation — COMPLETE (prior sessions)

See commit history on `feat/mdfetch-spider` for per-task detail.

## Task 8: spider.go — Crawl skips already-cached URLs — COMPLETE

- Started: 2026-04-17
- Goal: Verify that `Crawl` does not re-fetch URLs already present in `index.json`.
- TDD cycle:
  - **RED** (immediate pass): `TestCrawl_skipsCachedURLs` was appended to `spider_test.go`. Pre-populates `index.json` via `LoadIndex` + `Record`, then calls `Crawl` with the same URL as startURL. Asserts `fetchCount == 0` and that the result map contains the cached URL. Test passed immediately because `Crawl` already seeds `visited` from `idx.All()` and takes the `inFlight == 0` early-return path — correct per plan note "this test may already pass."
  - No production code change was required.
  - Added `"path/filepath"` import to `spider_test.go` (was absent; needed by `filepath.Join` in the new test).
- Tests: 27 passing, 0 failing, no races (`go test -race`)
- Coverage: 93.4% of statements (`internal/spider`)
- Build: Successful (`go build ./...`)
- Committed: `a201638` — `test(spider): verify Crawl skips already-cached URLs`
- Completed: 2026-04-17

## Task 9: cli/analyze.go — wire --docs-url, --cache-dir, --workers into Crawl — COMPLETE

- Started: 2026-04-17
- Goal: Replace the `analyze` stub with a real implementation that registers `--docs-url`, `--cache-dir`, and `--workers` flags and calls `spider.Crawl` with `spider.MdfetchFetcher`.
- TDD cycle:
  - **RED**: Created `internal/cli/analyze_test.go` with 2 plan-specified tests (`TestAnalyze_missingFlags_returnsError`, `TestAnalyze_helpFlag_exits0`) — both passed immediately on the stub (stub already errors when called; `--help` always exits 0). Added `TestAnalyze_flagsExist` to create a genuine RED: asserts `--docs-url` is a known flag. Confirmed FAIL: `unknown flag: --docs-url`.
  - **GREEN**: Replaced `internal/cli/analyze.go` with full implementation: 3 flags registered, `RunE` checks `docsURL != ""`, calls `spider.Crawl(docsURL, opts, spider.MdfetchFetcher(opts))`, prints `fetched N pages`.
  - **REFACTOR**: Updated `root_test.go` (renamed 2 stale tests from "not yet implemented" to "--docs-url is required") and updated `analyze_stub.txtar` testscript to match new behavior. Added `TestAnalyze_crawlFails_returnsError` to cover the `crawl failed` error branch by pointing `--cache-dir` at a regular file (triggers `MkdirAll` error in `Crawl`).
- Tests: all passing, 0 failing (4 packages)
- Coverage: internal/cli 94.3%, internal/spider 93.4%, internal/doctor 100.0%
- Build: Successful (`go build ./...`)
- Committed: `ca8eb07` — `feat(cli): wire --docs-url, --cache-dir, --workers into analyze`
- Completed: 2026-04-17
