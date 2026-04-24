# Homebrew Install — Design

**Date:** 2026-04-24
**Status:** Design approved, ready to implement
**Scope:** Make `brew install sandgardenhq/tap/find-the-gaps` a one-command install on macOS and Linux.

## Goal

Let users install Find the Gaps via Homebrew. After `brew install`, the user should have a working `ftg` binary and `mdfetch` already on `$PATH`, with no second step.

## Non-goals

- **First-run banner** — separate feature, not included here.
- **Goreleaser migration** — `CLAUDE.md` mentions goreleaser, but the repo's actual release pipeline is a hand-written GitHub Actions workflow. This design extends that workflow rather than rewriting it.
- **Submitting to `homebrew/homebrew-core`** — higher bar, not worth it yet.
- **Standalone `mdfetch.rb` formula** — this design installs mdfetch via npm from within `post_install`, not as a separate brew formula. A proper mdfetch formula is a future improvement that belongs in the mdfetch repo.

## Shape

Two repos are involved:

1. **New repo: `sandgardenhq/homebrew-tap`.** A Homebrew tap (GitHub repo whose name starts with `homebrew-`). Holds `Formula/find-the-gaps.rb`. Users tap it via `brew tap sandgardenhq/tap` or install fully-qualified with `brew install sandgardenhq/tap/find-the-gaps`.
2. **This repo (`find-the-gaps`).** Gets one new job appended to `.github/workflows/release.yml`. After the existing release job publishes the GitHub Release, the new `publish-formula` job renders `find-the-gaps.rb` from a template and pushes it to the tap.

### End-user experience

```sh
brew install sandgardenhq/tap/find-the-gaps
# → installs ftg binary
# → brew auto-installs node (depends_on)
# → post_install runs: ftg install-deps → npm i -g @sandgarden/mdfetch
ftg doctor   # confirms mdfetch present
```

## The formula

`Formula/find-the-gaps.rb` in the tap repo, rendered from `.github/homebrew/find-the-gaps.rb.tmpl` in this repo on each release:

```ruby
class FindTheGaps < Formula
  desc "Find documentation gaps between a codebase and its docs site"
  homepage "https://github.com/sandgardenhq/find-the-gaps"
  version "{{VERSION}}"
  license "MIT"

  depends_on "node"  # for npm, used by post_install to get mdfetch

  on_macos do
    on_arm do
      url "https://github.com/sandgardenhq/find-the-gaps/releases/download/v{{VERSION}}/find-the-gaps_v{{VERSION}}_darwin-arm64.tar.gz"
      sha256 "{{SHA_DARWIN_ARM64}}"
    end
    on_intel do
      url "https://github.com/sandgardenhq/find-the-gaps/releases/download/v{{VERSION}}/find-the-gaps_v{{VERSION}}_darwin-amd64.tar.gz"
      sha256 "{{SHA_DARWIN_AMD64}}"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/sandgardenhq/find-the-gaps/releases/download/v{{VERSION}}/find-the-gaps_v{{VERSION}}_linux-arm64.tar.gz"
      sha256 "{{SHA_LINUX_ARM64}}"
    end
    on_intel do
      url "https://github.com/sandgardenhq/find-the-gaps/releases/download/v{{VERSION}}/find-the-gaps_v{{VERSION}}_linux-amd64.tar.gz"
      sha256 "{{SHA_LINUX_AMD64}}"
    end
  end

  def install
    bin.install "ftg"
  end

  def post_install
    system bin/"ftg", "install-deps"
  end

  def caveats
    <<~EOS
      find-the-gaps also needs `mdfetch` (npm package @sandgarden/mdfetch).
      It was installed into Node's global prefix during post_install.
      `brew uninstall find-the-gaps` will NOT remove mdfetch — run
      `npm uninstall -g @sandgarden/mdfetch` if you want it gone.
      Verify with: ftg doctor
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/ftg --version")
  end
end
```

### Formula design notes

- **Binary name is `ftg`** inside the tarball (see `release.yml`), so `bin.install "ftg"` is correct.
- **`depends_on "node"`** — brew pulls Node in automatically so `post_install` has `npm` available. One-command install, no "oh also install Node" step.
- **No `depends_on` for mdfetch.** `CLAUDE.md` calls for `depends_on "britt/tap/mdfetch"`, but no such formula exists and mdfetch only ships on npm today. Installing via `post_install` delegates to the existing, already-tested `ftg install-deps` command instead of duplicating npm logic in Ruby — single source of truth.
- **Caveats** tell the user that `brew uninstall` won't remove mdfetch (because it was installed via npm, which brew doesn't track in the formula's manifest).
- **Test block** runs `ftg --version` and confirms it matches the formula's declared version. `brew test find-the-gaps` invokes this.

## Release-workflow wiring

### New template in this repo

`.github/homebrew/find-the-gaps.rb.tmpl` — the formula above with `{{VERSION}}` and `{{SHA_*}}` placeholders. Checked into this repo so shape changes are reviewed in PRs like any other code.

### New job in `.github/workflows/release.yml`

Appended after the existing `release` job:

```yaml
publish-formula:
  name: Update Homebrew formula
  needs: release
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v6

    - name: Download checksums from release
      env:
        GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        VERSION: ${{ github.ref_name }}
      run: gh release download "$VERSION" -p checksums.txt

    - name: Render formula
      env:
        VERSION: ${{ github.ref_name }}
      run: |
        V="${VERSION#v}"
        sha() { awk -v f="find-the-gaps_${VERSION}_$1.tar.gz" '$2==f{print $1}' checksums.txt; }
        sed \
          -e "s|{{VERSION}}|$V|g" \
          -e "s|{{SHA_DARWIN_ARM64}}|$(sha darwin-arm64)|" \
          -e "s|{{SHA_DARWIN_AMD64}}|$(sha darwin-amd64)|" \
          -e "s|{{SHA_LINUX_ARM64}}|$(sha linux-arm64)|" \
          -e "s|{{SHA_LINUX_AMD64}}|$(sha linux-amd64)|" \
          .github/homebrew/find-the-gaps.rb.tmpl > find-the-gaps.rb

    - name: Commit to tap
      env:
        TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
        VERSION: ${{ github.ref_name }}
      run: |
        git clone "https://x-access-token:$TOKEN@github.com/sandgardenhq/homebrew-tap.git" tap
        mkdir -p tap/Formula
        mv find-the-gaps.rb tap/Formula/
        cd tap
        git config user.name "release-bot"
        git config user.email "release-bot@users.noreply.github.com"
        git add Formula/find-the-gaps.rb
        git commit -m "find-the-gaps $VERSION"
        git push
```

### Access to the tap repo

- Fine-grained PAT scoped to `sandgardenhq/homebrew-tap` with `contents:write`.
- Stored as `HOMEBREW_TAP_TOKEN` in the `find-the-gaps` repo secrets.
- Direct push to `main` — the formula is mechanically generated, nothing meaningful to review on a per-release basis.

### Version handling

- `github.ref_name` is the tag (e.g., `v1.2.3`).
- Tarball filenames are `find-the-gaps_v1.2.3_darwin-arm64.tar.gz` (with the `v`).
- Formula `version` field drops the `v` (`1.2.3`, brew convention).
- Template uses `v{{VERSION}}` in URLs and `{{VERSION}}` in the `version` field; the sed step strips the `v` once and substitutes the clean value.

## Verification

### Automated (on every release)

- **`brew test find-the-gaps`** runs the formula's `test` block — confirms `ftg --version` matches the declared version.
- **Smoke-test job** appended after `publish-formula`: taps and installs from the tap on a fresh runner, then runs `ftg doctor`. Fails the release if the just-published formula is broken.

### Unit test for template rendering

Small bash test (e.g., `.github/homebrew/test-render.sh`) that runs the `sed` pipeline with a fake `checksums.txt` and diffs against a golden `find-the-gaps.rb.expected`. Catches substitution typos without needing a real release. Runs in the existing `test.yml` workflow.

### Manual one-time verification (before first real release)

1. Create `sandgardenhq/homebrew-tap` (empty, with a README pointing at `brew tap sandgardenhq/tap`).
2. Create the PAT; set `HOMEBREW_TAP_TOKEN` secret in this repo.
3. Cut a throwaway pre-release tag (`v0.0.1-test`). Watch the workflow publish the formula.
4. On a clean Mac (and a clean Linux box), run through Scenario 8 of `.plans/VERIFICATION_PLAN.md` as updated below.
5. Delete the test tag, release, and tap commit if everything worked.

### Update to `.plans/VERIFICATION_PLAN.md`

Scenario 8 currently asserts "installs `mdfetch` as a dependency" — that wording is incorrect for this design. Update to: brew install completes, `post_install` runs `ftg install-deps`, and `mdfetch --version` / `ftg doctor` pass afterward. Also update the caveats assertion to match the new caveats text.

## Documentation

`README.md`'s Install section gets reorganized:

```md
## Install

### Homebrew (macOS and Linux)

    brew install sandgardenhq/tap/find-the-gaps

### Other platforms

    go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest

Or build from source:

    git clone https://github.com/sandgardenhq/find-the-gaps.git
    cd find-the-gaps
    make build

Then install the required external tools:

    ftg install-deps
```

## Rollback

If a formula commit breaks users, a revert commit on the tap repo is a one-line fix. The tap is independent of the release binaries, so revert is safe and fast.

## Implementation checklist

Listed in the order they should be done:

- [ ] Create `sandgardenhq/homebrew-tap` repo (manual; contains `README.md` only at first).
- [ ] Create fine-grained PAT, add `HOMEBREW_TAP_TOKEN` secret to this repo.
- [ ] Add `.github/homebrew/find-the-gaps.rb.tmpl`.
- [ ] Add `.github/homebrew/find-the-gaps.rb.expected` (golden).
- [ ] Add `.github/homebrew/test-render.sh` (render-and-diff test).
- [ ] Wire `test-render.sh` into `.github/workflows/test.yml`.
- [ ] Append `publish-formula` job to `.github/workflows/release.yml`.
- [ ] Append smoke-test job after `publish-formula`.
- [ ] Update Scenario 8 in `.plans/VERIFICATION_PLAN.md`.
- [ ] Update `README.md` Install section.
- [ ] Cut throwaway pre-release tag and verify end-to-end on Mac and Linux.
- [ ] Clean up the throwaway tag, release, and tap commit.
