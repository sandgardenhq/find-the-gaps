# Bifrost Gateway Support — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `gateway` pseudo-provider to find-the-gaps so users can route LLM tier traffic through a self-hosted Bifrost gateway. The gateway's alias is the model name; find-the-gaps trusts the gateway to resolve the alias to a real provider+model.

**Architecture:** New `case "gateway"` in `NewBifrostClientWithProvider` (`internal/analyzer/bifrost_client.go`) and `buildTierClient` (`internal/cli/llm_client.go`), each mirroring the existing `groq` / `lmstudio` pattern (route through `schemas.OpenAI` with a custom `BaseURL`). Capabilities are static (`Vision=true, ToolUse=true, PromptCache=false`) registered via a single wildcard row in `knownModels` so the existing tier-validation pipeline picks it up unchanged.

**Tech Stack:** Go 1.26+, [Bifrost core SDK](https://github.com/maximhq/bifrost) v1.5.2 (already in `go.mod`), `testing` stdlib + `testify`, `testscript` for CLI integration.

**Reference:** Validated design at `.plans/2026-05-04-bifrost-gateway-support-design.md`. Read it first.

**Project conventions:** Read `CLAUDE.md` before starting. Hard rules summarized:

- TDD is mandatory. RED → GREEN → REFACTOR. No production line without a failing test first.
- Each commit must include the new test plus the minimal production change. Commit after every RED-GREEN cycle.
- Branch must use a type prefix (`feat/`, `fix/`, etc.). Suggested: `feat/bifrost-gateway`.
- Coverage target: ≥90% statement coverage per package (`go test -cover ./...`).
- Plans live under `.plans/` only.
- Format: `gofmt -w . && goimports -w .`. Lint: `golangci-lint run`.
- Build gate: `go build ./...` must succeed before any task is "done".
- Update `PROGRESS.md` after each task with timestamp, tests added, coverage.

---

## Pre-flight

### Task 0a: Branch and worktree

**Why:** The current workspace branch (`manila-v1`) doesn't follow the `feat/`-prefix convention. Implement on a fresh branch off `origin/main`.

**Steps:**

1. From a clean working tree on `origin/main`, create a worktree:
   ```bash
   git worktree add -b feat/bifrost-gateway ../find-the-gaps-bifrost-gateway origin/main
   cd ../find-the-gaps-bifrost-gateway
   ```
2. Copy both planning docs from the source workspace if not already on `main`:
   ```bash
   cp /Users/brittcrawford/conductor/workspaces/find-the-gaps/manila-v1/.plans/2026-05-04-bifrost-gateway-support-design.md .plans/
   cp /Users/brittcrawford/conductor/workspaces/find-the-gaps/manila-v1/.plans/2026-05-04-bifrost-gateway-support-plan.md .plans/
   git add .plans/2026-05-04-bifrost-gateway-support-design.md .plans/2026-05-04-bifrost-gateway-support-plan.md
   git commit -m "docs(plans): bring in Bifrost gateway design and plan"
   ```
3. Sanity check the baseline:
   ```bash
   go build ./...
   go test ./...
   ```
   Expected: build succeeds, all tests pass.

### Task 0b: Read these files end-to-end before writing any code

- `internal/analyzer/bifrost_client.go` — the file you'll modify in Task 1. Note the existing `groq` case at lines 144–148, the `bifrostAccount` plumbing (lines 68–121), and the `bifrostRequester` interface (line 33) that test doubles inject through.
- `internal/analyzer/bifrost_client_test.go` lines 94–242 — the existing groq/ollama/lmstudio coverage. Your new tests follow this style.
- `internal/cli/llm_client.go` — the `buildTierClient` switch at lines 100–166. Each provider follows the same shape: env lookup → `bifrostProvider` string → counter assignment.
- `internal/cli/capabilities.go` — `knownModels` registry and `ResolveCapabilities` lookup. Single source of truth for per-provider capability flags.
- `internal/cli/tier_validate.go` — `validateTierConfigs`. Wired to `knownProviders()` for the "valid: ..." error message.
- `internal/cli/tier_parse.go` — `parseTierString`. Splits on first `/`, so `gateway/team-x/alias-with-slashes` survives intact (model = `team-x/alias-with-slashes`).

---

## Task 1: Reject empty baseURL for `gateway` provider

**Why first:** Smallest possible RED — the new case doesn't exist yet, so calling `NewBifrostClientWithProvider("gateway", ...)` falls into the `default` branch and returns `unsupported Bifrost provider: "gateway"`. That's NOT the error we want to lock in. The test asserts the *gateway-specific* "requires a baseURL" message, which forces a real new case.

**Files:**
- Modify: `internal/analyzer/bifrost_client.go` (lines 132–159 — the `NewBifrostClientWithProvider` switch)
- Test: `internal/analyzer/bifrost_client_test.go` (append at the end of the file)

**Step 1: Write the failing test**

Append to `internal/analyzer/bifrost_client_test.go`:

```go
func TestNewBifrostClientWithProvider_Gateway_RequiresBaseURL(t *testing.T) {
	_, err := NewBifrostClientWithProvider("gateway", "fake-key", "cheap-tier", "", ModelCapabilities{})
	if err == nil {
		t.Fatal("expected error when baseURL is empty for gateway provider")
	}
	if !strings.Contains(err.Error(), "baseURL") {
		t.Fatalf("error should mention baseURL, got %v", err)
	}
}
```

**Step 2: Run the test and verify it fails for the right reason**

```bash
go test ./internal/analyzer/ -run TestNewBifrostClientWithProvider_Gateway_RequiresBaseURL -v
```

Expected: FAIL. The error message will say `unsupported Bifrost provider: "gateway"` (default branch), which does NOT contain "baseURL". Test fails on the `strings.Contains` check.

**Step 3: Add the gateway case**

In `internal/analyzer/bifrost_client.go`, add a new case to the switch starting at line 134 (between the existing `case "groq"` and `default`):

```go
case "gateway":
	provider = schemas.OpenAI
	if baseURL == "" {
		return nil, fmt.Errorf("gateway provider requires a baseURL")
	}
```

**Step 4: Run the test and verify it passes**

```bash
go test ./internal/analyzer/ -run TestNewBifrostClientWithProvider_Gateway_RequiresBaseURL -v
```

Expected: PASS.

**Step 5: Format, build, full-suite check**

```bash
gofmt -w . && goimports -w .
go build ./...
go test ./...
```

Expected: clean format, build succeeds, all tests pass.

**Step 6: Commit**

```bash
git add internal/analyzer/bifrost_client.go internal/analyzer/bifrost_client_test.go
git commit -m "feat(analyzer): reject empty baseURL for gateway provider

- RED: TestNewBifrostClientWithProvider_Gateway_RequiresBaseURL asserts
  the gateway-specific 'baseURL' error
- GREEN: add case \"gateway\" to NewBifrostClientWithProvider that
  routes through schemas.OpenAI and validates baseURL"
```

---

## Task 2: `NewBifrostClientWithProvider("gateway", ...)` returns a client routed through OpenAI

**Why:** Lock in that gateway construction succeeds with a non-empty `baseURL` and that the resulting client uses `schemas.OpenAI` as its underlying provider (so structured outputs and message rendering flow through the OpenAI lane).

**Files:**
- Test: `internal/analyzer/bifrost_client_test.go` (append)

**Step 1: Write the failing test**

```go
func TestNewBifrostClientWithProvider_Gateway_ReturnsClient(t *testing.T) {
	const baseURL = "http://gateway.local:8080"
	client, err := NewBifrostClientWithProvider("gateway", "gw-key", "cheap-tier", baseURL, ModelCapabilities{Vision: true, ToolUse: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.provider != schemas.OpenAI {
		t.Fatalf("expected provider schemas.OpenAI (gateway lane is OpenAI-compatible), got %q", client.provider)
	}
	if client.model != "cheap-tier" {
		t.Fatalf("expected model %q, got %q", "cheap-tier", client.model)
	}
}
```

**Step 2: Run the test**

```bash
go test ./internal/analyzer/ -run TestNewBifrostClientWithProvider_Gateway_ReturnsClient -v
```

Expected: PASS (Task 1 already wired the case correctly). This test exists to lock the contract.

If it FAILS: re-read your Task 1 change. Did you set `provider = schemas.OpenAI`? Did you fall through to the existing account/Init code below the switch? You should NOT have added a separate `bifrost.Init` call.

**Step 3: Commit**

```bash
git add internal/analyzer/bifrost_client_test.go
git commit -m "test(analyzer): lock gateway client routes through schemas.OpenAI"
```

---

## Task 3: Gateway path does not emit `cache_control` blocks

**Why:** Sending Anthropic-only `cache_control` to a gateway alias that resolves to (e.g.) GPT-4o is a wire-format error. The existing `cacheable` guard at `bifrost_client.go:226` already enforces this for non-Anthropic providers, but we want a regression test specifically named for the gateway path so future refactors can't accidentally turn caching on.

**Files:**
- Test: `internal/analyzer/bifrost_client_test.go` (append)

**Step 1: Write the failing test**

```go
func TestBifrostClient_Gateway_DoesNotEmitCacheControl(t *testing.T) {
	// Build a client through the gateway lane (schemas.OpenAI under the hood)
	// and feed it a CacheBreakpoint=true user message. The rendered Bifrost
	// messages must contain no CacheControl blocks — those are Anthropic-only
	// and the gateway alias is opaque.
	client := newBifrostClientWithFake(&fakeBifrostRequester{}, schemas.OpenAI, "cheap-tier")

	rendered := client.renderBifrostMessages([]ChatMessage{
		{Role: "user", Content: "hello", CacheBreakpoint: true},
	})

	require.Len(t, rendered, 1)
	require.NotNil(t, rendered[0].Content)
	if blocks := rendered[0].Content.ContentBlocks; len(blocks) > 0 {
		for _, b := range blocks {
			if b.CacheControl != nil {
				t.Fatalf("gateway lane must not emit CacheControl; got %+v", b.CacheControl)
			}
		}
	}
	// And the flat-string path is what we expect on the OpenAI lane.
	if rendered[0].Content.ContentStr == nil || *rendered[0].Content.ContentStr != "hello" {
		t.Fatalf("expected ContentStr=\"hello\", got %+v", rendered[0].Content)
	}
}
```

**Step 2: Run the test**

```bash
go test ./internal/analyzer/ -run TestBifrostClient_Gateway_DoesNotEmitCacheControl -v
```

Expected: PASS via reuse of the existing `cacheable` guard.

If it FAILS: read `bifrost_client.go:226`. The `cacheable := m.CacheBreakpoint && c.provider == schemas.Anthropic` line is what guarantees this. If the test fails, the guard logic was changed somewhere — fix the guard, do not weaken the test.

**Step 3: Commit**

```bash
git add internal/analyzer/bifrost_client_test.go
git commit -m "test(analyzer): lock gateway lane never emits Anthropic CacheControl"
```

---

## Task 4: Gateway path uses `response_format=json_schema` for structured outputs

**Why:** Locks in that `CompleteJSON` on a gateway client dispatches to `completeJSONOpenAIMessages` (line 409), not to the Anthropic forced-tool-use path. A gateway alias that resolves to Claude server-side will be translated by Bifrost; find-the-gaps must not pre-pick the Anthropic strategy.

**Files:**
- Test: `internal/analyzer/bifrost_client_test.go` (append)

**Step 1: Write the failing test**

```go
func TestBifrostClient_Gateway_CompleteJSON_UsesResponseFormat(t *testing.T) {
	// Stub a fake response that satisfies completeJSONOpenAIMessages: a single
	// choice with a ContentStr containing a JSON document conforming to schema.
	answer := `{"answer":"42"}`
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &answer}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "cheap-tier")

	schema := JSONSchema{
		Name: "TestAnswer",
		Doc: json.RawMessage(`{
			"type": "object",
			"properties": {"answer": {"type": "string"}},
			"required": ["answer"],
			"additionalProperties": false
		}`),
	}

	got, err := client.CompleteJSON(context.Background(), "what is the answer?", schema)
	require.NoError(t, err)
	require.JSONEq(t, answer, string(got))

	// Inspect the captured request: response_format must be set, tools must be empty.
	require.NotNil(t, fake.lastRequest, "fake should have captured the request")
	require.NotNil(t, fake.lastRequest.Params, "Params must be set")
	require.NotNil(t, fake.lastRequest.Params.ResponseFormat, "gateway lane must set ResponseFormat (json_schema)")
	if len(fake.lastRequest.Params.Tools) != 0 {
		t.Fatalf("gateway lane must NOT use forced-tool-use; got %d tool(s)", len(fake.lastRequest.Params.Tools))
	}

	// Optional: peek at the response_format value to confirm strict json_schema shape.
	rf, ok := (*fake.lastRequest.Params.ResponseFormat).(map[string]any)
	require.True(t, ok, "ResponseFormat must be a map[string]any")
	require.Equal(t, "json_schema", rf["type"])
	js, ok := rf["json_schema"].(map[string]any)
	require.True(t, ok, "json_schema field must be a map[string]any")
	require.Equal(t, true, js["strict"])
	require.Equal(t, "TestAnswer", js["name"])
}
```

**Step 2: Run the test**

```bash
go test ./internal/analyzer/ -run TestBifrostClient_Gateway_CompleteJSON_UsesResponseFormat -v
```

Expected: PASS via reuse — `completeJSONMessages` dispatches to `completeJSONOpenAIMessages` for `schemas.OpenAI`, which is what the gateway lane is.

If it FAILS: check `bifrost_client.go:394–403`. The `switch c.provider` must route `schemas.OpenAI` to `completeJSONOpenAIMessages`. If the test fails, that dispatch was changed — fix it, do not weaken the test.

**Step 3: Commit**

```bash
git add internal/analyzer/bifrost_client_test.go
git commit -m "test(analyzer): lock gateway uses response_format json_schema"
```

---

## Task 5: Gateway path passes image content blocks unchanged

**Why:** Vision is the user's responsibility on the gateway path. The wire format must just pass `ContentBlockImageURL` through to `ChatContentBlockTypeImage`. This is what `renderContentBlocks` does today; the test guards against accidental gating.

**Files:**
- Test: `internal/analyzer/bifrost_client_test.go` (append)

**Step 1: Write the failing test**

```go
func TestBifrostClient_Gateway_RendersImageBlocks(t *testing.T) {
	client := newBifrostClientWithFake(&fakeBifrostRequester{}, schemas.OpenAI, "cheap-tier")

	rendered := client.renderBifrostMessages([]ChatMessage{
		{
			Role: "user",
			ContentBlocks: []ContentBlock{
				{Type: ContentBlockText, Text: "describe this:"},
				{Type: ContentBlockImageURL, ImageURL: "https://example.com/screenshot.png"},
			},
		},
	})

	require.Len(t, rendered, 1)
	require.NotNil(t, rendered[0].Content)
	blocks := rendered[0].Content.ContentBlocks
	require.Len(t, blocks, 2, "gateway lane must pass through both content blocks")

	require.Equal(t, schemas.ChatContentBlockTypeText, blocks[0].Type)
	require.NotNil(t, blocks[0].Text)
	require.Equal(t, "describe this:", *blocks[0].Text)

	require.Equal(t, schemas.ChatContentBlockTypeImage, blocks[1].Type)
	require.NotNil(t, blocks[1].ImageURLStruct)
	require.Equal(t, "https://example.com/screenshot.png", blocks[1].ImageURLStruct.URL)
}
```

**Step 2: Run the test**

```bash
go test ./internal/analyzer/ -run TestBifrostClient_Gateway_RendersImageBlocks -v
```

Expected: PASS via reuse of `renderContentBlocks`.

**Step 3: Commit**

```bash
git add internal/analyzer/bifrost_client_test.go
git commit -m "test(analyzer): lock gateway passes image blocks unchanged"
```

---

## Task 6: Register `gateway` in the capabilities registry

**Why:** Without this row, `ResolveCapabilities("gateway", anyAlias)` returns `(_, false)` and `validateTierConfigs` rejects the tier with "unknown provider". Adding a wildcard row gives gateway aliases the static caps we agreed on (Vision + ToolUse on, PromptCache implicit-off) and lets them flow through validation unchanged.

**Files:**
- Modify: `internal/cli/capabilities.go` (the `knownModels` slice, lines 24–46)
- Test: `internal/cli/capabilities_test.go`

**Step 1: Read the existing capabilities tests to match style**

```bash
cat internal/cli/capabilities_test.go
```

**Step 2: Write the failing tests**

Append to `internal/cli/capabilities_test.go`:

```go
func TestResolveCapabilities_Gateway_TrustsUser(t *testing.T) {
	// The gateway provider exposes opaque aliases; find-the-gaps trusts the
	// user that whichever model the gateway resolves the alias to is
	// vision-capable and tool-use-capable. Capabilities are static for any
	// alias name (wildcard row).
	caps, ok := ResolveCapabilities("gateway", "cheap-tier")
	if !ok {
		t.Fatal("gateway provider must be recognized")
	}
	if !caps.Vision {
		t.Error("gateway aliases must report Vision=true")
	}
	if !caps.ToolUse {
		t.Error("gateway aliases must report ToolUse=true")
	}
	if caps.MaxCompletionTokens != 0 {
		t.Errorf("gateway aliases must use the BifrostClient default cap (0); got %d", caps.MaxCompletionTokens)
	}
}

func TestResolveCapabilities_Gateway_AnyAliasName(t *testing.T) {
	// Wildcard match: every alias the user invents flows through.
	for _, alias := range []string{"cheap-tier", "balanced", "team-a/best", "with.dots"} {
		caps, ok := ResolveCapabilities("gateway", alias)
		if !ok {
			t.Errorf("alias %q must resolve via wildcard", alias)
		}
		if !caps.Vision || !caps.ToolUse {
			t.Errorf("alias %q must inherit static caps", alias)
		}
	}
}

func TestKnownProviders_IncludesGateway(t *testing.T) {
	// validateTierConfigs's "valid: ..." error message reads from knownProviders().
	// Gateway must appear so users see it as a valid choice when they typo a tier.
	got := knownProviders()
	for _, p := range got {
		if p == "gateway" {
			return
		}
	}
	t.Fatalf("gateway must be in knownProviders(); got %v", got)
}
```

**Step 3: Run the tests and verify they fail**

```bash
go test ./internal/cli/ -run "TestResolveCapabilities_Gateway|TestKnownProviders_IncludesGateway" -v
```

Expected: FAIL. `ResolveCapabilities("gateway", ...)` returns `(_, false)` because no row matches. `knownProviders()` doesn't include `gateway`.

**Step 4: Add the wildcard row**

In `internal/cli/capabilities.go`, append to the `knownModels` slice (insert after the `lmstudio` row, around line 45):

```go
	// Gateway aliases are opaque to find-the-gaps: the gateway resolves the
	// alias to a real provider+model server-side. We trust the user that the
	// model behind any alias is vision- and tool-use-capable. Wildcard match
	// covers every alias name.
	{Provider: "gateway", Model: "*", ToolUse: true, Vision: true},
```

**Step 5: Run the tests and verify they pass**

```bash
go test ./internal/cli/ -run "TestResolveCapabilities_Gateway|TestKnownProviders_IncludesGateway" -v
```

Expected: PASS.

**Step 6: Run the whole package — make sure no existing test regressed**

```bash
go test ./internal/cli/ -v
```

Expected: all PASS. (Notably `TestNewLLMTiering_RejectsUnknownProvider` and `TestBuildTierClient_*` still pass — they don't reference the gateway provider.)

**Step 7: Commit**

```bash
git add internal/cli/capabilities.go internal/cli/capabilities_test.go
git commit -m "feat(cli): register gateway capabilities (vision+tool-use, wildcard alias)

- RED: TestResolveCapabilities_Gateway_TrustsUser plus wildcard and
  knownProviders coverage
- GREEN: add {Provider: \"gateway\", Model: \"*\", ToolUse: true,
  Vision: true} row so any alias resolves with static caps"
```

---

## Task 7: `buildTierClient` requires `BIFROST_GATEWAY_URL`

**Why:** Mirrors the existing groq pattern (`llm_client.go:135–148`). Without `BIFROST_GATEWAY_URL`, configuration is unrecoverable — fail fast at tier construction.

**Files:**
- Modify: `internal/cli/llm_client.go` (the `buildTierClient` switch at lines 100–151)
- Test: `internal/cli/llm_tiering_test.go` (append)

**Step 1: Write the failing test**

Append to `internal/cli/llm_tiering_test.go`:

```go
func TestBuildTierClient_Gateway_MissingURL(t *testing.T) {
	t.Setenv("BIFROST_GATEWAY_URL", "")
	t.Setenv("BIFROST_GATEWAY_API_KEY", "")
	_, _, err := buildTierClient("gateway", "cheap-tier")
	if err == nil {
		t.Fatal("expected error when BIFROST_GATEWAY_URL is unset")
	}
	if !strings.Contains(err.Error(), "BIFROST_GATEWAY_URL") {
		t.Fatalf("error must name BIFROST_GATEWAY_URL; got %v", err)
	}
}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/cli/ -run TestBuildTierClient_Gateway_MissingURL -v
```

Expected: FAIL. The `default` branch in `buildTierClient` returns `unknown provider "gateway"`, which doesn't contain `BIFROST_GATEWAY_URL`.

**Step 3: Add the gateway case**

In `internal/cli/llm_client.go`, add a new case to the switch starting at line 106 (between the existing `case "groq"` and `default`):

```go
	case "gateway":
		baseURL = os.Getenv("BIFROST_GATEWAY_URL")
		if baseURL == "" {
			return nil, nil, fmt.Errorf("BIFROST_GATEWAY_URL not set")
		}
		// Optional. Empty allowed for unauthenticated gateways; the analyzer
		// substitutes localServerPlaceholderKey to satisfy Bifrost's empty-
		// key filter (see bifrost_client.go:78–93).
		apiKey = os.Getenv("BIFROST_GATEWAY_API_KEY")
		bifrostProvider = "gateway"
		// tiktoken is approximate for non-OpenAI families — the gateway alias
		// could resolve to anything. Same caveat as groq/ollama. The screenshot
		// prompt budget already includes a 1.2x drift margin to absorb this.
		counter = analyzer.NewTiktokenCounter()
```

**Step 4: Run the test and verify it passes**

```bash
go test ./internal/cli/ -run TestBuildTierClient_Gateway_MissingURL -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/llm_client.go internal/cli/llm_tiering_test.go
git commit -m "feat(cli): wire gateway tier — require BIFROST_GATEWAY_URL

- RED: TestBuildTierClient_Gateway_MissingURL asserts the env-var-named
  error
- GREEN: add case \"gateway\" in buildTierClient mirroring the groq/
  lmstudio shape — schemas.OpenAI lane via BifrostClient with custom
  BaseURL"
```

---

## Task 8: `buildTierClient` succeeds when only `BIFROST_GATEWAY_URL` is set (no API key)

**Why:** The Bifrost gateway can be deployed unauthenticated (e.g., behind a private network). The `bifrostAccount.GetKeysForProvider` placeholder logic at `bifrost_client.go:88–93` already supports this for the OpenAI lane.

**Files:**
- Test: `internal/cli/llm_tiering_test.go` (append)

**Step 1: Write the test**

```go
func TestBuildTierClient_Gateway_AllowsEmptyAPIKey(t *testing.T) {
	t.Setenv("BIFROST_GATEWAY_URL", "http://gateway.local:8080")
	t.Setenv("BIFROST_GATEWAY_API_KEY", "")
	client, counter, err := buildTierClient("gateway", "cheap-tier")
	if err != nil {
		t.Fatalf("unexpected error with empty API key: %v", err)
	}
	if client == nil || counter == nil {
		t.Fatal("gateway path must return non-nil client and counter")
	}
	if _, ok := client.(*analyzer.BifrostClient); !ok {
		t.Fatalf("gateway must be served by *analyzer.BifrostClient, got %T", client)
	}
}

func TestBuildTierClient_Gateway_PassesAPIKey(t *testing.T) {
	// The api key value is internal to the BifrostClient; we cannot easily
	// observe it from this test layer. This test exists to prove the path
	// builds and to lock the env-var name BIFROST_GATEWAY_API_KEY.
	t.Setenv("BIFROST_GATEWAY_URL", "http://gateway.local:8080")
	t.Setenv("BIFROST_GATEWAY_API_KEY", "real-gw-key")
	client, counter, err := buildTierClient("gateway", "cheap-tier")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || counter == nil {
		t.Fatal("gateway path must return non-nil client and counter")
	}
}
```

**Step 2: Run and verify**

```bash
go test ./internal/cli/ -run TestBuildTierClient_Gateway -v
```

Expected: PASS for both (Task 7 already wired this).

**Step 3: Commit**

```bash
git add internal/cli/llm_tiering_test.go
git commit -m "test(cli): lock gateway tier — empty API key allowed, key passes through"
```

---

## Task 9: Mixed-lane tier configurations build cleanly

**Why:** A user may keep their large tier on a direct-Anthropic key (cheaper for prompt-cached drift judging) while using a gateway alias for the small/typical tiers. Lock in that mixing works.

**Files:**
- Test: `internal/cli/llm_tiering_test.go` (append)

**Step 1: Write the test**

```go
func TestNewLLMTiering_MixedLanes_GatewayAndAnthropic(t *testing.T) {
	// Small + typical via gateway, large via direct Anthropic.
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	t.Setenv("BIFROST_GATEWAY_URL", "http://gateway.local:8080")
	t.Setenv("BIFROST_GATEWAY_API_KEY", "")
	tg, err := newLLMTiering(
		"gateway/cheap-tier",
		"gateway/balanced",
		"anthropic/claude-opus-4-7",
	)
	if err != nil {
		t.Fatalf("mixed-lane tier configuration must build: %v", err)
	}
	if tg.Small() == nil || tg.Typical() == nil || tg.Large() == nil {
		t.Fatal("all three clients must be non-nil")
	}
}

func TestNewLLMTiering_AllGateway(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("BIFROST_GATEWAY_URL", "http://gateway.local:8080")
	t.Setenv("BIFROST_GATEWAY_API_KEY", "")
	tg, err := newLLMTiering(
		"gateway/cheap-tier",
		"gateway/balanced",
		"gateway/best",
	)
	if err != nil {
		t.Fatalf("all-gateway configuration must build: %v", err)
	}
	if tg.Small() == nil || tg.Typical() == nil || tg.Large() == nil {
		t.Fatal("all three clients must be non-nil")
	}
}
```

**Step 2: Run and verify**

```bash
go test ./internal/cli/ -run TestNewLLMTiering -v
```

Expected: PASS for both new tests, all existing tests still pass.

If `TestNewLLMTiering_MixedLanes_GatewayAndAnthropic` FAILS with a tool-use error: that means the typical tier validation rejected `gateway/balanced`. Recheck Task 6 — the gateway row must have `ToolUse: true`.

**Step 3: Commit**

```bash
git add internal/cli/llm_tiering_test.go
git commit -m "test(cli): lock mixed-lane and all-gateway tier configurations"
```

---

## Task 10: Tier validation error message lists `gateway` for typos

**Why:** When a user typos `gatway/cheap-tier`, `validateTierConfigs` should hint at `gateway` as a valid provider. This is implicitly handled by Task 6's addition to `knownModels`, but a test makes it explicit.

**Files:**
- Test: `internal/cli/tier_validate_test.go` (append)

**Step 1: Read the existing tier_validate tests for style**

```bash
cat internal/cli/tier_validate_test.go
```

**Step 2: Write the test**

```go
func TestValidateTierConfigs_TypoedGateway_ListsGatewayInError(t *testing.T) {
	// The user mis-typed "gatway" → "gateway". The error should list "gateway"
	// in the "valid: ..." hint so the user can spot the missing letter.
	err := validateTierConfigs("gatway/cheap-tier", "", "")
	if err == nil {
		t.Fatal("expected error for typoed provider")
	}
	if !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("error message must hint at 'gateway' as a valid provider; got %v", err)
	}
}
```

**Step 3: Run and verify**

```bash
go test ./internal/cli/ -run TestValidateTierConfigs_TypoedGateway_ListsGatewayInError -v
```

Expected: PASS — the `knownProviders()` slice already contains `gateway` from Task 6.

**Step 4: Commit**

```bash
git add internal/cli/tier_validate_test.go
git commit -m "test(cli): lock 'gateway' appears in valid-provider error hint"
```

---

## Task 11: testscript scenario — CLI parsing and missing-env-var error

**Why:** End-to-end check that `find-the-gaps analyze --llm-small=gateway/cheap-tier` (without `BIFROST_GATEWAY_URL`) surfaces the env-var error to the user with a clear exit code, and that setting the env var lets the tier build.

**Files:**
- Test: `cmd/find-the-gaps/testdata/gateway_cli.txtar` (new)

**Step 1: Read existing testscript scenarios for style**

```bash
ls cmd/find-the-gaps/testdata/
cat cmd/find-the-gaps/testdata/$(ls cmd/find-the-gaps/testdata/ | head -1)
```

Pick one that exercises a tier-flag error (likely a small `*.txtar` covering `--llm-*` validation). Match its `unenv`/`env`/`stderr` patterns.

**Step 2: Write the new scenario**

Create `cmd/find-the-gaps/testdata/gateway_cli.txtar`:

```
# Gateway tier without BIFROST_GATEWAY_URL set: build error names the env var.
unenv BIFROST_GATEWAY_URL
unenv BIFROST_GATEWAY_API_KEY
env ANTHROPIC_API_KEY=fake-key

! find-the-gaps analyze --repo=. --docs-url=http://example.com --llm-small=gateway/cheap-tier
stderr 'BIFROST_GATEWAY_URL'

# Sanity: with the env var set, the tier flag is accepted and the build
# proceeds past tier construction. (Analysis itself will fail later because
# example.com isn't a real docs site, but that's downstream of what we're
# verifying here.)
env BIFROST_GATEWAY_URL=http://gateway.local:8080
! find-the-gaps analyze --repo=. --docs-url=http://example.com --llm-small=gateway/cheap-tier
! stderr 'BIFROST_GATEWAY_URL'
! stderr 'unknown provider'
```

**Step 3: Run the scenario**

```bash
go test ./cmd/find-the-gaps/ -run TestScript/gateway_cli -v
```

Expected: PASS. If the test runner can't find your new scenario, check the test file (probably `main_test.go` in the same dir) for the `testscript.Run` invocation — typically it picks up every `.txtar` in `testdata/` automatically.

If the second `! find-the-gaps` block fails with `BIFROST_GATEWAY_URL`: your env var didn't propagate. testscript's `env` line sets it for subsequent commands; double-check syntax.

**Step 4: Commit**

```bash
git add cmd/find-the-gaps/testdata/gateway_cli.txtar
git commit -m "test(cli): add testscript scenario for gateway tier env-var validation"
```

---

## Task 12: Verification plan — Scenario 14

**Why:** Per project rules, integration tests against the real Bifrost gateway live in `.plans/VERIFICATION_PLAN.md`. Mocks are forbidden.

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md` (append a new scenario)

**Step 1: Append Scenario 14**

Add at the end of `.plans/VERIFICATION_PLAN.md`, after Scenario 13:

```markdown

---

### Scenario 14: Bifrost Gateway

**Context**: Verifies that find-the-gaps routes traffic through a self-hosted Bifrost gateway when a tier uses the `gateway/<alias>` form. The gateway is the source of truth for which provider/model the alias resolves to. find-the-gaps trusts the user that the alias for the small tier is vision-capable. All sub-cases run against a real Bifrost gateway instance the developer is operating.

**Steps**:
1. **Sub-case (a) — Vision-capable alias.** On the gateway, configure an alias (e.g., `cheap-tier`) that resolves to a vision-capable Claude or GPT model. Set `BIFROST_GATEWAY_URL` and `BIFROST_GATEWAY_API_KEY`. Run:
   ```
   find-the-gaps analyze --repo ./testdata/fixtures/known-good \
     --docs-url https://<known-good-docs> \
     --llm-small=gateway/cheap-tier \
     --llm-typical=gateway/balanced \
     --llm-large=gateway/best \
     -v
   ```
   Inspect `<projectDir>/screenshots.md`, `<projectDir>/gaps.md`, and the per-page audit log.
2. **Sub-case (b) — Non-vision alias on small tier.** Reconfigure `cheap-tier` on the gateway to a non-vision text model. Re-run the same command. Inspect stderr and `screenshots.md`.
3. **Sub-case (c) — Mixed lanes.** Run with `--llm-small=gateway/cheap-tier` (gateway) and `--llm-large=anthropic/claude-opus-4-7` (direct). Inspect that both lanes produce output and that drift detection still applies prompt caching on the direct-Anthropic lane.
4. **Sub-case (d) — Missing env var.** Unset `BIFROST_GATEWAY_URL` and re-run any gateway-tier command. Capture stderr.

**Success Criteria**:
- [ ] **(a)** analyze exits `0`. `mapping.md`, `gaps.md`, and `screenshots.md` are produced. Findings reference real symbols and real docs pages. No Bifrost or gateway errors in stderr.
- [ ] **(a)** Audit log lines under `-v` show no `cache_write` / `cache_read` token usage on the gateway path (the OpenAI lane does not emit `cache_control`).
- [ ] **(b)** analyze exits `0`. `screenshots.md`'s `## Image Issues` section is empty or absent. Per-page logs show vision attempts that the gateway rejected, surfaced as per-page warnings — NOT a run-fatal error.
- [ ] **(c)** Both lanes succeed. Audit logs for the large-tier (Anthropic direct) calls show non-zero `cache_read` on repeated invocations within the 5-min cache TTL. Gateway-tier calls show zero cache tokens.
- [ ] **(d)** analyze exits non-zero. Stderr contains the literal string `BIFROST_GATEWAY_URL`. No partial output is written under `<projectDir>/`.

**If Blocked**: If sub-case (a) produces empty `gaps.md` against a fixture you know has drift, the gateway alias may be returning truncated output (default `max_tokens` cap on the alias's underlying model). Capture the audit log and the gateway's request log, then ask. Do NOT silently re-introduce client-side `MaxCompletionTokens` overrides — the gateway should be configured server-side.
```

**Step 2: No tests to run for this task — it's a manual acceptance plan.** Commit:

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "docs(verification): add Scenario 14 for Bifrost gateway"
```

---

## Task 13: README — document `gateway` tier and env vars

**Why:** Users need to find this. The README's installation / configuration section is the canonical landing.

**Files:**
- Modify: `README.md` (find the section that documents `--llm-*` flags and the `*_API_KEY` env vars; add `gateway` alongside)

**Step 1: Read the current README configuration section**

```bash
grep -n -A 5 -B 2 "BIFROST\|llm-small\|GROQ_API_KEY\|OLLAMA_BASE_URL" README.md | head -80
```

Identify the right place to insert. Typical locations: a "Configuration" or "Tiers" section.

**Step 2: Add the gateway documentation**

Append (or insert) a subsection like:

```markdown
#### Bifrost Gateway

If you run a self-hosted [Bifrost gateway](https://github.com/maximhq/bifrost), point any tier at it with the `gateway/` provider prefix and a gateway-side alias name:

    --llm-small=gateway/cheap-tier
    --llm-typical=gateway/balanced
    --llm-large=gateway/best

The gateway resolves the alias to a real provider+model. find-the-gaps stays out of that decision and trusts that the alias used for the small tier is vision-capable (it's used by the screenshot relevance pass).

| Env var | Required | Notes |
| --- | --- | --- |
| `BIFROST_GATEWAY_URL` | yes (when any tier uses `gateway/*`) | Must NOT include `/v1` — Bifrost's OpenAI handler appends `/v1/chat/completions` itself. Example: `http://gateway.local:8080`. |
| `BIFROST_GATEWAY_API_KEY` | optional | Empty allowed for unauthenticated gateways. |

Tiers can mix lanes — e.g., `--llm-small=anthropic/claude-haiku-4-5 --llm-large=gateway/best` keeps the small tier on direct Anthropic (so prompt caching applies) while routing the large tier through the gateway.
```

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document gateway tier and BIFROST_GATEWAY_* env vars"
```

---

## Task 14: PROGRESS.md update + final checks

**Why:** CLAUDE.md mandates `PROGRESS.md` updates. Also the final gate-check before opening a PR.

**Files:**
- Modify: `PROGRESS.md`

**Step 1: Run the full gate**

```bash
gofmt -w . && goimports -w .
golangci-lint run
go build ./...
go test ./...
go test -coverprofile=coverage.out ./internal/analyzer/ ./internal/cli/
go tool cover -func=coverage.out | tail -20
```

Expected: lint clean, build succeeds, all tests pass, coverage on the touched packages ≥90%.

If coverage on `internal/analyzer/` or `internal/cli/` dropped below 90%: identify which lines are uncovered (`go tool cover -html=coverage.out`) and add tests for them before continuing. Do NOT lower the threshold.

**Step 2: Append a `PROGRESS.md` entry**

```markdown
## Bifrost Gateway Support - COMPLETE
- Started: <YYYY-MM-DD HH:MM>
- Tests: <N> new passing, 0 failing
- Coverage: internal/analyzer/ X%, internal/cli/ Y%
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: <YYYY-MM-DD HH:MM>
- Notes: New `gateway` pseudo-provider; static caps via wildcard row;
  reuses OpenAI lane for wire format. Doctor extension and per-tier
  gateway URLs deferred (out of scope). Scenario 14 added to
  VERIFICATION_PLAN.md for manual acceptance.
```

**Step 3: Commit and push**

```bash
git add PROGRESS.md
git commit -m "docs: log Bifrost gateway support completion"
git push -u origin feat/bifrost-gateway
```

**Step 4: Open the PR (per CLAUDE.md PR rules — merge commit, no squash)**

```bash
gh pr create --base main --title "feat: Bifrost gateway support" --body "$(cat <<'EOF'
## Summary

Adds a `gateway` pseudo-provider so find-the-gaps can route LLM tier traffic through a self-hosted Bifrost gateway. Gateway-side aliases become tier model names (`--llm-small=gateway/cheap-tier`); find-the-gaps trusts the gateway to resolve aliases to real provider+model pairs.

Mechanically the same trick already used for Groq: route through `schemas.OpenAI` with a custom `BaseURL`. Capabilities for gateway aliases are static (`Vision=true, ToolUse=true, PromptCache=false`) registered via a wildcard row in `knownModels`.

## Test plan

- [ ] `go test ./...` clean
- [ ] `go build ./...` clean
- [ ] `golangci-lint run` clean
- [ ] testscript scenario `gateway_cli.txtar` exercises env-var validation
- [ ] Manual acceptance: `.plans/VERIFICATION_PLAN.md` Scenario 14 sub-cases (a)–(d) executed against the developer's running gateway

## Out of scope (explicit)

- `ftg doctor` LLM-side liveness check (separate concern, separate PR)
- Per-tier independent gateway URLs
- Server-side alias capability probing
- `--llm-mode=gateway-only` umbrella flag

Design: `.plans/2026-05-04-bifrost-gateway-support-design.md`
Plan: `.plans/2026-05-04-bifrost-gateway-support-plan.md`

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Recovery / If something goes wrong

- **Test fails at GREEN step that should pass via reuse (Tasks 2, 3, 4, 5):** something earlier in the codebase changed. Read the file path noted in the task ("If it FAILS:" hint) and check the indicated line range. Fix the underlying guard / dispatch — do NOT weaken the test.
- **`go test ./internal/cli/` regresses on existing tests after Task 6:** the new wildcard row may have changed the order of `knownProviders()`. Inspect the failing assertion — if it's an order-sensitive comparison on the providers slice, fix the test's matcher (e.g., use `assert.ElementsMatch`), not the code.
- **testscript can't find `gateway_cli.txtar`:** read `cmd/find-the-gaps/main_test.go` (or wherever `testscript.Run` is invoked). The `Dir` argument must point at `testdata/`.
- **Coverage drops below 90% on `internal/analyzer/`:** the new gateway case is small enough that it shouldn't move coverage measurably. If it does, the existing OpenAI-lane tests aren't running in `bifrost_client_test.go`. Re-check by running `go test -coverprofile=coverage.out -run TestBifrostClient ./internal/analyzer/` and inspecting `go tool cover -html=coverage.out`.
- **PR fails CI on goreleaser/homebrew:** the `gateway` provider does not introduce a new external runtime dependency. If goreleaser fails, it's unrelated to this PR — open a separate issue.

---

## Out of scope (explicit, do NOT add to this PR)

- `ftg doctor` extension for LLM-side reachability checks
- Per-tier independent gateway URLs (`--small-llm-gateway-url=...`)
- Server-side alias capability probing endpoint
- `--llm-mode=gateway-only` umbrella flag
- Gateway-side prompt-caching opt-in (`--gateway-cache-anthropic`)
