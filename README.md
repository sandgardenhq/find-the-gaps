<p align="center">
  <img src="assets/find-the-gaps.png" alt="Find the Gaps" width="640">
</p>

# Find the Gaps

A CLI tool that analyzes a codebase alongside its documentation site to identify outdated or missing documentation.

## Why

Project maintainers know their docs rot. It persists not because the problem is hard, but because it's the fourth most important problem on a list where they only have bandwidth for the top three. Link checkers catch broken URLs. Spell checkers catch typos. Neither can tell you that the function signature in `README.md` no longer matches the code, or that a new public API shipped last month without a single page describing it.

Find the Gaps closes that gap.

## Supported languages

Find the Gaps uses [tree-sitter](https://github.com/smacker/go-tree-sitter) to extract symbols (functions, types, exports) from these languages:

| Language | Extensions |
| --- | --- |
| Go | `.go` |
| Python | `.py`, `.pyw` |
| TypeScript / JavaScript | `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs` |
| Rust | `.rs` |

Unrecognized text files are still scanned as plain text so they can be cross-referenced against docs, but no symbols are extracted from them. Binary files (images, archives, fonts, audio, compiled libraries, etc.) are skipped entirely.

## What this installs

Find the Gaps shells out to one runtime dependency that must be on your `$PATH`:

- [`mdfetch`](https://www.npmjs.com/package/@sandgarden/mdfetch) — downloads a documentation site as markdown

Run `ftg doctor` at any time to check that it is available and see its detected version.

## Install

```sh
go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest
```

Or build from source:

```sh
git clone https://github.com/sandgardenhq/find-the-gaps.git
cd find-the-gaps
make build   # produces ./ftg
```

Then install the required external tools:

```sh
ftg install-deps
```

## Usage

```
ftg analyzes a codebase alongside its documentation site to identify outdated or missing documentation.

Usage:
  ftg [command]

Available Commands:
  analyze      Analyze a codebase against its documentation site for gaps.
  completion   Generate the autocompletion script for the specified shell
  doctor       Check that the required external tool (mdfetch) is installed.
  help         Help about any command
  install-deps Install the required external tool (mdfetch).

Flags:
  -h, --help      help for ftg
  -v, --verbose   show debug logs
      --version   version for ftg

Use "ftg [command] --help" for more information about a command.
```

### analyze

```
Analyze a codebase against its documentation site for gaps.

Usage:
  ftg analyze [flags]

Flags:
      --cache-dir string     base directory for all cached results (default ".find-the-gaps")
      --docs-url string      URL of the documentation site to analyze
  -h, --help                 help for analyze
      --llm-large string     large-tier model as "provider/model" (default: anthropic/claude-opus-4-7)
      --llm-small string     small-tier model as "provider/model" (default: anthropic/claude-haiku-4-5)
      --llm-typical string   typical-tier model as "provider/model" (default: anthropic/claude-sonnet-4-6)
      --no-cache             force full re-scan, ignoring any cached results
      --no-symbols           map features to files only, skipping symbol-level analysis
      --repo string          path to the repository to analyze (default ".")
      --workers int          number of parallel mdfetch workers (default 5)

Global Flags:
  -v, --verbose   show debug logs
```

#### LLM tier configuration

Find the Gaps routes LLM work across three reasoning tiers so cheap, high-volume
calls land on cheaper models while the hardest calls use a frontier model:

| Tier      | Used for                                            | Default                         |
|-----------|-----------------------------------------------------|---------------------------------|
| `small`   | Per-page doc summaries and release-note classifier  | `anthropic/claude-haiku-4-5`    |
| `typical` | Extracting features from code                       | `anthropic/claude-sonnet-4-6`   |
| `large`   | Feature-to-code mapping and agentic drift detection | `anthropic/claude-opus-4-7`     |

Each tier accepts a combined `provider/model` string. Bare model names default
to the `anthropic` provider. The `large` tier must name a provider that supports
tool use (currently `anthropic` or `openai`) — the CLI refuses to start otherwise.

Configure tiers via flag, environment variable, or TOML:

```toml
[llm]
small   = "anthropic/claude-haiku-4-5"
typical = "anthropic/claude-sonnet-4-6"
large   = "anthropic/claude-opus-4-7"
```

Environment variables:

- `FIND_THE_GAPS_LLM_SMALL`
- `FIND_THE_GAPS_LLM_TYPICAL`
- `FIND_THE_GAPS_LLM_LARGE`
- `ANTHROPIC_API_KEY` — required when any tier points at an Anthropic model
- `OPENAI_API_KEY` — required when any tier points at an OpenAI model
- `OLLAMA_BASE_URL` — overrides the default Ollama endpoint

> **Breaking change.** The old `--llm-provider`, `--llm-model`, and
> `--llm-base-url` flags were removed. Replace `--llm-provider X --llm-model Y`
> with `--llm-typical X/Y` (or the tier that matches your use case).

### doctor

```
Check that the required external tool (mdfetch) is installed.

Usage:
  ftg doctor [flags]

Flags:
  -h, --help   help for doctor

Global Flags:
  -v, --verbose   show debug logs
```

### install-deps

```
Install mdfetch if it is not already on $PATH. An already-present tool is skipped.

Usage:
  ftg install-deps [flags]

Flags:
  -h, --help   help for install-deps

Global Flags:
  -v, --verbose   show debug logs
```

## Output

`ftg analyze` writes two reports to `.find-the-gaps/<project>/`:

- **`gaps.md`** — documentation issues in three sections:
  - *Undocumented Code* — features implemented in code but absent from docs
  - *Unmapped Features* — features mentioned in docs with no matching code
  - *Stale Documentation* — specific inaccuracies in pages that do cover a feature
- **`mapping.md`** — full feature inventory with documentation status, implementing files, and symbols

## Development

See [CLAUDE.md](CLAUDE.md) for project conventions, tech stack, and TDD rules. See [.plans/VERIFICATION_PLAN.md](.plans/VERIFICATION_PLAN.md) for acceptance testing procedures.

## License

[MIT](LICENSE) © Sandgarden, Inc.
