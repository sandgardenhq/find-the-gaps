<p align="center">
  <img src="assets/find-the-gaps.png" alt="Find the Gaps" width="640">
</p>

# Find the Gaps

A CLI tool that analyzes a codebase alongside its documentation site to identify outdated or missing documentation.

## Why

Project maintainers know their docs rot. It persists not because the problem is hard, but because it's the fourth most important problem on a list where they only have bandwidth for the top three. Link checkers catch broken URLs. Spell checkers catch typos. Neither can tell you that the function signature in `README.md` no longer matches the code, or that a new public API shipped last month without a single page describing it.

Find the Gaps closes that gap.

## What this installs

Find the Gaps shells out to two runtime dependencies that must be on your `$PATH`:

- [`ripgrep`](https://github.com/BurntSushi/ripgrep) — fast codebase searching
- [`mdfetch`](https://www.npmjs.com/package/@sandgarden/mdfetch) — downloads a documentation site as markdown

Run `ftg doctor` at any time to check that both are available and see their detected versions.

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

## Usage

```
ftg analyzes a codebase alongside its documentation site to identify outdated or missing documentation.

Usage:
  ftg [command]

Available Commands:
  analyze     Analyze a codebase against its documentation site for gaps.
  completion  Generate the autocompletion script for the specified shell
  doctor      Check that required external tools (ripgrep, mdfetch) are installed.
  help        Help about any command

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
      --cache-dir string      base directory for all cached results (default ".find-the-gaps")
      --docs-url string       URL of the documentation site to analyze
  -h, --help                  help for analyze
      --llm-base-url string   base URL for local providers (required for openai-compatible; default: provider-specific)
      --llm-model string      model name (default varies by provider; e.g. llama3 for ollama)
      --llm-provider string   LLM provider: anthropic | openai | ollama | lmstudio | openai-compatible (default "anthropic")
      --no-cache              force full re-scan, ignoring any cached results
      --no-symbols            map features to files only, skipping symbol-level analysis
      --repo string           path to the repository to analyze (default ".")
      --workers int           number of parallel mdfetch workers (default 5)

Global Flags:
  -v, --verbose   show debug logs
```

### doctor

```
Check that required external tools (ripgrep, mdfetch) are installed.

Usage:
  ftg doctor [flags]

Flags:
  -h, --help   help for doctor

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
