# Changelog

## Unreleased

### Changed
- Drift detection's tool-use requirement moved from the `large` tier to the
  `typical` tier. The drift investigator now runs as a tool-use agent on the
  typical tier (Sonnet by default); the large tier only makes a single
  non-tool `CompleteJSON` call (the drift judge). As a result, `--llm-large`
  may now name any supported provider (e.g. `ollama/...`), and `--llm-typical`
  must name a provider that supports tool use (currently `anthropic` or
  `openai`).

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
