# Multi-Language Support — Design

**Date:** 2026-04-24
**Status:** Validated, ready for implementation
**Implementation plan:** `.plans/MULTI_LANG_SUPPORT_PLAN.md`

## Problem

Find the Gaps currently extracts symbols from 4 languages (Go, Python, TypeScript, Rust). Every other file type falls through to `GenericExtractor`, which returns zero symbols. Docs for projects in other popular languages cannot be meaningfully analyzed.

## Goal

Raise the first-class language set from 4 to 13 by adding 9 new extractors: C, C++, Java, C#, Ruby, PHP, Kotlin, Swift, Scala. JavaScript remains supported through the existing `TypeScriptExtractor` (it already handles `.js`/`.jsx`/`.mjs`) and will be documented explicitly as supported.

Non-goals:
- No "thin tier" or generic fallback above `GenericExtractor`. Unsupported code languages continue to be treated as opaque text, the same as today.
- No change to markup/config formats (Markdown, YAML, TOML, HTML, CSS, Dockerfile, Protobuf) — they stay on `GenericExtractor`.
- No change to the `Extractor` interface.
- No change to language detection logic in `Detect()` beyond adding entries to the registry.

## Approach

Each new language gets its own file under `internal/scanner/lang/` that implements the existing `Extractor` interface, following the pattern established by `go.go`, `python.go`, `typescript.go`, and `rust.go`. No new abstractions.

### Per-language rules matrix

| Language | "Exported" rule | Signature shape | Doc comment | Imports |
|---|---|---|---|---|
| **C** | Declarations in `.h` headers are public. In `.c`, `static` = private; non-`static` top-level = public. | `ret_type name(params)` | Preceding `/** */` or `/* */` | `#include` |
| **C++** | Same as C + classes in headers; anonymous-namespace = private. | `ret_type Class::name(params)` for methods | Same as C | `#include` + `using` |
| **Java** | `public` keyword only. | `public ret Name(params)` with modifiers | `/** */` Javadoc | `import` |
| **C#** | `public` keyword only. | `public ret Name(params)` with modifiers | `///` XML doc comments | `using` |
| **Ruby** | Top-level methods/classes/modules emit by default. Skip anything after a bare `private` directive in the same scope. | `def name(params)` / `class Name` | Preceding `#` comment block | `require` / `require_relative` |
| **PHP** | `public` on class members; top-level functions/classes always public. Skip `private`/`protected`. | `function name(params): ret` | `/** */` PHPDoc | `use` |
| **Kotlin** | Default is public. Skip `private`/`internal`/`protected`. | `fun name(params): ret` / `class Name` | `/** */` KDoc | `import` |
| **Swift** | `public` or `open` only. Skip `internal` (default), `fileprivate`, `private`. | `func name(params) -> ret` | `///` or `/** */` | `import` |
| **Scala** | Default is public. Skip `private`, `private[pkg]`, `protected`. | `def name(params): ret` / `class Name` | `/** */` Scaladoc | `import` |

### YAGNI exclusions

- **C/C++ macros (`#define`):** skipped. Text substitution, not symbols.
- **Nested classes/modules:** top-level only, matching current Go behavior.
- **JavaScript non-ESM/non-CJS patterns (IIFEs, UMD wrappers, globals):** not added. `export` / `module.exports` covers the modern surface. CJS support is a future targeted change inside `typescript.go` if needed.

### Sequencing

Implementation order groups languages by shape-of-work, starting with those closest to existing code so interface friction surfaces early:

1. Java (modifier-keyword → easy second reference)
2. C# (mirrors Java)
3. Kotlin (JVM family)
4. Swift (similar to Kotlin)
5. Scala (similar to Kotlin)
6. PHP (modifier + PHPDoc)
7. Ruby (scope-based exportedness — different shape)
8. C (header-vs-source — novel)
9. C++ (extends C with classes)

### Testing

Each language's extractor gets table-driven unit tests in `<lang>_test.go` covering:

1. Exported function extracted
2. Non-exported function skipped
3. Class/type/struct extracted
4. Imports extracted
5. Doc comment captured

This matches the exact fixture set used for Go, Python, TypeScript, and Rust today. No new test infrastructure needed.

Each new extractor also gets registered in `detect.go` and picks up coverage automatically from `detect_test.go`'s table-driven extension-mapping tests (the test file gains new expected-entries; no new test function is needed per language).

## Out of scope / future work

- Generic/thin-tier support for the long tail of `smacker/go-tree-sitter` grammars (Lua, Bash, Elixir, etc.). Intentionally cut to keep scope tight — `GenericExtractor` already handles these gracefully as opaque text.
- Cross-language import resolution.
- Grammar version pinning beyond what `go.mod` already does.
- Promoting Protobuf to first-class. It is a real API surface and a reasonable future addition, but the current analyzer doesn't need it.
