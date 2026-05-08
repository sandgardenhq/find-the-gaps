# Forge-Aware Docs Ingestion — Design

## Problem

`find-the-gaps analyze` crawls `--docs-url` by following same-host links (`internal/spider/spider.go:50`, `internal/spider/links.go:34`). When the docs URL points at a source-control forge — e.g., `github.com/<owner>/<repo>` — same-host means *all of `github.com`*: every issue, PR, commit, blame view, marketplace page, and unrelated repo. The crawl never converges, the LLM passes never start, and the run effectively hangs.

The fix is not a smarter crawler. Forges are huge multi-tenant apps where ~99% of the link graph is irrelevant; tuning a denylist is a treadmill. The fix is to refuse to crawl forges and read the markdown directly from disk when we already have the repo locally.

## Goal

1. Detect when `--docs-url` points at a known forge.
2. If `--repo` is a clone of that same `<owner>/<repo>`, skip the crawl and walk markdown on disk.
3. Otherwise, halt with one clear message telling the user to clone the repo and pass `--repo`.

Crawling a forge silently must become impossible.

## Forge detection

Initial host list:

| Host | Forge |
|---|---|
| `github.com`, `www.github.com` | GitHub |
| `gitlab.com` | GitLab.com |
| `bitbucket.org` | Bitbucket Cloud |
| `codeberg.org` | Codeberg (Forgejo) |
| `git.sr.ht` | SourceHut |

Self-hosted forges (Gitea, Forgejo, Gogs, GitLab, Bitbucket Server, OneDev) cannot be detected by host. They are addressed by an explicit `--forge` flag (see "Escape hatch").

## Decision tree

```
docs-url host ∈ forge list  OR  --forge is set
├── --repo's git remote matches docs-url's <owner>/<repo>
│   └── Print: "docs-url is a forge URL; reading markdown from --repo on disk."
│       Skip the crawl. Walk repo for documentation files.
│
└── Anything else (no --repo, no .git, mismatched remote, wiki path, etc.)
    └── Halt with exit code 1:
        "Find the Gaps can't crawl source-control forges
         (github.com, gitlab.com, bitbucket.org, codeberg.org, git.sr.ht).
         To analyze these docs, clone the repo locally and pass
         --repo /path/to/it. Got: <docs-url>"
```

No silent fallback to crawling. The whole point is to refuse.

## Same-repo matching

1. Parse `--docs-url` → `(host, owner, repo, branch?, subpath?)`. Strip a trailing `.git` from `repo`.
2. Read `--repo`'s `origin` remote (`git -C <repo> remote get-url origin`). If `origin` is missing, treat as no match.
3. Normalize the remote URL — handle both forms:
   - HTTPS: `https://github.com/foo/bar.git`
   - SSH: `git@github.com:foo/bar.git`
4. Compare `(host, owner, repo)` case-insensitively. GitHub and most forges treat owner/repo case-insensitively for routing.

If any step fails, fall through to the halt branch. Cheaper than guessing.

## On-disk ingestion

When the shortcut engages, replace the spider's output (`map[url]filepath`) with the same shape built from the local filesystem.

1. **Determine the subtree** from the docs URL path:
   - `…/owner/repo` → repo root
   - `…/owner/repo/tree/<branch>/<sub>` → `<repo>/<sub>`
   - `…/owner/repo/blob/<branch>/<file>` → that single file
2. **Walk** the subtree using `git ls-files` to honor `.gitignore` (avoids `vendor/`, `node_modules/`, build artifacts), then filter to the documentation extensions below.
3. **Synthesize a stable URL** for each file: `https://<host>/<owner>/<repo>/blob/<branch>/<relpath>`. Branch comes from the docs URL when present; otherwise the repo's current `HEAD` symbolic ref. Reports use these URLs for clickable links.
4. **Read working-tree contents**, not `git show <branch>:path`. Users want to analyze "what's there now." If the docs URL named a different branch than `HEAD`, log one warning line and continue.

The downstream analyzer phases see a `map[url]filepath` indistinguishable from the spider's output and remain untouched.

## Documentation file extensions

| Extension | Format | Notes |
|---|---|---|
| `.md`, `.markdown` | Markdown | Default. |
| `.mdx` | Markdown + JSX | Docusaurus, Next.js docs. JSX components are noise but harmless. |
| `.rst` | reStructuredText | Python ecosystem, Sphinx. |
| `.adoc`, `.asciidoc` | AsciiDoc | Spring, Asciidoctor projects, Linux kernel docs. |

Skipped by default (high false-positive rate):
- `.txt` — collides with `LICENSE.txt`, `requirements.txt`.
- `.html` — would pull in build artifacts.
- `.org`, `.pod`, `.textile` — niche; defer until requested.

The LLM analyzer reads page content as plain text. RST and AsciiDoc pass through verbatim — no Pandoc dependency. Code-fence-aware drift prompting is a known limitation to revisit if RST/AsciiDoc projects produce noticeably worse drift findings.

The extension set is a constant in code. Not a flag until real demand surfaces.

## Escape hatch — `--forge`

```
ftg analyze --repo . --docs-url https://git.example.com/foo/bar --forge gitea
```

When `--forge` is set, the host check is bypassed and the same on-disk decision tree runs against the URL's path. Accepted values: `github`, `gitlab`, `bitbucket`, `gitea`, `forgejo`, `gogs`. All assume GitHub-shape paths (`/owner/repo/{tree,blob}/branch/...`). SourceHut's `~user/repo/tree/...` shape is out of scope for v1.

## Out of scope (v1)

- Wiki ingestion. Wikis live in a separate `<repo>.wiki.git`. Halt message tells the user to clone it and treat it as the `--repo` for a wiki-only analysis.
- GitHub API / GitLab API ingestion. Adds auth complexity, rate-limit failure modes, and a parallel adapter per forge. The on-disk path covers the cases that matter today.
- Long-tail forges (Phabricator/Phorge, RhodeCode, Beanstalk, Allura, etc.). Covered by `--forge` for users who hit them.
- SourceHut on-disk ingestion. Different URL shape; explicit halt with a hint that SourceHut isn't yet supported.

## Test plan

Unit:
- Forge host detection across the table above.
- `--forge` flag bypasses host check.
- Remote URL normalization for HTTPS and SSH forms; trailing `.git` stripped; case-insensitive compare.
- Subpath extraction from `tree/`, `blob/`, and root URLs.
- Synthesized URL is stable across runs.
- Extension filter accepts `.md`/`.mdx`/`.markdown`/`.rst`/`.adoc`/`.asciidoc`; rejects `.txt`/`.html`.

Integration (testscript):
- Forge URL + matching local repo → on-disk mode prints the notice, produces a non-empty page map, exits 0.
- Forge URL + mismatched remote → halts with the standard message, exit 1.
- Forge URL with no `--repo` → same halt.
- Forge URL pointing at `/wiki` → halt with wiki-specific hint.
- Non-forge URL is unaffected (existing spider path).

## Sources

- [Comparison of source-code-hosting facilities — Wikipedia](https://en.wikipedia.org/wiki/Comparison_of_source-code-hosting_facilities)
- [Top Git Hosting Services for 2026 — GitProtect.io](https://gitprotect.io/blog/top-git-hosting-services/)
- [Compared to other Git hosting — Gitea Documentation](https://docs.gitea.com/installation/comparison)
