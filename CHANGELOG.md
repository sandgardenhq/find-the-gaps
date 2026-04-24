# Changelog

## Unreleased

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
