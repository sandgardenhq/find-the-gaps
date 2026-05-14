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

The formula installs the `ftg` binary and pulls in `hugo` and `sandgardenhq/tap/mdfetch` as brew dependencies (the `mdfetch` formula in the same tap wraps the `@sandgarden/mdfetch` npm package). Run `ftg doctor` after install to confirm everything is wired up.

### Other platforms

```sh
go install github.com/sandgardenhq/find-the-gaps/cmd/ftg@latest
```

`go install` only installs the `ftg` binary. `analyze` and `render` also need `mdfetch` and `hugo` on your `$PATH`:

```sh
npm install -g @sandgarden/mdfetch@latest
```

For `hugo`, grab a binary from the [Hugo releases page](https://github.com/gohugoio/hugo/releases) (or use your distro's package manager).

Run `ftg doctor` to confirm. If `mdfetch` or `hugo` is missing, the offending command will refuse to run and tell you exactly what to install.

Or build from source — `make build` produces `./ftg` directly:

```sh
git clone https://github.com/sandgardenhq/find-the-gaps.git
cd find-the-gaps
make build
```

Then install the required external tools:

```sh
brew install sandgardenhq/tap/mdfetch  # or: npm install -g @sandgarden/mdfetch@latest
brew install hugo                      # or see https://github.com/gohugoio/hugo/releases
```

## Quick start

Once `ftg` is installed and `ftg doctor` exits `0`, point it at a local checkout. If your docs live in the repo (the common case), that's all you need:

```sh
git clone https://github.com/spf13/cobra
export ANTHROPIC_API_KEY=sk-ant-...

ftg analyze --repo ./cobra
```

If your docs live on a separate site, pass `--docs` with the URL:

```sh
ftg analyze --repo ./cobra --docs https://cobra.dev
```

`--docs` also accepts a local directory if your docs live somewhere else on disk (e.g., a sibling repo): `ftg analyze --repo ./cobra --docs ./cobra-website/content`.

When `--docs` is omitted (or is a local path), `ftg` reads markdown directly from disk. When `--docs` is an `http(s)` URL, `ftg` ingests the live site with `mdfetch`. Either way, it scans the repo for features and symbols, then uses the LLM tiers to map docs↔code and flag drift. The first run takes a few minutes; later runs reuse the cache under `.find-the-gaps/<project>/` (pass `--no-cache` to force a full re-scan).

Reports land at `.find-the-gaps/<project>/`:

- `gaps.md` — undocumented code, unmapped features, stale docs
- `mapping.md` — full feature → file/symbol inventory
- `site/` — browsable Hextra-themed report

Open the rendered report locally:

```sh
ftg serve --repo ./cobra --open
```

## Usage

```
ftg analyzes a codebase alongside its documentation site to identify outdated or missing documentation.

Usage:
  ftg [command]

Available Commands:
  analyze     Analyze a codebase against its documentation site for gaps.
  completion  Generate the autocompletion script for the specified shell
  doctor      Check that the required external tools (mdfetch, hugo) are installed.
  help        Help about any command
  serve       Serve the find-the-gaps report site over HTTP.

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
      --cache-dir string                 base directory for all cached results (default ".find-the-gaps")
      --docs string                      URL or local path of docs to analyze (default: scan --repo on disk)
      --experimental-check-screenshots   enable experimental missing-screenshot detection pass
  -h, --help                             help for analyze
      --keep-site-source                 preserve generated Hugo source at <projectDir>/site-src/ (default true; pass --keep-site-source=false to discard) (default true)
      --llm-large string                 large-tier model as "provider/model" (default: anthropic/claude-opus-4-7)
      --llm-small string                 small-tier model as "provider/model" (default: anthropic/claude-haiku-4-5)
      --llm-typical string               typical-tier model as "provider/model" (default: anthropic/claude-sonnet-4-6)
      --no-cache                         force full re-scan, ignoring any cached results
      --no-site                          skip the Hugo site build; markdown reports still emitted
      --no-symbols                       map features to files only, skipping symbol-level analysis
      --repo string                      path to the repository to analyze (default ".")
      --site-mode string                 site content shape: "mirror" or "expanded" (default "mirror")
      --workers int                      number of parallel mdfetch workers (default 5)

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
supports tool use (currently `anthropic`, `openai`, or `groq`) because it runs the drift
investigator's tool-use loop — the CLI refuses to start otherwise. The `large`
tier may use any supported provider; it only makes single non-tool calls.

If `OPENAI_API_KEY` is set and `ANTHROPIC_API_KEY` is not, the tier defaults
flip to OpenAI (`openai/gpt-5.4-nano`, `openai/gpt-5.4-mini`, `openai/gpt-5.5`)
so OpenAI-only users can run `ftg analyze` without spelling out three
`--llm-*` flags. Any explicit tier flag still wins. With both keys set,
Anthropic defaults stand.

Configure tiers via flag or environment variable:

- `FIND_THE_GAPS_LLM_SMALL`
- `FIND_THE_GAPS_LLM_TYPICAL`
- `FIND_THE_GAPS_LLM_LARGE`
- `ANTHROPIC_API_KEY` — required when any tier points at an Anthropic model
- `OPENAI_API_KEY` — required when any tier points at an OpenAI model
- `GROQ_API_KEY` — required when any tier points at a Groq model
- `GROQ_BASE_URL` — overrides the default Groq endpoint (`https://api.groq.com/openai`); optional
- `OLLAMA_BASE_URL` — overrides the default Ollama endpoint (`http://localhost:11434`)
- `LMSTUDIO_BASE_URL` — overrides the default LM Studio endpoint (`http://localhost:1234`)

#### Documentation living on disk

For most projects the docs live alongside the code. Run `ftg analyze` with
no `--docs` and it walks `--repo` for markdown, treating those files as the
documentation set:

```sh
ftg analyze --repo .
```

If your docs live in a sibling directory (or anywhere else on the local
filesystem), pass that path as `--docs`:

```sh
ftg analyze --repo ./my-app --docs ./my-app-website/content
```

Recognized documentation extensions: `.md`, `.markdown`, `.mdx`, `.rst`,
`.adoc`, `.asciidoc`. Files under `.git/`, `node_modules/`, and `vendor/`
are skipped.

`--docs` accepts a path or an `http(s)` URL. Anything not starting with
`http://` or `https://` is treated as a local filesystem path; if the path
doesn't exist, `ftg analyze` errors out before doing any work. URLs go
through `mdfetch`.

#### Documentation hosted on a source-control forge

Find the Gaps does not crawl source-control forges — the link graph there is
the entire forge, not your docs. When `--docs` points at github.com,
gitlab.com, bitbucket.org, codeberg.org, or git.sr.ht, the tool reads
markdown directly from `--repo` on disk:

```sh
ftg analyze --repo . --docs https://github.com/sandgardenhq/find-the-gaps
```

The match is verified against the local repo's `origin` remote. If `origin`
points at a different repo (or `--repo` isn't a git checkout), `ftg analyze`
halts with a message asking you to clone the docs repo locally and re-run.
Wiki URLs (`/owner/repo/wiki`) also halt — clone `<repo>.wiki.git` and pass
that as `--repo` for a wiki-only analysis.

For self-hosted forges (Gitea, Forgejo, Gogs, GitLab CE, Bitbucket Server)
on custom domains, host detection can't engage automatically. Pass the
checkout as `--docs` directly:

```sh
ftg analyze --repo ./my-clone --docs ./my-clone
```

#### Vision-aware screenshot analysis

When `--llm-small` resolves to a vision-capable model, the screenshot pass
auto-engages an extra image-relevance check: every `<img>` it pulls in from a
docs page is shown to the model alongside the surrounding prose so the model
can flag images that don't actually depict what the prose claims. The result
also feeds the missing-screenshot prompt, suppressing suggestions for moments
where an existing image already covers the ground. There is no flag — capable
small-tier model in, image issues out.

Vision-capable small-tier models today:

| Provider | Models |
|---|---|
| `anthropic` | `claude-haiku-4-5`, `claude-sonnet-4-6`, `claude-opus-4-7` |
| `openai` | `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.4-nano`, `gpt-5`, `gpt-5-mini`, `gpt-4o`, `gpt-4o-mini` |
| `groq` | `meta-llama/llama-4-scout-17b-16e-instruct` |

Run `ftg doctor` to see what your current configuration resolves to — it
prints each tier's model and capability flags (`tool_use`, `vision`).

Groq is a hosted API, so there is nothing extra to install — set
`GROQ_API_KEY` and you're done. Groq's vision endpoint caps each request at
five images, which the screenshot pass handles transparently by batching.

Findings land in a new `## Image Issues` section in `screenshots.md`,
appended after the missing-screenshots list (and rendered the same way on
the Hugo page). Per-page audit log lines document the counts: `vision=on/off
relevance_batches=N images_seen=N image_issues=N missing_screenshots=N
possibly_covered=N detection_skipped=true|false`.

> **Breaking change.** The old `--llm-provider`, `--llm-model`, and
> `--llm-base-url` flags were removed. Replace `--llm-provider X --llm-model Y`
> with `--llm-typical X/Y` (or the tier that matches your use case).

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
      --project string     name of an analyzed project under <cache-dir>/; bypasses the picker
      --repo string        path to the repository whose report should be served (default ".")

Global Flags:
  -v, --verbose   show debug logs
```

Pass `--open` to launch the report in your default browser. Pass `--addr 127.0.0.1:0` to grab a random free port (a bare `:0` would bind to every interface); `serve` always prints the URL it bound to. `serve` exits with a clear message and non-zero status when no rendered site exists yet — run `ftg analyze` first.

### doctor

```
Check that the required external tools (mdfetch, hugo) are installed.

Usage:
  ftg doctor [flags]

Flags:
  -h, --help   help for doctor

Global Flags:
  -v, --verbose   show debug logs
```

## Output

`ftg analyze` writes these reports to `.find-the-gaps/<project>/`:

- **`gaps.md`** — documentation issues in three sections:
  - *Undocumented Code* — features implemented in code but absent from docs
  - *Unmapped Features* — features mentioned in docs with no matching code
  - *Stale Documentation* — specific inaccuracies in pages that do cover a feature
- **`screenshots.md`** — passages describing user-facing moments with no nearby screenshot, plus a `## Image Issues` section listing existing images whose surrounding prose doesn't match what they show (populated when the small tier resolves to a vision-capable model). The detection pass is **experimental and off by default**; pass `--experimental-check-screenshots` to opt in. When the pass runs, this file is written even on zero findings (body is `_None found._`); when the pass is off, the file is not written.
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

Set `FIND_THE_GAPS_QUIET=1` to suppress this summary line — handy in CI logs.

## Use as a GitHub Action

Find the Gaps ships as a composite GitHub Action so maintainers can run audits
on a schedule, before a release, or on demand — without installing anything
locally.

### Quickstart

```yaml
- uses: actions/checkout@v6
- uses: sandgardenhq/find-the-gaps@v1
  with:
    docs: https://docs.example.com
    anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

### Inputs

| Name | Required | Default | Description |
|---|---|---|---|
| `docs` | no | — | URL or local path of docs to analyze. Omit to scan the checked-out repo on disk. |
| `anthropic-api-key` | yes | — | Anthropic API key (use a repo secret) |
| `create-issue` | no | `true` | When `true`, open or update a single tracking issue (label: `find-the-gaps`) |
| `experimental-check-screenshots` | no | `false` | Run the experimental missing-screenshot detection pass |

The action runs `ftg analyze` with the CLI's default LLM tiers (Anthropic Haiku/Sonnet/Opus). Tier customization isn't exposed through inputs — to swap models or providers, run the CLI directly.

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

## Running on Fly.io

Find the Gaps ships a `fly.toml` and `Dockerfile` so you can run analysis as a one-shot job on [Fly.io](https://fly.io). Each invocation spins up a throwaway Machine, clones the target repo, runs `ftg analyze --experimental-check-screenshots`, uploads the report artifacts as a tarball to Fly Storage, and prints a presigned download URL.

### Prerequisites

- A [Fly.io](https://fly.io) account.
- The `flyctl` CLI installed and authenticated (`fly auth login`).

You do **not** need to clone this repo. The published image at `ghcr.io/sandgardenhq/find-the-gaps:latest` is public and runs everything that `fly machine run` needs.

### One-time setup

Create your Fly app from the public image, then provision a Fly Storage (Tigris) bucket. The bucket step sets `BUCKET_NAME`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_ENDPOINT_URL_S3` as app secrets, which `run-job.sh` reads on every invocation.

```sh
fly launch \
  --image ghcr.io/sandgardenhq/find-the-gaps:latest \
  --no-deploy \
  --copy-config=false \
  --name <your-app>

fly storage create --app <your-app>
```

Pick any app name you like — your Fly org pays for the Machines and owns the Tigris bucket. Accept the default region (or pass `--region <code>`); analyses can target any region at run time. Accept the bucket defaults when prompted.

### Running a job

Use `fly machine run` to launch a throwaway Machine for a single analysis, pulling the image directly from GHCR. Two positional arguments after `--` are required: the repository URL and the docs URL.

```sh
fly machine run ghcr.io/sandgardenhq/find-the-gaps:latest \
  --app <your-app> \
  --rm \
  --region ord \
  --vm-cpus 4 \
  --vm-memory 8192 \
  -- \
  https://github.com/owner/repo \
  https://owner.example.com/docs
```

`--rm` deletes the Machine on exit so stopped Machines don't accumulate. Pick a region close to the docs source for faster ingestion. The Machine inherits your app's secrets, so the report tarball lands in your Tigris bucket.

### Retrieving the report

The presigned download URL is the **last line of stdout**.

- **Foreground run** (logs stream to your terminal): the URL appears at the end of the output. `curl -L -o report.tar.gz "<url>"` downloads the tarball, which extracts to `<repo-name>/site/`, `<repo-name>/site-src/`, and the generated markdown reports.
- **Detached run** (`fly machine run --detach`): retrieve the URL from the Machine's logs:

  ```sh
  fly logs -i <machine-id> | tail -n 1
  ```

URLs are valid for 30 days from generation.

### Cleanup

- Machines launched with `--rm` self-destroy when the job exits — no manual cleanup needed.
- Tarballs in the Fly Storage bucket are **kept indefinitely**. Operators who want time-based retention should configure a bucket lifecycle rule directly with Tigris; Find the Gaps does not manage tarball cleanup.

## Development

See [CLAUDE.md](CLAUDE.md) for project conventions, tech stack, and TDD rules. See [.plans/VERIFICATION_PLAN.md](.plans/VERIFICATION_PLAN.md) for acceptance testing procedures.

## License

[MIT](LICENSE) © Sandgarden, Inc.
