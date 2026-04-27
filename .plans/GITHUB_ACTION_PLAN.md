# GitHub Action Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a composite GitHub Action at the root of this repo that runs `find-the-gaps analyze` against the consuming repo, uploads the report as a workflow artifact, and (optionally) opens or updates a single tracking issue.

**Architecture:** `action.yml` at the repo root orchestrates a composite action. Pure-logic shell helpers live at `internal/action/scripts/` and are unit-tested via Go test wrappers using `os/exec`. `gh`-calling logic is exercised end-to-end by a self-test workflow that runs the action against this repo. No new third-party dependencies — `gh`, `curl`, `tar`, `npm`, and standard Go test tooling only.

**Tech Stack:** GitHub Actions composite action, bash, `gh` CLI, `actions/checkout@v6`, `actions/upload-artifact@v7`, Go 1.26+ for tests.

**Reference:** See [.plans/GITHUB_ACTION_DESIGN.md](GITHUB_ACTION_DESIGN.md) for the validated design.

---

## Project Facts (verified before plan)

- **Release asset naming:** `find-the-gaps_<vX.Y.Z>_<os>-<arch>.tar.gz` (note: hyphen between os and arch, version includes `v` prefix). See `.github/workflows/release.yml:76`.
- **Binary inside tarball:** named `ftg` (not `find-the-gaps`).
- **`mdfetch` install:** `npm install -g @sandgarden/mdfetch` (verified in `internal/doctor/install_test.go:17`).
- **Linux-amd64 only for v1 of the action.** The release workflow builds linux-amd64 too, so the asset will exist.
- **Latest external action versions (verified, Node 24):** `actions/checkout@v6.0.2`, `actions/upload-artifact@v7.0.1`.

---

## Task List Overview

1. Skeleton `action.yml` with declared inputs (validated by Go test).
2. `resolve-version.sh` — pure logic mapping `action_ref` → release tag.
3. `build-issue-body.sh` — pure logic assembling issue body from report files.
4. `update-issue.sh` — wrap `gh` calls (no unit tests; integration via self-test).
5. Wire `action.yml` to call scripts in order (install ftg, install mdfetch, run analyze, upload artifact, update issue).
6. Self-test workflow at `.github/workflows/action-self-test.yml`.
7. Example workflows under `docs/examples/`.
8. README section: "Use as a GitHub Action".
9. Add Scenario 10 to `.plans/VERIFICATION_PLAN.md`.

---

### Task 1: Skeleton `action.yml` with declared inputs

**Files:**
- Create: `action.yml`
- Create: `internal/action/manifest_test.go`
- Create: `internal/action/doc.go`

**Step 1: Write the failing test**

```go
// internal/action/manifest_test.go
package action

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestActionManifest_DeclaresExpectedInputs(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(repoRoot, "action.yml"))
	require.NoError(t, err, "action.yml must exist at repo root")

	var manifest struct {
		Name    string `yaml:"name"`
		Runs    struct {
			Using string `yaml:"using"`
		} `yaml:"runs"`
		Inputs map[string]struct {
			Required bool   `yaml:"required"`
			Default  string `yaml:"default"`
		} `yaml:"inputs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &manifest))

	require.Equal(t, "composite", manifest.Runs.Using, "must be a composite action")

	required := []string{"docs-url", "bifrost-api-key"}
	optional := []string{"create-issue", "skip-screenshot-check"}
	for _, k := range required {
		got, ok := manifest.Inputs[k]
		require.True(t, ok, "input %q missing", k)
		require.True(t, got.Required, "input %q must be required", k)
	}
	for _, k := range optional {
		_, ok := manifest.Inputs[k]
		require.True(t, ok, "input %q missing", k)
	}
}
```

**Step 2: Add `gopkg.in/yaml.v3` dependency**

```bash
go get gopkg.in/yaml.v3
```

**Step 3: Create `internal/action/doc.go`**

```go
// Package action contains test wrappers and helpers for the composite GitHub
// Action defined by action.yml at the repo root.
package action
```

**Step 4: Run test to verify it fails**

```bash
go test ./internal/action/... -run TestActionManifest -v
```

Expected: FAIL — `action.yml must exist at repo root`.

**Step 5: Create `action.yml`**

```yaml
name: Find the Gaps
description: Analyze a repository against its docs site to surface documentation gaps.
author: Sandgarden

inputs:
  docs-url:
    description: URL of the live documentation site to analyze against.
    required: true
  bifrost-api-key:
    description: Bifrost API key. Provide via `${{ secrets.BIFROST_API_KEY }}`.
    required: true
  create-issue:
    description: When 'true', open or update a tracking issue with findings.
    required: false
    default: 'true'
  skip-screenshot-check:
    description: When 'true', skip screenshot-gap detection.
    required: false
    default: 'false'

runs:
  using: composite
  steps:
    - name: Placeholder
      shell: bash
      run: echo "action.yml skeleton; steps wired in Task 5"
```

**Step 6: Run test to verify it passes**

```bash
go test ./internal/action/... -run TestActionManifest -v
```

Expected: PASS.

**Step 7: Commit**

```bash
git add action.yml internal/action/doc.go internal/action/manifest_test.go go.mod go.sum
git commit -m "feat(action): add action.yml skeleton with declared inputs

- RED: TestActionManifest_DeclaresExpectedInputs fails (no manifest)
- GREEN: skeleton action.yml with required/optional inputs
- Status: 1 test passing, build successful"
```

---

### Task 2: `resolve-version.sh` pure logic

**Files:**
- Create: `internal/action/scripts/resolve-version.sh`
- Create: `internal/action/resolve_version_test.go`

**Step 1: Write the failing test**

```go
// internal/action/resolve_version_test.go
package action

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runScript(t *testing.T, script string, args ...string) (stdout string, exitCode int) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{abs}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("script %s: %v\n%s", script, err, out.String())
	}
	return strings.TrimSpace(out.String()), exitCode
}

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{"semver tag", "v1.2.3", "v1.2.3"},
		{"semver with patch zero", "v0.1.0", "v0.1.0"},
		{"branch name falls back to latest", "main", "latest"},
		{"floating major falls back to latest", "v1", "latest"},
		{"empty ref falls back to latest", "", "latest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, code := runScript(t, "resolve-version.sh", tc.ref)
			if code != 0 {
				t.Fatalf("exit code %d, output %q", code, got)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/action/... -run TestResolveVersion -v
```

Expected: FAIL — script not found.

**Step 3: Create `internal/action/scripts/resolve-version.sh`**

```bash
#!/usr/bin/env bash
# Resolves the action's git ref to a downloadable release tag.
# Usage: resolve-version.sh <action_ref>
# Outputs:
#   - "vX.Y.Z" verbatim if the ref matches a full semver tag
#   - "latest" otherwise (branches, floating majors, empty)
set -euo pipefail

ref="${1:-}"

if [[ "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "$ref"
else
  echo "latest"
fi
```

```bash
chmod +x internal/action/scripts/resolve-version.sh
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/action/... -run TestResolveVersion -v
```

Expected: PASS (5 subtests).

**Step 5: Commit**

```bash
git add internal/action/scripts/resolve-version.sh internal/action/resolve_version_test.go
git commit -m "feat(action): resolve-version.sh maps action ref to release tag

- RED: TestResolveVersion fails (script missing)
- GREEN: regex-based mapping (full semver -> verbatim, else 'latest')
- Documented limitation: floating majors fall back to latest"
```

---

### Task 3: `build-issue-body.sh` pure logic

**Files:**
- Create: `internal/action/scripts/build-issue-body.sh`
- Create: `internal/action/build_issue_body_test.go`

**Step 1: Write the failing test**

```go
// internal/action/build_issue_body_test.go
package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildIssueBody_GapsOnly(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	if err := os.WriteFile(gaps, []byte("# Findings\n- foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUN_URL", "https://gh.example/run/1")
	t.Setenv("COMMIT_SHA", "abc123")
	t.Setenv("RUN_TIMESTAMP", "2026-04-24T12:00:00Z")

	out, code := runScript(t, "build-issue-body.sh", gaps, "/nonexistent/screenshots.md")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "https://gh.example/run/1") {
		t.Errorf("body missing run URL: %s", out)
	}
	if !strings.Contains(out, "abc123") {
		t.Errorf("body missing commit sha: %s", out)
	}
	if !strings.Contains(out, "# Findings") {
		t.Errorf("body missing gaps content: %s", out)
	}
	if strings.Contains(out, "Screenshot Gaps") {
		t.Errorf("body should NOT have screenshots section: %s", out)
	}
}

func TestBuildIssueBody_WithScreenshots(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	shots := filepath.Join(dir, "screenshots.md")
	_ = os.WriteFile(gaps, []byte("gaps content"), 0o644)
	_ = os.WriteFile(shots, []byte("shots content"), 0o644)
	t.Setenv("RUN_URL", "u")
	t.Setenv("COMMIT_SHA", "s")
	t.Setenv("RUN_TIMESTAMP", "t")

	out, code := runScript(t, "build-issue-body.sh", gaps, shots)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "## Screenshot Gaps") {
		t.Errorf("body missing screenshots heading: %s", out)
	}
	if !strings.Contains(out, "shots content") {
		t.Errorf("body missing screenshots content: %s", out)
	}
}

func TestBuildIssueBody_EmptyGapsFile(t *testing.T) {
	dir := t.TempDir()
	gaps := filepath.Join(dir, "gaps.md")
	_ = os.WriteFile(gaps, []byte(""), 0o644)
	t.Setenv("RUN_URL", "u")
	t.Setenv("COMMIT_SHA", "s")
	t.Setenv("RUN_TIMESTAMP", "t")

	out, code := runScript(t, "build-issue-body.sh", gaps, "/nope")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "u") {
		t.Errorf("body missing run URL: %s", out)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/action/... -run TestBuildIssueBody -v
```

Expected: FAIL — script not found.

**Step 3: Create `internal/action/scripts/build-issue-body.sh`**

```bash
#!/usr/bin/env bash
# Builds the markdown body for the tracking issue.
# Usage: build-issue-body.sh <gaps_path> <screenshots_path>
# Reads env: RUN_URL, COMMIT_SHA, RUN_TIMESTAMP
# Outputs: full markdown body to stdout.
set -euo pipefail

gaps_path="${1:-}"
shots_path="${2:-}"

cat <<EOF
> Generated by [find-the-gaps]($RUN_URL) at $RUN_TIMESTAMP for commit \`$COMMIT_SHA\`.

EOF

if [[ -f "$gaps_path" ]]; then
  cat "$gaps_path"
fi

if [[ -f "$shots_path" ]]; then
  echo
  echo "## Screenshot Gaps"
  echo
  cat "$shots_path"
fi
```

```bash
chmod +x internal/action/scripts/build-issue-body.sh
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/action/... -run TestBuildIssueBody -v
```

Expected: PASS (3 subtests).

**Step 5: Commit**

```bash
git add internal/action/scripts/build-issue-body.sh internal/action/build_issue_body_test.go
git commit -m "feat(action): build-issue-body.sh assembles tracking-issue body

- RED: TestBuildIssueBody_* fail (script missing)
- GREEN: header + gaps.md + optional screenshots.md
- Env-driven metadata (RUN_URL, COMMIT_SHA, RUN_TIMESTAMP)"
```

---

### Task 4: `update-issue.sh` (gh wrapper, no unit tests)

**Files:**
- Create: `internal/action/scripts/update-issue.sh`

**Note:** This script calls `gh` against a real repo. Per `.plans/VERIFICATION_PLAN.md` the project does not mock external services, so this script is exercised only by Task 6's self-test workflow and by Scenario 10 verification.

**Step 1: Create `internal/action/scripts/update-issue.sh`**

```bash
#!/usr/bin/env bash
# Opens or updates a single tracking issue with the latest findings.
# Behavior:
#   - Search OPEN issues with label `find-the-gaps`.
#   - If one exists: edit body. If findings empty, post a "no gaps" comment
#     instead of editing the body, and do not close the issue.
#   - If none exists AND findings non-empty: create a new issue.
#   - Closed issues are never reopened.
# Usage: update-issue.sh <body_file> <findings_present>
# Env: GH_TOKEN, GITHUB_REPOSITORY
set -euo pipefail

body_file="${1:?body file required}"
findings_present="${2:?true/false required}"

label="find-the-gaps"
title="Documentation gaps detected by find-the-gaps"

existing=$(gh issue list --repo "$GITHUB_REPOSITORY" --state open --label "$label" --json number --jq '.[0].number // empty')

if [[ -n "$existing" ]]; then
  if [[ "$findings_present" == "true" ]]; then
    gh issue edit "$existing" --repo "$GITHUB_REPOSITORY" --body-file "$body_file"
    echo "Updated issue #$existing"
  else
    gh issue comment "$existing" --repo "$GITHUB_REPOSITORY" --body "Latest run found no gaps."
    echo "Commented on issue #$existing (no findings)"
  fi
else
  if [[ "$findings_present" == "true" ]]; then
    gh issue create --repo "$GITHUB_REPOSITORY" --title "$title" --label "$label" --body-file "$body_file"
    echo "Created tracking issue"
  else
    echo "No findings and no existing issue; nothing to do"
  fi
fi
```

```bash
chmod +x internal/action/scripts/update-issue.sh
```

**Step 2: Verify it parses**

```bash
bash -n internal/action/scripts/update-issue.sh
```

Expected: exit 0 (syntax check passes).

**Step 3: Commit**

```bash
git add internal/action/scripts/update-issue.sh
git commit -m "feat(action): update-issue.sh manages single tracking issue via gh

- One open issue at a time; never reopen closed ones
- Empty findings -> comment, do not auto-close
- Integration tested via self-test workflow (no mocks per VERIFICATION_PLAN)"
```

---

### Task 5: Wire `action.yml` to call all scripts

**Files:**
- Modify: `action.yml`
- Create: `internal/action/scripts/install-binary.sh`
- Modify: `internal/action/manifest_test.go` (add assertions for step count and key step names)

**Step 1: Extend the manifest test**

Add to `internal/action/manifest_test.go`:

```go
func TestActionManifest_StepsWired(t *testing.T) {
	repoRoot, _ := filepath.Abs("../..")
	data, _ := os.ReadFile(filepath.Join(repoRoot, "action.yml"))

	var manifest struct {
		Runs struct {
			Steps []struct {
				Name string `yaml:"name"`
				If   string `yaml:"if"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &manifest))

	names := []string{}
	for _, s := range manifest.Runs.Steps {
		names = append(names, s.Name)
	}
	for _, want := range []string{
		"Resolve find-the-gaps version",
		"Install find-the-gaps",
		"Install mdfetch",
		"Run analyze",
		"Upload report artifact",
		"Update tracking issue",
	} {
		require.Contains(t, names, want, "missing step %q", want)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/action/... -run TestActionManifest_StepsWired -v
```

Expected: FAIL — placeholder step is the only one present.

**Step 3: Create `internal/action/scripts/install-binary.sh`**

```bash
#!/usr/bin/env bash
# Downloads + extracts a tarball release asset to a target dir.
# Usage: install-binary.sh <download_url> <dest_dir>
set -euo pipefail
url="${1:?download url required}"
dest="${2:?dest dir required}"
mkdir -p "$dest"
curl --fail --silent --show-error --location "$url" | tar -xz -C "$dest"
```

```bash
chmod +x internal/action/scripts/install-binary.sh
```

**Step 4: Replace `action.yml` runs block**

```yaml
runs:
  using: composite
  steps:
    - name: Verify Linux runner
      shell: bash
      run: |
        if [[ "${RUNNER_OS}" != "Linux" ]]; then
          echo "::error::find-the-gaps action supports linux runners only (got ${RUNNER_OS})"
          exit 1
        fi

    - name: Verify Bifrost API key
      shell: bash
      env:
        BIFROST_API_KEY: ${{ inputs.bifrost-api-key }}
      run: |
        if [[ -z "${BIFROST_API_KEY}" ]]; then
          echo "::error::bifrost-api-key input is empty; provide via secrets.BIFROST_API_KEY"
          exit 1
        fi

    - name: Resolve find-the-gaps version
      id: ftg-version
      shell: bash
      env:
        ACTION_REF: ${{ github.action_ref }}
      run: |
        version=$("${GITHUB_ACTION_PATH}/internal/action/scripts/resolve-version.sh" "${ACTION_REF}")
        echo "Resolved version: ${version}"
        echo "version=${version}" >> "${GITHUB_OUTPUT}"

    - name: Install find-the-gaps
      shell: bash
      env:
        VERSION: ${{ steps.ftg-version.outputs.version }}
      run: |
        if [[ "${VERSION}" == "latest" ]]; then
          tag=$(gh release view --repo sandgardenhq/find-the-gaps --json tagName --jq .tagName)
        else
          tag="${VERSION}"
        fi
        url="https://github.com/sandgardenhq/find-the-gaps/releases/download/${tag}/find-the-gaps_${tag}_linux-amd64.tar.gz"
        "${GITHUB_ACTION_PATH}/internal/action/scripts/install-binary.sh" "${url}" "${RUNNER_TEMP}/ftg"
        echo "${RUNNER_TEMP}/ftg" >> "${GITHUB_PATH}"
      env:
        GH_TOKEN: ${{ github.token }}

    - name: Install mdfetch
      shell: bash
      run: npm install -g @sandgarden/mdfetch

    - name: Run analyze
      id: analyze
      shell: bash
      env:
        DOCS_URL: ${{ inputs.docs-url }}
        BIFROST_API_KEY: ${{ inputs.bifrost-api-key }}
        SKIP_SHOTS: ${{ inputs.skip-screenshot-check }}
      run: |
        out_dir="${RUNNER_TEMP}/ftg-report"
        mkdir -p "${out_dir}"
        flags=()
        if [[ "${SKIP_SHOTS}" == "true" ]]; then flags+=("--skip-screenshot-check"); fi
        ftg analyze --repo . --docs-url "${DOCS_URL}" --output "${out_dir}" "${flags[@]}"
        echo "out_dir=${out_dir}" >> "${GITHUB_OUTPUT}"
        if [[ -s "${out_dir}/gaps.md" || -s "${out_dir}/screenshots.md" ]]; then
          echo "findings=true" >> "${GITHUB_OUTPUT}"
        else
          echo "findings=false" >> "${GITHUB_OUTPUT}"
        fi

    - name: Upload report artifact
      if: always()
      uses: actions/upload-artifact@v7
      with:
        name: find-the-gaps-report-${{ github.run_id }}
        path: ${{ steps.analyze.outputs.out_dir }}
        if-no-files-found: warn

    - name: Build issue body
      id: body
      if: ${{ inputs.create-issue == 'true' }}
      shell: bash
      env:
        RUN_URL: ${{ format('{0}/{1}/actions/runs/{2}', github.server_url, github.repository, github.run_id) }}
        COMMIT_SHA: ${{ github.sha }}
        RUN_TIMESTAMP: ${{ github.event.repository.updated_at }}
      run: |
        body_file="${RUNNER_TEMP}/ftg-issue-body.md"
        "${GITHUB_ACTION_PATH}/internal/action/scripts/build-issue-body.sh" \
          "${{ steps.analyze.outputs.out_dir }}/gaps.md" \
          "${{ steps.analyze.outputs.out_dir }}/screenshots.md" \
          > "${body_file}"
        echo "body_file=${body_file}" >> "${GITHUB_OUTPUT}"

    - name: Update tracking issue
      if: ${{ inputs.create-issue == 'true' }}
      shell: bash
      env:
        GH_TOKEN: ${{ github.token }}
        GITHUB_REPOSITORY: ${{ github.repository }}
        FINDINGS: ${{ steps.analyze.outputs.findings }}
      run: |
        "${GITHUB_ACTION_PATH}/internal/action/scripts/update-issue.sh" \
          "${{ steps.body.outputs.body_file }}" \
          "${FINDINGS}"
```

**Note for implementer:** Verify `ftg analyze` actually accepts `--output` for the report directory. If the flag is named differently, adjust both `action.yml` and the design doc.

**Step 5: Run test to verify it passes**

```bash
go test ./internal/action/... -v
```

Expected: PASS (all manifest + script tests).

**Step 6: Commit**

```bash
git add action.yml internal/action/scripts/install-binary.sh internal/action/manifest_test.go
git commit -m "feat(action): wire composite steps in action.yml

- RED: TestActionManifest_StepsWired fails
- GREEN: 8 steps (linux check, key check, resolve version, install ftg,
  install mdfetch, analyze, upload artifact, update issue)
- Reuses install-binary.sh for ftg download
- Status: all action tests passing"
```

---

### Task 6: Self-test workflow

**Files:**
- Create: `.github/workflows/action-self-test.yml`

This workflow exercises the action against this repo on every PR that touches the action surface.

**Step 1: Create the workflow**

```yaml
name: Action self-test
on:
  pull_request:
    paths:
      - 'action.yml'
      - 'internal/action/**'
      - '.github/workflows/action-self-test.yml'
  workflow_dispatch:

jobs:
  self-test:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      issues: write
    steps:
      - uses: actions/checkout@v6
      - name: Run the action against this repo's checkout
        uses: ./
        with:
          docs-url: ${{ vars.SELF_TEST_DOCS_URL }}
          bifrost-api-key: ${{ secrets.BIFROST_API_KEY }}
          create-issue: 'false'   # don't spam the issues tracker on every PR
          skip-screenshot-check: 'true'
```

**Step 2: Sanity-check workflow syntax**

```bash
gh workflow view action-self-test.yml --repo sandgardenhq/find-the-gaps 2>/dev/null || true
# locally:
yamllint .github/workflows/action-self-test.yml
```

(yamllint is optional — the real validation happens when GitHub parses the file.)

**Step 3: Commit**

```bash
git add .github/workflows/action-self-test.yml
git commit -m "ci: add action self-test workflow

- Triggers on PRs touching action.yml or internal/action/**
- Calls ./ (the local action) with create-issue=false to avoid issue spam
- Requires repo-level vars.SELF_TEST_DOCS_URL and secrets.BIFROST_API_KEY"
```

**Step 4: Out-of-band setup (manual, document in README)**

After merging:
1. Set repo variable `SELF_TEST_DOCS_URL` (Settings → Secrets and variables → Actions → Variables) to a real docs URL for self-test.
2. Confirm `BIFROST_API_KEY` secret is set at repo level.

---

### Task 7: Example workflows

**Files:**
- Create: `docs/examples/schedule.yml`
- Create: `docs/examples/release.yml`
- Create: `docs/examples/manual.yml`

**Step 1: Schedule example**

```yaml
# docs/examples/schedule.yml
# Nightly drift audit. Opens or updates a tracking issue when gaps are detected.
name: Docs drift
on:
  schedule:
    - cron: '0 7 * * *'   # 07:00 UTC daily
  workflow_dispatch:

jobs:
  audit:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      issues: write
    steps:
      - uses: actions/checkout@v6
      - uses: sandgardenhq/find-the-gaps@v1
        with:
          docs-url: https://docs.example.com
          bifrost-api-key: ${{ secrets.BIFROST_API_KEY }}
```

**Step 2: Release example**

```yaml
# docs/examples/release.yml
# Audit on every version tag; artifact only, no issue.
name: Pre-release docs check
on:
  push:
    tags:
      - 'v*'

jobs:
  audit:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@v6
      - uses: sandgardenhq/find-the-gaps@v1
        with:
          docs-url: https://docs.example.com
          bifrost-api-key: ${{ secrets.BIFROST_API_KEY }}
          create-issue: 'false'
```

**Step 3: Manual example**

```yaml
# docs/examples/manual.yml
# Maintainer-triggered docs audit.
name: Docs audit (manual)
on:
  workflow_dispatch:

jobs:
  audit:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      issues: write
    steps:
      - uses: actions/checkout@v6
      - uses: sandgardenhq/find-the-gaps@v1
        with:
          docs-url: https://docs.example.com
          bifrost-api-key: ${{ secrets.BIFROST_API_KEY }}
```

**Step 4: Commit**

```bash
git add docs/examples/*.yml
git commit -m "docs(examples): add schedule, release, and manual workflow examples"
```

---

### Task 8: README section

**Files:**
- Modify: `README.md`

**Step 1: Append a "Use as a GitHub Action" section to README.md**

```markdown
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
    bifrost-api-key: ${{ secrets.BIFROST_API_KEY }}
```

### Inputs

| Name | Required | Default | Description |
|---|---|---|---|
| `docs-url` | yes | — | URL of the live documentation site |
| `bifrost-api-key` | yes | — | Bifrost API key (use a repo secret) |
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
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): document use as a GitHub Action"
```

---

### Task 9: Add Scenario 10 to VERIFICATION_PLAN.md

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`

**Step 1: Append Scenario 10**

Append the following before the "Verification Rules" section:

```markdown
### Scenario 10: GitHub Action

**Context**: Verify the composite action installs binaries, runs analysis, uploads the artifact, and manages the tracking issue per spec.

**Steps**:
1. Tag a release of this repo (or use the most recent published tag).
2. In a fixture GitHub repository, add a workflow that calls `sandgardenhq/find-the-gaps@<tag>` with a real `docs-url`, `BIFROST_API_KEY` secret, `create-issue: 'true'`.
3. Trigger the workflow via `workflow_dispatch`.
4. Wait for completion. Inspect: artifact, issues tab, run logs.
5. Manually edit the fixture repo to introduce a new exported function (mirrors Scenario 2).
6. Re-trigger the workflow.
7. Inspect the issue (should be edited, not duplicated).
8. Close the issue manually.
9. Re-trigger the workflow.
10. Inspect: a new issue should NOT be created if findings are unchanged from step 7.
11. Re-run with `create-issue: 'false'`. Confirm artifact only.

**Success Criteria**:
- [ ] Step 4: artifact `find-the-gaps-report-<run_id>` is uploaded; issue exists with label `find-the-gaps` and the expected title; run exits `0`.
- [ ] Step 7: same issue number as step 4, body updated to reflect step-5 change.
- [ ] Step 10: no new issue created — closed issues stay dismissed.
- [ ] Step 11: no issue created or modified; only the artifact is produced.

**If Blocked**: If the action fails to download the release binary, capture the URL it tried and ask the developer — the asset-naming convention may have drifted.
```

**Step 2: Commit**

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "docs(verification): add Scenario 10 for the GitHub Action

- Tagged-release usage, issue update, closed-issue dismissal, artifact-only mode"
```

---

## Final checks before opening PR

```bash
go test ./...
go build ./...
golangci-lint run
gofmt -l .
```

All four must come back clean (no failures, no output from `gofmt -l`, no lint errors). Then open the PR per CLAUDE.md (merge commit, link the design doc, reference Scenario 10 as the verification gate).

```bash
gh pr create --base main --title "feat: ship find-the-gaps as a GitHub Action" --body "$(cat <<'EOF'
## Summary
- Composite GitHub Action at `action.yml` runs `find-the-gaps analyze` against the consuming repo.
- Pure-logic shell helpers under `internal/action/scripts/` with Go test wrappers.
- Self-test workflow exercises the action end-to-end on PRs.
- Examples + README + Scenario 10 in VERIFICATION_PLAN.

Design: `.plans/GITHUB_ACTION_DESIGN.md`
Plan: `.plans/GITHUB_ACTION_PLAN.md`

## Test plan
- [ ] Unit: `go test ./internal/action/...` (manifest, version resolver, body builder)
- [ ] CI: action self-test workflow passes on this PR
- [ ] Verification Scenario 10 (post-tag) — see `.plans/VERIFICATION_PLAN.md`
EOF
)"
```
