# LLM Analysis Verification Plan

Validates that the LLM analysis pipeline works against a real codebase, a real documentation site, and a real LLM — end-to-end, no mocks, no fakes.

## Prerequisites

Before any scenario runs:

- **Binary built**: `go build -o find-the-gaps ./cmd/find-the-gaps` in the worktree root. The resulting binary must be on `$PATH` for the duration of testing.
- **`ANTHROPIC_API_KEY`**: a valid Anthropic key exported in the environment. All LLM calls in verification are real.
- **`rg` on `$PATH`**: `rg --version` must succeed.
- **`mdfetch` on `$PATH`**: `mdfetch --version` must succeed.
- **Fixture repo**: a checked-out Go project with at least 5 exported symbols and a corresponding public docs URL. Recommended: clone `github.com/charmbracelet/bubbles` at a known commit and use its docs at `https://pkg.go.dev/github.com/charmbracelet/bubbles`. Store the clone under `testdata/fixtures/bubbles/`.
- **Clean cache**: before every scenario that tests first-run or caching behavior, delete the project cache: `rm -rf .find-the-gaps/`.
- **Network**: the machine must reach the docs URL and the Anthropic API endpoint.

---

## Scenarios

### Scenario 1: AnalyzePage produces summary and features

**Context**: Verify `AnalyzePage` calls the real LLM and returns a non-empty summary and at least one feature for a real markdown file.

**Steps**:
1. Build and install the binary.
2. Run the integration test directly:
   ```
   ANTHROPIC_API_KEY=<key> go test -tags integration ./internal/analyzer/... -run TestBifrostClient_RealCompletion -v
   ```
3. Inspect the test output for the raw response.

**Success Criteria**:
- [ ] Test exits 0.
- [ ] The logged response is non-empty text (not an error message or JSON parse failure).
- [ ] No panic or timeout occurs within 30 seconds.

**If Blocked**: If the test fails with an auth error, verify `ANTHROPIC_API_KEY` is exported. If it times out, check network access to `api.anthropic.com`.

---

### Scenario 2: Full `analyze` command — no docs URL

**Context**: Verify the scan-only path (no `--docs-url`) still works after the LLM pipeline was wired in.

**Steps**:
1. Run:
   ```
   find-the-gaps analyze --repo testdata/fixtures/bubbles --cache-dir /tmp/ftg-verify
   ```
2. Capture stdout and exit code.

**Success Criteria**:
- [ ] Exit code is `0`.
- [ ] stdout contains `scanned N files` where N > 0.
- [ ] No mention of LLM errors or missing API key.

**If Blocked**: If the binary returns a non-zero exit code, run with the fixture repo swapped for `.` to rule out a fixture problem.

---

### Scenario 3: Full `analyze` command — LLM pipeline end-to-end

**Context**: Run the complete pipeline: scan → crawl → per-page analysis → product synthesis → feature mapping → reports.

**Steps**:
1. Delete any existing cache: `rm -rf /tmp/ftg-verify/bubbles`.
2. Run:
   ```
   find-the-gaps analyze \
     --repo testdata/fixtures/bubbles \
     --docs-url https://pkg.go.dev/github.com/charmbracelet/bubbles \
     --cache-dir /tmp/ftg-verify
   ```
3. Wait for the command to complete (may take 1–3 minutes for crawl + LLM calls).
4. Inspect stdout.
5. Inspect `/tmp/ftg-verify/bubbles/mapping.md`.
6. Inspect `/tmp/ftg-verify/bubbles/gaps.md`.

**Success Criteria**:
- [ ] Exit code is `0`.
- [ ] stdout contains `scanned N files, fetched M pages, K features mapped` where N > 0, M > 0, K > 0.
- [ ] stdout contains a path to `mapping.md` and `gaps.md`.
- [ ] `mapping.md` exists and contains a `## Product Summary` section with non-empty text.
- [ ] `mapping.md` contains at least one `### <feature>` entry with a `**Documented on:**` or `**Implemented in:**` line.
- [ ] `gaps.md` exists and contains both `## Undocumented Code` and `## Unmapped Features` sections.
- [ ] No entry in either report references a file path that does not exist in `testdata/fixtures/bubbles/`.
- [ ] The run completes within 5 minutes.

**If Blocked**: If the crawl returns 0 pages, verify `mdfetch` is installed and `mdfetch https://pkg.go.dev/github.com/charmbracelet/bubbles` succeeds standalone. If LLM calls fail, check `ANTHROPIC_API_KEY`.

---

### Scenario 4: Analysis cache prevents re-analysis on second run

**Context**: Verify that pages already analyzed are not re-sent to the LLM on a subsequent run.

**Steps**:
1. Complete Scenario 3 successfully (cache is populated).
2. Inspect `/tmp/ftg-verify/bubbles/docs/index.json` — note that page entries have `summary` fields.
3. Run the same `analyze` command again without `--no-cache`.
4. Time the second run.
5. Compare stdout from run 1 and run 2.

**Success Criteria**:
- [ ] Second run completes significantly faster than the first (no LLM calls for already-analyzed pages).
- [ ] `mapping.md` and `gaps.md` are regenerated and contain the same or equivalent content.
- [ ] `index.json` still contains `summary` fields — they were not cleared.

**If Blocked**: If the second run is the same speed as the first, the cache-hit path in `analyze.go` is not working. Inspect whether `idx.Analysis(url)` returns `ok == true` for crawled URLs by adding a temporary log line.

---

### Scenario 5: `--no-cache` forces re-analysis

**Context**: Verify that `--no-cache` causes the LLM to be called again even for already-analyzed pages.

**Steps**:
1. Complete Scenario 4 (cache is warm).
2. Note the current `fetched_at` timestamps in `index.json`.
3. Run:
   ```
   find-the-gaps analyze \
     --repo testdata/fixtures/bubbles \
     --docs-url https://pkg.go.dev/github.com/charmbracelet/bubbles \
     --cache-dir /tmp/ftg-verify \
     --no-cache
   ```
4. Inspect `index.json` after the run.

**Success Criteria**:
- [ ] The run takes as long as the first run (LLM is called for all pages).
- [ ] `index.json` page entries have updated `fetched_at` timestamps.
- [ ] `mapping.md` and `gaps.md` are regenerated.

**If Blocked**: If the run is still fast, the `--no-cache` flag is not clearing the analysis cache. Check that `noCache` is threaded through to the spider `Options` and the index-hit check.

---

### Scenario 6: Undocumented symbol appears in `gaps.md`

**Context**: Add a new exported Go function to the fixture repo that has no documentation equivalent, then verify it surfaces in the gap report.

**Steps**:
1. In `testdata/fixtures/bubbles/`, add a new exported function in an existing package:
   ```go
   // VerifyGapDetection is a synthetic symbol added to test gap detection.
   func VerifyGapDetection() string { return "gap" }
   ```
2. Save the file. Do NOT add documentation for this function anywhere.
3. Run:
   ```
   find-the-gaps analyze \
     --repo testdata/fixtures/bubbles \
     --docs-url https://pkg.go.dev/github.com/charmbracelet/bubbles \
     --cache-dir /tmp/ftg-verify \
     --no-cache
   ```
4. Inspect `gaps.md`.

**Success Criteria**:
- [ ] `gaps.md` contains an entry for `VerifyGapDetection`.
- [ ] The entry names the file path where the function was added.
- [ ] The entry is in the `## Undocumented Code` section.

**If Blocked**: If `VerifyGapDetection` is absent from `gaps.md`, check whether the scanner extracted it from the fixture file (`go test ./internal/scanner/... -run TestScan` on the fixture path). If the scanner misses it, the issue is in `internal/scanner/lang/`.

**Cleanup**: After the scenario, remove the synthetic function from the fixture.

---

### Scenario 7: Unmapped doc feature appears in `gaps.md`

**Context**: The LLM may extract doc features that have no corresponding code. Verify they surface in `gaps.md`.

**Steps**:
1. Inspect `gaps.md` from Scenario 3.
2. Find any entry in the `## Unmapped Features` section.
3. Pick one feature name from that section and search for it in the fixture codebase:
   ```
   rg "<feature name>" testdata/fixtures/bubbles/
   ```

**Success Criteria**:
- [ ] At least one `## Unmapped Features` entry exists.
- [ ] The ripgrep search for that entry name returns no direct symbol match (confirming the gap is real, not a false positive).

**If Blocked**: If `## Unmapped Features` is empty and `mapping.md` shows all features mapped to files, the fixture may be too well-covered. Switch to a fixture with richer documentation than code (e.g., a project with a roadmap page).

---

### Scenario 8: `mapping.md` cross-references are accurate

**Context**: Verify that file paths cited in `mapping.md` actually exist in the fixture repo, and that the feature→code links are plausible.

**Steps**:
1. Open `/tmp/ftg-verify/bubbles/mapping.md`.
2. For each `**Implemented in:**` entry, check that the listed file exists in `testdata/fixtures/bubbles/`.
3. For each `**Symbols:**` entry, check that the symbol is exported and exists in the listed file.

**Steps (automated)**:
```bash
# Extract file paths from mapping.md and check existence
grep '^\- \*\*Implemented in:\*\*' /tmp/ftg-verify/bubbles/mapping.md \
  | sed 's/.*\*\*Implemented in:\*\* //' \
  | tr ',' '\n' \
  | xargs -I{} test -f testdata/fixtures/bubbles/{}
echo "exit: $?"
```

**Success Criteria**:
- [ ] Every file path in `**Implemented in:**` lines exists in the fixture.
- [ ] Every symbol in `**Symbols:**` lines is findable via `rg <symbol> testdata/fixtures/bubbles/`.
- [ ] No entry says `nil` or `<nil>` (JSON null leaked into output).

**If Blocked**: If phantom file paths appear, the LLM hallucinated paths. This is a known LLM failure mode — the `MapFeaturesToCode` prompt may need tightening to constrain output to paths from the provided symbol list only. Stop and ask the developer before changing the prompt.

---

---

### Scenario 9: Local model via Ollama

**Context**: Verify the tool works end-to-end with a locally hosted Ollama model instead of a hosted API.

**Prerequisites for this scenario only:**
- Ollama installed and running: `ollama serve`
- A model pulled locally: `ollama pull llama3` (or another model of your choice)
- `ollama list` confirms the model is available

**Steps**:
1. Run:
   ```
   find-the-gaps analyze \
     --repo testdata/fixtures/bubbles \
     --docs-url https://pkg.go.dev/github.com/charmbracelet/bubbles \
     --cache-dir /tmp/ftg-verify-ollama \
     --llm-provider ollama \
     --llm-model llama3
   ```
2. Wait for the command to complete.
3. Inspect stdout, `mapping.md`, and `gaps.md`.

**Success Criteria**:
- [ ] Exit code is `0`.
- [ ] stdout contains `scanned N files, fetched M pages, K features mapped`.
- [ ] `mapping.md` and `gaps.md` are written and non-empty.
- [ ] No mention of `ANTHROPIC_API_KEY` in error output.
- [ ] The run completes within 10 minutes (local models are slower than hosted APIs).

**If Blocked**: If Ollama returns errors, run `ollama run llama3 "ping"` standalone to verify the model is working. If `--llm-provider` is unknown, the CLI flag was not registered — check `analyze.go`.

---

### Scenario 10: Local model via LM Studio

**Context**: Verify the tool works with LM Studio's local server.

**Prerequisites for this scenario only:**
- LM Studio installed with a model loaded and the local server started (default port 1234).
- The loaded model name is visible in the LM Studio UI → Local Server tab.

**Steps**:
1. Note the exact model identifier shown in LM Studio's Local Server tab (e.g., `lmstudio-community/Meta-Llama-3-8B-Instruct-GGUF`).
2. Run:
   ```
   find-the-gaps analyze \
     --repo testdata/fixtures/bubbles \
     --docs-url https://pkg.go.dev/github.com/charmbracelet/bubbles \
     --cache-dir /tmp/ftg-verify-lmstudio \
     --llm-provider lmstudio \
     --llm-model <model-identifier-from-step-1>
   ```
3. Inspect stdout, `mapping.md`, and `gaps.md`.

**Success Criteria**:
- [ ] Exit code is `0`.
- [ ] stdout contains `scanned N files, fetched M pages, K features mapped`.
- [ ] `mapping.md` and `gaps.md` are written and non-empty.
- [ ] The tool did not fall back to Anthropic (no `ANTHROPIC_API_KEY` used).

**If Blocked**: If the tool returns `connection refused`, verify LM Studio's local server is running (`curl http://localhost:1234/v1/models`). If `--llm-model` is missing, error message must tell the user to set it.

---

### Scenario 11: Missing `--llm-model` for lmstudio returns actionable error

**Context**: `lmstudio` has no sensible default model name. Verify the error message tells the user exactly what to do.

**Steps**:
1. Run:
   ```
   find-the-gaps analyze \
     --repo . \
     --docs-url https://example.com \
     --llm-provider lmstudio
   ```
2. Capture stderr.

**Success Criteria**:
- [ ] Exit code is non-zero.
- [ ] Error message mentions `--llm-model` and references the LM Studio Local Server tab.
- [ ] No panic occurs.

**If Blocked**: If the error message is generic or missing, the factory in `llm_client.go` is not returning the right error for missing model. Stop and ask the developer.

---

## Verification Rules

- **No mocks, ever.** All LLM calls, all crawl calls, all binary invocations are real.
- If any success criterion fails, verification fails — partial success is failure.
- If blocked, stop and document the exact observed output, then ask the developer. Do not guess, patch, or skip.
- Scenarios must be run in order — each builds on the cache state from the previous.
- All scenarios must pass before the `feat/llm-analysis` branch is merged.
