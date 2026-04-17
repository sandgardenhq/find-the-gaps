<p align="center">
  <img src="assets/find-the-gaps.png" alt="Find the Gaps" width="640">
</p>

# Find the Gaps

A CLI tool that analyzes a codebase alongside its documentation site to identify outdated or missing documentation.

## Why

Project maintainers know their docs rot. It persists not because the problem is hard, but because it's the fourth most important problem on a list where they only have bandwidth for the top three. Link checkers catch broken URLs. Spell checkers catch typos. Neither can tell you that the function signature in `README.md` no longer matches the code, or that a new public API shipped last month without a single page describing it.

Find the Gaps closes that gap.

## What this installs

Installing via Homebrew pulls in two runtime dependencies:

- [`ripgrep`](https://github.com/BurntSushi/ripgrep) — fast codebase searching
- `mdfetch` — downloads a documentation site as markdown

If you install via `go install`, you are responsible for installing both tools yourself. Run `find-the-gaps doctor` at any time to check that both are available and see their detected versions.

## Install

```sh
brew install <tap>/find-the-gaps
```

Or with Go:

```sh
go install github.com/britt/find-the-gaps/cmd/find-the-gaps@latest
```

## Usage

```sh
find-the-gaps analyze --repo ./path/to/repo --docs-url https://your-docs-site
```

See `find-the-gaps --help` for full usage.

## Development

See [CLAUDE.md](CLAUDE.md) for project conventions, tech stack, and TDD rules. See [.plans/VERIFICATION_PLAN.md](.plans/VERIFICATION_PLAN.md) for acceptance testing procedures.

## License

[MIT](LICENSE) © Sandgarden, Inc.
