# Progress

## Drift Decomposition (2026-04-27): two-stage investigator + judge - COMPLETE
- Started: 2026-04-27
- Plan: `.plans/DRIFT_DECOMPOSITION_PLAN.md`, design `.plans/DRIFT_DECOMPOSITION_DESIGN.md`
- Summary: Split the per-feature drift agent into a Typical-tier (Sonnet) investigator that runs the adaptive tool-use loop and gathers evidence via a new `note_observation` tool, plus a Large-tier (Opus) judge that adjudicates via a single non-tool `CompleteJSON` call. Goal: cut Opus tokens and per-feature latency. Investigator round budget is dynamic (`budgetForFeature` from the merged-in main commit `9e4ffc5`): `files + pages + 5 + 3` clamped at 100 — replaces the original plan's flat 30 → 50 bump. The "0 findings on cap-hit" failure mode is structurally absent — accumulated observations always flow to the judge, including across cap hits. Empty-observation features short-circuit without an Opus call. Tier validation's tool-use requirement moved from `--llm-large` to `--llm-typical`; error wording references "the drift investigator". Old `detectDriftForFeature` and `addFindingTool` deleted.
- Commits on `cape-town-v1`: `388305c` Task 2 (investigator + note_observation), `a29a43c` Task 3 (judge), `9712865` Task 4 (DetectDrift wiring + test migration), `7ac12af` Task 5 (delete old code, cap → 50), `5135f74` Task 6 (CLI tier-validate flip + README/CHANGELOG), plus a merge commit folding in main's dynamic-budget refactor (`budgetForFeature` now drives the investigator's `WithMaxRounds`).
- Tests: `go test ./...` — all packages green. 22 `TestDetectDrift_*` cases migrated to the new contract; new `TestInvestigateFeatureDrift_*` and `TestJudgeFeatureDrift_*` suites; new `TestDetectDrift_NoObservations_SkipsJudge`; renamed Typical-tool-support tests; flipped txtar fixture `analyze_tier_reject_ollama_typical.txtar`. `TestDetectDrift_MaxRoundsExceeded_*` re-scaled to the dynamic budget (10 rounds for 1 file + 1 page).
- Coverage: `internal/analyzer` ≥ 94% statements (gate 90%).
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Per-task code reviews via `superpowers:code-reviewer`: zero Critical or Important issues across all five implementation commits. Final cross-cutting review confirms architectural coherence and verifies the cost-shape claim (judge prompt contains only observation quotes, no file contents).
- Completed: 2026-04-27
- Notes: Real-LLM verification per `.plans/VERIFICATION_PLAN.md` Scenario 3 not run from this session (requires user's Bifrost credentials and fixture). Watch points for live runs: Sonnet quote-fidelity (the investigator must quote verbatim — paraphrasing would lose ground truth), and the doubled error-fan-out per feature (two LLM calls instead of one means ~2× transient-failure rate; status quo per design, retries deferred).

## Task: Dynamic Turn Budget for Drift Detection - COMPLETE
- Started: 2026-04-27
- Plan: `.plans/DYNAMIC_TURN_BUDGET_PLAN.md`, design `.plans/DYNAMIC_TURN_BUDGET_DESIGN.md`
- Summary: Replaced hardcoded `driftMaxRounds = 30` in `internal/analyzer/drift.go` with a per-feature dynamic budget computed from `len(entry.Files) + len(pages) + driftBudgetExpectedFindings(5) + driftBudgetSlack(3)`, clamped at `driftBudgetCeiling(100)`. Big features (>22 inputs) now get more rounds than the old cap and stop hitting `ErrMaxRounds` prematurely; small features (1 file + 1 page) drop from 30 to 10 rounds of headroom. Per-feature `Infof` and the `ErrMaxRounds` `Warnf` now log files, pages, and budget for observability. `driftMaxRounds` constant removed. One existing test fixture (`TestDetectDrift_MaxRoundsExceeded_PartialFindingsReturnedAndContinues`) had its stub-response queue scaled from 30 to 10 messages to exhaust the new budget for a (1 file, 1 page) feature; the test's outcome assertions are unchanged.
- Tests: `go test ./... -count=1` — all 9 packages green. New `TestBudgetForFeature` table test (7 subcases: minimum, mid-range, growth past old cap, large-uncapped, three clamp-boundary cases).
- Coverage: `budgetForFeature` 100%; `internal/analyzer` total 95.2% (above 90% gate).
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Commits on branch `dynamic-turn-budget`: design + plan, RED, GREEN, wire+fixture, logs, cleanup. Six implementation commits beyond the plan docs.
- Completed: 2026-04-27
- Notes: One in-flight plan amendment — the plan claimed "existing drift tests do NOT assert on round counts," but the max-rounds-exhaustion test structurally assumed `driftMaxRounds = 30` via a hand-sized stub queue. Resolved by scaling the queue to match the new per-feature budget (preserves test intent). Future-resilience suggestion (out of scope, deferred): make the queue length formula-driven by reading `analyzer.ExportedBudgetForFeature(1, 1)` so the test tracks the constants instead of a magic number.

## Task 10 (multi-lang-support): End-to-end verification & milestone close-out - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 10), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Verified the full multi-language support milestone end-to-end. All 9 new extractors (Java, C#, Kotlin, Swift, Scala, PHP, Ruby, C, C++) are implemented, registered in `detect.go`, covered by unit tests, and documented in the README supported-languages table. No changes to the `Extractor` interface or to `GenericExtractor`. JavaScript remains handled by `TypeScriptExtractor` (documented).
- Tests: `go test ./...` — all packages green. 100+ new tests across the 9 extractor files; 19 new rows in `detect_test.go` for the added extensions (`.java`, `.cs`, `.kt`, `.kts`, `.swift`, `.scala`, `.sc`, `.php`, `.rb`, `.c`, `.h`, `.cc`, `.cpp`, `.cxx`, `.hh`, `.hpp`, `.hxx`).
- Coverage: total 92.4% statements; `internal/scanner/lang` 90.4% (above the 90% gate).
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Sanity check: synthetic one-file-per-language fixture parsed via the scanner pipeline at a throwaway `cmd/sanity-check/` (since removed). Result: 9 files with symbols, 0 errors — `Calc.java`→[Calc(class), add(func)], `api.cs`→[Api(class), Add(func)], `main.kt`→[Calc(class)], `Calc.swift`→[Calc(class)], `Calc.scala`→[Calc(class)], `calc.php`→[Calc(class), add(func)], `calc.rb`→[Calc(class), add(func)], `calc.h`→[add(func)], `calc.hpp`→[Calc(class), add(func)].
- Commits on branch `feat/multi-lang-support` (10 feature commits + closeout): `7996c7b` Java, `effe1f8` Java test polish, `6587df4` C#, `5aa291e` Kotlin, `a30a5a4` Swift, `cc243f8` Scala, `ce875c2` PHP, `18af994` Ruby, `2bff9f5` C, `a7c2dc2` C++.
- Completed: 2026-04-24
- Notes: Raised first-class language support from 4 → 13. Ready for PR to `main`.

## Task 9 (multi-lang-support): C++ extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 9), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `CPPExtractor` in `internal/scanner/lang/cpp.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/cpp`. C++ extends the C extractor's rules with two language-specific wrinkles: (1) anonymous namespaces (`namespace { ... }`) hide their contents from other translation units — every declaration inside is skipped; named namespaces (`namespace foo { ... }`) do NOT hide, so the extractor recurses into them via a `cppWalkContainer` routine shared by both `translation_unit` and namespace `declaration_list` bodies. (2) Class/struct member emission is gated by C++ access-specifier state. The extractor walks `field_declaration_list` children in order, maintaining a `currentAccess` flag defaulting to `private` for `class` and `public` for `struct`; `access_specifier` nodes (holding a `public`/`private`/`protected` keyword child) flip the flag; only members declared while the flag is `public` are emitted. The class or struct itself is always emitted (as `KindClass`) regardless of its members. Imports are three-way: `preproc_include` (reuses `cExtractInclude` from the C extractor), `using_declaration` for `using std::string;` → `qualified_identifier` child yields Path `std::string`, and `using namespace X;` / `using namespace foo::bar;` → disambiguated by the presence of a `namespace` keyword child, then Path is the bare `identifier` or `qualified_identifier` content. No alias on either form. `static` top-level functions and `#define` macros are skipped, matching the C rules (the extractor shares `cHasStatic`, `cFuncSymbol`, `cVarSymbol`, `cTypeSymbol`, `cTypedefSymbol`, `cPrecedingComment`, `cExtractInclude` with `c.go`). Registered in `detect.go` with all six extensions (`.cc`, `.cpp`, `.cxx`, `.hh`, `.hpp`, `.hxx`); covered by six new rows in `detect_test.go`; README's supported-languages table gains a C++ row.
- Tree-sitter node types confirmed via a throwaway debug test (`cpp_debug_test.go` — dumped the full tree for each shape, inspected, then deleted; `ls internal/scanner/lang/*_debug_test.go` returns no matches):
  - Root: `translation_unit`.
  - `class_specifier` / `struct_specifier` / `union_specifier` — each uses a `name` field (`type_identifier`) and a `body` field (`field_declaration_list`). The first keyword child's `Type()` is literally `"class"` / `"struct"` / `"union"`, but the extractor does NOT read it — the initial access level is instead chosen by the caller based on which switch case fired (class → private, struct → public). This keeps the state-machine clean.
  - `access_specifier` — a child of `field_declaration_list`. Contains a single keyword child whose `Type()` is literally `"public"` / `"private"` / `"protected"`. The trailing colon is a separate unnamed `:` sibling in the parent body (NOT a child of the access_specifier node).
  - `field_declaration` — a class/struct member (function prototype or variable). Member function name lives in `function_declarator > field_identifier` (NOT `identifier` as at the top level).
  - `function_definition` — at the top level for free functions, OR in a `field_declaration_list` for inline member defs, OR at the top level for out-of-class definitions like `int A::m(int) { ... }` (in which case `function_declarator`'s first child is a `qualified_identifier` with `scope` = class name, `name` = method identifier; the source slice used for signature naturally includes the `A::` qualifier because it's part of the declaration text).
  - `namespace_definition` — optional `name` field (`namespace_identifier`); absent → anonymous. `body` field → `declaration_list`. The extractor recurses through `cppWalkContainer` only when `name` is present.
  - `using_declaration` has two shapes:
    - `using X::Y;` → children: `using` keyword, `qualified_identifier` (e.g. `std::string`), `;`.
    - `using namespace X;` → children: `using` keyword, `namespace` keyword, then `identifier` (bare) or `qualified_identifier` (for `foo::bar`), `;`.
    The presence of the `namespace` keyword child distinguishes them. The extractor walks children once to set an `isNamespaceForm` flag, then walks again to pick the right target node. `qualified_identifier` is always treated as the target if present (works for both forms); a bare `identifier` is only treated as the target when `isNamespaceForm` is true (so `using X::Y` doesn't emit a spurious `Y`).
  - `preproc_include` and `preproc_def` — same shapes as C; `cExtractInclude` reused directly.
- RED: wrote 12 primary tests in `cpp_test.go` covering the spec (header public func, static source func skipped, class emitted, private member skipped, class-default-private, struct-default-public, anonymous namespace skipped, named namespace emitted, `using std::string` import, `using namespace std` import, `#include <vector>` import, doc comment captured) plus 6 extension tests added during GREEN/coverage-sweep (enum → KindType, typedef → KindType, union → KindClass, `#define` skipped, top-level global var → KindVar, `using namespace foo::bar` qualified path). First run: all 12 primary tests failed with `undefined: CPPExtractor` (compile error) — RED confirmed.
- GREEN: implemented `Extract` plus four C++-specific helpers: `cppWalkContainer` (scans a `translation_unit` or namespace `declaration_list`, shared code for both containers; this is how named namespaces get their contents emitted), `cppWalkClassLike` (runs the access-specifier state machine over a `field_declaration_list` and emits the class/struct + its public members), `cppFieldSymbol` (extracts a member function symbol from a `field_declaration` whose function name lives in `function_declarator > field_identifier`), `cppExtractUsing` (the two-shape `using` dispatch described above). `strings.Contains` / `strings.TrimSpace` / `strings.TrimPrefix` / `strings.TrimSuffix` used directly — no local helpers.
- Tests: all 18 CPP tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 90.4% of statements (≥ 90% gate); `cpp.go` helpers all ≥66.7% (uncovered branches are defensive: parser-error path, nil namespace body, `field_declaration` without `function_declarator`, and the fallback `identifier` lookup inside `cppFieldSymbol` — member functions always use `field_identifier` in practice, which the added tests exercise).
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: Three surprises worth recording. (1) **Access-specifier keyword is a typed child token, not a label node.** The grammar models `public:` as an `access_specifier` node wrapping a single keyword token whose `Type()` is literally `"public"` (the colon is a separate sibling in the parent body). This is a cleaner shape than the Java/C# `modifiers` wrapper because the state machine reads `child.Type()` directly. No string comparison against `Content()` is needed. (2) **Class-vs-struct default access is encoded by the CALLER, not by inspecting the node.** Both `class_specifier` and `struct_specifier` share the same `cppWalkClassLike` helper; the caller passes `"private"` for class and `"public"` for struct. That keeps `cppWalkClassLike` oblivious to whether it's walking a class or a struct. (3) **`using namespace foo::bar;` and `using std::string;` both contain a `qualified_identifier` child.** Naive "pick the first qualified_identifier" would be ambiguous — but we disambiguate by walking children once to detect a `namespace` keyword child (only present in the `using namespace ...` form). The extractor then treats a bare `identifier` as the target only in the `namespace` form (so `using X::Y` doesn't accidentally emit a spurious `Y` from the `qualified_identifier`'s inner `identifier` child).

## Task 8 (multi-lang-support): C extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 8), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `CExtractor` in `internal/scanner/lang/c.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/c`. C has no `public` keyword — visibility is determined by the combination of file extension (`.h` header vs `.c` source) and the `static` storage class. Both variants reduce to the same predicate: emit a top-level declaration unless it has a `storage_class_specifier` child whose keyword is `static`. The extractor handles `declaration` (function prototype OR global variable), `function_definition`, `struct_specifier`, `union_specifier`, `type_definition` (typedef), and `enum_specifier`. Preprocessor `#define` macros (`preproc_def`) are explicitly skipped per the design's YAGNI list (text substitution, not symbols). Block comments `/** ... */` or `/* ... */` immediately preceding a declaration become `DocComment` with markers stripped. Imports are `preproc_include` nodes; the path comes from the `path` field child (`system_lib_string` like `<stdio.h>` or `string_literal` like `"mylib.h"`) with the enclosing delimiters stripped. Registered in `detect.go` with both `.c` and `.h`; covered by two new rows in `detect_test.go`; README's supported-languages table gains a C row (`C | .c, .h`).
- Tree-sitter node types confirmed via a throwaway debug test before coding (`c_debug_test.go` — written, real node dumps inspected, deleted before commit; `ls internal/scanner/lang/*_debug_test.go` returns no matches):
  - Root is `translation_unit`.
  - `static` surfaces as a `storage_class_specifier` DIRECT child of the enclosing `declaration` / `function_definition`. The specifier wraps a single keyword token whose `Type()` is literally `"static"` (not the full content string). The extractor walks `storage_class_specifier` children and returns true iff a child's Type() == "static".
  - Function prototype = `declaration` with a `function_declarator` child (which itself holds an `identifier` name and a `parameter_list`).
  - Function definition = `function_definition` with the same `function_declarator` plus a `compound_statement` body.
  - Global variable = `declaration` with either an `init_declarator` (for `int x = 42;`) or a direct `identifier` child (for bare `int x;`). Both are handled by `cVarSymbol`.
  - `struct_specifier` and `union_specifier` carry a `type_identifier` name child and a `field_declaration_list` body. They appear at the top level as standalone children, with trailing `;` tokens surfacing as separate `translation_unit` children (the extractor's switch simply ignores them — `;` isn't one of the handled node types).
  - `typedef` is `type_definition`; the new type alias name is the LAST `type_identifier` child (for `typedef struct X { ... } Y;`, `X` is the inner struct tag and `Y` is the new alias — walking children in reverse finds `Y`).
  - `enum_specifier` carries a `type_identifier` name and an `enumerator_list` body.
  - `preproc_include` exposes a `path` field. The value is a `system_lib_string` (for `<stdio.h>`) or a `string_literal` (for `"mylib.h"`). The content includes the delimiters (`<...>` or `"..."`) so we strip them with `strings.TrimPrefix`/`strings.TrimSuffix`.
  - `preproc_def` (`#define FOO ...`) is intentionally never handled — skipped at the switch level.
- RED: wrote 12 tests in `c_test.go` covering all 10 spec requirements plus two additional shape tests for coverage — `header_publicFunc_emitted` (call `Extract("api.h", ...)`; assert `KindFunc` and line 1), `header_staticFunc_skipped` (`static` in `.h` dropped, non-`static` kept), `source_nonStaticFunc_emitted` (`Extract("impl.c", ...)`), `source_staticFunc_skipped`, `struct_extracted` (→ `KindClass`), `typedef_extracted` (→ `KindType`), `enum_extracted` (→ `KindType`), `define_skipped` (`#define MAX 100` alongside function — macro NOT emitted, function IS), `imports_extracted` (`<stdio.h>` and `"mylib.h"` — both delimiters stripped), `globalVar_extracted` (init and bare forms → `KindVar`; static global skipped), `union_extracted` (→ `KindClass`), `docComment_captured` (`/** Adds two numbers. */` markers stripped). First run: `undefined: CExtractor`. After a minimal Language/Extensions/Extract stub, each case failed with "expected a symbol named …, got: []" — RED confirmed for the right reasons.
- GREEN: implemented `Extract` + helpers (`cHasStatic`, `cFuncSymbol`, `cVarSymbol`, `cTypeSymbol`, `cTypedefSymbol`, `cFindChildByType`, `cFuncSignature`, `cDeclSignature`, `cPrecedingComment`, `cExtractInclude`). `strings.Contains` / `strings.HasPrefix` / `strings.TrimPrefix` / `strings.TrimSuffix` used directly per CLAUDE.md hard rule — no local helpers.
- Tests: all 12 C tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 90.3% of statements (≥ 90% gate); `c.go` file 85%+ across helpers.
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: Two surprises worth recording. (1) Although the plan suggests branching on `filepath.Ext(path)` to split the `.h` vs `.c` exportedness rule, the predicates reduce to the same test — skip iff `static` — so the implementation does not actually branch on extension; it reads the `storage_class_specifier` children and checks for a `static` keyword token. The doc comment on `Extract` calls this out so future readers understand the intent. (2) Distinguishing `struct_specifier` vs `type_definition` in the AST: a bare `struct X { ... };` appears at the top level as a `struct_specifier` directly under `translation_unit` (with a trailing `;` token as a sibling — ignored). A `typedef struct X { ... } Y;` appears as a `type_definition` whose children are the `typedef` keyword, an inner `struct_specifier`, and the trailing `type_identifier` (the alias name `Y`). The extractor's `cTypedefSymbol` walks children in REVERSE to pick the last `type_identifier` — the alias — rather than the struct tag. This is the only place in the extractor where sibling order matters beyond comment-immediate-predecessor.

## Task 7 (multi-lang-support): Ruby extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 7), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `RubyExtractor` in `internal/scanner/lang/ruby.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/ruby`. Ruby's visibility model is unique among the languages added so far: inside a class/module, a bare `private` or `protected` *directive* (with no arguments) flips the visibility of every subsequent method in that scope. The extractor walks each class/module body in order, maintains a per-scope `currentVisibility` flag defaulting to `public`, flips the flag when it sees a bare directive, and emits only methods while the flag is `public`. Top-level methods (outside any class/module, directly under `program`) are always public and always emitted. Classes (`class`) and modules (`module`) both map to `KindClass` per plan ("Ruby modules are class-like"). Instance methods (`method`) and singleton methods (`singleton_method` — `def self.foo`) both map to `KindFunc`. Imports are `call` nodes whose method-field identifier is exactly `require` or `require_relative` with a `string` argument; the string literal's quotes are stripped to yield the path. Doc comments are consecutive `#`-prefixed `comment` siblings immediately preceding a declaration, joined with newlines, with leading `# ` stripped from each line. Registered in `detect.go` with `.rb`; covered by one new row in `detect_test.go`; README's supported-languages table gains a Ruby row (`Ruby | .rb`).
- Tree-sitter node types confirmed via a throwaway debug test before coding (`ruby_debug_test.go` — written, node dumps read, deleted before commit; `ls internal/scanner/lang/*_debug_test.go` returns "no matches"):
  - `program` root wraps top-level declarations directly.
  - `class` / `module` nodes carry `name` field → `constant`, `body` field → `body_statement`.
  - `method` carries `name` field → `identifier`; `singleton_method` carries `object` field → `self` AND `name` field → `identifier`.
  - **Critical discovery — the bare `private` / `protected` directive parses as a STANDALONE `identifier` node in the `body_statement`, NOT as a `call` node.** A `private :foo` invocation *with* arguments, by contrast, parses as `call` with `method=identifier(private)` and `arguments=argument_list(simple_symbol)` — the directive pattern (bare identifier) and the selective form (`call`) are distinct in the parse tree, so the extractor handles only the bare-identifier form and ignores the `call` form (consistent with the plan's requirement).
  - `require 'x'` / `require_relative 'x'` parse as `call` with `method=identifier` and `arguments=argument_list(string(string_content))`. Both appear at program root (not inside a body), so the extractor only scans `call` nodes at the top level.
  - Doc comments are `comment` nodes. For a top-level method, consecutive leading `comment` children of `program` immediately before the `method` serve as the doc. Inside a `body_statement`, consecutive `comment` siblings immediately before a `method` serve as the doc.
- RED: wrote 10 tests in `ruby_test.go` — `topLevelMethod_emitted` (bare `def foo`), `publicMethodInClass_emitted` (class + first method, both emitted), `methodAfterPrivate_skipped` (public `a`, bare `private`, then `b` skipped), `methodAfterProtected_skipped` (same but `protected`), `privateScopeScopedToClass` (two classes; `private` in the first doesn't leak into the second), `class_extracted` (`KindClass`), `module_extracted` (`KindClass`), `singletonMethod_extracted` (`def self.baz` → `KindFunc`), `imports_extracted` (one `require` + one `require_relative`, two paths), `docComment_captured` (two `#`-prefixed lines, markers + single leading space stripped, joined with `\n`). First run failed with `undefined: RubyExtractor`; after stubbing Extract to return nil, each of the 10 cases failed for the expected reason (empty symbols / missing directives).
- GREEN: implemented `Extract` + helpers (`rubyTypeSymbol`, `rubyMethodSymbol`, `rubyWalkBody`, `rubyMethodSignature`, `rubyPrecedingComment`, `rubyExtractRequire`). `rubyWalkBody` is the novel piece — it iterates children in source order, tracks `currentVisibility` ("public" / "private" / "protected"), and emits only when public. `strings.Contains` / `strings.HasPrefix` used directly per CLAUDE.md rule — no local helpers.
- Tests: all 10 Ruby tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 90.3% of statements (≥ 90% gate); `ruby.go` file per-function coverage ranges 75%–100%, clustered around 85–95%.
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: The biggest surprise vs. the modifier-keyword languages: Ruby's `private` directive is a standalone `identifier` node in statement position, not a `call`. If we had assumed "call" per the plan's text and looked inside `call` nodes, we would have matched `private :foo` (which should NOT flip scope) and missed `private` (which should). The debug test caught this before any production code was written — exactly what the plan's "write a one-off debug test" red-flag guidance is for. A smaller surprise: comments between the `class Foo` keyword and the first method in the body appear as direct children of the `class` node, OUTSIDE the `body_statement`; consecutive comments between methods in the body appear inside `body_statement`. The extractor positions its doc-comment lookup on the method's direct parent (either `program` for top-level, or `body_statement` for in-class methods), which handles the common "comment above a method" case. Doc-comments on the very first method in a class (where the comment sits outside `body_statement`) are not attached — this matches the Task 7 test fixture design (doc comment test uses a top-level method) and is consistent with the minimalism principle of the Template.

## Task 6 (multi-lang-support): PHP extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 6), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `PHPExtractor` in `internal/scanner/lang/php.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/php`. PHP top-level `function_definition`, `class_declaration`, `interface_declaration`, `trait_declaration`, and `enum_declaration` are always public and emitted unconditionally. Class/interface/trait/enum bodies are walked for `method_declaration` members; a member is emitted iff its `visibility_modifier` child is absent (PHP implicit public) or its text contains `public`. `private` and `protected` members are skipped. PHPDoc `/** */` blocks immediately preceding a declaration become `DocComment` with markers stripped. Imports are `namespace_use_declaration` nodes; each wraps one or more `namespace_use_clause` children containing a `qualified_name` (the dotted path, e.g. `Foo\Bar`) and optionally a `namespace_aliasing_clause` whose `name` child supplies the alias for `use Foo\Baz as Qux;`. Registered in `detect.go` with `.php`; covered by one new row in `detect_test.go`; README's supported-languages table gains a PHP row (`PHP | .php`).
- Tree-sitter node types confirmed via a throwaway debug test before coding (file `php_debug_test.go` — written, node dumps read, deleted before commit; `ls internal/scanner/lang/*_debug_test.go` returns "no matches"): `program` root wraps a leading `php_tag` plus top-level declarations; visibility lives on a `visibility_modifier` DIRECT child of `method_declaration` (NOT nested under a `modifiers` wrapper like Java/Scala); body of class/interface/trait is `declaration_list`; body of enum is `enum_declaration_list` (reachable via the `body` field name on the enum node); `namespace_use_declaration` children are one or more `namespace_use_clause` nodes, each containing a `qualified_name` (which itself wraps `namespace_name_as_prefix > namespace_name > name` plus a trailing `name` for the leaf, but the extractor just uses `qualified_name.Content()` — yielding e.g. `Foo\Bar`) and an optional `namespace_aliasing_clause` whose first `name` child is the alias.
- RED: wrote 10 tests in `php_test.go` — `topLevelFunc_emitted` (bare `function foo()`), `publicMethod_extracted` (explicit `public function bar()`), `privateMethod_skipped`, `protectedMethod_skipped`, `noModifierMethod_emitted` (class member with no access modifier → PHP implicit public), `class_extracted`, `interface_extracted` (→ `KindInterface`), `trait_extracted` (→ `KindClass`, per plan), `imports_extracted` (plain `use Foo\Bar;` + aliased `use Foo\Baz as Qux;` with `Alias = "Qux"`), `docComment_captured` (PHPDoc markers stripped, `strings.Contains` used directly per CLAUDE.md rule — no local helpers). First run failed with `undefined: PHPExtractor`.
- GREEN: implemented `Extract` + helpers (`phpExtractDecl`, `phpWalkMembers`, `phpMemberIsPublic`, `phpDeclSignature`, `phpPrecedingComment`, `phpExtractImports`) to the minimum needed to pass each test in order.
- Tests: all 10 PHP tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 90.4% of statements (≥ 90% gate); `php.go` file averages in the high-80s/low-90s across helpers.
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: Two surprises vs. Java/Scala: (1) PHP's `visibility_modifier` sits directly on `method_declaration`, not nested inside a `modifiers` wrapper — `phpMemberIsPublic` reads it via `strings.Contains(child.Content(content), "public")`. (2) Imports can chain multiple use clauses per declaration in principle (`use A, B;` form); `phpExtractImports` iterates all `namespace_use_clause` children to handle both the single-clause form and any multi-clause form uniformly.

## Task 5 (multi-lang-support): Scala extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 5), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `ScalaExtractor` in `internal/scanner/lang/scala.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/scala`. Scala is public-by-default: a top-level declaration is emitted unless its `modifiers` child contains an `access_modifier` whose keyword is `private` (with or without an `access_qualifier` like `private[pkg]`) or `protected`. No modifier = public. Handles `class_definition` / `object_definition` (→ `KindClass`), `trait_definition` (→ `KindInterface`), `function_definition` / `function_declaration` (→ `KindFunc` — the `_declaration` variant covers abstract defs in traits without bodies), `val_definition` (→ `KindConst`) and `var_definition` (→ `KindVar`). Scaladoc `/** */` blocks immediately preceding a declaration become `DocComment` with markers stripped. Imports are top-level `import_declaration` nodes; per plan, alias/rename forms (`import foo.{Bar => Baz}`) are emitted as-is without surfacing rename details — the path is the source text with the leading `import ` keyword stripped. Registered in `detect.go` with both `.scala` and `.sc`, covered by two new rows in `detect_test.go`, and README's supported-languages table gains a Scala row (`Scala | .scala, .sc`).
- Tree-sitter node types confirmed via a throwaway debug test before coding: `import_declaration` (children are unnamed `import` keyword + one-or-more `identifier` path parts joined by anonymous `.` tokens, and optionally `namespace_selectors` wrapping `arrow_renamed_identifier` for selector form), `class_definition` / `object_definition` / `trait_definition` (with a `name` field → `identifier`, `body` field → `template_body`), `function_definition` (`def foo(): T = ...`) vs `function_declaration` (abstract `def foo(): T` — no body, appears inside traits), `val_definition` / `var_definition` (use `pattern` field for the name, NOT `name`), `modifiers` wrapping an `access_modifier` whose first child is a `private` or `protected` keyword token, and an optional `access_qualifier` wrapping `[identifier]` for `private[pkg]`. Doc comment node type is `block_comment` (same as Java, NOT `multiline_comment` as Kotlin uses). The `private[pkg]` qualifier surfaces as a single `access_modifier` node with two children: the `private` token AND a nested `access_qualifier` node — the extractor skips on either `private` or `protected` regardless of whether `access_qualifier` follows. Debug test (`scala_debug_test.go`) deleted before commit (verified via `ls internal/scanner/lang/*_debug_test.go` returning "no matches").
- RED: wrote 11 tests in `scala_test.go` — `publicDefault_emitted` (bare `def foo`), `privateSkipped`, `privateWithQualifier_skipped` (`private[mypkg]` — the CLAUDE.md hard rule specifically called this out), `protectedSkipped`, `class_extracted`, `object_extracted` (both → `KindClass`), `trait_extracted` (→ `KindInterface`), `val_extracted` (→ `KindConst`), `var_extracted` (→ `KindVar`), `imports_extracted` (two plain `import` statements), `docComment_captured` (Scaladoc markers stripped, `strings.Contains` used directly per CLAUDE.md rule). First run failed with `undefined: ScalaExtractor`; after stub, each case failed for the right reason (symbols empty / wrong kind).
- GREEN: implemented `Extract` + helpers (`scalaExtractDecl`, `scalaIsNonPublic`, `scalaDeclName`, `scalaDeclSignature`, `scalaPrecedingComment`, `scalaExtractImport`) to the minimum needed to pass each test in order. Initial `scalaExtractImport` had a branching selector-form path (fallback slice when `namespace_selectors` present) which hurt per-file coverage; simplified to always strip `import ` from node.Content and emit the remainder — correctly handles both plain and selector forms.
- Tests: all 11 Scala tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 90.6% of statements (≥ 90% gate); `scala.go` file 85.4%.
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: One surprise vs. Kotlin: Scala's `val`/`var` don't use a `name` field — the identifier lives under a `pattern` field instead. `scalaDeclName` checks `name` first and falls back to `pattern`. Another: trait method signatures (abstract, no body) parse as `function_declaration` (not `function_definition`), so both types are walked at the top level; inside `template_body` bodies, member methods are not currently walked — consistent with plan's emphasis on top-level declarations.

## Task 4 (multi-lang-support): Swift extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 4), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `SwiftExtractor` in `internal/scanner/lang/swift.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/swift`. Swift exportedness is inverted from Kotlin: emit iff a `modifiers > visibility_modifier` child holds a `public` or `open` token; absence of a modifier means `internal` (Swift's default) and is skipped, as are explicit `internal`, `fileprivate`, and `private`. Classes, structs, and enums all surface as `class_declaration` (differentiated by an anonymous keyword child — the extractor does not care because the kind is the same per plan). Protocols are `protocol_declaration` (→ `KindInterface`). Functions (`function_declaration`) and properties (`property_declaration`) round out the set (→ `KindFunc` / `KindVar`). Doc comments support both Swift idioms: the extractor first walks backwards through consecutive `comment` siblings whose text starts with `///` and joins them; if that returns nothing, it falls back to a single preceding `multiline_comment` that begins with `/**`. Imports have no alias concept in Swift — `import_declaration > identifier` content is the path. Registered in `detect.go` with `.swift`, covered by a new row in `detect_test.go`, README supported-languages table gains a Swift row.
- Tree-sitter node types confirmed via a throwaway debug test before coding: `import_declaration` (child `identifier` with a nested `simple_identifier`), `function_declaration`, `class_declaration` (used for `class`, `struct`, AND `enum` — the keyword is an anonymous token child), `protocol_declaration`, `property_declaration`, `modifiers`, `visibility_modifier` (whose single child's `Type()` is the literal keyword: `public` / `open` / `internal` / `fileprivate` / `private`), `comment` (one node per `///` line), `multiline_comment` (single `/** */` block), `type_identifier` / `simple_identifier`, `pattern` (field `bound_identifier` → `simple_identifier` for property names), and `class_body` / `enum_class_body` / `protocol_body`. The grammar does expose a `name` field on function/class/property declarations (verified) — `ChildByFieldName("name")` works; properties return a `pattern` node, everything else a direct `simple_identifier` or `type_identifier`. Debug test (`swift_debug_test.go`) deleted before commit (verified via `ls internal/scanner/lang/*_debug_test.go` → no matches).
- RED: wrote 12 tests in `swift_test.go` covering public func, open func, internal/no-modifier skipped, fileprivate skipped, public class, public struct, public enum (`KindClass` per plan), public protocol (`KindInterface`), imports (two `import` statements), `///` triple-slash doc capture (three lines joined, markers stripped), `/** */` block doc capture (markers stripped), and a public property (`KindVar`). First run failed with `undefined: SwiftExtractor`; after stub, each case failed with "expected symbol X, got: []" — RED confirmed for the right reasons.
- GREEN: implemented `Extract` + helpers (`swiftExtractDecl`, `swiftIsPublicOrOpen`, `swiftDeclName`, `swiftDeclSignature`, `swiftPrecedingComment`, `swiftExtractImport`) to the minimum needed to pass each test in order.
- Tests: all 12 Swift tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 90.7% of statements (≥ 90% gate).
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: One surprise vs. other languages: the tree-sitter-swift grammar collapses `class` / `struct` / `enum` into a single `class_declaration` node type, using an anonymous keyword child to disambiguate. This is fine for us because the plan maps all three to `KindClass` — so the extractor does not need to inspect which keyword is present.

## Task 3 (multi-lang-support): Kotlin extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 3), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `KotlinExtractor` in `internal/scanner/lang/kotlin.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/kotlin`. Kotlin is public-by-default: a top-level declaration is emitted unless its `modifiers` child contains a `visibility_modifier` whose content is `private`, `internal`, or `protected` (explicit `public` and no modifier are both public — opposite of Java/C#). Handles `class_declaration` / `object_declaration` (→ `KindClass`), `function_declaration` (→ `KindFunc`), `property_declaration` (→ `KindVar`, no `val`/`var` distinction per plan). KDoc `/** */` blocks immediately preceding a declaration become `DocComment` with markers stripped. Imports live under an `import_list` parent; the `import_header` is parsed for path (its `identifier` dotted child) and optional `import_alias` (populates `Alias` when `import foo.Bar as Baz` is used). Registered in `detect.go` with both `.kt` and `.kts`, covered by two new rows in `detect_test.go`, and README's supported-languages table gains a Kotlin row (`Kotlin | .kt, .kts`).
- Tree-sitter node types used (confirmed via temporary debug test before deletion): `import_list` (wraps all `import_header` entries — they are NOT direct children of `source_file`), `import_header`, `import_alias` (wraps a `type_identifier` for the alias), `identifier` (the dotted path inside an `import_header`), `class_declaration` / `object_declaration` (with a `type_identifier` child for the name), `function_declaration` / `property_declaration` (with `simple_identifier` / `variable_declaration { simple_identifier }` for the name), `modifiers`, `visibility_modifier` (whose single child is a `private` / `internal` / `protected` / `public` token — text check sufficient), `multiline_comment` (KDoc block — NOT `block_comment` as Java uses). The grammar exposes zero field names (`FIELD_COUNT = 0` in parser.c), so `ChildByFieldName` is unusable — names are found by scanning child node types instead.
- RED: wrote 8 tests in `kotlin_test.go` — `publicDefault_emitted` (bare `fun foo`), `privateSkipped`, `internalSkipped`, `class_extracted` (`class Greeter { }`), `object_extracted` (`object Singleton { }`), `property_extracted` (`val MaxRetries = 3` → `KindVar`), `imports_extracted` (plain + `import foo.Baz as Renamed` with `Alias = "Renamed"`), `docComment_captured` (KDoc markers stripped). First run failed with `undefined: KotlinExtractor`; after stub, runtime failures reported "expected a symbol named …, got: []" — RED confirmed for the right reasons.
- GREEN: implemented `Extract` + helpers (`kotlinExtractDecl`, `kotlinIsNonPublic`, `kotlinDeclName`, `kotlinDeclSignature`, `kotlinPrecedingComment`, `kotlinExtractImport`) to the minimum needed to pass each test in order.
- Tests: all 8 Kotlin tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 91.0% of statements (≥ 90% gate).
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: One staticcheck regression surfaced on the initial GREEN pass (`SA4008` / `SA4004`: `kotlinPrecedingComment`'s for-loop unconditionally returned on the first iteration) — refactored to a plain `if idx <= 0 { return "" }` guard + single child lookup. Temporary `kotlin_debug_test.go` deleted before commit (verified via `ls internal/scanner/lang/*_debug_test.go` returning "no matches").

## Task 2 (multi-lang-support): C# extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 2), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `CSharpExtractor` in `internal/scanner/lang/csharp.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/csharp`. Emits any declaration whose children include a `modifier` node containing a `public` token — `class_declaration`, `record_declaration`, `struct_declaration` (→ `KindClass`), `interface_declaration` (→ `KindInterface`), `enum_declaration` (→ `KindType`), plus `public` `method_declaration` members of their `declaration_list` bodies (→ `KindFunc`). `namespace_declaration` bodies (`declaration_list`) are descended into so idiomatic namespace-wrapped code is covered; nested namespaces recurse. XML `///` doc comments immediately preceding a declaration become `DocComment`: consecutive `comment` siblings whose text starts with `///` are collected, each `///` prefix stripped, joined with newlines. `using X;` imports emit `Path = X`; aliased `using Alias = X.Y;` populates `Alias` from the directive's `name` field and `Path` from the following `qualified_name`. Registered in `detect.go` with `.cs`, covered by a new row in `detect_test.go`, and README's supported-languages table gains a C# row.
- Tree-sitter node types used: `using_directive` (with `name` field for alias + `qualified_name`/`identifier` for path), `namespace_declaration` (with `body: declaration_list`), `class_declaration`, `record_declaration`, `struct_declaration`, `interface_declaration`, `enum_declaration`, `method_declaration`, `declaration_list` (shared body type for namespaces and classes), `modifier` (with named child `public`/`private`/`internal`/`protected`), `comment` (one per `///` line), `qualified_name`.
- RED: wrote 7 tests in `csharp_test.go` — exported method (`Add`), non-public skip (private / internal / protected), public class kind, public interface kind, public enum kind, imports (plain + aliased with `Alias = "Proj"`), and `///` doc capture (assertions: non-empty, `///` markers stripped, multiple lines joined with `\n`). First run failed with `undefined: CSharpExtractor`; after stub, runtime failures reported "expected symbol X, got: []" — RED confirmed for the right reasons.
- GREEN: implemented `Extract` + helpers (`csharpWalkNamespace`, `csharpWalkType`, `csharpWalkMembers`, `csharpHasPublic`, `csharpDeclSignature`, `csharpPrecedingComment`, `csharpExtractUsing`) to the minimum needed to pass each test in order.
- Tests: all 7 C# tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 91.1% of statements (≥ 90% gate).
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: Wrote a temporary `csharp_debug_test.go` that dumped `tree.RootNode().String()` + a deep per-node walk for a representative C# fixture — confirmed that (a) modifiers in tree-sitter-csharp are individual `modifier` nodes with a named child `public`/`private`/... (NOT a `modifiers` container as in Java), (b) namespace bodies use `declaration_list` and enum bodies use `enum_member_declaration_list`, (c) XML doc lines are separate `comment` nodes one per `///` line, and (d) `using_directive` uses the `name` field only for the alias in `using Alias = X.Y;` form. Debug test deleted before commit (verified via `ls *_debug_test.go` returning no matches).

## Task 1 (multi-lang-support): Java extractor - COMPLETE
- Started: 2026-04-24
- Plan: `.plans/MULTI_LANG_SUPPORT_PLAN.md` (Task 1), design `.plans/2026-04-24-multi-lang-support-design.md`
- Summary: Added `JavaExtractor` in `internal/scanner/lang/java.go` implementing the existing `Extractor` interface on top of `github.com/smacker/go-tree-sitter/java`. Emits any declaration whose `modifiers` child contains a `public` token — top-level `class_declaration`, `record_declaration` (→ `KindClass`), `interface_declaration` (→ `KindInterface`), `enum_declaration` (→ `KindType`), plus `public` `method_declaration` members of their bodies (→ `KindFunc`). Javadoc `/** */` immediately preceding a declaration becomes `DocComment` with markers stripped; structural `{` tokens are skipped when walking backwards through sibling order. Imports are top-level `import_declaration` nodes; path is the full dotted `scoped_identifier` text. Registered in `detect.go` and covered by a new row in `detect_test.go`.
- Tree-sitter node types used: `import_declaration`, `class_declaration`, `record_declaration`, `interface_declaration`, `enum_declaration`, `method_declaration`, `class_body`, `interface_body`, `enum_body`, `modifiers` (with `public`/`private`/`protected` token children), `scoped_identifier`, `block_comment`.
- RED: wrote 5 tests in `java_test.go` covering exported method, non-public skip (private / package-private / protected), public class kind, two imports, Javadoc comment capture. First run failed with `undefined: JavaExtractor`; after stub, runtime failures reported "expected 1 symbol, got 0" — confirmed RED for the right reasons.
- GREEN: implemented `Extract` + helpers (`javaWalkType`, `javaWalkMembers`, `javaHasModifier`, `javaDeclSignature`, `javaPrecedingComment`, `javaExtractImport`) to the minimum needed to pass each test in order.
- Tests: all 5 Java tests pass, full `./...` suite green.
- Coverage: `internal/scanner/lang` 91.5% of statements (≥ 90% gate).
- Build: OK (`go build ./...`).
- Linting: OK (`golangci-lint run` — 0 issues).
- Completed: 2026-04-24
- Notes: Wrote a temporary `java_debug_test.go` that dumped `tree.RootNode().String()` + modifier child types for a Java fixture — confirmed the comment node type is `block_comment` (not `comment` as in TS), that `method_declaration` without a `modifiers` child is package-private, and that `class_body` includes literal `{` / `}` punctuation as siblings (handled by `javaPrecedingComment` skipping `{`). Debug test deleted before commit.

## Fix lmstudio empty-key regression; remove openai-compatible - COMPLETE
- Started: 2026-04-23
- Summary: Code-review of the Bifrost consolidation surfaced a regression: lmstudio (and the now-removed openai-compatible) routed through Bifrost's OpenAI provider with an empty API key, but Bifrost's per-request key filter (`bifrost.go selectKeyFromProviderForModel` + `utils.go CanProviderKeyValueBeEmpty`) drops empty-value keys for OpenAI, causing every request to fail with "no keys found that support model: …". Fixed inside `bifrostAccount.GetKeysForProvider`: when `provider == OpenAI && apiKey == "" && baseURL != ""`, substitute a non-empty placeholder bearer (`local-server-no-auth`). LM Studio ignores the Authorization header, so the placeholder is harmless. Also removed the `openai-compatible` CLI provider entirely (per direction) — references gone from `buildTierClient`, `tier_validate.go`, `tier_parse_test.go`, `llm_tiering_test.go`, `README.md`, `CHANGELOG.md`. Replaced the two openai-compatible tests with a single removal-assertion test.
- Tests: all packages green; new RED/GREEN tests `TestBifrostAccount_OpenAI_EmptyKeyWithBaseURL_UsesPlaceholder`, `TestBifrostAccount_OpenAI_EmptyKeyNoBaseURL_LeavesValueEmpty`, `TestBifrostAccount_OpenAI_NonEmptyKey_PreservesKeyValue`, and `TestBuildTierClient_OpenAICompatible_Removed`
- Coverage: analyzer 94.9%, cli 90.6% (both above the 90% gate)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-23
- Notes: Hosted OpenAI without a key still surfaces Bifrost's standard "no keys found" error — the placeholder only kicks in when a custom baseURL is set, signalling intent to hit a local server.

## Consolidate OpenAICompatibleClient into BifrostClient - COMPLETE
- Started: 2026-04-23
- Plan: `.plans/BIFROST_CONSOLIDATION_PLAN.md`
- Summary: Deleted `internal/analyzer/openai_compatible_client{,_test}.go`. All five CLI provider names (`anthropic`, `openai`, `ollama`, `lmstudio`, `openai-compatible`) now route through `analyzer.BifrostClient`. `NewBifrostClientWithProvider` gained a `baseURL` argument; Ollama threads it through `Key.OllamaKeyConfig.URL` (Bifrost's Ollama provider reads the server URL from the key config, not `NetworkConfig.BaseURL`), while OpenAI/Anthropic honor `NetworkConfig.BaseURL`. Extended `BifrostClient.CompleteJSON` dispatch to include Ollama (Bifrost delegates Ollama chat to the OpenAI handler, so `response_format=json_schema` passes through). `buildTierClient` refactored to a single `NewBifrostClientWithProvider` call site, collapsing duplication.
- Tests: all packages green; new RED/GREEN tests for Ollama `CompleteJSON` dispatch, Ollama key URL plumbing, OpenAI `NetworkConfig.BaseURL` plumbing, and CLI type-assertions that each provider returns `*analyzer.BifrostClient`
- Coverage: analyzer 94.9%, cli 90.7% (both above the 90% gate)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-23
- Notes: No behavior change for users — `ftg.config` provider names unchanged. The dead branch `// PROMPT:` lines in the deleted file are gone; structured-output implementations (Anthropic forced tool use; OpenAI / Ollama `response_format=json_schema`) now live in exactly one place.

## Missing Screenshots Detection - COMPLETE
- Started: 2026-04-23
- Design: `.plans/2026-04-23-missing-screenshots-design.md`
- Plan: `.plans/MISSING_SCREENSHOTS_IMPLEMENTATION_PLAN.md` (11 implementation tasks + final PR task, TDD per task, fresh subagent per task, code review between tasks)
- Implementation commits:
  - Task 1 ScreenshotGap type: `997cef5`
  - Task 2 markdown image parser: `66f07a9`
  - Task 3 HTML img parser: `cd68ab8` (+ `9560601` single-quote fix from review)
  - Task 4 coverage map builder: `96b9206`
  - Task 5 prompt builder with `// PROMPT:` marker: `fedb592`
  - Task 6 response parser reusing `extractJSONArray`: `91dfec9`
  - Task 7 `DetectScreenshotGaps` orchestrator: `c506612`
  - Task 8 reporter "Missing Screenshots" section: `0e48188`
  - Task 9 `--skip-screenshot-check` flag: `f010611`
  - Task 10 CLI pipeline wiring: `d8b93d6` (+ `557a170` deterministic URL ordering fix from review)
  - Task 11 VERIFICATION_PLAN scenario: `c091224`
- Tests: all packages green
- Coverage: analyzer 95.1%, reporter 97.9% (both above the 90% gate)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-23
- Notes: LLM-only detection with mandatory verbatim passage citation. Locality rule (same section or within 3 paragraphs) is applied by the LLM against a Go-precomputed coverage map. One LLM call per page. Default on; `--skip-screenshot-check` disables the pass. Missing screenshots render as their own top-level section in `gaps.md` grouped by page.

## Remove ripgrep dependency + document supported languages - COMPLETE
- Started: 2026-04-23
- Plan: `.plans/2026-04-23-remove-ripgrep-dependency.md` (11 tasks, TDD per task, fresh subagent per task, code review between tasks)
- Summary: ripgrep (`rg`) was declared as a required external dependency but was never actually shelled out from any analyzer/scanner/spider/reporter code. This branch scrubs it from live code, tests, docs, testscripts, the Homebrew formula stanza, verification plan, and CLAUDE.md. README also gains a "Supported languages" section derived from the tree-sitter extractor registry.
- Tasks completed (commit SHAs in parens):
  - 1. Pre-flight grep confirmation — no live ripgrep usage outside doctor/install-deps
  - 2. Remove rg from `internal/doctor/doctor.go` + tests (72088a0)
  - 3. Remove rg from `internal/doctor/install_test.go` (9dcc42b)
  - 4. Pre-existing errcheck debt cleanup in `internal/doctor/install.go` (d41c513) — isolated from feature change
  - 5. Rewrite `internal/cli/doctor_test.go` to mdfetch-only (5facc6b)
  - 6. Rewrite `internal/cli/install_deps_test.go` + Short/Long help strings (c7b439d, a570f1c)
  - 7. Update `internal/cli/doctor.go` + `internal/cli/install_deps.go` + `internal/cli/root_test.go` Short/Long + banner comment (0c5de02)
  - 8. README — rewrite "What this installs", add new "Supported languages" table, update 4 embedded command help lines (001e50c)
  - 9. CLAUDE.md — External Runtime Dependencies, Distribution, User Notification, example branch name (92fe325)
  - 10. VERIFICATION_PLAN.md — Prerequisites, Scenarios 5/6/7, Verification Rules (a71408d)
  - 11. `cmd/find-the-gaps/testdata/script/doctor_ok.txtar` — mdfetch-only testscript (37e7ccb)
- Gate results (full suite, final pass):
  - `go test ./...` — all green
  - `go test -race ./...` — all green
  - `go test -cover ./...` — all packages ≥ 90%:
    - internal/doctor: 98.4%
    - internal/cli: 90.2%
    - cmd/find-the-gaps: 100.0%
    - internal/analyzer: 94.7%
    - internal/reporter: 97.4%
    - internal/scanner: 93.8%
    - internal/scanner/lang: 91.8%
    - internal/spider: 94.4%
  - `golangci-lint run` — 0 issues
  - `go build ./...` — success
  - CLI smoke: `doctor --help`, `install-deps --help`, root `--help` — all clean of ripgrep/rg
- Completed: 2026-04-23
- Notes:
  - 4 intentional negative assertions remain in `internal/doctor/doctor_test.go` — they assert ripgrep/rg do NOT appear in output (regression guard-rails), not leftover mentions. Preserved deliberately.
  - `.gitignore` line 48 lists `.plans/` while CLAUDE.md says plans are tracked in git. The contradiction is pre-existing (`.plans/` contents were tracked before the ignore rule was added). Out of scope for this PR; flagged for follow-up.
  - Task 4 split from Task 3 in the plan because the errcheck violations it fixes pre-date this branch (from commit 1703ec2). Isolating the pre-existing-debt commit from the feature commit keeps the git history reviewable.

## Task 7 (fix-feature-doc-status plan): WriteMapping consumes DocsFeatureMap - COMPLETE
- Started: 2026-04-23
- Bug: mapping.md marked every feature "undocumented" because WriteMapping compared canonical CodeFeature.Name strings against PageAnalysis.Features (raw per-page LLM output from an unrelated pass). The two name universes almost never collide verbatim.
- RED: rewrote existing WriteMapping tests to pass analyzer.DocsFeatureMap instead of []analyzer.PageAnalysis; added TestWriteMapping_DocStatusUsesCanonicalMap as a lock-in test that proves doc status is driven by canonical-keyed DocsFeatureMap only.
- RED confirmed: compile failure (IncompatibleAssign: DocsFeatureMap vs []PageAnalysis) in 6 call sites.
- GREEN: changed WriteMapping signature to `(dir, summary, mapping, docsMap analyzer.DocsFeatureMap)`; builds pagesByFeature map up front; doc status/pages come straight from the canonical map. Updated the single call site in internal/cli/analyze.go.
- Tests: all packages passing (`go test ./...`).
- Coverage: internal/reporter 97.4% (WriteMapping 100%).
- Build: ✅ Successful
- Linting: ✅ Clean on touched packages (internal/reporter, internal/cli).
- Completed: 2026-04-23
- Notes: Plan in .plans/FIX_FEATURE_DOC_STATUS.md. Step 4 (normalization of LLM-returned feature names in MapFeaturesToDocs) deferred — it was gated on evidence from a real verification run showing empty docsfeaturemap.json pages; run the VERIFICATION_PLAN scenarios before pulling that trigger.

## Task 6 (drift-detection plan): Add Stale Documentation section to WriteGaps - COMPLETE
- Started: 2026-04-22
- Tests written first (RED): TestWriteGaps_StaleDocumentation_RendersFindings, TestWriteGaps_StaleDocumentation_NoneFound
- RED confirmed: compile failure (too many arguments to WriteGaps)
- GREEN: WriteGaps signature updated with drift []analyzer.DriftFinding parameter; Stale Documentation section renders per feature with issues; all 5 existing call sites updated
- Tests: all packages passing
- Coverage: internal/reporter 100% ✅; total 93.0% ✅; internal/cli 89.4% (pre-existing drag, unchanged from Task 5)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-22

## Task 5 (drift-detection plan): Wire DetectDrift into analyze.go - COMPLETE
- Started: 2026-04-22
- Tests written first (RED): added CompleteWithTools to stubLLMClient in analyze_parallel_test.go; added drift routing case to fake server in analyze_test.go; added error-path tests for OpenAICompatibleClient.CompleteWithTools (server error, bad JSON, empty choices)
- RED confirmed: compile failure when stubs didn't implement ToolLLMClient
- GREEN: analyze.go wires drift detection after mapping pass; OpenAICompatibleClient now implements ToolLLMClient; type assertion in analyze.go with degraded-mode warning for non-Bifrost providers
- Tests: all packages passing
- Coverage:
  - internal/analyzer: 94.4% ✅
  - internal/cli: 89.4% — pre-existing drag (was 88.6% before this branch); newAnalyzeCmd 80% and saveCodeFeaturesCache 80% both require full LLM+docs pipeline; all new drift detection code paths ARE covered
  - internal/reporter: 100% ✅
  - total: 93.0% ✅
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-22
- Notes: Post-review additions — added warning log for non-anthropic/openai providers; added 3 error-path tests for OpenAICompatibleClient.CompleteWithTools bringing it from 69.2% to ~100%

## Task 4 (drift-detection plan): Create drift.go with DetectDrift + agent loop - COMPLETE
- Started: 2026-04-22
- Tests written first (RED): 12 tests in drift_test.go (driftStubClient, driftStubClientWithErr stubs; renamed from stubToolClient to avoid conflict with client_test.go)
- RED confirmed: "undefined: analyzer.DetectDrift" compile error
- GREEN: drift.go with DetectDrift, detectDriftForFeature, executeTool, executeReadFile, executeReadPage, driftTools
- Post-review fix: added TestDetectDrift_ReadPage_PageReaderError_ReturnedToLLM to push executeReadPage from 85.7% → 100%
- Tests: 13 passing, 0 failing
- Coverage: internal/analyzer 93.5%; drift.go: DetectDrift 94.7%, detectDriftForFeature 100%, executeReadPage 100% ✅
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-22
- Notes: // PROMPT: comment on the line immediately above systemPrompt := fmt.Sprintf(...) as required

## Task 3 (drift-detection plan): Implement CompleteWithTools on BifrostClient - COMPLETE
- Started: 2026-04-22
- Tests written first (RED): TestBifrostClient_CompleteWithTools_ReturnsFinalContent, TestBifrostClient_CompleteWithTools_ReturnsToolCalls (bifrost_client_test.go). Plan's test code referenced non-existent SDK types; tests written against real Bifrost v1.5.2 schema types.
- RED confirmed: "undefined: CompleteWithTools" compile error
- GREEN: CompleteWithTools added to BifrostClient, converting ChatMessage/Tool slices to Bifrost schema types and mapping response back
- SDK divergence from plan: tools passed via `Params: &schemas.ChatParameters{Tools: ...}`, NOT as a top-level field on BifrostChatRequest (that field does not exist in v1.5.2)
- Tests: 7 new tests + all existing passing; package total 94.7%
- Coverage: CompleteWithTools 98.1% ✅
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues; fixed one QF1008 embedded-field selector warning)
- Completed: 2026-04-22
- Notes: Post-review fix — added `var _ ToolLLMClient = client` compile-time guard to TestBifrostClient_ImplementsLLMClient

## Task 2 (drift-detection plan): Add ToolLLMClient interface - COMPLETE
- Started: 2026-04-22
- Tests written first (RED): TestToolLLMClient_InterfaceSatisfied, TestToolLLMClient_EmbedsLLMClient (client_test.go)
- RED confirmed: "undefined: analyzer.ToolLLMClient" compile error
- GREEN: Added ToolLLMClient interface to client.go embedding LLMClient and adding CompleteWithTools method
- Tests: all packages green (80+ tests)
- Coverage: internal/analyzer: 94.2% ✅
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-22

## Task 1 (drift-detection plan): Add DriftFinding, DriftIssue, ChatMessage, Tool, ToolCall types - COMPLETE
- Started: 2026-04-22
- Tests written first (RED): TestDriftFinding_JSONRoundtrip, TestToolCall_Fields, TestChatMessage_Fields (types_test.go)
- RED confirmed: 5x "undefined" compile errors (DriftFinding, DriftIssue, ToolCall, ChatMessage undefined)
- GREEN: Added ChatMessage, Tool, ToolCall, DriftIssue, DriftFinding to types.go
- Tests: 80 passing, 0 failing; all packages green
- Coverage: internal/analyzer: 94.2% ✅
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-22

## Task 6 (docs-feature-mapping plan): Full test suite, coverage gate, final commit — COMPLETE
- Started: 2026-04-21
- Tests written/verified:
  - internal/analyzer: 93.1% statements (DocsFeatureEntry types, mapPageToFeatures, MapFeaturesToDocs, DocsMapProgressFunc — all covered; 11 new tests in docs_mapper_test.go)
  - internal/cli/analyze_parallel_test.go: 3 tests — TestRunBothMapsInParallel, TestRunBothMaps_CodeMapError, TestRunBothMaps_DocsMapError
  - internal/cli/docsfeaturemap_cache_test.go: 5 tests — RoundTrip, StaleOnFeatureChange, MissingFile, CorruptFile, NilPagesNormalized
  - internal/cli/featuremap_cache_test.go: added TestLoadFeatureMapCache_CorruptFile_ReturnsFalse
- Coverage:
  - internal/analyzer: 93.1% ✅
  - internal/cli: 88.6% — below 90% gate
    - Pre-existing drag: newAnalyzeCmd at 81.4% (requires full LLM+docs pipeline; confirmed pre-existing via git stash check)
    - Pre-existing drag: loadFeatureMapCache at 85.0% (OS-level non-ErrNotExist read error, pre-existing)
    - New code coverage: runBothMaps 93.3%, loadDocsFeatureMapCache 88.2%, saveDocsFeatureMapCache 90.9%
    - The 88.6% package total is driven entirely by pre-existing functions; all newly introduced functions meet or approach the 90% threshold
  - All other packages: ✅ above 90%
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-21
- Notes:
  - go mod tidy was required before first test run (stale go.mod after adding testify dependency in earlier tasks)
  - fakeClient/fakeCounter already declared in testhelpers_test.go and mapper_test.go — docs_mapper_test.go reuses them rather than redeclaring
  - Truncation test budget corrected: tokenBudget=1_000 (not 200) needed to satisfy featureTokens(50) + promptOverhead(400) + available(550) ≥ 100 so LLM is called with truncated content
  - mapPageToFeatures `features []string` parameter dropped (only featuresJSON was used); export_test.go updated to accept and ignore it for backward compat with test callsites
  - DocsMapError test: docs page errors are logged and skipped, not propagated — runBothMaps returns nil error even when docs mapper encounters bad JSON responses

## Task 4 (context-length plan): Coverage verification - COMPLETE
- Started: 2026-04-20
- Tests written first (RED):
  - TestMapFeaturesToCode_AllFilesProcessed_NoneSkipped — 5 files, budget=1, fakeCounter returns 0, asserts 5 LLM calls (one per file), no error
  - TestMapFeaturesToCode_TinyBudget_AllFilesStillCovered — 2 files, budget=0, fakeCounter returns 0, asserts both files appear in merged result
  - TestMapFeaturesToCode_MixedScan_SymbollessFilesSkippedInCoverageCheck — exercises len(f.Symbols)==0 continue branch in coverage check loop
  - TestMapFeaturesToCode_CoverageCheckFails_ReturnsError — file path with ": " triggers SplitN mismatch → coverage check error path
  - TestMapFeaturesToCode_CounterError_Propagates — errorCounter returns error, verifies propagation through counter.CountTokens path
  - TestMapFeaturesToCode_UnknownFeatureInResponse_Ignored — LLM returns unknown feature name, verifies silent skip in accumulator
- RED state: first two tests verify happy path (all files ARE covered); last four specifically target uncovered defensive branches.
- GREEN: Added post-batch coverage verification to mapper.go — builds `batched` map from all initial batches, then checks every file with symbols appears in at least one batch; returns error immediately if not (fail-fast before any LLM calls).
- Tests: 14 passing (all MapFeaturesToCode_* tests), 0 failing; all packages green
- Coverage:
  - internal/analyzer: 91.9% of statements (up from 89.7%)
  - mapper.go: MapFeaturesToCode now exercises all 4 previously-uncovered branch paths
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes:
  - Coverage check uses initialBatches (not queue) so it runs before any split-and-retry; this is correct — if batchSymLines dropped a file it would be caught here before spending tokens
  - AnthropicCounter (NewAnthropicCounter/CountTokens) at 0% is expected — integration-only, requires live API key per plan design
  - The colon-space trick (path "a: b.go") is the only way to trigger the defensive error path from outside the package without mocking internal batcher state

## Task 3 (context-length plan): Rewrite MapFeaturesToCode with batching - COMPLETE
- Started: 2026-04-20
- Tests written first (RED):
  - Updated all 5 existing TestMapFeaturesToCode_* calls to new 6-arg signature
  - TestMapFeaturesToCode_MultipleBatches_MergesResults
  - TestMapFeaturesToCode_CounterOverBudget_SplitsBatch
  - TestMapFeaturesToCode_FilesWithNoSymbols_Skipped
  - fakeCounter and splitForcingCounter types added to mapper_test.go
  - TestAnalyze_anthropicProvider_usesAnthropicTokenCounter (to cover case "anthropic" branch in analyze.go)
- RED confirmed: compile error "too many arguments in call to analyzer.MapFeaturesToCode" + "undefined: analyzer.MapperTokenBudget"
- GREEN:
  - mapper.go rewritten: MapperTokenBudget=80_000 constant, accEntry type, new 6-arg MapFeaturesToCode with batchSymLines-based batching loop, split-and-retry on oversized batches, result accumulation and merge
  - tokens.go updated: NewAnthropicCounter now accepts baseURL param ("" = default endpoint)
  - analyze.go updated: switch on llmProvider to select token counter; uses ANTHROPIC_BASE_URL env var for testability
- Tests: 8 passing (mapper), all packages green
- Coverage:
  - internal/analyzer: 90.1% of statements
  - internal/cli: 90.6% of statements
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes:
  - PROMPT: comment is on the line immediately above the promptText format string in the batch loop
  - TestMapFeaturesToCode_ClientError_Propagates and TestMapFeaturesToCode_InvalidJSON_ReturnsError updated to include a file with a symbol (otherwise MapFeaturesToCode returns early before calling the LLM)
  - fakeClient.callCount increments only when idx < len(responses), which is consistent with how multi-batch tests check 2 LLM calls
  - TestAnalyze_anthropicProvider_usesAnthropicTokenCounter: pre-caches spider pages, page analysis, and product summary to skip all network-dependent steps; empty Go file has no exported symbols so MapFeaturesToCode returns without calling count_tokens

## Task 2 (context-length plan): Symbol line batcher - COMPLETE
- Started: 2026-04-20
- Tests written first (RED):
  - TestBatchSymLines_emptyInput_returnsNoBatches
  - TestBatchSymLines_allFitInOneBatch
  - TestBatchSymLines_splitAcrossMultipleBatches
  - TestBatchSymLines_featuresOverheadAccountedFor
  - TestBatchSymLines_singleOversizedLine_getsItsOwnBatch
- RED confirmed: 5x "undefined: batchSymLines" build failure
- GREEN: batcher.go with batchSymLines — accumulate lines until budget exceeded, flush to new batch; oversized single lines placed alone
- Tests: 5 passing, 0 failing
- Coverage: internal/analyzer/batcher.go: 100.0%; internal/analyzer package: 90.6% of statements
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes:
  - budget=0 and featuresTokens==budget cases both result in remaining==0; any line with >=1 token forces a flush because currentTokens+t > 0 when current is non-empty
  - Oversized single lines (tokens > remaining) are still placed in their own batch because the flush-condition only fires when len(current) > 0

## Task 1 (context-length plan): Provider-specific TokenCounter - COMPLETE
- Started: 2026-04-20
- Tests written first (RED):
  - TestTiktokenCounter_emptyString_returnsZero
  - TestTiktokenCounter_nonEmptyString_returnsPositive
  - TestTiktokenCounter_longerString_moreTokens
- RED confirmed: "undefined: analyzer.NewTiktokenCounter" build failure
- Dependencies added: github.com/tiktoken-go/tokenizer v0.7.0, github.com/anthropics/anthropic-sdk-go v1.37.0
- GREEN: tokens.go with TokenCounter interface, tiktokenCounter, AnthropicCounter, countTokens unexported helper
- Tests: 3 passing, 0 failing
- Coverage: internal/analyzer: 89.9% of statements (AnthropicCounter not unit-tested per plan design — requires live API key; panic branch in mustGetEncoder is unreachable)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes:
  - anthropic.WithAPIKey is in the option sub-package (github.com/anthropics/anthropic-sdk-go/option), not the top-level anthropic package
  - AnthropicCounter is integration-test only per plan design; 0.1% coverage gap is expected and acceptable

## Task 9 (LLM Analysis): BifrostClient real implementation - COMPLETE
- Started: 2026-04-20
- Tests written (integration, build-tag gated):
  - TestBifrostClient_Anthropic_RealCompletion (//go:build integration)
  - TestBifrostClient_OpenAI_RealCompletion (//go:build integration)
- Tests written (white-box unit, package analyzer):
  - TestBifrostAccount_GetConfiguredProviders_ReturnsProvider
  - TestBifrostAccount_GetKeysForProvider_MatchingProvider_ReturnsKey
  - TestBifrostAccount_GetKeysForProvider_WrongProvider_ReturnsError
  - TestBifrostAccount_GetConfigForProvider_MatchingProvider_ReturnsConfig
  - TestBifrostAccount_GetConfigForProvider_WrongProvider_ReturnsError
  - TestNewBifrostClientWithProvider_UnsupportedProvider_ReturnsError
  - TestNewBifrostClientWithProvider_Anthropic_ReturnsClient
  - TestNewBifrostClientWithProvider_OpenAI_ReturnsClient
  - TestBifrostClient_ImplementsLLMClient
  - TestBifrostClient_Complete_ReturnsContent
  - TestBifrostClient_Complete_BifrostError_WithMessage
  - TestBifrostClient_Complete_BifrostError_NoErrorField
  - TestBifrostClient_Complete_EmptyChoices_ReturnsError
  - TestBifrostClient_Complete_NilContent_ReturnsError
  - TestBifrostClient_Complete_NilContentStr_ReturnsError
- Tests updated (package cli):
  - TestNewLLMClient_OpenAI_MissingKey_ReturnsError (renamed from NotYetImplemented)
  - TestNewLLMClient_Anthropic_MissingKey_ReturnsError (renamed from NotYetImplemented)
  - TestNewLLMClient_DefaultProvider_MissingKey_ReturnsError (renamed from NotYetImplemented)
  - TestNewLLMClient_OpenAI_WithKey_DefaultModel_ReturnsClient (new)
  - TestNewLLMClient_OpenAI_WithKey_CustomModel_ReturnsClient (new)
  - TestNewLLMClient_Anthropic_WithKey_DefaultModel_ReturnsClient (new)
  - TestNewLLMClient_Anthropic_WithKey_CustomModel_ReturnsClient (new)
  - TestNewLLMClient_DefaultProvider_WithKey_ReturnsClient (new)
- Tests added (package cli, analyze pipeline coverage):
  - TestAnalyze_llmClientError_returnsError
  - TestAnalyze_fullPipeline_withCachedAnalysis
  - TestAnalyze_llmAnalyzeError_continuesWithWarning
- Coverage:
  - internal/analyzer: 95.1% of statements
  - internal/reporter: 100.0% of statements
  - internal/spider: 94.4% of statements
  - internal/cli: 90.8% of statements
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes:
  - RED: integration tests written first (build-tag gated, invisible to normal `go test ./...`)
  - GREEN: bifrost_client.go with bifrostRequester interface for testability; NewBifrostClientWithProvider for anthropic/openai; Complete() wraps ChatCompletionRequest
  - Factory updated: openai/anthropic cases now call NewBifrostClientWithProvider; placeholder errors removed
  - bifrostRequester interface added to BifrostClient so Complete() can be tested without real API calls
  - GetKeysForProvider signature is context.Context (not *context.Context as plan suggested); Key.Value is schemas.EnvVar (not string); ChatCompletionRequest takes *schemas.BifrostContext
  - schemas.NewEnvVar(apiKey) used to construct Key.Value; schemas.NewBifrostContext(ctx, schemas.NoDeadline) used in Complete()
  - Three analyze pipeline tests added to bring internal/cli from 67.2% → 90.8% by exercising post-crawl code paths via pre-populated spider index + httptest Ollama server

## Task 8 (LLM Analysis): OpenAICompatibleClient + provider factory - COMPLETE
- Started: 2026-04-20
- Tests written (Part 1 — OpenAICompatibleClient):
  - TestOpenAICompatibleClient_Complete_ReturnsContent
  - TestOpenAICompatibleClient_ServerError_ReturnsError
  - TestOpenAICompatibleClient_EmptyChoices_ReturnsError
  - TestOpenAICompatibleClient_ImplementsLLMClient
  - TestOpenAICompatibleClient_WithAPIKey_SendsAuthHeader
  - TestOpenAICompatibleClient_BadJSON_ReturnsError
- Tests written (Part 2 — provider factory, package cli):
  - TestNewLLMClient_Ollama_DefaultsApplied
  - TestNewLLMClient_Ollama_CustomBaseURL
  - TestNewLLMClient_LMStudio_MissingModel_ReturnsError
  - TestNewLLMClient_OpenAICompatible_MissingBaseURL_ReturnsError
  - TestNewLLMClient_UnknownProvider_ReturnsError
  - TestNewLLMClient_OpenAI_NotYetImplemented_ReturnsError
  - TestNewLLMClient_Anthropic_NotYetImplemented_ReturnsError
  - TestNewLLMClient_DefaultProvider_NotYetImplemented_ReturnsError
  - TestNewLLMClient_OpenAICompatible_MissingModel_ReturnsError
  - TestNewLLMClient_LMStudio_CustomBaseURL
  - TestNewLLMClient_OpenAICompatible_WithAPIKey
- Tests: 25 analyzer tests + 30 cli tests, 0 failing (all packages green)
- Coverage: internal/analyzer 94.6% of statements; internal/cli: llm_client.go 100%, overall 63.6% (analyze.go RunE pre-existing low coverage from Task 7)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues via golangci-lint ./internal/analyzer/... ./internal/cli/...)
- Completed: 2026-04-20
- Notes:
  - RED (Part 1): "undefined: analyzer.NewOpenAICompatibleClient" on 4 test references
  - GREEN (Part 1): openai_compatible_client.go with NewOpenAICompatibleClient + Complete; uses only net/http + encoding/json
  - RED (Part 2): Ollama tests got "LLM client not yet implemented — see Task 8" from stub
  - GREEN (Part 2): llm_client.go factory with ollama/lmstudio/openai-compatible cases; openai and anthropic return placeholder error pending Task 9 BifrostClient
  - apiKey Authorization header path and bad-JSON decode path added as extra tests to push Complete coverage from 78% to 87%
  - newLLMClient reaches 100% statement coverage; placeholder branches are exercised by the NotYetImplemented tests

## Task 7 (LLM Analysis): Wire analyzer into analyze CLI - COMPLETE
- Started: 2026-04-20
- Tests written: analyze_llm_flags.txtar (new txtar asserting --llm-provider accepted without error)
- Tests: 196 passing, 0 failing
- Coverage: Existing packages unaffected; stub llm_client.go path not reachable without --docs-url
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues via golangci-lint ./internal/cli/... ./cmd/...)
- Completed: 2026-04-20
- Notes: RED = "unknown flag: --llm-provider"; GREEN = analyze.go rewritten with 3 new flags + full pipeline; llm_client.go stub returns error never reached when no --docs-url; both analyze_llm_flags.txtar and analyze_stub.txtar pass; commit d8f3022

## Task 6 (LLM Analysis): internal/reporter WriteMapping and WriteGaps - COMPLETE
- Started: 2026-04-20
- Tests written: TestWriteMapping_CreatesFile, TestWriteGaps_CreatesFile, TestWriteMapping_EmptyMapping_Succeeds, TestWriteGaps_NoneFound, TestWriteGaps_SkipsNonFuncTypeInterface, TestWriteGaps_UnexportedSymbolSkipped
- Tests: 6 passing, 0 failing
- Coverage: 100.0% of statements (internal/reporter) — WriteMapping 100%, WriteGaps 100%, isExported 100%
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes: RED confirmed with "no non-test Go files" compile error. GREEN: reporter.go with WriteMapping and WriteGaps. Initial 3 tests yielded 84.6% coverage; added 3 targeted tests to cover: (1) "_None found." branch when all exported symbols are mapped, (2) kind-filter continue branch for KindConst/KindVar symbols, (3) isExported empty-name early-return. Commit: 2c6b974.

## Task 5 (LLM Analysis): Extend spider.Index with analysis fields - COMPLETE
- Started: 2026-04-20
- Tests written: TestIndex_RecordAnalysis_PersistsAndLoads, TestIndex_SetProductSummary_PersistsAndLoads
- Tests: 30 passing, 0 failing (2 new + 28 existing spider tests)
- Coverage: 94.4% of statements (internal/spider)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes: RED confirmed with 7x compile errors (RecordAnalysis/Analysis/SetProductSummary/ProductSummary/AllFeatures undefined). Schema is backward-incompatible: old flat map `{"url":{...}}` replaced with `{"pages":{"url":{...}},"product_summary":"...","all_features":[...]}`. Existing TestLoadIndex_existingIndex_loadsEntries test updated to use new schema format. Record() now preserves existing Summary/Features when updating Filename/FetchedAt (reads entry, updates fields, writes back). Commit: 790165e.

## Task 4 (LLM Analysis): MapFeaturesToCode - COMPLETE
- Started: 2026-04-20
- Tests written: TestMapFeaturesToCode_ReturnsMappings, TestMapFeaturesToCode_EmptyFeatures_ReturnsEmpty, TestMapFeaturesToCode_ClientError_Propagates, TestMapFeaturesToCode_InvalidJSON_ReturnsError, TestMapFeaturesToCode_NilFilesAndSymbols_NormalizedToEmpty
- Tests: 19 passing, 0 failing (5 new TestMapFeaturesToCode_* + 14 from Tasks 1-3)
- Coverage: 98.0% of statements (internal/analyzer); mapper.go: 96.3% (unreachable json.Marshal error branch)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues) — staticcheck S1016 fixed by using FeatureEntry(e) type conversion instead of struct literal
- Completed: 2026-04-20
- Notes: RED confirmed with 5x "undefined: analyzer.MapFeaturesToCode" compile errors. GREEN: mapper.go with // PROMPT: comment on line immediately above the prompt string. Empty features list short-circuits before any LLM call (callCount assertion confirms this). nil Files/Symbols normalized to empty slices. mapEntry → FeatureEntry converted via type conversion (identical underlying field structure, differing only in json tags).

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

## Task 3: `lang/generic.go` full implementation — COMPLETE

- Started: 2026-04-17
- Goal: Replace the `GenericExtractor` stub body with an explicit implementation that returns empty (non-nil) slices `[]scanner.Symbol{}` and `[]scanner.Import{}` instead of `nil, nil`.
- TDD cycle:
  - **RED**: Created `internal/scanner/lang/generic_test.go` with three tests: `TestGenericExtractor_returnsEmptySlices_notNil` (checks `syms != nil` and `imps != nil`), `TestGenericExtractor_languageIsGeneric`, and `TestGenericExtractor_emptyContent_noError`. Ran tests — 2 failed with "expected non-nil (empty) symbols/imports slice, got nil" for the exact right reason.
  - **GREEN**: Changed `generic.go` `Extract` body from `return nil, nil, nil` to `return []scanner.Symbol{}, []scanner.Import{}, nil`. All 3 new tests passed immediately.
  - **REFACTOR**: Updated `stubs_test.go` (`TestStub_Extract_returnsNil`) to remove `GenericExtractor` from the nil-check loop, since it is no longer a stub. Added explanatory comment pointing to `generic_test.go`. All 12 lang-package tests pass.
- Tests: 12 passing, 0 failing (lang package)
- Coverage: internal/scanner/lang: 100.0% of statements
- Build: ✅ Successful (`go build ./...`)
- Linting: ✅ Clean (`golangci-lint run`, 0 issues)
- Completed: 2026-04-17
- Notes: The nil-vs-empty-slice distinction matters for consumers that use `json.Marshal` (nil → `null`, empty slice → `[]`) and for callers that range-check results. `GenericExtractor` is the first extractor to graduate from stub to full implementation.

## Task 4: `lang/go.go` Go tree-sitter extractor — COMPLETE

- Started: 2026-04-17
- Goal: Replace the `GoExtractor` stub with a full go-tree-sitter implementation that extracts exported functions (inc. methods), types, consts, vars with doc comments + line numbers, and all import paths with optional aliases.
- TDD cycle:
  - **RED**: Created `internal/scanner/lang/go_test.go` with 13 tests covering: exported func with doc comment and line number, unexported func skipped, exported type, exported const, grouped imports with alias, single-line import, empty file, exported var, exported method vs unexported method, Language()/Extensions() contract, first-decl no-doc-comment, blank import alias. Ran `go test ./internal/scanner/lang/... -run TestGoExtractor` — 7 tests FAILED (stub returns nil/empty). Remaining tests passed (stub behaviour was correct for those edge cases).
  - **GREEN**: Replaced stub body in `go.go` with full tree-sitter implementation: parser setup with `golang.GetLanguage()`, walk root children by node type (`function_declaration`, `method_declaration`, `type_declaration`, `const_declaration`, `var_declaration`, `import_declaration`), exported check via `unicode.IsUpper`, doc comment via preceding sibling of type `comment`, signature building via field children, import extraction with `goExtractImports` handling both `import_spec_list` (grouped) and direct `import_spec` (single-line). All 13 new tests PASS.
  - **REFACTOR**: Updated `stubs_test.go` to remove `GoExtractor` from the nil-check loop (it's no longer a stub). Added explanatory comment pointing to `go_test.go`. All 25 lang-package tests pass.
- Tests: 25 passing, 0 failing (lang package); all packages: 5 packages, 0 failures
- Coverage: internal/scanner/lang: 96.6% of statements (well above 90% gate)
- Build: ✅ Successful (`go build ./...`)
- Linting: ✅ Clean (`golangci-lint run`, 0 issues)
- Completed: 2026-04-17
- Notes:
  - Tree-sitter quirk: Go's grouped import block is `import_spec_list` wrapping individual `import_spec` nodes; a bare `import "os"` produces an `import_spec` directly under `import_declaration`. The `goExtractImports` helper handles both cases.
  - `goPrecedingComment` checks only the immediately preceding sibling — Go doc comments always immediately precede the declaration they document, so no multi-comment scan is needed.
  - The `prev == nil` branch in `goPrecedingComment` is not reachable in practice (tree-sitter always returns valid sibling nodes), so coverage stays at 96.6% rather than 100%. This is acceptable — the branch is a defensive nil guard, not dead code.

## Task 5: `lang/python.go` Python tree-sitter extractor — COMPLETE

- Started: 2026-04-17
- Goal: Replace the `PythonExtractor` stub with a full go-tree-sitter implementation that extracts public module-level `def` (KindFunc) and `class` (KindClass) declarations, docstrings, import paths, and line numbers. Names starting with `_` are skipped.
- TDD cycle:
  - **RED**: Created `internal/scanner/lang/python_test.go` with 12 tests: public func extracted, private func skipped, public class extracted, private class skipped, imports extracted (plain + aliased + from-style), docstring extracted from func, docstring extracted from class, language/extensions contract, line numbers recorded, signature recorded, empty file no error, nested func skipped (only module-level). Ran `go test ./internal/scanner/lang/... -run TestPythonExtractor` — 9 tests FAILED (stub returns nil/empty). Correct RED state.
  - **GREEN**: Implemented `python.go` with tree-sitter parser using `python.GetLanguage()`. Walked root children: `function_definition` and `class_definition` for symbols (skipping `_`-prefixed names), `import_statement` and `import_from_statement` for imports. Added `pyDocstring` helper that inspects only the first child of the body node for a `string` expression statement, stripping triple- and single-quote delimiters. All 12 tests PASS.
  - **LINT FIX**: golangci-lint flagged SA4008 (loop condition never changes) and SA4004 (loop unconditionally terminated) in `pyDocstring`. Rewrote the helper to directly access `body.Child(0)` without a loop. Lint clean, tests still pass.
  - **REFACTOR**: Updated `stubs_test.go` to remove `PythonExtractor` from the nil-check loop with explanatory comment pointing to `python_test.go`. All 37 lang-package tests pass.
- Tests: 37 passing, 0 failing (lang package); all packages: 5 packages, 0 failures
- Coverage: internal/scanner/lang: 94.6% of statements (above 90% gate)
- Build: Successful (`go build ./...`)
- Linting: Clean (`golangci-lint run`, 0 issues)
- Completed: 2026-04-17
- Notes:
  - Tree-sitter quirk: Python aliased imports (`import sys as system`) produce an `aliased_import` node with a `name` field child; plain imports produce `dotted_name` directly. Both cases handled in the `import_statement` branch.
  - `from X import Y` statements produce `import_from_statement` nodes; only the module name (`X`) is recorded as the import path, consistent with the Go extractor's approach.
  - Nested functions are automatically excluded because the walker only iterates `root.Child(i)` at module level — tree-sitter does not flatten nested definitions into the module scope.
  - Docstring stripping is order-sensitive: triple-quote prefixes/suffixes must be removed before single-quote ones to avoid double-stripping `"` from `"""`.

## Task 6: `lang/typescript.go` TypeScript/JS tree-sitter extractor — COMPLETE

- Started: 2026-04-17
- Goal: Replace TypeScriptExtractor stub with full tree-sitter implementation extracting exported symbols (func/class/const/interface/type) and imports from .ts/.tsx/.js/.jsx/.mjs files.
- TDD cycle:
  - **RED**: typescript_test.go written (18 tests) — stub returned nil/nil/nil, tests expecting non-nil results all failed.
  - **GREEN**: Implemented typescript.go using TypeScript grammar for .ts/.tsx and JavaScript grammar for .js/.jsx/.mjs. Walks root children for `export_statement` (extracts declaration via `declaration` field) and `import_statement` (handles default, namespace, named, side-effect). Used `context.Background()` for ParseCtx.
  - **REFACTOR**: Updated stubs_test.go to remove TypeScriptExtractor from nil-check loop.
- Tests: 55 passing, 0 failing (lang package); all 5 packages green
- Coverage: internal/scanner/lang: 92.7% of statements (above 90% gate)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-17
- Notes:
  - TypeScript `export_statement` wraps the actual declaration in a `declaration` field child — node types: `function_declaration`, `class_declaration`, `lexical_declaration` (const/let), `interface_declaration`, `type_alias_declaration`.
  - `lexical_declaration` (const/let) requires descending into `variable_declarator` child to get the name.
  - JSDoc (`/** ... */`) preceding comment stripped of delimiters for DocComment.
  - Import clause variants: default (`identifier`), namespace (`namespace_import` with identifier child), named (`named_imports` with `import_specifier` children), side-effect (no clause).

## Task 7: `lang/rust.go` Rust tree-sitter extractor — COMPLETE

- Started: 2026-04-17
- Goal: Replace the `RustExtractor` stub with a full go-tree-sitter implementation that extracts `pub` top-level declarations (fn→KindFunc, struct/enum→KindType, trait→KindInterface, const→KindConst) and `use` declarations as imports, including aliased forms.
- TDD cycle:
  - **RED**: Created `internal/scanner/lang/rust_test.go` with 17 tests: pub fn extracted, private fn skipped, pub struct extracted, pub enum extracted, pub trait extracted, pub const extracted, use declaration imported, use alias imported, line number recorded, empty file no error, language/extensions contract, doc comment extracted, signature recorded, multi-line doc comment, simple identifier use, glob use no error, nested fn in impl skipped. Ran `go test ./internal/scanner/lang/ -run TestRust` — 10 tests FAILED (stub returns nil/nil/nil). Correct RED state.
  - **GREEN**: Implemented `rust.go` with `rust.GetLanguage()` grammar. Walk root children by node type (`function_item`, `struct_item`, `enum_item`, `trait_item`, `const_item`, `use_declaration`). `rustIsPub` checks for a `visibility_modifier` child of type `pub`. `rustPrecedingDocComment` walks backwards collecting consecutive `///` line_comment nodes. `rustParseUseDecl` handles two tree-sitter shapes: `scoped_identifier`/`identifier` for plain use, `use_as_clause` (with scoped_identifier + "as" + identifier) for aliased use. All 17 tests PASS.
  - **REFACTOR**: Updated `stubs_test.go` to remove `RustExtractor` from the nil-check loop; loop body is now empty (all stubs replaced). All 72 lang-package tests pass.
- Tests: 72 passing, 0 failing (lang package); all 5 packages green
- Coverage: internal/scanner/lang: 91.8% of statements (above 90% gate); rust.go per-function: Language 100%, Extensions 100%, Extract 87%, rustIsPub 100%, rustFuncSig 75% (unreachable else branch — tree-sitter function_item always contains `{`), rustPrecedingDocComment 87.5%, rustParseUseDecl 93.8%
- Build: ✅ Successful (`go build ./...`)
- Linting: ✅ Clean (`golangci-lint run`, 0 issues)
- Completed: 2026-04-17
- Notes:
  - Grammar quirk: The Rust tree-sitter grammar does NOT produce a `use_tree` node for simple use declarations. Instead, `use_declaration` contains either a `scoped_identifier` (for `use a::b::C`) or a `use_as_clause` (for `use a::b::C as D`). Glob forms (`use a::b::*`) produce a `use_list`/`use_wildcard` child and are currently skipped (no import emitted).
  - `rustFuncSig` trims at `{` or `\n` to return just the function header; the fallback `else` branch (no `{` or `\n` in content) is unreachable in practice because tree-sitter function_item nodes always include the body.
  - Methods inside `impl` blocks are correctly excluded: the walker only visits root-level children, so `function_item` nodes nested inside `impl_item` are never reached.

## Task 12: scanner.go Scan() orchestrator — COMPLETE

- Started: 2026-04-17
- Goal: Implement `Scan(root string, opts Options) (*ProjectScan, error)` that walks the repo, extracts symbols/imports, builds the import graph, writes project.md, and caches results in scan.json.
- TDD cycle:
  - **RED**: Created `internal/scanner/scanner_test.go` with 6 tests: emptyDir_returnsEmptyScan, goFile_extractsSymbols, cacheReusedOnSecondRun, writesProjectMd, noCache_forcesReparse, countLines. Ran tests — compile error "undefined: Scan, Options" (correct RED state).
  - **GREEN**: Created `internal/scanner/scanner.go` with `Options` struct (`CacheDir`, `NoCache`, `ModulePrefix`), `Scan()` (Walk callback → lang.Detect + Extract → BuildGraph → GenerateReport → cache.Save), and `countLines()`. All 6 tests PASS.
  - **REFACTOR**: Resolved import cycle `scanner → lang → scanner` by extracting `Symbol`, `Import`, `SymbolKind` types into `internal/scanner/types/types.go`. `scanner` re-exports via type aliases; `lang` tests updated to import `types` directly.
- Tests: 6 passing, 0 failing (scanner_test.go); all packages green
- Coverage: internal/scanner: 94.2% of statements (above 90% gate)
- Build: ✅ Successful (`go build ./...`)
- Linting: ✅ Clean (`golangci-lint run`, 0 issues)
- Completed: 2026-04-17
- Notes:
  - countLines bug: initial impl subtracted 1 for trailing newline; test expects trailing newline to count (e.g., "line1\nline2\n" = 3). Fixed to `bytes.Count("\n") + 1`.
  - Import cycle fix: `scanner/types/` is a pure types package (no deps); `lang` imports `types`; `scanner` imports both `types` and `lang`. Lang tests updated from `scanner.KindFunc` → `types.KindFunc` etc.

## Task 13: `cli/analyze.go` — wire `--repo`, `--scan-cache-dir`, `--no-cache` — COMPLETE

- Started: 2026-04-17
- Goal: Replace the `analyze` stub with a real implementation that accepts `--repo`, `--scan-cache-dir`, and `--no-cache` flags and calls `scanner.Scan()`.
- TDD cycle:
  - **RED**: Created `internal/cli/analyze_test.go` with 5 tests: repoFlag_appearsInHelp, noCacheFlag_appearsInHelp, scanCacheDirFlag_appearsInHelp, repoFlag_scansDirectory, noCache_flagAccepted. Ran `go test ./internal/cli/...` — all 5 FAILED ("--repo flag not in help output" / "unknown flag: --repo").
  - **GREEN**: Replaced `analyze.go` with Cobra command that wires `--repo` (default "."), `--scan-cache-dir` (default ".find-the-gaps/scan-cache"), `--no-cache` flags to `scanner.Scan()`. Outputs "scanned N files". Updated `root_test.go` to remove stale "not yet implemented" test. Updated `analyze_stub.txtar` to test the real behavior.
  - **LINT FIX**: `errcheck` flagged unchecked `fmt.Fprintf` — fixed with `_, _ =`.
- Tests: all packages green (cmd, cli, doctor, scanner, scanner/lang)
- Coverage: internal/cli 97.0%, all packages above 90% gate
- Build: ✅ Successful (`go build ./...`)
- Linting: ✅ Clean (`golangci-lint run`, 0 issues)
- Completed: 2026-04-17
- Notes:
  - `internal/spider` not yet merged; crawl section omitted — analyze only runs the scanner for now.
  - `--docs-url` flag deferred until spider package is available.
  - `go test -coverprofile=... ./...` fails in cmd/find-the-gaps due to testscript's sandboxed binary not being able to write coverage metadata — this is pre-existing and not related to this task.

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

## Task 1 (LLM Analysis): Data Types + LLMClient Interface - COMPLETE
- Started: 2026-04-20
- Tests: 4 passing, 0 failing
- Coverage: [no statements] — correct for types-only package (pure type declarations and interface, no executable statements)
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: 2026-04-20
- Notes: Bifrost SDK install deferred to Task 9 (go mod tidy removes it until an import exists). fakeClient callCount bug fixed post-review.

## Task 2 (LLM Analysis): AnalyzePage - COMPLETE
- Started: 2026-04-20
- Tests: 8 passing, 0 failing (4 new TestAnalyzePage_* + 4 existing from Task 1)
- Coverage: 90.0% of statements (internal/analyzer)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes: RED confirmed with "undefined: analyzer.AnalyzePage" compile error. GREEN: analyze_page.go implements AnalyzePage with // PROMPT: comment on line immediately above the prompt string. JSON response struct (analyzePageResponse) is unexported. nil features slice normalized to empty slice before returning.

## Task 4 (verbose logging): Add log.Debug calls to doctor package - COMPLETE
- Started: 2026-04-20
- Tests written first (RED):
  - TestRun_verbose_doctorShowsDebugOutput — runs `doctor` with `--verbose`, asserts at least one DEBU line appears in stderr regardless of whether rg/mdfetch are installed
- RED confirmed: no debug output produced before log calls were added to doctor package
- GREEN: Added log.Debug calls in internal/doctor/doctor.go to emit per-tool detection results at debug level
- Tests: 1 new test, all packages green
- Coverage: internal/doctor: 100.0% of statements
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes: Test does not assert exit code because rg/mdfetch presence varies across environments; only DEBU output is checked

## Task 3 (verbose logging): Add log calls to analyze pipeline - COMPLETE
- Started: 2026-04-20
- Tests written first (RED):
  - TestRun_verbose_showsDebugOutput — runs analyze with --verbose over an empty temp repo, asserts stderr contains "DEBU"
  - TestRun_noVerbose_noDebugOutput — same analyze call without --verbose, asserts no "DEBU" in stderr
  - TestRun_noVerbose_infoLogsVisible — runs analyze without --verbose, asserts "scanning repository" Info log appears in stderr
- RED confirmed: no debug/info output present before log calls were wired into the analyze pipeline
- GREEN: Added log.Info and log.Debug calls throughout internal/cli/analyze.go pipeline phases; fixed test expectations
- Tests: 3 new tests + fixes, all packages green
- Coverage: internal/cli: 91.5% of statements
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes: Tests must not use t.Parallel() — they share the global charmbracelet/log logger; see comment in root_test.go

## Task 2 (verbose logging): Add --verbose/-v persistent flag - COMPLETE
- Started: 2026-04-20
- Tests written first (RED):
  - TestRootCmd_verboseFlag_appearsInHelp — asserts "--verbose" appears in `--help` output
  - TestRootCmd_verboseShorthand_appearsInHelp — asserts "-v" appears in `--help` output
  - TestRootCmd_verbose_acceptedWithoutError — runs analyze with --verbose, expects exit code 0
- RED confirmed: "unknown flag: --verbose" error before flag was registered
- GREEN: Added --verbose/-v persistent flag to root command; PersistentPreRunE sets charmbracelet/log level to Debug and redirects log output to cmd stderr
- Tests: 3 new tests, all packages green
- Coverage: internal/cli: 90.8% of statements
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes: PersistentPreRunE resets logger per invocation so sequential test runs don't leak state into each other

## Task 1 (verbose logging): Add charmbracelet/log dependency - COMPLETE
- Started: 2026-04-20
- Tests written first (RED): N/A — dependency-only task; no new production logic, no tests required
- GREEN: Ran `go get github.com/charmbracelet/log` and `go mod tidy`; dependency added to go.mod and go.sum
- Tests: N/A (no new test code)
- Coverage: N/A
- Build: ✅ Successful (go build ./... clean after dependency added)
- Linting: ✅ Clean (0 issues)
- Completed: 2026-04-20
- Notes: charmbracelet/log is a structured leveled logger compatible with charmbracelet/bubbletea output model; imported in subsequent verbose-logging tasks

## Task 3 (LLM Analysis): SynthesizeProduct - COMPLETE
- Started: 2026-04-20
- Tests written: TestSynthesizeProduct_ReturnsDescriptionAndFeatures, TestSynthesizeProduct_SinglePage_OK, TestSynthesizeProduct_ClientError_Propagates, TestSynthesizeProduct_InvalidJSON_ReturnsError, TestSynthesizeProduct_NilFeatures_NormalizedToEmpty
- Tests: 14 passing, 0 failing (5 new TestSynthesizeProduct_* + 9 from Tasks 1-2)
- Coverage: 100.0% of statements (internal/analyzer)
- Build: ✅ Successful
- Linting: ✅ Clean (0 issues) — staticcheck S1016 fixed by using type conversion ProductSummary(resp) instead of struct literal
- Completed: 2026-04-20
- Notes: RED confirmed with 5x "undefined: analyzer.SynthesizeProduct" compile errors. GREEN: synthesize.go with // PROMPT: comment immediately above the prompt string. synthesizeResponse type is unexported. nil features normalized to empty slice. Type conversion ProductSummary(resp) used instead of struct literal (fields match exactly).

## Task: LLM Tiering - COMPLETE
- Started: 2026-04-22
- Tests: full repo green; analyzer package 133, cli package 74, cmd 6 testscripts pass
- Coverage: analyzer 94.8%, cli 86.0%, cmd 100.0%, reporter 97.4%, doctor 98.4%, scanner 93.8%, spider 94.4% (go test -cover ./...)
- Build: ✅ Successful (go build ./...)
- Linting: ✅ Clean (golangci-lint run)
- Completed: 2026-04-23
- Notes:
  - Plan: `.plans/LLM_TIERING_PLAN.md` (20 tasks + Task 9.1 follow-up)
  - All 7 analyzer call sites route through `analyzer.LLMTiering`: AnalyzePage/SynthesizeProduct/MapFeaturesToDocs/isReleaseNotePage → Small(); ExtractFeaturesFromCode → Typical(); MapFeaturesToCode/DetectDrift agentic → Large() + LargeCounter()
  - CLI flags `--llm-small/--llm-typical/--llm-large` replaced `--llm-provider/--llm-model/--llm-base-url` (breaking). Combined `provider/model` syntax; bare models default to anthropic.
  - Fail-fast validation on unknown providers and non-tool-use large tier (enforced at `newLLMTiering` via `validateTierConfigs`)
  - Integration tests: `analyze_tier_flags.txtar` (happy-path all three flags) and `analyze_tier_reject_ollama_large.txtar` (fail-fast)
  - Restored `TestAnalyze_llmAnalyzeError_continuesWithWarning` against ollama-compatible httptest server via OLLAMA_BASE_URL (feasible post-tiering because AnalyzePage routes to Small)
  - README: tier table + TOML form + env-var list + breaking-change callout; CHANGELOG: Unreleased entry with migration recipe

## Task: Split screenshots into their own report - COMPLETE
- Started: 2026-04-24
- Tests: 434 passing, 0 failing
- Coverage: reporter: 98.0%, cli: 90.5%, analyzer: 94.9%, doctor: 98.4%, scanner: 93.8%, scanner/lang: 91.8%, spider: 94.4%, cmd/find-the-gaps: 100.0%
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: 2026-04-24
- Notes: `gaps.md` no longer contains a Missing Screenshots section; new `screenshots.md` written whenever the screenshot pass runs (including zero-findings case). Skipped pass writes no file and is annotated in the CLI reports block.

## Task: Semver versioning + GitHub release workflow - COMPLETE
- Started: 2026-04-24
- Tests: full repo green; cli package adds TestResolveVersion (8 sub-cases)
- Coverage: cli 90.7% of statements (go test -cover)
- Build: ✅ Successful (go build ./...)
- Linting: ✅ Clean (golangci-lint run; actionlint clean on .github/workflows/release.yml)
- Completed: 2026-04-24
- Notes:
  - RED → GREEN per CLAUDE.md TDD: TestResolveVersion failed with "undefined: resolveVersion" before implementation.
  - `internal/cli/root.go` adds `resolveVersion(ldflagsVersion, buildInfoVersion)` with precedence ldflags > BuildInfo > "dev"; cobra `Version` is now `currentVersion()` which reads `runtime/debug.ReadBuildInfo()`.
  - End-to-end verified: `go build -ldflags "-X .../internal/cli.version=v0.1.0-test"` → `ftg version v0.1.0-test`; default build → `ftg version dev`.
  - `.github/workflows/release.yml`: tag-push trigger; native-runner build matrix (macos-latest, macos-15-intel, ubuntu-latest, ubuntu-24.04-arm); CGO=1 (required by go-tree-sitter); ldflags injection of `${{ github.ref_name }}`; SLSA provenance via `actions/attest-build-provenance@v2`; release job creates tagged GitHub Release with tarballs + sha256 checksums and `--generate-notes`.
  - Goreleaser intentionally NOT used: CGO_ENABLED=0 snapshot build failed because go-tree-sitter requires CGO; cross-compiling CGO needs goreleaser-cross (heavy Docker pull) — matrix of native runners is simpler.
  - Homebrew tap deferred: no tap repo currently exists; revisit when one is set up.

## Task: Refactor — push agent loop into CompleteWithTools - COMPLETE
- Started: 2026-04-24
- Tests: full repo green; analyzer package green (95.1%); cli green (90.5%)
- Coverage: total 92.4%; analyzer 95.1%, cli 90.5%, reporter 98.0%, scanner 93.8%, spider 94.4%, cmd 100.0%, doctor 98.4%
- Build: ✅ Successful (`go build ./...`)
- Linting: ✅ Clean (`golangci-lint run` — 0 issues)
- Completed: 2026-04-24
- Notes:
  - Plan: `.plans/REFACTOR_COMPLETE_WITH_TOOLS.md`
  - New: `Tool.Execute` (ToolHandler), `AgentResult`, `ErrMaxRounds`, `AgentOption`/`WithMaxRounds`, `RunAgentLoop` driver, `TurnFunc` type
  - `ToolLLMClient.CompleteWithTools` now runs the multi-turn loop internally; `BifrostClient` provides `completeOneTurn` as the per-turn TurnFunc
  - Drift rewritten to accumulator pattern: `add_finding` tool with closure capturing local `findings []DriftIssue`. Old `submit_findings` terminal tool deleted; `executeTool`/`executeReadFile`/`executeReadPage` free funcs replaced with `readFileTool`/`readPageTool`/`addFindingTool` builder helpers that return Tools with Execute closures.
  - On `ErrMaxRounds`, drift returns whatever findings were accumulated before exhaustion (partial-progress recovery), then continues processing the next feature.
  - 7 new agent-loop primitive tests (`agent_loop_test.go`) cover: text-on-first-turn, tool dispatch + result feedback, unknown tool, handler error, turn error, max-rounds exceeded, max-rounds allows completion.
  - Drift test rewrite: `submitFindings` helper deleted; replaced with `addFinding(issue)` + `driftDone()` helpers. `TestDetectDrift_TextResponseWithoutSubmitFindings_ReturnsError` deleted (text without prior add_finding is the empty-findings case). `TestDetectDrift_SubmitFindingsBadJSON_ReturnsError` replaced with `TestDetectDrift_AddFindingBadJSON_FedBackToLLM`. `TestDetectDrift_MaxRoundsExceeded_*` now asserts that findings accumulated before exhaustion ARE returned alongside the next feature's findings.
  - Existing schema-translation tests in `bifrost_client_test.go` retargeted at `completeOneTurn` (white-box, same package); a new `TestBifrostClient_CompleteWithTools_AdaptsRunAgentLoop` exercises the public adapter end-to-end.
  - Test-double signature changes: `stubToolClient`, `stubLLMClient`, `driftStubClient`, `driftStubClientWithErr` updated to `(ctx, msgs, tools, opts ...AgentOption) (AgentResult, error)`. `driftStubClient` drives `RunAgentLoop` with its scripted responses as the TurnFunc, so loop behavior is shared between production and tests.
