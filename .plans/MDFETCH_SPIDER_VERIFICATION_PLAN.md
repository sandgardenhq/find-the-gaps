# MDFetch Spider — Verification Plan

## Test Site

**URL:** `https://winter-bush-da7b.britt-e30.workers.dev/`

The site is a fictional "Tech Frontier" corporate site with ~24 pages across four URL
namespaces: root, `/customers/`, `/level1/`, `/level1/level2/`, and
`/level1/level2/level3/`.

**Important:** `mdfetch` applies readability processing that strips navigation links,
preserving only links that appear in the main article body. This means the spider's
reachable set is determined by content links, not navigation. On this site:

- **Root (`/`)** — readability output has no content links → spider fetches 1 page.
- **`/customers/index.html`** — readability output contains 4 absolute content links
  to case studies → spider fetches 5 pages total.
- **All other pages** (leadership profiles, case studies) — no content links in
  readability output → dead ends.

These counts are ground truth, verified against `mdfetch` output before writing this
plan. Do not inflate expected counts.

---

## Prerequisites

Before any scenario runs:

- **Binary on `$PATH`:** `find-the-gaps` (built from the worktree:
  `cd .worktrees/feat-mdfetch-spider && go build -o find-the-gaps ./cmd/find-the-gaps`).
- **`mdfetch` on `$PATH`** — `mdfetch --version` must succeed.
- **Network access** to `https://winter-bush-da7b.britt-e30.workers.dev/`.
- **Clean cache** — before each scenario, remove any existing cache directory so
  tests start from a known state:
  ```bash
  rm -rf .find-the-gaps/cache
  ```
- All scenarios are run from the `.worktrees/feat-mdfetch-spider` directory.

---

## Scenarios

### Scenario 1: Single-Page Crawl (No Content Links)

**Context:** The root URL's readability output has no content links. The spider should
fetch exactly one page and stop cleanly.

**Steps:**
1. Remove any existing cache: `rm -rf .find-the-gaps/cache`
2. Run:
   ```bash
   ./find-the-gaps analyze --docs-url https://winter-bush-da7b.britt-e30.workers.dev/ \
     --cache-dir .find-the-gaps/cache --workers 1
   ```
3. Inspect stdout for the page count.
4. Inspect the cache directory:
   ```bash
   ls .find-the-gaps/cache/
   cat .find-the-gaps/cache/index.json
   ```

**Success Criteria:**
- [ ] Command exits with code `0`.
- [ ] Stdout reports `fetched 1 pages`.
- [ ] Cache contains exactly one `.md` file and one `index.json`.
- [ ] `index.json` contains exactly one entry whose key is
  `https://winter-bush-da7b.britt-e30.workers.dev/`.
- [ ] The `.md` file is non-empty and contains "Tech Frontier".

**If Blocked:** If the command panics or exits non-zero, capture full output and stop.
Do not proceed to Scenario 2.

---

### Scenario 2: Multi-Page Crawl (Content Links Followed)

**Context:** `/customers/index.html` has 4 absolute content links to case study pages.
Starting there, the spider should fetch 5 pages total.

**Steps:**
1. Remove any existing cache: `rm -rf .find-the-gaps/cache`
2. Run:
   ```bash
   ./find-the-gaps analyze \
     --docs-url https://winter-bush-da7b.britt-e30.workers.dev/customers/index.html \
     --cache-dir .find-the-gaps/cache --workers 2
   ```
3. Inspect stdout.
4. Inspect the cache:
   ```bash
   cat .find-the-gaps/cache/index.json
   ```

**Success Criteria:**
- [ ] Command exits with code `0`.
- [ ] Stdout reports `fetched 5 pages`.
- [ ] `index.json` contains exactly 5 entries.
- [ ] The following URLs are all present as keys in `index.json`:
  - `https://winter-bush-da7b.britt-e30.workers.dev/customers/index.html`
  - `https://winter-bush-da7b.britt-e30.workers.dev/customers/meridian-global.html`
  - `https://winter-bush-da7b.britt-e30.workers.dev/customers/nordic-defense.html`
  - `https://winter-bush-da7b.britt-e30.workers.dev/customers/apex-ventures.html`
  - `https://winter-bush-da7b.britt-e30.workers.dev/customers/harwick-institute.html`
- [ ] Each URL maps to a distinct `.md` filename (no collisions).
- [ ] Each `.md` file is non-empty.

**If Blocked:** If fewer than 5 pages are fetched, print the `index.json` contents
verbatim and ask the developer before proceeding.

---

### Scenario 3: Re-Run Skips Cached Pages

**Context:** Running the spider a second time against a fully-cached site should make
zero network calls to `mdfetch`.

**Steps:**
1. Complete Scenario 2 (cache now contains 5 pages).
2. Run the same command again:
   ```bash
   ./find-the-gaps analyze \
     --docs-url https://winter-bush-da7b.britt-e30.workers.dev/customers/index.html \
     --cache-dir .find-the-gaps/cache --workers 2
   ```
3. Compare cache state before and after.

**Success Criteria:**
- [ ] Command exits with code `0`.
- [ ] Stdout reports `fetched 5 pages` (already-cached pages appear in results).
- [ ] `index.json` still contains exactly 5 entries — no new entries added.
- [ ] No `.md` file modification timestamps changed (files were not re-written):
  ```bash
  ls -lT .find-the-gaps/cache/*.md
  ```
  Compare timestamps to Scenario 2 output.

**If Blocked:** If more than 5 entries appear in the index, the de-duplication logic
is broken — stop and report.

---

### Scenario 4: Same-Host Scoping (External Links Not Followed)

**Context:** Verify that links to external domains are ignored. Any case study page
that mentions external URLs in its body text must not cause those URLs to be fetched.

**Steps:**
1. Remove any existing cache: `rm -rf .find-the-gaps/cache`
2. Run a crawl starting from a case study page that contains prose (potential bare URLs):
   ```bash
   ./find-the-gaps analyze \
     --docs-url https://winter-bush-da7b.britt-e30.workers.dev/customers/meridian-global.html \
     --cache-dir .find-the-gaps/cache --workers 1
   ```
3. Inspect `index.json`.

**Success Criteria:**
- [ ] Command exits with code `0`.
- [ ] Every key in `index.json` begins with
  `https://winter-bush-da7b.britt-e30.workers.dev/`.
- [ ] No entry references a different host (e.g., `meridian-global.com`,
  `techfrontier.com`, or any other domain).

**If Blocked:** Print `index.json` verbatim and ask the developer. Do not guess
whether an external URL appearing in the cache is acceptable.

---

### Scenario 5: Worker Concurrency

**Context:** With 4 URLs discoverable from `/customers/index.html` in a single pass,
a 3-worker crawl should complete faster than a 1-worker crawl. This verifies that the
worker pool actually runs in parallel, not sequentially.

**Steps:**
1. Remove cache: `rm -rf .find-the-gaps/cache`
2. Time a 1-worker crawl:
   ```bash
   time ./find-the-gaps analyze \
     --docs-url https://winter-bush-da7b.britt-e30.workers.dev/customers/index.html \
     --cache-dir .find-the-gaps/cache --workers 1
   ```
   Note the wall-clock time.
3. Remove cache: `rm -rf .find-the-gaps/cache`
4. Time a 3-worker crawl:
   ```bash
   time ./find-the-gaps analyze \
     --docs-url https://winter-bush-da7b.britt-e30.workers.dev/customers/index.html \
     --cache-dir .find-the-gaps/cache --workers 3
   ```
   Note the wall-clock time.

**Success Criteria:**
- [ ] Both runs exit with code `0` and report `fetched 5 pages`.
- [ ] The 3-worker run completes in less time than the 1-worker run.
  (A network-bound fetch of 4 pages sequentially vs. 3-at-once should show a
  measurable difference unless the site responds in under 100ms per page.)

**If Blocked:** If both runs take the same time, document both durations and ask the
developer — the worker pool may be serialising.

---

### Scenario 6: Configurable Cache Directory

**Context:** The `--cache-dir` flag must write to the specified path, not the default.

**Steps:**
1. Run:
   ```bash
   ./find-the-gaps analyze \
     --docs-url https://winter-bush-da7b.britt-e30.workers.dev/ \
     --cache-dir /tmp/ftg-verify-cache --workers 1
   ```
2. Inspect:
   ```bash
   ls /tmp/ftg-verify-cache/
   cat /tmp/ftg-verify-cache/index.json
   ```
3. Confirm `.find-the-gaps/cache/` was NOT created or modified:
   ```bash
   ls .find-the-gaps/cache/ 2>&1
   ```

**Success Criteria:**
- [ ] Command exits with code `0`.
- [ ] `/tmp/ftg-verify-cache/` contains one `.md` file and `index.json`.
- [ ] `.find-the-gaps/cache/` does not exist (or was not modified if left from prior
  scenarios).

**Cleanup:** `rm -rf /tmp/ftg-verify-cache`

---

## Verification Rules

- **No mocks.** Every run uses the real `mdfetch` binary against the live test site.
- **No skipping scenarios.** All six must pass before the implementation is considered
  complete.
- **If any success criterion fails, stop.** Document the exact failure, print relevant
  output verbatim, and ask the developer.
- **Do not adjust expected counts** (e.g., "close enough") — page counts must match
  exactly.
- **Re-run from scratch** if network errors cause intermittent failures — do not
  mark passing on a flaky run.
