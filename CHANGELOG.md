# Changelog

## Unreleased

### Changed (breaking)
- Removed `--llm-provider`, `--llm-model`, and `--llm-base-url` flags.
- Introduced `--llm-small`, `--llm-typical`, `--llm-large` with combined
  `provider/model` syntax (e.g. `anthropic/claude-opus-4-7`). Bare model names
  default to the `anthropic` provider. Each tier is configurable independently
  via CLI flag, `[llm]` section in TOML config, or `FIND_THE_GAPS_LLM_*` env var.
- Migration: replace `--llm-provider X --llm-model Y` with
  `--llm-typical X/Y` (or the tier that matches your use case).

### Added
- Per-tier client construction with eager startup validation; unknown providers
  or non-tool-use `large` tiers now fail fast.
