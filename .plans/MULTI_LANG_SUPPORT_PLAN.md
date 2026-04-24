# Multi-Language Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add first-class extractor support for 9 new languages (C, C++, Java, C#, Ruby, PHP, Kotlin, Swift, Scala) so Find the Gaps can analyze docs-vs-code drift in them.

**Architecture:** Each new language gets one extractor file under `internal/scanner/lang/` implementing the existing `Extractor` interface. No changes to the interface, no new abstractions, no generic/thin tier. Unsupported code languages keep using `GenericExtractor`. Design doc: `.plans/2026-04-24-multi-lang-support-design.md`.

**Tech Stack:** Go 1.26+, `github.com/smacker/go-tree-sitter` (language grammars already vendored: `c`, `cpp`, `csharp`, `java`, `kotlin`, `php`, `ruby`, `scala`, `swift`), stdlib `testing`.

---

## Reference material

Before starting, read these files to understand the patterns you'll be copying:

- `internal/scanner/lang/extractor.go` — the interface you're implementing.
- `internal/scanner/lang/typescript.go` — best reference for modifier-keyword languages (Java, C#, Kotlin, Swift, Scala, PHP).
- `internal/scanner/lang/python.go` — best reference for convention-based exportedness (Ruby).
- `internal/scanner/lang/go.go` — best reference for languages with signature/receiver concepts (C, C++).
- `internal/scanner/lang/typescript_test.go` — test style to match (plain stdlib, one fixture per behavior, no testify in these files).
- `internal/scanner/lang/detect.go` — the registry you'll append to.
- `internal/scanner/lang/detect_test.go` — table-driven test you'll extend per language.
- `internal/scanner/types/types.go` — the `Symbol` / `Import` / `SymbolKind` shapes you return.
- `internal/scanner/lang/stubs_test.go` — shared fixture helpers, if needed.

## Ground rules (from CLAUDE.md — non-negotiable)

- **TDD**: write the failing test FIRST, run it, watch it fail for the right reason, then write the minimal code, watch it pass, commit. Every task below follows this cycle.
- **No task is complete** until: all tests pass, `go build ./...` succeeds with zero errors, `golangci-lint run` is clean, coverage per package ≥90%, and `PROGRESS.md` is updated.
- **Commit per language.** One language per atomic commit. Commit message format includes `- RED:` / `- GREEN:` lines. No more than 30 minutes of uncommitted work.
- **No plan files at repo root.** All plans under `.plans/`.

## Task dependencies

Tasks 1–9 (the 9 new extractors) are **independent** — each one only adds a new file plus appends to the registry. They can be executed in any order, or in parallel. The sequencing below (Java → C# → Kotlin → Swift → Scala → PHP → Ruby → C → C++) is the recommended order for a single engineer working serially; it groups by shape-of-work to maximize pattern reuse.

Task 0 and Task 10 are sequencing bookends.

---

## Task 0: Bootstrap — confirm grammars compile & baseline is green

**Files:**
- Read only: `go.mod`, `internal/scanner/lang/*.go`
- Create: None
- Modify: None

**Step 1:** Run the existing test suite to confirm you're starting from green.

```
go test ./...
```

Expected: all packages pass.

**Step 2:** Confirm every new-language grammar is reachable from Go imports. Create a scratch file to verify, then delete it:

```
cat > /tmp/grammar_check.go <<'EOF'
package main

import (
    _ "github.com/smacker/go-tree-sitter/c"
    _ "github.com/smacker/go-tree-sitter/cpp"
    _ "github.com/smacker/go-tree-sitter/csharp"
    _ "github.com/smacker/go-tree-sitter/java"
    _ "github.com/smacker/go-tree-sitter/kotlin"
    _ "github.com/smacker/go-tree-sitter/php"
    _ "github.com/smacker/go-tree-sitter/ruby"
    _ "github.com/smacker/go-tree-sitter/scala"
    _ "github.com/smacker/go-tree-sitter/swift"
)

func main() {}
EOF
go build -o /dev/null /tmp/grammar_check.go
rm /tmp/grammar_check.go
```

Expected: exits 0. If any grammar is missing, stop and ask — do not add modules on your own.

**Step 3:** No commit — this task writes no files.

---

## Tasks 1–9: Per-language extractor

Tasks 1–9 follow the **same shape**. The template below is the contract; the per-language sections below fill in the specifics (grammar import path, extractor struct name, extensions, exportedness rule, fixture source, etc.).

### Template (read once, then apply to each language)

**Files (substitute `<lang>` with the lowercase language name — e.g. `java`):**
- Create: `internal/scanner/lang/<lang>.go`
- Create: `internal/scanner/lang/<lang>_test.go`
- Modify: `internal/scanner/lang/detect.go` (append to `registry`)
- Modify: `internal/scanner/lang/detect_test.go` (add table entry asserting each of the new extensions resolves to the new extractor)
- Modify: `README.md` (add row to the "Supported languages" table)
- Modify: `PROGRESS.md` (append completion entry)

**Step 1: Write the failing tests first.** Create `<lang>_test.go` with these five cases, in this order:

1. `TestXExtractor_exportedFunc_extracted` — one exported function, asserts the returned symbol's `Name`, `Kind`, and `Line`.
2. `TestXExtractor_nonExportedFunc_skipped` — one non-exported (by that language's rules) function alongside an exported one; asserts only the exported one is returned.
3. `TestXExtractor_classOrType_extracted` — one exported class/struct/record; asserts `Kind`.
4. `TestXExtractor_imports_extracted` — one file with two import statements (one plain, one with alias if the language supports aliases); asserts both paths appear.
5. `TestXExtractor_docComment_captured` — one exported function with an idiomatic doc comment immediately above it; asserts the `DocComment` field is populated and leading/trailing comment markers are stripped.

Tests use plain stdlib patterns (no testify in these files) — follow the shape of `internal/scanner/lang/typescript_test.go`.

**Step 2: Run tests — expect RED.**

```
go test ./internal/scanner/lang/ -run '^TestXExtractor_' -v
```

Expected: all 5 tests fail with `undefined: XExtractor`.

**Step 3: Create `<lang>.go` with a minimal stub that makes only the first test compile.** The stub must implement `Extractor`:

```go
type XExtractor struct{}

func (e *XExtractor) Language() string     { return "X" }
func (e *XExtractor) Extensions() []string { return []string{".ext"} }
func (e *XExtractor) Extract(_ string, _ []byte) ([]types.Symbol, []types.Import, error) {
    return nil, nil, nil
}
```

Re-run tests — all still RED, but now "expected 1 symbol, got 0" instead of compile errors. This confirms the test harness is wired correctly.

**Step 4: Implement `Extract` incrementally, test-by-test.** For each of the 5 tests in order:
- Make the test green with the minimum code required.
- Re-run the full file's tests — earlier ones must stay green.
- Do NOT skip ahead to implement features for later tests.

Reference the matching existing extractor (listed in each per-language task below) for patterns: how to walk the root node, how to fetch names via `ChildByFieldName`, how to assemble signatures, how to find preceding comment nodes.

**Step 5: Register the extractor.** Open `internal/scanner/lang/detect.go` and append one line to the `registry` slice in the `init()` function:

```go
&XExtractor{},
```

**Step 6: Add detect coverage.** In `internal/scanner/lang/detect_test.go`, add one row per new extension asserting `Detect("foo.ext").Language() == "X"`.

**Step 7: Run full scanner tests.**

```
go test ./internal/scanner/... -v
```

Expected: all pass.

**Step 8: Run full suite + lint + coverage.**

```
go test ./...
golangci-lint run
go test -coverprofile=coverage.out ./internal/scanner/lang/ && go tool cover -func=coverage.out | tail -5
```

Expected: all tests green, zero lint findings, per-package coverage ≥90% statements.

**Step 9: Update README.** Add one row to the "Supported languages" table in `README.md`.

**Step 10: Update PROGRESS.md** with the timestamp, task ID, test count, coverage number, and any notes.

**Step 11: Commit.** One commit per language.

```
git add internal/scanner/lang/<lang>.go internal/scanner/lang/<lang>_test.go \
        internal/scanner/lang/detect.go internal/scanner/lang/detect_test.go \
        README.md PROGRESS.md
git commit -m "$(cat <<'EOF'
feat(scanner): add <Lang> extractor

- RED: added 5 tests covering exported/non-exported/class/imports/docs for <Lang>
- GREEN: minimal tree-sitter-based extractor using `github.com/smacker/go-tree-sitter/<lang>`
- Registered in detect.go; README supported-languages table updated
- Status: N tests passing, build ✅, lint ✅
- Coverage: internal/scanner/lang ≥90%
EOF
)"
```

### Task 1: Java

**Grammar import:** `github.com/smacker/go-tree-sitter/java`
**Struct:** `JavaExtractor` — `Language() = "Java"`, `Extensions() = []string{".java"}`
**Exportedness:** a declaration is exported iff its modifiers include the `public` keyword. `protected`, package-private, and `private` are all skipped.
**Relevant tree-sitter node types:** `class_declaration`, `interface_declaration`, `record_declaration`, `method_declaration`, `enum_declaration`. Modifiers live in a `modifiers` child of each declaration.
**Signature:** include modifier keywords + return type + name + parameters (slice the source from declaration start up to the opening `{` or `;`, trim whitespace).
**Doc comment:** immediately-preceding `comment` sibling whose text starts with `/**`; strip `/**` and trailing `*/`, trim.
**Imports:** top-level `import_declaration` nodes. Path is the full dotted name as a string; no alias concept in Java.
**Kind mapping:** `class_declaration` / `record_declaration` → `KindClass`; `interface_declaration` → `KindInterface`; `method_declaration` → `KindFunc`; `enum_declaration` → `KindType`.
**Reference extractor to copy from:** `typescript.go` (closest shape — modifier-keyword gating).

### Task 2: C#

**Grammar import:** `github.com/smacker/go-tree-sitter/csharp`
**Struct:** `CSharpExtractor` — `Language() = "C#"`, `Extensions() = []string{".cs"}`
**Exportedness:** `public` modifier only. `internal` (the C# default), `protected`, `private` all skipped.
**Relevant node types:** `class_declaration`, `interface_declaration`, `struct_declaration`, `record_declaration`, `enum_declaration`, `method_declaration`, `namespace_declaration` (descend into it to find nested public types).
**Signature:** modifiers + return type + name + parameters; same slice-to-brace approach as Java.
**Doc comment:** C# uses `///` XML doc comments on consecutive lines above the declaration. Collect consecutive immediately-preceding `comment` nodes whose text starts with `///`, strip the `///` prefix, join with newlines.
**Imports:** `using_directive` nodes. Path is the dotted name; if a `using X = Y.Z` alias is present, populate `Alias` with `X`.
**Kind mapping:** classes/records/structs → `KindClass`; interfaces → `KindInterface`; methods → `KindFunc`; enums → `KindType`.
**Reference:** `typescript.go` plus study `go.go`'s comment-block joining.

### Task 3: Kotlin

**Grammar import:** `github.com/smacker/go-tree-sitter/kotlin`
**Struct:** `KotlinExtractor` — `Language() = "Kotlin"`, `Extensions() = []string{".kt", ".kts"}`
**Exportedness:** Kotlin is **public by default**. Skip a declaration iff its modifiers include `private`, `internal`, or `protected`.
**Relevant node types:** `class_declaration`, `function_declaration`, `property_declaration`, `object_declaration`. Modifiers are in a `modifiers` child.
**Signature:** slice declaration source to the opening `{`, `=`, or newline.
**Doc comment:** KDoc `/** */` block immediately preceding; strip markers.
**Imports:** `import_header` nodes; Kotlin supports `as` aliasing (`import foo.Bar as Baz`).
**Kind mapping:** `class_declaration` / `object_declaration` → `KindClass`; `function_declaration` → `KindFunc`; `property_declaration` → `KindVar` (use `KindConst` if the property is `val` and its value is a compile-time constant — if you can't tell cheaply, use `KindVar` and move on).
**Reference:** `typescript.go`.

### Task 4: Swift

**Grammar import:** `github.com/smacker/go-tree-sitter/swift`
**Struct:** `SwiftExtractor` — `Language() = "Swift"`, `Extensions() = []string{".swift"}`
**Exportedness:** Emit iff an access-level modifier `public` or `open` is present. Skip `internal` (the default when no modifier is given), `fileprivate`, `private`.
**Relevant node types:** check the actual tree-sitter-swift grammar node types before coding. Likely include `class_declaration`, `struct_declaration`, `enum_declaration`, `protocol_declaration`, `function_declaration`, `property_declaration`. Node names in this grammar can be different from other languages — confirm with a quick tree-sitter parse of a sample file if names don't match the above.
**Signature:** slice to first `{` or `=` or newline.
**Doc comment:** either consecutive `///` comments or a single `/** */` block. Support both.
**Imports:** `import_declaration` nodes; no alias concept.
**Kind mapping:** class/struct/enum → `KindClass`; protocol → `KindInterface`; func → `KindFunc`; property → `KindVar`.
**Reference:** `typescript.go` for modifier gating; if node-type guesses above are wrong, write a one-off debug test that prints `root.String()` for a sample file and adjust.

### Task 5: Scala

**Grammar import:** `github.com/smacker/go-tree-sitter/scala`
**Struct:** `ScalaExtractor` — `Language() = "Scala"`, `Extensions() = []string{".scala", ".sc"}`
**Exportedness:** **Public by default.** Skip iff modifiers include `private` (with or without `private[pkg]` suffix) or `protected`.
**Relevant node types:** `class_definition`, `object_definition`, `trait_definition`, `function_definition`, `val_definition`, `var_definition`. Verify names against the vendored grammar.
**Signature:** slice source to first `{` or `=` or newline.
**Doc comment:** Scaladoc `/** */` block preceding.
**Imports:** `import_declaration` nodes.
**Kind mapping:** class/object → `KindClass`; trait → `KindInterface`; function → `KindFunc`; val → `KindConst`; var → `KindVar`.
**Reference:** `typescript.go`.

### Task 6: PHP

**Grammar import:** `github.com/smacker/go-tree-sitter/php`
**Struct:** `PHPExtractor` — `Language() = "PHP"`, `Extensions() = []string{".php"}`
**Exportedness:** top-level `function_definition` / `class_declaration` / `interface_declaration` / `trait_declaration` / `enum_declaration` are always public. For class members: emit only if `public` modifier is present; skip `private`/`protected`. Members without an access modifier are implicitly public in PHP — emit them.
**Relevant node types:** `function_definition`, `class_declaration`, `interface_declaration`, `trait_declaration`, `method_declaration`, `enum_declaration`.
**Signature:** slice to `{`. Include return-type annotation if present.
**Doc comment:** PHPDoc `/** */` block immediately preceding; strip markers.
**Imports:** `namespace_use_declaration` nodes. Populate `Alias` if the `as` form is used (`use Foo\Bar as Baz`).
**Kind mapping:** class/trait/enum → `KindClass`; interface → `KindInterface`; function/method → `KindFunc`.
**Reference:** `typescript.go`.

### Task 7: Ruby

**Grammar import:** `github.com/smacker/go-tree-sitter/ruby`
**Struct:** `RubyExtractor` — `Language() = "Ruby"`, `Extensions() = []string{".rb"}`
**Exportedness:** conceptually different from the modifier-keyword languages. Ruby marks visibility by emitting a bare `private` (or `protected`) **directive** in a class/module scope, after which all subsequent method definitions in that scope are private. Implementation approach: walk each class/module's body children in order, maintain a per-scope `currentVisibility` flag initialized to public; when you see a `call`/`identifier` node whose text is exactly `private` or `protected` with no arguments, flip the flag for the remainder of that scope; only emit methods while the flag is public. Top-level methods (outside any class/module) are always public.
**Relevant node types:** `class`, `module`, `method` (instance method), `singleton_method` (class method — emit these too, e.g. `self.foo`), `method_declaration` (some grammar versions use this instead of `method`). Verify against the vendored grammar.
**Signature:** `def name(params)` — slice to newline or first statement.
**Doc comment:** consecutive `#`-prefixed comment lines immediately above the declaration. Join with newlines, strip leading `# ` from each line.
**Imports:** `call` nodes where the method name is `require` or `require_relative`, with a string argument. Path is the string literal contents.
**Kind mapping:** class → `KindClass`; module → `KindClass` (Ruby modules are class-like); method/singleton_method → `KindFunc`.
**Reference:** `python.go` for convention-based extractors, but Ruby's scope-tracking for `private` is unique — study existing extractors before coding, then write it yourself.

### Task 8: C

**Grammar import:** `github.com/smacker/go-tree-sitter/c`
**Struct:** `CExtractor` — `Language() = "C"`, `Extensions() = []string{".c", ".h"}`
**Exportedness:** split by file extension:
- For `.h` headers: every top-level `function_declarator` / `declaration` / `struct_specifier` / `type_definition` is public.
- For `.c` source: a top-level declaration is public iff it does **not** have a `storage_class_specifier` child whose text is `static`.
Do not emit `#define` macros — skip them entirely (see design doc's YAGNI list).
**Relevant node types:** `function_definition`, `declaration` (for global var/const), `struct_specifier`, `type_definition`, `enum_specifier`.
**Signature:** for functions, slice source from declaration start to the opening `{` and trim. For types, use `typedef struct X ...` or `struct X` as written.
**Doc comment:** `/** */` or `/* */` block immediately preceding; strip markers.
**Imports:** `preproc_include` nodes. Path is the contents of the `<...>` or `"..."` (strip the delimiters). No alias.
**Kind mapping:** function → `KindFunc`; struct/union → `KindClass` (closest match); typedef/enum → `KindType`; top-level variable → `KindVar`.
**Reference:** `go.go` for signature assembly; the header-vs-source split is novel — implement via `filepath.Ext(path)` inside `Extract`.

### Task 9: C++

**Grammar import:** `github.com/smacker/go-tree-sitter/cpp`
**Struct:** `CPPExtractor` — `Language() = "C++"`, `Extensions() = []string{".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx"}`
**Exportedness:** the C rule plus:
- Anonymous namespaces (`namespace { ... }`) are file-private — skip everything inside.
- Class members: the tree-sitter-cpp grammar surfaces `access_specifier` nodes (`public:`, `private:`, `protected:`) inside class bodies. Maintain a `currentAccess` flag per class body, default `private` for `class`, `public` for `struct`; only emit members while the flag is `public`.
**Relevant node types:** everything in Task 8 plus `class_specifier`, `namespace_definition`, `function_definition` with `field_identifier` name (member functions).
**Signature:** for member functions, include the class scope: `ReturnType Class::method(params)`.
**Doc comment:** same as C.
**Imports:** `preproc_include` plus `using_declaration` (`using std::string`). For `using namespace X`, emit an import with `Path = X`, no alias.
**Kind mapping:** same as C plus `class_specifier` → `KindClass`.
**Reference:** `go.go` + your own Task 8 code.

---

## Task 10: End-to-end verification

**Files:**
- Modify: `PROGRESS.md`

**Step 1: Full test suite + coverage.**

```
go test ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1
```

Expected: all green, `internal/scanner/lang` total coverage ≥90%.

**Step 2: Lint & build.**

```
golangci-lint run
go build ./...
```

Expected: zero findings, build success.

**Step 3: Smoke test `Detect()` for every new extension from the command line.**

Run the repo's `testdata/` detection tests if any exist (`go test ./internal/scanner/lang -run TestDetect -v`). Expected: the new extensions all resolve to their new extractors, not `GenericExtractor`.

**Step 4: Real-world sanity check** (per project verification doctrine: no mocks).

Pick any one small open-source repository in each of the 9 new languages (or as many as you can find in ≤30 minutes total) and run:

```
go run ./cmd/find-the-gaps analyze --repo <path-to-repo> --docs-url <docs-url>
```

This is a sanity check, not a full Scenario-9 verification. You are looking for:
- Analyzer does not panic or produce tree-sitter parse errors.
- The scan report names at least one symbol from at least one file in the new language.

If any language panics or produces empty output from a file that clearly contains public symbols, treat that as a bug in that language's extractor and fix before continuing.

**Step 5: Close out `PROGRESS.md`** with the aggregate result (all 9 languages complete, coverage number, any notes).

**Step 6: Commit closeout.**

```
git add PROGRESS.md
git commit -m "$(cat <<'EOF'
docs(progress): close out multi-language support milestone

- All 9 new extractors (Java, C#, Kotlin, Swift, Scala, PHP, Ruby, C, C++) complete
- Full suite green, coverage ≥90%, lint clean
EOF
)"
```

---

## Red flags — stop and ask

- A language's tree-sitter grammar has node types that differ from the names guessed in the per-language task. Do not guess — write a one-line debug test that prints `tree.RootNode().String()` for a sample file and copy the real node type names into the extractor.
- Coverage drops below 90% in `internal/scanner/lang`. Find the uncovered branches and add a test; do not lower the threshold.
- A test passes immediately on first run without going RED first. That means the test is wrong — fix the test (likely it's asserting on something already default).
- You find yourself wanting to add a new helper in `internal/scanner/types/` or a new method on the `Extractor` interface. Stop and ask — the design explicitly forbids interface changes.
- A language needs exportedness logic the design didn't anticipate. Capture the ambiguity in the commit message and ask before making a unilateral call.

---

## Completion criteria

This plan is complete when:

1. All 9 new extractor files exist, each with ≥5 passing tests.
2. `detect.go` registers all 9.
3. `README.md` "Supported languages" table lists all 14 (4 original + 9 new + JavaScript documented under the TypeScript row).
4. `go test ./...` is green.
5. `golangci-lint run` is clean.
6. Coverage for `internal/scanner/lang` is ≥90% statements.
7. `PROGRESS.md` has a dated entry for each task.
8. All 10 task commits are on the `feat/multi-lang-support` branch, ready for PR to `main`.
