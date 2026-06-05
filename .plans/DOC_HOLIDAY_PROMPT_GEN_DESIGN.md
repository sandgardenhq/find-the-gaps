# Doc Holiday Prompt Generation — Design

**Date:** 2026-06-05
**Branch:** `doc-holiday-prompt-gen`
**Status:** Design complete, ready for implementation plan

## Summary

`ftg analyze` produces a new default-on artifact, `prompts.md`, containing
ready-to-paste, LLM-authored prompts that drive **Doc Holiday**
(https://doc.holiday, an AI documentation agent) to fix the gaps the tool
found. Scope is limited to two writing-centric gap categories: **stale
documentation** and **missing documentation**. Screenshots and dead links are
out of scope — the existing reports already give a maintainer what they need
for those.

The meta-prompts that instruct the LLM how to author each Doc Holiday prompt
are authored as **agent-skill-format markdown files**, `//go:embed`-ed into the
binary, so they are easy to read and edit independently of generation logic.

## Decisions (from brainstorming)

| # | Decision |
|---|----------|
| Integration surface | Copy-paste: emit a well-formatted document; no API/CLI integration with Doc Holiday. |
| Prompt granularity | Group by page (stale) / by feature (missing). |
| Split rule | Group by page **and** gap-type as the base unit; cap by issue count as a safety valve (overflow → "part N"). |
| Gap categories | Stale documentation + missing documentation only. |
| Missing-docs unit | One prompt per undocumented feature. |
| Context depth | Doc Holiday has repo + docs access; prompts are **pointers** that name the page/feature and state the specific gap — no embedded code/doc bodies. |
| Artifact | Standalone `prompts.md`, default-on, listed in the stdout `reports:` block. Not woven into the PDF or Hugo site. |
| Generation | LLM-authored (not a static template). |
| Skill form | Agent-skill-compatible markdown, stored in the Go source and loaded via `//go:embed`. |
| Number of skills | Two: `fix-stale-docs`, `document-new-feature`. |
| Document order | By category, then priority (Large → Medium → Small). |
| Tier | Typical tier (same as drift detection). |
| Caching | Per-unit content-hash cache (`prompts-cache.json`), SIGINT-resumable, mirroring the drift/screenshot caches. |
| Opt-in? | No — default-on. |

## Data flow

Inputs are already computed by the existing pipeline; this feature adds **no new
analysis**, only prompt authoring:

- `[]analyzer.DriftFinding` — stale-docs findings, each issue page-anchored with
  a `Priority`.
- `[]analyzer.FeatureEntry` from `reporter.UndocumentedFeatures(...)` —
  undocumented user-facing features, each with files, symbols, description, and
  a "why document this" rationale.

**Prompt units** (one unit → one LLM call → one Doc Holiday prompt):

- **Stale:** bucket drift issues by `Issue.Page`; within a page, split into
  chunks of ≤ N issues (`N` a package const, ~5); overflow chunks get a "(part
  K of M)" heading. A chunk's priority is the highest priority among its issues.
- **Missing:** one unit per `FeatureEntry`.

## Components

### `internal/docholiday` (new package)

> Note: `internal/forge` is unrelated (source-control forge URL detection). New
> package is `internal/docholiday`.

```go
func GeneratePrompts(ctx context.Context, gen Generator, in Input, cache Cache) ([]Prompt, error)
```

- `Generator` — narrow interface (meta-prompt + user message + tier → text),
  satisfied by the existing tiered Bifrost client (or a thin adapter). Tests
  inject a fake; no network.
- `Input` — carries `[]DriftFinding` and `[]FeatureEntry`.
- `Prompt` — `{Category, Heading, Priority, Body}`.

**Unit assembly** is pure (no LLM): bucketing, chunking, priority rollup.

**LLM call** per unit: meta-prompt = the matching embedded skill body
(frontmatter stripped), user message = a deterministic rendering of the unit's
facts, tier = **typical**. Output text = the Doc Holiday prompt body.

**Parallelism:** units dispatch through the existing bounded worker pool
(`internal/parallel`). Results are sorted deterministically (category →
priority → page/feature) before writing, so parallel and serial runs produce
byte-identical `prompts.md`.

### Embedded skills

`internal/docholiday/skills/`:

- `fix-stale-docs/SKILL.md`
- `document-new-feature/SKILL.md`

Each is valid agent-skill format — YAML frontmatter (`name`, `description`) plus
an imperative markdown body — so the same file works if dropped into
`.claude/skills/`. Loaded via `//go:embed`:

```go
//go:embed skills/fix-stale-docs/SKILL.md
var fixStaleDocsSkill string

//go:embed skills/document-new-feature/SKILL.md
var documentNewFeatureSkill string
```

At runtime the skill **body** is the system/meta-prompt; the per-unit findings
are the user message. Frontmatter is stripped before injection (a small helper)
so YAML is not fed to the model. Per CLAUDE.md's `// PROMPT:` rule, the embed var
and the call site carry `// PROMPT:` comments pointing at the skill file.

### Caching

`prompts-cache.json` in the project dir, keyed by a content hash of each unit
(category + page/feature + sorted issue/symbol text + skill version). On hit,
skip the LLM call. The cache is flushed incrementally so a SIGINT mid-run leaves
a valid partial cache and a re-run regenerates only misses — mirroring
`screenshots-cache.json`. (Note the cache filename is distinct from the
user-visible `prompts.md`.)

### `internal/reporter/prompts_writer.go` (new)

`BuildPromptsBytes([]docholiday.Prompt) []byte` (pure, trivially testable) plus
`WritePrompts(dir, ...)`. Follows the `screenshots_writer.go` shape.

**Document layout:**

```markdown
# Doc Holiday Prompts

_Generated by Find the Gaps. Paste any block into Doc Holiday (https://doc.holiday)._

## Fix Stale Documentation

### Large

#### docs/api.md — Connect() signature (+2 more)
_Addresses 3 stale-doc issues on this page._

```
<LLM-authored prompt body>
```

## Document New Features

### Large

#### New page: Frobnicate
_No docs page covers this user-facing feature._

```
<LLM-authored prompt body>
```
```

- Empty priority buckets omitted; an empty category renders `_None found._`.
- Each entry: an `####` heading + a one-line italic note + the fenced prompt.
- Fence escalation: if a prompt body contains triple-backticks, the writer uses
  a longer fence so the block stays valid (`fenceFor(body)` helper).

## Wiring into `analyze`

In `internal/cli/analyze.go`, after `drift` and `undocFeatures` are available:

1. `prompts, err := docholiday.GeneratePrompts(ctx, tieredClient, docholiday.Input{Drift: drift, Undocumented: undocFeatures}, cache)`
2. `reporter.WritePrompts(projectDir, prompts)`

- **Stdout `reports:` block:** add `prompts.md (N stale · M new)`. Zero prompts
  still writes the file with empty-section markers and reports `prompts.md (0)`.
- **`ftg render`:** regenerates `prompts.md` from cached drift + mapping, reading
  `prompts-cache.json` (no fresh LLM calls if the cache is warm), consistent with
  how `render` handles the PDF.
- **Flags:** none new. Rides global `--no-cache` and `--workers`. No `--no-prompts`
  for now (YAGNI; add if requested).
- **Types:** new `internal/docholiday` package + new
  `internal/reporter/prompts_writer.go`. No changes to `analyzer.types`.

## Testing (TDD, ≥90% statement coverage)

- `internal/docholiday/unit_test.go` — page bucketing, chunk cap + part
  numbering, chunk priority rollup, one unit per undocumented feature.
- `internal/docholiday/generate_test.go` — fake `Generator` records calls:
  correct skill body per category, user message contains unit facts, typical
  tier, deterministic sort.
- `internal/docholiday/cache_test.go` — hash stability, hit skips generator,
  partial cache after interrupt, warm re-run = zero calls.
- `internal/docholiday/skills_test.go` — both `SKILL.md` parse as valid
  agent-skill frontmatter; strip leaves non-empty body; `// PROMPT:` markers
  present.
- `internal/reporter/prompts_writer_test.go` — ordering, empty-bucket omission,
  `_None found._`, fence escalation, preamble.
- `cmd/ftg/testdata/*.txtar` — `analyze` writes `prompts.md`, `reports:` line
  appears, `render` re-emits from cache.

## Verification

Add **Scenario 20: Doc Holiday Prompt Generation** to
`.plans/VERIFICATION_PLAN.md`: default-on emission, cache hit on re-run, SIGINT
resume, parallel/serial byte-identity, and that prompts reference only real
pages/features.
