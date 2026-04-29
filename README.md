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
| Java | `.java` |
| C# | `.cs` |
| Kotlin | `.kt`, `.kts` |
| Swift | `.swift` |
| Scala | `.scala`, `.sc` |
| PHP | `.php` |
| Ruby | `.rb` |
| C | `.c`, `.h` |
| C++ | `.cc`, `.cpp`, `.cxx`, `.hh`, `.hpp`, `.hxx` |

Unrecognized text files are still scanned as plain text so they can be cross-referenced against docs, but no symbols are extracted from them. Binary files (images, archives, fonts, audio, compiled libraries, etc.) are skipped entirely.

## What this installs

Find the Gaps shells out to runtime dependencies that must be on your `$PATH`:

- [`mdfetch`](https://www.npmjs.com/package/@sandgarden/mdfetch) — downloads a documentation site as markdown
- [`hugo`](https://gohugo.io) — static site generator used to render the analyze report as a browsable Hextra-themed site

Run `ftg doctor` at any time to check that they are available and see their detected versions.

## Install

### Homebrew (macOS and Linux)

```sh
brew install sandgardenhq/tap/find-the-gaps
```

The formula installs the `ftg` binary, pulls in `node` as a dependency, and runs `ftg install-deps` during post-install to fetch `mdfetch` from npm. Run `ftg doctor` after install to confirm everything is wired up.

### Other platforms

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
      --repo string              path to the repository to analyze (default ".")
      --skip-screenshot-check    skip the missing-screenshot detection pass
      --workers int              number of parallel mdfetch workers (default 5)

Global Flags:
  -v, --verbose   show debug logs
```

#### LLM tier configuration

Find the Gaps routes LLM work across three reasoning tiers so cheap, high-volume
calls land on cheaper models while the hardest calls use a frontier model:

| Tier      | Used for                                                       | Default                         |
|-----------|----------------------------------------------------------------|---------------------------------|
| `small`   | Per-page doc summaries and release-note classifier             | `anthropic/claude-haiku-4-5`    |
| `typical` | Feature extraction and the drift investigator (tool-use agent) | `anthropic/claude-sonnet-4-6`   |
| `large`   | Feature-to-code mapping and the drift judge (single JSON call) | `anthropic/claude-opus-4-7`     |

Each tier accepts a combined `provider/model` string. Bare model names default
to the `anthropic` provider. The `typical` tier must name a provider that
supports tool use (currently `anthropic` or `openai`) because it runs the drift
investigator's tool-use loop — the CLI refuses to start otherwise. The `large`
tier may use any supported provider; it only makes single non-tool calls.

Configure tiers via flag or environment variable:

- `FIND_THE_GAPS_LLM_SMALL`
- `FIND_THE_GAPS_LLM_TYPICAL`
- `FIND_THE_GAPS_LLM_LARGE`
- `ANTHROPIC_API_KEY` — required when any tier points at an Anthropic model
- `OPENAI_API_KEY` — required when any tier points at an OpenAI model
- `OLLAMA_BASE_URL` — overrides the default Ollama endpoint (`http://localhost:11434`)
- `LMSTUDIO_BASE_URL` — overrides the default LM Studio endpoint (`http://localhost:1234`)

> **Breaking change.** The old `--llm-provider`, `--llm-model`, and
> `--llm-base-url` flags were removed. Replace `--llm-provider X --llm-model Y`
> with `--llm-typical X/Y` (or the tier that matches your use case). Base URLs
> for `ollama` and `lmstudio` are now set via the `*_BASE_URL` env vars listed
> above.

### serve

Browse the rendered Hextra report locally:

```sh
ftg serve --repo ./myrepo
```

Boots a static HTTP server against `<cache-dir>/<repo>/site/` (default `.find-the-gaps/<repo>/site/`). The site is the same one `ftg analyze` produces — no Hugo runtime needed.

```
Serve the find-the-gaps report site over HTTP.

Usage:
  ftg serve [flags]

Flags:
      --addr string        bind address for the local server (host:port; use 127.0.0.1:0 to pick a free port) (default "127.0.0.1:8080")
      --cache-dir string   base directory containing analyze output (default ".find-the-gaps")
  -h, --help               help for serve
      --open               open the served URL in the default browser after the server is up
      --repo string        path to the repository whose report should be served (default ".")

Global Flags:
  -v, --verbose   show debug logs
```

Pass `--open` to launch the report in your default browser. Pass `--addr 127.0.0.1:0` to grab a random free port (a bare `:0` would bind to every interface); `serve` always prints the URL it bound to. `serve` exits with a clear message and non-zero status when no rendered site exists yet — run `ftg analyze` first.

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

`ftg analyze` writes these reports to `.find-the-gaps/<project>/`:

- **`gaps.md`** — documentation issues in three sections:
  - *Undocumented Code* — features implemented in code but absent from docs
  - *Unmapped Features* — features mentioned in docs with no matching code
  - *Stale Documentation* — specific inaccuracies in pages that do cover a feature
- **`screenshots.md`** — passages describing user-facing moments with no nearby screenshot. Written whenever the screenshot pass runs (zero findings produces a `_None found._` body). Not written when `--skip-screenshot-check` is passed.
- **`mapping.md`** — full feature inventory with documentation status, implementing files, and symbols

## Ignored files

Find the Gaps ships a curated list of files it never analyses — lockfiles,
build artifacts, generated bindings, binary assets, test files, and the usual
VCS / IDE noise. The full list is
[`internal/scanner/ignore/defaults.ftgignore`](internal/scanner/ignore/defaults.ftgignore).
The repo's own `.gitignore` is layered on top, so anything you tell git to
ignore is also skipped by `ftg`.

Override either layer with a `.ftgignore` at the repo root. It uses gitignore
syntax, including `!` to re-include something an earlier layer skipped:

```gitignore
# .ftgignore
!vendor/
!*_test.go
custom_build_dir/
```

The scan summary printed by `ftg analyze` shows how many files each layer
skipped:

```
scanned 412 files, skipped 1,847 (defaults: 1,801, .gitignore: 38, .ftgignore: 8)
```

## Use as a GitHub Action

Find the Gaps ships as a composite GitHub Action so maintainers can run audits
on a schedule, before a release, or on demand — without installing anything
locally.

### Quickstart

```yaml
- uses: actions/checkout@v6
- uses: sandgardenhq/find-the-gaps@v1
  with:
    docs-url: https://docs.example.com
    anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

### Inputs

| Name | Required | Default | Description |
|---|---|---|---|
| `docs-url` | yes | — | URL of the live documentation site |
| `anthropic-api-key` | yes | — | Anthropic API key (use a repo secret) |
| `create-issue` | no | `true` | When `true`, open or update a single tracking issue (label: `find-the-gaps`) |
| `skip-screenshot-check` | no | `false` | Skip screenshot-gap detection |

### Outputs

- **Artifact** (`find-the-gaps-report-<run_id>`): contains `gaps.md` and `screenshots.md`. Always uploaded.
- **Issue** (when `create-issue=true`): a single open issue labeled `find-the-gaps` is created or updated.
  - Closed issues are never reopened.
  - Empty findings: a comment is posted, the issue is not auto-closed.

### Permissions

```yaml
permissions:
  contents: read     # for actions/checkout
  issues: write      # only when create-issue=true
```

### Runner support

Linux x86_64 only (`runs-on: ubuntu-latest`). The action exits with an error on macOS or Windows runners.

### Examples

See [`docs/examples/`](docs/examples/) for ready-to-copy workflows: schedule, release, manual.

## Development

See [CLAUDE.md](CLAUDE.md) for project conventions, tech stack, and TDD rules. See [.plans/VERIFICATION_PLAN.md](.plans/VERIFICATION_PLAN.md) for acceptance testing procedures.

## License

[MIT](LICENSE) © Sandgarden, Inc.
