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

## Task 1: Add dependencies + `internal/scanner/symbols.go` data types — COMPLETE

- Started: 2026-04-17
- Goal: Add go-tree-sitter and go-gitignore dependencies; define core data types for the scanner package.
- TDD cycle:
  - **RED**: Wrote `internal/scanner/symbols_test.go` with `TestProjectScan_JSONRoundTrip` and `TestSymbolKind_constants`. Ran `go test ./internal/scanner/...` — failed with compile errors (package `scanner` does not exist, all types undefined). Correct RED state.
  - **GREEN**: Created `internal/scanner/symbols.go` defining `SymbolKind`, `Symbol`, `Import`, `ScannedFile`, `GraphNode`, `GraphEdge`, `ImportGraph`, `ProjectScan`. Minimal — types only, no logic.
- Tests: 2 passing, 0 failing
- Coverage: [no statements] — correct; `symbols.go` contains only type/const declarations, no executable statements
- Build: Successful (`go build ./...`)
- Linting: Clean (`golangci-lint run`, 0 issues)
- Dependencies added: `github.com/smacker/go-tree-sitter@v0.0.0-20240827094217-dd81d9e9be82`, `github.com/sabhiram/go-gitignore@v0.0.0-20210923224102-525f6e181f06`
- Completed: 2026-04-17

## Task 2: Extractor Interface + Language Stubs - COMPLETE

- Started: 2026-04-17
- Goal: Define the `Extractor` interface in `internal/scanner/lang/extractor.go`, implement `Detect()` in `detect.go`, and add stub extractors for Go, Python, TypeScript, Rust, and Generic.
- TDD cycle:
  - **RED**: Wrote `detect_test.go` with 7 tests (per-language detection, generic fallback, binary nil return). All failed — package did not exist.
  - **GREEN**: Created `extractor.go` (interface), `detect.go` (registry + `Detect`), `go.go`, `python.go`, `typescript.go`, `rust.go`, `generic.go` (stub extractors). Tests passed; coverage was 75% due to uncovered `Extract()` stubs and `GenericExtractor.Extensions()`.
  - **COVERAGE FIX**: Added `stubs_test.go` with `TestStub_Extract_returnsNil` and `TestGenericExtractor_Extensions` to exercise all stub bodies. Coverage reached 100%.
- Tests: 9 passing, 0 failing (7 detect tests + 2 stub coverage tests)
- Coverage: internal/scanner/lang: 100.0% of statements
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-17
- Notes: Stub `Extract()` bodies intentionally return `nil, nil, nil` (to be replaced in Tasks 3-7 with real tree-sitter implementations). Coverage tests satisfy the 90% gate without adding logic.
