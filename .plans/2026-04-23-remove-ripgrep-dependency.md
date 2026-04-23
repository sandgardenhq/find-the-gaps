# Remove Ripgrep Dependency Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove `ripgrep` (`rg`) as a required external runtime dependency because the codebase does not shell out to it anywhere, and document the set of programming languages the scanner supports in `README.md`. `mdfetch` remains the sole external runtime dependency.

**Architecture:** Ripgrep is currently declared as a required tool in `internal/doctor/doctor.go` (the `RequiredTools` slice), advertised in command help strings, exercised in tests and a testscript, and documented in `README.md`, `CLAUDE.md`, and `.plans/VERIFICATION_PLAN.md`. Removal is purely subtractive: drop the entry from `RequiredTools`, delete or refocus tests that assert `rg` behavior, and scrub all mentions from live documentation. Historical files (`PROGRESS.md` and dated `.plans/*` implementation plans already shipped) are frozen snapshots and MUST NOT be rewritten.

**Tech Stack:** Go 1.26+, `testing` stdlib, `testscript`, Cobra, Charmbracelet log.

**Pre-flight confirmation:**

Before starting Task 1, re-run the grep below and stop if any match is in code outside the files listed in this plan — that would mean ripgrep *is* used somewhere and the plan's premise is wrong:

```bash
rg -n -i 'ripgrep|\brg\b' -- cmd internal
```

Expected matches are ALL inside these files (no others):
- `internal/doctor/doctor.go`
- `internal/doctor/doctor_test.go`
- `internal/doctor/install_test.go`
- `internal/cli/doctor.go`
- `internal/cli/doctor_test.go`
- `internal/cli/install_deps.go`
- `internal/cli/install_deps_test.go`
- `internal/cli/root_test.go` (one comment on line 195)
- `cmd/find-the-gaps/testdata/script/doctor_ok.txtar`

If rg appears in `internal/analyzer`, `internal/scanner`, `internal/spider`, or `internal/reporter`, STOP and escalate.

---

## Files NOT to modify (historical snapshots)

Do not edit these — they are historical logs/plans:

- `PROGRESS.md`
- `.plans/2026-04-17-codebase-scanner-design.md`
- `.plans/2026-04-20-llm-analysis-plan.md`
- `.plans/2026-04-20-llm-analysis-design.md`
- `.plans/2026-04-20-context-length-plan.md`
- `.plans/2026-04-21-docs-feature-mapping.md`
- `.plans/LLM_ANALYSIS_VERIFICATION_PLAN.md`
- `.plans/CODEBASE_SCANNER_IMPLEMENTATION_PLAN.md`
- `.plans/MDFETCH_SPIDER_IMPLEMENTATION_PLAN.md`
- `.plans/IMPLEMENTATION_PLAN.md` (if present — only if referenced by CLAUDE.md; if it mentions ripgrep as a live item, include in the docs sweep task; otherwise leave)

---

### Task 1: Rewrite `internal/doctor/doctor_test.go` to expect only mdfetch (RED)

**Files:**
- Modify: `internal/doctor/doctor_test.go`

**Step 1: Update the test file**

Rewrite tests so the expected contract is "doctor checks mdfetch only". Replace the existing test bodies as follows (keep `writeFakeBin` and `writeFailingBin` helpers unchanged):

```go
func TestRun_AllPresent_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr)

	if code != 0 {
		t.Errorf("code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "mdfetch 1.2.3") {
		t.Errorf("stdout missing mdfetch version; stdout=%q", stdout.String())
	}
	if strings.Contains(stdout.String(), "ripgrep") || strings.Contains(stdout.String(), " rg ") {
		t.Errorf("stdout must not mention ripgrep/rg; got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty on success, got %q", stderr.String())
	}
}

func TestRun_MdfetchMissing_ReturnsOne(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr)

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "mdfetch") {
		t.Errorf("stderr should mention mdfetch, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "ripgrep") {
		t.Errorf("stderr must not mention ripgrep, got %q", stderr.String())
	}
}

func TestRun_VersionCommandFails_ReturnsOne(t *testing.T) {
	dir := t.TempDir()
	writeFailingBin(t, dir, "mdfetch")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr)

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "mdfetch") {
		t.Errorf("stderr should mention mdfetch failure, got %q", stderr.String())
	}
}
```

Delete these tests entirely (they are ripgrep-specific or now redundant):
- `TestRun_RgMissing_ReturnsOne`
- `TestRun_BothMissing_ReturnsOne` (the new `TestRun_MdfetchMissing_ReturnsOne` covers "nothing on PATH")

**Step 2: Run tests and verify RED**

Run: `go test ./internal/doctor/...`

Expected: FAIL. With the current `RequiredTools` still containing ripgrep:
- `TestRun_AllPresent_ReturnsZero` fails because `Run` returns 1 (ripgrep missing in the temp dir), the success assertion on exit code 0 trips, and stderr is non-empty mentioning ripgrep.
- `TestRun_MdfetchMissing_ReturnsOne` still passes for the exit code but may additionally still see ripgrep in stderr — the `!strings.Contains(stderr.String(), "ripgrep")` assertion FAILS.

If tests pass, STOP — the premise is wrong. Investigate.

**Step 3: Commit the RED state**

Do not commit a failing test alone. Proceed to Task 2 in the same cycle.

---

### Task 2: Remove ripgrep from `RequiredTools` (GREEN)

**Files:**
- Modify: `internal/doctor/doctor.go`

**Step 1: Update the package doc comment**

Replace lines 1–3:

```go
// Package doctor checks that the external tool find-the-gaps shells out to
// (mdfetch) is installed and reports a clear install hint if not.
package doctor
```

**Step 2: Update the `Tool` struct field comments**

Replace the `Name` / `Binary` inline comments so the examples reference mdfetch:

```go
type Tool struct {
	Name        string // display name, e.g. "mdfetch"
	Binary      string // executable name on PATH, e.g. "mdfetch"
	VersionArg  string // argument that prints the version, e.g. "--version"
	InstallHint string // human-readable install fallback shown when automated install is unavailable
	InstallCmds map[string][]string // GOOS → {cmd, arg1, ...} for automated install
}
```

**Step 3: Remove the ripgrep entry from `RequiredTools`**

Change `RequiredTools` so only the `mdfetch` entry remains:

```go
var RequiredTools = []Tool{
	{
		Name:        "mdfetch",
		Binary:      "mdfetch",
		VersionArg:  "--version",
		InstallHint: "npm install -g @sandgarden/mdfetch",
		InstallCmds: map[string][]string{
			"darwin":  {"npm", "install", "-g", "@sandgarden/mdfetch"},
			"linux":   {"npm", "install", "-g", "@sandgarden/mdfetch"},
			"windows": {"npm", "install", "-g", "@sandgarden/mdfetch"},
		},
	},
}
```

**Step 4: Run tests to verify GREEN**

Run: `go test ./internal/doctor/...`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/doctor/doctor.go internal/doctor/doctor_test.go
git commit -m "$(cat <<'EOF'
refactor(doctor): drop ripgrep from required external tools

- RED: rewrote doctor tests to assert only mdfetch is checked and that
  no ripgrep mention appears in stdout/stderr
- GREEN: removed ripgrep Tool entry from RequiredTools; updated package
  and Tool struct doc comments
- Why: ripgrep is not shelled out to anywhere in the codebase (no
  exec.Command, no LookPath for "rg" outside the doctor list itself)
- Status: go test ./internal/doctor/... passes
EOF
)"
```

---

### Task 3: Refocus `internal/doctor/install_test.go` to mdfetch only

**Files:**
- Modify: `internal/doctor/install_test.go`

**Step 1: Delete ripgrep-specific tests**

Delete these two tests entirely:
- `TestRunInstall_UnsupportedPlatform_ReturnsOne` (uses a ripgrep Tool with only darwin in InstallCmds)
- `TestRunInstall_MultipleTools_SkipsInstalledInstallsMissing` (constructs both rg + mdfetch Tools to test multi-tool behavior; with only one tool the test is meaningless)

**Step 2: Re-add an unsupported-platform test using mdfetch**

Add this replacement so coverage of the "no install command for this GOOS" branch is preserved. Mdfetch's real `InstallCmds` map in production covers darwin/linux/windows, so pick a GOOS not present in the test-constructed map:

```go
func TestRunInstall_UnsupportedPlatform_ReturnsOne(t *testing.T) {
	tools := []Tool{{
		Name:        "mdfetch",
		Binary:      "mdfetch",
		InstallHint: "npm install -g @sandgarden/mdfetch",
		InstallCmds: map[string][]string{
			"darwin": {"npm", "install", "-g", "@sandgarden/mdfetch"},
		},
	}}
	lookup := func(_ string) bool { return false }
	runner := func(_ context.Context, _, _ io.Writer, _ string, _ ...string) error { return nil }
	var stdout, stderr bytes.Buffer
	code := runInstall(context.Background(), tools, "linux", &stdout, &stderr, lookup, runner)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "mdfetch") {
		t.Errorf("stderr missing tool name; got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "npm install -g @sandgarden/mdfetch") {
		t.Errorf("stderr missing install hint; got %q", stderr.String())
	}
}
```

**Step 3: Update `TestRunInstall_PublicFunc_AllPresent_ReturnsZero`**

Remove the rg fake binary. The test becomes:

```go
func TestRunInstall_PublicFunc_AllPresent_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.0.0")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := RunInstall(context.Background(), &stdout, &stderr)
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already installed") {
		t.Errorf("stdout missing 'already installed'; got %q", stdout.String())
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/doctor/...`

Expected: PASS.

**Step 5: Run coverage gate**

Run: `go test -cover ./internal/doctor/...`

Expected: ≥90% statement coverage. If below, write additional focused tests before moving on.

**Step 6: Commit**

```bash
git add internal/doctor/install_test.go
git commit -m "$(cat <<'EOF'
test(doctor): drop ripgrep fixtures from install tests

- Removed TestRunInstall_MultipleTools_SkipsInstalledInstallsMissing
  (multi-tool behavior is no longer meaningful with a single tool)
- Rewrote TestRunInstall_UnsupportedPlatform_ReturnsOne around mdfetch
- Dropped rg fake binary from TestRunInstall_PublicFunc_AllPresent
- Status: go test ./internal/doctor/... passes, coverage holds
EOF
)"
```

---

### Task 4: Update `cmd/find-the-gaps/testdata/script/doctor_ok.txtar`

**Files:**
- Modify: `cmd/find-the-gaps/testdata/script/doctor_ok.txtar`

**Step 1: Rewrite the script**

Replace the full file contents with:

```
# doctor exits zero and reports the version from a fake mdfetch on PATH.
# PATH prepends $WORK/bin so the fake shadows any real binary on the system.
chmod 755 bin/mdfetch
env PATH=$WORK/bin:$PATH
exec ftg doctor
stdout 'mdfetch 42\.0\.0'
! stderr .

-- bin/mdfetch --
#!/bin/sh
echo "mdfetch 42.0.0"
```

**Step 2: Run the testscript**

Run: `go test ./cmd/find-the-gaps/...`

Expected: PASS. All scripts (including `doctor_ok.txtar`) green.

**Step 3: Commit**

```bash
git add cmd/find-the-gaps/testdata/script/doctor_ok.txtar
git commit -m "$(cat <<'EOF'
test(cmd): remove ripgrep fixture from doctor_ok testscript

- doctor now only checks mdfetch; script no longer stages a fake rg binary
- Status: go test ./cmd/find-the-gaps/... passes
EOF
)"
```

---

### Task 5: Update `internal/cli/doctor_test.go`

**Files:**
- Modify: `internal/cli/doctor_test.go`

**Step 1: Rewrite tests**

Replace the two test bodies so they only reference mdfetch:

```go
func TestRun_Doctor_AllPresent_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != 0 {
		t.Errorf("code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "mdfetch 1.2.3") {
		t.Errorf("stdout missing mdfetch version; got %q", stdout.String())
	}
}

func TestRun_Doctor_Missing_ReturnsOne(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != 1 {
		t.Errorf("code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "mdfetch") {
		t.Errorf("stderr should mention mdfetch, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "Error:") {
		t.Errorf("ExitCodeError should not print 'Error:' preamble, got %q", stderr.String())
	}
}
```

**Step 2: Run tests**

Run: `go test ./internal/cli/...`

Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cli/doctor_test.go
git commit -m "test(cli): drop ripgrep assertions from doctor command tests"
```

---

### Task 6: Update `internal/cli/install_deps_test.go`

**Files:**
- Modify: `internal/cli/install_deps_test.go`

**Step 1: Remove `rg` from the fake-binary list**

Change the loop so only `mdfetch` is staged:

```go
for _, name := range []string{"mdfetch"} {
```

(Alternatively, inline the single fake-binary creation; the loop form is fine and keeps the diff minimal.)

**Step 2: Run tests**

Run: `go test ./internal/cli/...`

Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cli/install_deps_test.go
git commit -m "test(cli): drop ripgrep from install-deps fake-binary fixture"
```

---

### Task 7: Update CLI help strings and stray comment

**Files:**
- Modify: `internal/cli/doctor.go:11`
- Modify: `internal/cli/install_deps.go:11-12`
- Modify: `internal/cli/root_test.go:194-195`

**Step 1: `internal/cli/doctor.go`**

Change the `Short` field:

```go
Short: "Check that the required external tool (mdfetch) is installed.",
```

**Step 2: `internal/cli/install_deps.go`**

Change the `Short` and `Long` fields:

```go
Short: "Install the required external tool (mdfetch).",
Long:  "Install mdfetch if it is not already on $PATH. An already-present tool is skipped.",
```

**Step 3: `internal/cli/root_test.go`**

Update the comment on lines 194–195 from "rg/mdfetch" to "mdfetch":

```go
// Running doctor --verbose must produce DEBU lines in stderr
// regardless of whether mdfetch is installed.
```

**Step 4: Run the full test suite**

Run: `go test ./...`

Expected: PASS.

**Step 5: Run the linter**

Run: `golangci-lint run`

Expected: No errors, no warnings.

**Step 6: Run the build**

Run: `go build ./...`

Expected: Success.

**Step 7: Commit**

```bash
git add internal/cli/doctor.go internal/cli/install_deps.go internal/cli/root_test.go
git commit -m "$(cat <<'EOF'
chore(cli): drop ripgrep from help text and doctor comment

- doctor Short, install-deps Short/Long no longer mention ripgrep
- root_test comment refreshed to match new single-tool reality
EOF
)"
```

---

### Task 8: Update `README.md`

**Files:**
- Modify: `README.md`

**Step 1: Rewrite the "What this installs" section (lines ~15–22)**

Change it to describe a single dependency:

```markdown
## What this installs

Find the Gaps shells out to one runtime dependency that must be on your `$PATH`:

- [`mdfetch`](https://www.npmjs.com/package/@sandgarden/mdfetch) — downloads a documentation site as markdown

Run `ftg doctor` at any time to check that it is available and see its detected version.
```

**Step 2: Update the embedded command help blocks**

Replace the help text under `## Usage`, `### doctor`, and `### install-deps` so the summary lines match the new `Short`/`Long` strings set in Task 7:

- In the top-level command list: change `doctor       Check that required external tools (ripgrep, mdfetch) are installed.` to `doctor       Check that the required external tool (mdfetch) is installed.`
- In the same block: change `install-deps Install required external tools (ripgrep, mdfetch).` to `install-deps Install the required external tool (mdfetch).`
- Under `### doctor`: change the first line to `Check that the required external tool (mdfetch) is installed.`
- Under `### install-deps`: change the first line to `Install mdfetch if it is not already on $PATH. An already-present tool is skipped.`

**Step 3: Add a "Supported languages" section**

Source of truth: `internal/scanner/lang/detect.go` (the `registry` populated in `init()`) plus each extractor's `Language()` and `Extensions()` methods. As of this plan, the extractor registry is: `GoExtractor`, `PythonExtractor`, `TypeScriptExtractor`, `RustExtractor`, with a `GenericExtractor` fallback for unrecognized text files and a binary-skip list in `binaryExts`.

Before writing the section, re-derive the list live so the README does not drift from code:

```bash
rg -n 'func \(\w+ \*\w+Extractor\) Language\(\) string' internal/scanner/lang
rg -n 'func \(\w+ \*\w+Extractor\) Extensions\(\) \[\]string' internal/scanner/lang
```

Verify the derived list matches: Go (`.go`), Python (`.py`, `.pyw`), TypeScript/JavaScript (`.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`), Rust (`.rs`). If it does not match, STOP and update the list below before writing the README.

Insert the new section immediately after `## Why` and before `## What this installs` (so a reader sees "what it can do" before "what you need to install"):

```markdown
## Supported languages

Find the Gaps uses [tree-sitter](https://github.com/smacker/go-tree-sitter) to extract symbols (functions, types, exports) from these languages:

| Language | Extensions |
| --- | --- |
| Go | `.go` |
| Python | `.py`, `.pyw` |
| TypeScript / JavaScript | `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs` |
| Rust | `.rs` |

Unrecognized text files are still scanned as plain text so they can be cross-referenced against docs, but no symbols are extracted from them. Binary files (images, archives, fonts, audio, compiled libraries, etc.) are skipped entirely.
```

**Step 4: Grep to confirm no stale mentions**

Run: `rg -n -i 'ripgrep|\brg\b' README.md`

Expected: no matches.

**Step 5: Manually verify the rendered README**

Run: `cat README.md | head -80`

Eyeball the new "Supported languages" table: correct markdown, no broken pipes, table sits between `## Why` and `## What this installs`.

**Step 6: Commit**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
docs(readme): drop ripgrep and document supported languages

- Removed ripgrep from installation and command help blocks
- Added a Supported languages section listing Go, Python,
  TypeScript/JavaScript, and Rust with their file extensions
- Source of truth: internal/scanner/lang extractor registry
EOF
)"
```

---

### Task 9: Update `CLAUDE.md`

**Files:**
- Modify: `CLAUDE.md`

**Step 1: "External Runtime Dependencies" block (around line 34)**

Replace the two bullets under the heading with a single bullet for mdfetch. Also drop the "(installed automatically by Homebrew; must be on `$PATH` for other install methods)" parenthetical if it no longer makes sense with a single tool — keep the spirit: external tool, must be on PATH.

Proposed replacement:

```markdown
- **External Runtime Dependencies** (must be on `$PATH`; installable via `ftg install-deps`):
  - `mdfetch` — our own utility that downloads websites as markdown; used to ingest the docs site
```

**Step 2: "Distribution" block (around lines 38–47)**

Rewrite so the Homebrew formula block and the `go install` paragraph reflect a single dependency:

```markdown
### Distribution

- **GitHub Releases** (cross-platform binaries via goreleaser)
- **Homebrew tap** (formula generated by goreleaser). The generated formula MUST declare:
  ```ruby
  depends_on "britt/tap/mdfetch"  # adjust to the actual tap path for mdfetch
  ```
  Configure this in `.goreleaser.yaml` under `brews[].dependencies` so every release installs mdfetch automatically.
- `go install` works as a fallback for Go users, but users are then responsible for installing `mdfetch` themselves. The CLI should detect the missing binary on startup and print a clear install hint.
```

**Step 3: "User Notification About External Dependencies" block (around lines 49–58)**

Rewrite the three-place notice so each mentions only mdfetch:

```markdown
#### User Notification About External Dependencies

Users must be told upfront that Find the Gaps installs and uses an external tool. Do this in three places:

1. **Homebrew `caveats`** (goreleaser `brews[].caveats`) — printed after `brew install`:
   > Find the Gaps shells out to `mdfetch` (docs ingestion). It was installed as a dependency of this formula.
2. **First-run banner** — on the first invocation (detected via the absence of `~/.find-the-gaps/config.toml` or equivalent), print a short notice naming `mdfetch` as a required external tool, where it came from, and how to verify it (`mdfetch --version`). Gate behind a `--quiet` / `FIND_THE_GAPS_QUIET=1` escape hatch.
3. **`find-the-gaps doctor`** — a subcommand that prints the detected version of mdfetch and exits non-zero if it is missing. Useful for users who installed via `go install` and for CI environments.

README installation docs must list `mdfetch` under a "What this installs" heading so users aren't surprised.
```

**Step 4: Stray commit example on line 351**

The line reads: `Examples: \`feat/analyze-subcommand\`, \`fix/rg-timeout-on-large-repos\`, \`chore/setup-project\`.`

Change `fix/rg-timeout-on-large-repos` to `fix/mdfetch-timeout-on-large-repos` so the example does not imply ripgrep is still a concern.

**Step 5: Grep to confirm CLAUDE.md is clean**

Run: `rg -n -i 'ripgrep|\brg\b' CLAUDE.md`

Expected: no matches.

**Step 6: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude): remove ripgrep from tech stack, distribution, and notices"
```

---

### Task 10: Update `.plans/VERIFICATION_PLAN.md`

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`

**Step 1: Prerequisites**

In the "Binaries on `$PATH`" list, delete the `rg` (ripgrep) bullet. Update the intro paragraph so it reads `real mdfetch binary` instead of `real ripgrep / mdfetch binaries`.

**Step 2: Scenario 5 (`find-the-gaps doctor`)**

Rewrite so only mdfetch is checked. Steps become:

1. With `mdfetch` on `$PATH`, run `find-the-gaps doctor`.
2. Temporarily make `mdfetch` unavailable.
3. Run `find-the-gaps doctor` again.
4. Restore `mdfetch` and run `find-the-gaps doctor` once more.

Success criteria adjusted to reference only mdfetch.

**Step 3: Scenario 6 (First-Run Banner)**

Change the success criterion that reads "prints the external-deps notice naming `ripgrep` and `mdfetch`" to `names mdfetch`.

**Step 4: Scenario 7 (Homebrew Install)**

Drop `ripgrep` from the preconditions bullet and the `brew install` success assertion, and drop the `rg --version` step. The scenario becomes:

1. On a clean machine, run `brew install <tap>/find-the-gaps`.
2. Observe brew's output.
3. Run `find-the-gaps --version`.
4. Run `mdfetch --version`.
5. Run `find-the-gaps doctor`.

Success criteria: mdfetch is installed as a formula dependency, caveats still print (content updated), `mdfetch --version` succeeds, `doctor` exits 0.

**Step 5: "Verification Rules" block**

Change the `real rg, real mdfetch` wording to `real mdfetch`.

**Step 6: Grep**

Run: `rg -n -i 'ripgrep|\brg\b' .plans/VERIFICATION_PLAN.md`

Expected: no matches.

**Step 7: Commit**

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "docs(plans): drop ripgrep from verification plan prerequisites and scenarios"
```

---

### Task 11: Final full-repo verification

**Step 1: Grep live surface**

Run: `rg -n -i 'ripgrep|\brg\b' -- cmd internal README.md CLAUDE.md .plans/VERIFICATION_PLAN.md Makefile`

Expected: no matches.

**Step 2: Historical files sanity check**

Run: `rg -l -i 'ripgrep|\brg\b' .plans | sort`

Expected: only the older dated/named plans listed in the "Files NOT to modify" section above. If `VERIFICATION_PLAN.md` appears, Task 10 was incomplete.

**Step 3: Full test suite**

Run: `go test ./...`

Expected: PASS.

**Step 4: Coverage**

Run: `go test -cover ./...`

Expected: every touched package ≥90% statement coverage.

**Step 5: Race detector**

Run: `go test -race ./...`

Expected: PASS, no race warnings.

**Step 6: Lint**

Run: `golangci-lint run`

Expected: clean.

**Step 7: Build**

Run: `go build ./...`

Expected: success.

**Step 8: Smoke test the CLI**

Run:

```bash
go build -o /tmp/ftg ./cmd/find-the-gaps
/tmp/ftg doctor --help
/tmp/ftg install-deps --help
/tmp/ftg --help
```

Expected: No `ripgrep` or stray ` rg ` token appears anywhere in the output.

**Step 9: Update `PROGRESS.md`**

Append a new entry at the bottom of `PROGRESS.md` following the existing format — do NOT rewrite earlier entries. Record:

- Task completed (remove ripgrep dependency)
- Tests: count before/after, all passing
- Coverage per package (`doctor`, `cli`) after change
- Build/lint status
- Timestamp (absolute date, e.g., `2026-04-23`)
- Notes: rationale (unused), files touched, files intentionally left untouched (historical plans, `PROGRESS.md` prior entries)

**Step 10: Commit**

```bash
git add PROGRESS.md
git commit -m "$(cat <<'EOF'
chore(progress): record ripgrep removal

- All live code and docs now mention only mdfetch as the external runtime dep
- Historical plans and prior PROGRESS entries left untouched
EOF
)"
```

**Step 11: Push and open a PR**

Push the branch and open a PR against `origin/main` with a merge-commit strategy. Title: `Remove ripgrep as a required dependency`. Body should summarize:

- Why: ripgrep was never shelled out to in the code.
- What changed: `RequiredTools`, CLI help, README, CLAUDE.md, VERIFICATION_PLAN, tests, testscript.
- What was intentionally NOT changed: PROGRESS.md prior entries, dated `.plans/*` files, `LLM_ANALYSIS_VERIFICATION_PLAN.md`, `CODEBASE_SCANNER_IMPLEMENTATION_PLAN.md`, `MDFETCH_SPIDER_IMPLEMENTATION_PLAN.md` — they are frozen historical artifacts.
- Test plan: `go test ./...`, `golangci-lint run`, `go build ./...`, CLI smoke tests.

---

## Done checklist

- [ ] README has a "Supported languages" section whose rows match `internal/scanner/lang` extractors
- [ ] `rg -n -i 'ripgrep|\brg\b' -- cmd internal README.md CLAUDE.md .plans/VERIFICATION_PLAN.md Makefile` returns zero matches
- [ ] `go test ./...` green
- [ ] `go test -cover ./...` shows ≥90% per touched package
- [ ] `golangci-lint run` clean
- [ ] `go build ./...` success
- [ ] `/tmp/ftg doctor --help`, `install-deps --help`, `--help` output free of ripgrep/rg
- [ ] `PROGRESS.md` entry appended (not rewritten)
- [ ] PR opened against `main` with merge-commit strategy
