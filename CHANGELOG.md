# Changelog

## v0.4.0

### Added
- **Per-model capability registry.** Replaces the flat provider whitelist with
  a `(provider, model) -> {tool_use, vision}` table. Tier validation and the
  screenshot pipeline both consume it, so adding a new model is a one-row
  change. Self-hosted providers (`ollama`, `lmstudio`) match a wildcard row
  with capabilities defaulted to off.
- **Groq provider.** New `--llm-*=groq/<model>` syntax routed through
  Bifrost's OpenAI-compat endpoint at `https://api.groq.com/openai`. Reads
  `GROQ_API_KEY` (required) and `GROQ_BASE_URL` (optional override).
- **Vision-aware screenshot analysis.** When `--llm-small` resolves to a
  vision-capable model, the screenshot pass adds an image-relevance check
  (does each `<img>` actually depict what the prose claims?) and uses the
  result to suppress missing-screenshot suggestions where an existing image
  already covers the moment. Auto-engages — no flag.
- **`## Image Issues` section in `screenshots.md`.** Lists images whose
  surrounding prose doesn't match what they show, with the page URL, image
  src, and the model's reasoning. Populated when the small tier is
  vision-capable; absent otherwise. The Hugo site picks it up automatically.
- **Per-page screenshot audit log.** New `log.Infof` line per page —
  `page=<url> vision=on/off relevance_batches=N images_seen=N
  image_issues=N missing_screenshots=N possibly_covered=N
  detection_skipped=true|false` — for run-time observability.
- **`ftg doctor` capability output.** Prints the resolved tier model and its
  capabilities (`tool_use`, `vision`) so users can tell at a glance whether
  vision will engage on a given configuration.

### Changed
- **BREAKING:** missing-screenshot detection is now off by default and
  marked experimental. Pass `--experimental-check-screenshots` (CLI) or
  set `experimental-check-screenshots: 'true'` (Action) to opt in.
- **BREAKING:** removed `--skip-screenshot-check` flag and the
  `skip-screenshot-check` Action input. Workflows passing the old input
  will see a GitHub Actions "Unexpected input" warning until updated.
- **Site materialization refactor.** `internal/site` now reads
  `screenshots.md` from the reporter instead of re-rendering from typed
  structs (matching the existing `gaps.md` pattern). Net negative line count
  and the `## Image Issues` section flows through to the Hugo site without
  any per-section template.
- **OpenAI default tiers refreshed to the 2026 lineup.** When only
  `OPENAI_API_KEY` is set, `tierFallbacks()` now resolves to
  `openai/gpt-5.4-nano` (small), `openai/gpt-5.4-mini` (typical), and
  `openai/gpt-5.5` (large) — all vision- and tool-use-capable. The capability
  registry gained rows for `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`, and
  `gpt-5.4-nano`; legacy `gpt-5`/`gpt-5-mini`/`gpt-4o`/`gpt-4o-mini` rows
  remain so existing configs keep working.

### Fixed
- **`ftg doctor` reported the wrong tier defaults to OpenAI-only users.**
  `printTierCapabilities` now calls `tierFallbacks()` instead of the static
  Anthropic constants, so the output matches what the next `ftg analyze`
  actually resolves to (including the OpenAI flip when only `OPENAI_API_KEY`
  is set).
- **Vision `## Image Issues` now reaches the rendered site in expanded mode.**
  Previously, only mirror mode picked up the section (it reads `screenshots.md`
  verbatim). Expanded mode rendered the screenshots section from typed inputs
  with no path for image issues, so the README/CHANGELOG promise that vision
  findings appear on the rendered Hugo page only held in mirror mode.
  `site.Inputs` gained an `ImageIssues` field; `materializeExpanded` renders
  the section into `content/screenshots/_index.md`.

## v0.2.0

### Added
- **Hugo-rendered report site.** `ftg analyze` now renders a Hextra-themed
  static site at `<projectDir>/site/` alongside the markdown reports (#21).
  Toggle with `--no-site`; choose layout via `--site-mode=mirror|expanded`;
  preserve the generated Hugo source with `--keep-site-source`.
- **`ftg serve`.** Local HTTP server for the rendered report site (#26),
  with an interactive picker when the cache contains multiple projects (#32)
  and an `--open` flag to launch the default browser.
- **GitHub Action.** Find the Gaps now ships as a composite GitHub Action so
  maintainers can run audits on a schedule, before a release, or on demand
  without installing anything locally (#20). The action uploads a report
  artifact and (optionally) opens or updates a tracking issue.
- **Default ignore list.** A curated `defaults.ftgignore` ships in the binary
  and is layered with the repo's `.gitignore` and an optional project-level
  `.ftgignore` (#30). The scan summary in `ftg analyze` reports per-layer
  skip counts.
- **Drift-detection cache.** Analyze re-runs with unchanged inputs skip the
  drift pass and reuse the previous `gaps.md` (#34, #37). Bust with
  `--no-cache` or by deleting the report files.
- **Docs-page classifier.** A binary `is_docs` classifier now filters drift
  and screenshot detection to canonical documentation URLs, keeping
  blog/team/careers pages out of the report (#38).
- **Per-tier LLM call counter.** `-v` now prints a per-tier summary of LLM
  calls and tokens used during analyze (#29).
- **Prompt caching on Anthropic.** Bifrost prompt-caching breakpoints are
  wired through analyze flows — the user-prompt cache, drift investigator
  prompt, and a rotating breakpoint on the latest agent-loop message all
  emit `cache_control` blocks. Cache usage is logged on every LLM call (#24).
- **Drift detection split into investigator + judge.** A tool-use agent on
  the typical tier (Sonnet) gathers evidence, then a single non-tool call on
  the large tier (Opus) renders a verdict (#23). Includes a dynamic
  turn-budget cap (#22).
- **mdfetch upgrade behavior.** `ftg install-deps` now always reinstalls
  mdfetch (`npm install -g @sandgarden/mdfetch@latest`) so re-running the
  command picks up new releases (#40). hugo keeps skip-when-present.
- **`--wrap-images`.** Spider invocations of mdfetch now pass
  `--wrap-images` so screenshot detection can find images in the markdown
  output (#40).

### Changed
- **Drift tier requirements (mildly breaking).** `--llm-large` may now name
  any supported provider (e.g. `ollama/...`); `--llm-typical` must name a
  tool-use-capable provider (currently `anthropic` or `openai`). Configs that
  pointed `--llm-large` at a tool-use-only provider can be relaxed; configs
  that pointed `--llm-typical` at a non-tool-use provider must be updated
  (#23).
- Screenshot-gap prompt tightened to reduce over-flagging (#31).
- Hugo output formatting cleaned up (#25).

### Fixed
- Spider Index now guarded by a mutex so concurrent crawler workers cannot
  trigger a `concurrent map writes` panic (#28).
- `ftg analyze --repo` now resolves to an absolute path before deriving the
  project name, so relative paths produce stable cache directories (#27).
- Screenshot-gap prompt capped to fit Claude's 200K input window (#35) and
  later tightened further to absorb tokenizer drift (#36).

### Build / CI
- Release build-job actions bumped to Node 24 majors (#19).

## v0.1.1

### Added
- Homebrew install path. `brew install sandgardenhq/tap/find-the-gaps`
  installs the `ftg` binary, pulls in `node` as a dependency, and runs
  `ftg install-deps` during post-install to fetch `mdfetch` from npm.
  Works on macOS and Linux. The release workflow now renders the formula
  from `.github/homebrew/find-the-gaps.rb.tmpl` on each tag and pushes it
  to the `sandgardenhq/homebrew-tap` repo.

## v0.1.0

### Changed (breaking)
- Removed `--llm-provider`, `--llm-model`, and `--llm-base-url` flags.
- Introduced `--llm-small`, `--llm-typical`, `--llm-large` with combined
  `provider/model` syntax (e.g. `anthropic/claude-opus-4-7`). Bare model names
  default to the `anthropic` provider. Each tier is configurable via CLI flag
  or the corresponding `FIND_THE_GAPS_LLM_SMALL` / `_TYPICAL` / `_LARGE` env var.
- Base URLs for local providers moved from `--llm-base-url` to provider-specific
  env vars: `OLLAMA_BASE_URL` and `LMSTUDIO_BASE_URL`.
- Migration: replace `--llm-provider X --llm-model Y` with
  `--llm-typical X/Y` (or the tier that matches your use case), and move any
  `--llm-base-url` value into the matching `*_BASE_URL` env var.

### Added
- Per-tier client construction with eager startup validation; unknown providers
  or non-tool-use `large` tiers now fail fast.
