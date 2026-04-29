# Default Ignore List — Design

Status: validated, ready to plan
Branch: `default-ignore-list`

## Problem

`internal/scanner/walker.go` skips five hardcoded directory names (`vendor`, `node_modules`, `__pycache__`, `venv`, `target`) and respects a single `.gitignore` at the repo root. Everything else — lockfiles, build artifacts, generated bindings, minified bundles, binary assets — reaches the analyzer. That noise pollutes LLM signal and inflates token cost. The five-entry list also covers only a fraction of the languages we support (C, C++, C#, Go, Java, Kotlin, PHP, Python, Ruby, Rust, Scala, Swift, TypeScript).

## Goal

Ship an opinionated, curated default ignore list covering common dependency manifests, lockfiles, vendored code, build artifacts, generated files, and binary assets across every supported language. Provide one clean escape hatch for projects that need to override.

## Non-goals

- A configurable system-wide defaults file. Defaults ship inside the binary.
- A TOML config file. None exists today; this feature does not introduce one.
- CLI flags for ignore overrides (`--no-default-ignores`, `--ignore-file`, `--ignore PATTERN`). YAGNI until a real use case appears.
- Detecting generated code by header content (`// Code generated ... DO NOT EDIT`). Suffix-based detection is enough for v1.

## Decisions

| Question | Choice | Why |
|---|---|---|
| Hardcoded vs configurable | Hardcoded defaults + project-local override | Strong defaults; one targeted escape hatch |
| Primary motivation | Signal quality (cost is a side benefit) | Manifests stay in (high-signal); lockfiles go out |
| Test files | Skip by default | Tests aren't user-facing surface |
| Generated code | Skip by suffix only | `.proto` / `.openapi` source-of-truth gives cleaner signal |
| Override file | `.ftgignore` at repo root, gitignore syntax | Discoverable; reuses `sabhiram/go-gitignore` already in `go.mod` |
| Layering | defaults → `.gitignore` → `.ftgignore`, last-write-wins, `!` negation works across all three | Mirrors how users already think about `.gitignore` |
| Reporting | One summary line in `analyze` output | Cheap confidence signal; surfaces broken defaults immediately |

## Categories

### Always skip — directories

- VCS / IDE: `.git/`, `.svn/`, `.hg/`, `.idea/`, `.vscode/`, `.vs/`
- Dependencies: `node_modules/`, `vendor/`, `bower_components/`, `.bundle/`, `.pnpm/`
- Python: `venv/`, `.venv/`, `env/`, `__pycache__/`, `.pytest_cache/`, `.mypy_cache/`, `.ruff_cache/`, `.tox/`
- Build artifacts: `dist/`, `build/`, `target/`, `out/`, `bin/`, `obj/`, `_build/`, `cmake-build-*/`, `Build/`
- JVM: `.gradle/`, `.mvn/`, `classes/`
- Coverage: `coverage/`, `.nyc_output/`, `htmlcov/`
- Site / doc generators: `_site/`, `public/` (Hugo), `.docusaurus/`, `.next/`, `.nuxt/`, `.svelte-kit/`
- Test scaffolding: `tests/`, `__tests__/`, `spec/`, `testdata/`, `__fixtures__/`, `__mocks__/`

### Always skip — file patterns

- Lockfiles: `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `go.sum`, `Cargo.lock`, `Gemfile.lock`, `Pipfile.lock`, `poetry.lock`, `composer.lock`, `*.lockb`
- Test files: `*_test.go`, `*.test.ts`, `*.test.js`, `*.spec.ts`, `*.spec.js`, `*Test.java`, `*Tests.swift`, `*_spec.rb`, `test_*.py`, `*_test.py`
- Generated code: `*.pb.go`, `*_pb2.py`, `*_pb2.pyi`, `*_pb.d.ts`, `*.gen.go`, `*.gen.ts`, `*_generated.go`, `*.g.dart`
- Minified / bundled: `*.min.js`, `*.min.css`, `*.bundle.js`, `*.bundle.css`, `*.map`
- Binaries / assets: `*.png`, `*.jpg`, `*.jpeg`, `*.gif`, `*.svg`, `*.webp`, `*.ico`, `*.pdf`, `*.zip`, `*.tar`, `*.tar.gz`, `*.tgz`, `*.7z`, `*.rar`, `*.woff`, `*.woff2`, `*.ttf`, `*.eot`, `*.mp3`, `*.mp4`, `*.mov`, `*.wav`, `*.exe`, `*.dll`, `*.so`, `*.dylib`, `*.class`, `*.jar`, `*.war`, `*.o`, `*.a`, `*.wasm`, `*.bin`, `*.dat`
- OS noise: `.DS_Store`, `Thumbs.db`, `desktop.ini`
- Logs / dumps: `*.log`, `*.tmp`, `*.cache`

### Always KEEP (documented, not patterns)

These files almost-match the categories above but should never be skipped. Listed here so future-us doesn't accidentally exclude them.

- Manifests: `package.json`, `go.mod`, `pyproject.toml`, `Cargo.toml`, `Gemfile`, `composer.json`, `pom.xml`, `build.gradle*`, `Package.swift`
- READMEs at any depth
- `examples/`, `example/`, `demos/`, `samples/`
- `docs/` in the repo

## Architecture

```
internal/scanner/
├── defaults.ftgignore          (NEW; gitignore syntax, embedded via //go:embed)
├── ignore/                     (NEW package)
│   ├── defaults.go             (//go:embed of defaults.ftgignore)
│   ├── matcher.go              (Matcher, Decision, Stats)
│   └── matcher_test.go
├── walker.go                   (slimmed; loses skippedDirs map)
└── walker_test.go              (expanded)
```

### Matcher

```go
package ignore

type Matcher struct { layers []layer }

type layer struct {
    name    string                 // "defaults", ".gitignore", ".ftgignore"
    matcher *gitignore.GitIgnore
}

type Decision struct {
    Skip   bool
    Reason string  // layer name; "" if no layer matched
}

type Stats struct {
    Scanned int
    Skipped map[string]int  // layer name -> count
}

func Load(repoRoot string) (*Matcher, error)
func (m *Matcher) Match(relPath string, isDir bool) Decision
```

### Layering semantics

Each layer compiles independently. Per path, evaluate layers in order; the last layer that matches (positively or negatively) wins. This preserves cross-layer negation (`!vendor/` in `.ftgignore` overrides defaults) and gives clean attribution for the summary line.

### Walker

`Walk` returns `(Stats, error)`. The walk loop loads the matcher once, then for each path asks for a `Decision`. Skipped directories return `filepath.SkipDir`; skipped files return `nil`. Counts increment in `Stats.Skipped[reason]`.

The current `skippedDirs` map is deleted. Its five entries move into `defaults.ftgignore`.

## CLI Surface

No new flags. After the scan phase and before LLM analysis, `analyze` prints:

```
scanned 412 files, skipped 1,847 (defaults: 1,801, .gitignore: 38, .ftgignore: 8)
```

Suppress zero-count segments. Suppress the whole line under `FIND_THE_GAPS_QUIET=1`.

`.ftgignore` syntax errors fail `analyze` with `error: .ftgignore line N: <message>`. Cache is not invalidated.

## Testing Strategy

- **Unit (matcher)**: table-driven, no filesystem; covers single-layer hits, cross-layer negation, dir vs file, anchored vs floating, missing layers, syntax errors.
- **Defaults validation**: asserts every non-comment line in `defaults.ftgignore` compiles. A curated `testdata/scenarios/` tree contains one file per category; tests assert each file's expected skip decision.
- **Walker integration**: real on-disk fixtures via `t.TempDir()`; assert returned files and `Stats`.
- **testscript**: `cmd/find-the-gaps/testdata/ignore.txtar` runs `ftg analyze` and asserts the summary line appears. A second scenario verifies `!vendor/` re-includes vendored files.

Coverage gates: `internal/scanner/ignore/` ≥90% statements; `internal/scanner/` ≥90% statements after refactor.

## Verification

LLM round-trips and real-public-repo coverage already exist in `.plans/VERIFICATION_PLAN.md` Scenario 1. No new verification scenario needed for this feature beyond the unit + integration suites above.
