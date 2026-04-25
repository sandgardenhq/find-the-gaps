# Hugo Site Publishing — Design

## Goal

Every `find-the-gaps analyze` run emits a browsable, Hextra-themed Hugo site alongside the existing markdown reports. The tool ships both artifacts: the flat markdown the user already gets, and a rendered static site they can host.

## Decisions Summary

| Question | Decision |
|---|---|
| When is the site emitted? | Always on every `analyze`; `--no-site` opt-out. |
| Mirror or expanded content? | Both ship. Default `--site-mode=mirror`; `--site-mode=expanded` is opt-in. |
| Hugo as a dependency | Shell out to `hugo` on `$PATH`, mirroring the `mdfetch` pattern (Homebrew `depends_on`, `doctor` check, `install-deps` support, first-run banner). |
| Theme | Hextra, embedded into the binary via `go:embed`. |
| What "publish" means | Render local HTML only. No `--serve`, no deploy. |
| Output layout | Clean by default: markdown reports + `<projectDir>/site/` HTML. `--keep-site-source` flag exposes source at `<projectDir>/site-src/`. |
| Mirror-mode site shape | Hextra site with synthesized **Home** dashboard + one page each for Mapping, Gaps, Screenshots. Screenshots nav item omitted when `screenshots.md` was not produced. |
| Expanded-mode site shape | Feature-centric. `Features` nav replaces `Mapping`. One page per feature; taxonomy pages for layer/status/user-facing; per-docs-page screenshot detail. |

## What Stays the Same

- The three markdown files (`mapping.md`, `gaps.md`, `screenshots.md`) keep their current contents and writers in `internal/reporter`. They are not coupled to Hugo.
- All existing CLI flags, exit codes, and progress output are unchanged.
- Caches under `<projectDir>/scan/`, `codefeatures.json`, `featuremap.json`, `docsfeaturemap.json` are unchanged.

## What's New

- An `internal/site` package that consumes the same in-memory analyzer outputs the reporter consumes, materializes a Hugo project to disk, and shells out to `hugo` to build into `<projectDir>/site/`.
- Runtime dep on `hugo` integrated into all four touchpoints (`doctor`, Homebrew formula, `install-deps`, first-run banner).
- New `analyze` flags: `--no-site`, `--keep-site-source`, `--site-mode`.

## Architecture

### Package Layout

```
internal/site/
  site.go              // public Build() + Options + Inputs
  site_test.go
  materialize.go       // writes Hugo source to disk
  materialize_test.go
  slug.go              // featureSlug()
  slug_test.go
  templates.go         // parses embedded templates
  templates_test.go
  assets/
    theme/hextra/...          // pinned Hextra snapshot, embedded
    templates/
      hugo.toml.tmpl          // site config (title, theme, baseURL="/", menus, taxonomies)
      home.md.tmpl            // synthesized dashboard
      section_index.md.tmpl   // per-section _index.md
      feature.md.tmpl         // expanded mode: per-feature page
      features_index.md.tmpl  // expanded mode: sortable feature table
      screenshot_page.md.tmpl // expanded mode: per-docs-page screenshot detail
```

### Public Surface

```go
package site

type Mode int

const (
    ModeMirror Mode = iota
    ModeExpanded
)

type BuildOptions struct {
    ProjectDir  string    // <projectDir>
    ProjectName string    // for site title and home dashboard
    KeepSource  bool      // --keep-site-source
    Mode        Mode      // ModeMirror default
    GeneratedAt time.Time
}

type Inputs struct {
    Summary         analyzer.ProductSummary
    Mapping         analyzer.FeatureMap
    DocsMap         analyzer.DocsFeatureMap
    AllDocFeatures  []string
    Drift           []analyzer.DriftFinding
    Screenshots     []analyzer.ScreenshotGap
    ScreenshotsRan  bool
}

func Build(ctx context.Context, in Inputs, opts BuildOptions) error
```

### Build Flow

After the reporter has written the markdown files:

1. **Choose source dir** — `os.MkdirTemp` if `!KeepSource`, else `<projectDir>/site-src/` (cleaned first).
2. **Materialize**:
   - Write embedded `theme/hextra/` to `themes/hextra/`.
   - Render `hugo.toml.tmpl` (taxonomy block emitted only in expanded mode).
   - Render `home.md.tmpl` into `content/_index.md`.
   - Mode-specific content:
     - **Mirror**: copy `mapping.md` / `gaps.md` / (optionally) `screenshots.md` from `<projectDir>` into `content/`, prepending frontmatter (title + nav weight).
     - **Expanded**: render `features_index.md.tmpl` into `content/features/_index.md`; render `feature.md.tmpl` once per feature into `content/features/<slug>.md`; render `/gaps/` with linked feature names; render `screenshot_page.md.tmpl` once per docs page with gaps into `content/screenshots/<page-slug>.md`.
3. **Exec** — `hugo --source <srcDir> --destination <projectDir>/site/ --minify --quiet --baseURL /`. Capture stderr.
4. **Clean up** — if `!KeepSource` and build succeeded, `os.RemoveAll(srcDir)`. On failure, the source dir is preserved (even in the default case) for debugging, and the error message includes its path.

### Conditional Screenshots

If `ScreenshotsRan == false`:
- `screenshots.md` is not copied into `content/` (mirror) and no per-page screenshot files are rendered (expanded).
- The `Screenshots` menu entry is omitted from `hugo.toml`.

### Feature Slug Helper

```go
func featureSlug(name string) string
```

Deterministic kebab-case: lowercase, Unicode-NFC, replace runs of non-alphanumerics with a single `-`, trim leading/trailing `-`. Collisions (from case-folding or diacritic normalization) are resolved by the caller by appending `-2`, `-3`, …; the helper itself is pure and stateless. The slug-collision resolver lives in `materialize.go` and operates on the sorted feature list so the suffix assignment is deterministic across runs.

## Expanded Mode Details

### Nav

```
Home  /  Features  /  Gaps  /  Screenshots (conditional)
```

`Features` replaces `Mapping`. Mapping data is distributed across per-feature pages plus the features index.

### Pages

- `/` — dashboard (counts + links; counts hyperlink into filtered views where possible).
- `/features/` — sortable HTML table rendered from template: name → page, layer, user-facing, doc status, file count, drift-finding count. No JavaScript required for sort; columns are pre-sorted alphabetically by name, with rendered HTML `<table>` inside the markdown for styled display.
- `/features/<slug>/` — description, layer, user-facing, doc status, files, symbols, doc URLs (hyperlinked), inlined drift findings, prominent "Undocumented" callout when applicable.
- `/features/layer/<value>/`, `/features/status/<value>/`, `/features/user-facing/` — Hugo taxonomy pages. Each feature page declares `tags = ["layer:db", "status:undocumented", "user-facing:yes"]`; Hextra renders the index pages automatically.
- `/gaps/` — same content as `gaps.md`, but every feature name is a hyperlink to `/features/<slug>/`.
- `/screenshots/<page-slug>/` — one page per docs URL with gaps.

### Taxonomy Block (hugo.toml)

Emitted only when `Mode == ModeExpanded`:

```toml
[taxonomies]
layer        = "layers"
status       = "statuses"
user_facing  = "user_facing"
```

### Cross-Linking Invariants

- Every feature name in `/gaps/` links to a real `/features/<slug>/` page.
- Every doc URL on a feature page is a valid, escaped hyperlink.
- Drift findings appear on both the per-feature page and `/gaps/`, sourced from the same `[]DriftFinding`.

## CLI Surface

### New Flags on `analyze`

| Flag | Default | Behavior |
|---|---|---|
| `--no-site` | `false` | Skip site build. Markdown reports are still written. Stdout `reports:` block shows `site/ (skipped)`. |
| `--keep-site-source` | `false` | Preserve materialized Hugo source at `<projectDir>/site-src/` after a successful build. |
| `--site-mode` | `mirror` | `mirror` or `expanded`. Unrecognized values return a clear flag-validation error. |

### Stdout `reports:` Block

Existing block lists `mapping.md`, `gaps.md`, optionally `screenshots.md`. Adds:
- `site/` (or `site/ (skipped)` when `--no-site`).
- `site-src/` when `--keep-site-source`.

### `find-the-gaps doctor`

Adds `hugo` alongside `mdfetch`. Detect via `hugo version`, parse the version, and print it. Missing `hugo` → non-zero exit with an install hint identical in shape to the existing `mdfetch` hint.

### `find-the-gaps install-deps`

Adds Hugo. macOS: `brew install hugo`. Other platforms: print platform-specific guidance (pointer to https://github.com/gohugoio/hugo/releases) and exit gracefully — same shape as the existing flow.

### Homebrew formula (`.goreleaser.yaml`)

- Add `"hugo"` to `brews[].dependencies`.
- Update `caveats` to name `hugo` alongside `mdfetch`.

### First-Run Banner

Extend the existing notice from "uses `mdfetch`" to "uses `mdfetch` and `hugo`," with the same `--quiet` / `FIND_THE_GAPS_QUIET=1` escape hatch.

### README

Under "What this installs," add `hugo` with a one-line description.

## Failure Modes

1. **`hugo` missing and `--no-site` unset** — print a `doctor`-style hint, exit non-zero.
2. **`hugo` missing and `--no-site` set** — no error; site is silently skipped.
3. **`hugo` present but build fails** — wrap stderr, exit non-zero, preserve the source dir (even in the default temp-dir case) and include its path in the error.
4. **Unrecognized `--site-mode` value** — flag-validation error from Cobra, non-zero exit.

## Testing Strategy

### Package-Level Tests

1. **`featureSlug` unit tests** — table-driven. Lowercase/trim, non-alphanum → hyphen, collapse runs, Unicode normalization, empty-string safety. Collision resolver tested separately with sorted-input inputs.
2. **Template rendering tests** — render each `.tmpl` with fixture `Inputs`; assert structural invariants of the produced markdown. No Hugo involved.
3. **`materialize` tests** — call with a temp dir; assert the resulting tree (files present, theme extracted, `hugo.toml` valid TOML, taxonomy block present iff expanded, `screenshots/` absent iff `!ScreenshotsRan`).
4. **Build integration tests** — under `//go:build integration`, skipped by `go test -short`. Asserts real Hugo output:
   - Mirror: `site/index.html`, `site/mapping/index.html`; `site/screenshots/…` absent when `ScreenshotsRan=false`.
   - Expanded: `site/features/<slug>/index.html` for every feature; every `/gaps/` feature-name hyperlink resolves to a real file on disk (parse HTML, stat hrefs).
   - `--keep-site-source` materializes `site-src/`; default does not.
5. **Error-path tests** — stub `hugo` via a fake script on `$PATH` that exits non-zero. Assert the error wraps stderr and the source dir is preserved.

### End-to-End Tests (`cmd/find-the-gaps/testdata/*.txtar`)

- `analyze_site_mirror.txtar`
- `analyze_site_expanded.txtar`
- `analyze_no_site.txtar`
- `analyze_keep_source.txtar`
- `analyze_site_missing_hugo.txtar`
- `doctor_hugo.txtar`

### Coverage

≥90% statement coverage on `internal/site` per the project's existing bar.

### Verification Plan

Add a Scenario 10 to `.plans/VERIFICATION_PLAN.md` covering:
- Site builds against the Scenario-1 fixture in both modes.
- Every link in `/gaps/` resolves.
- `--no-site` suppresses the site cleanly.
- Removing `hugo` from `PATH` produces a clear install hint.
