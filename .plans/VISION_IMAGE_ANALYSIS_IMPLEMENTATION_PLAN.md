# Vision-Aware Screenshot Analysis — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add vision-capable screenshot analysis to Find the Gaps so the screenshot-detection pass can (a) flag images whose content does not match the surrounding prose, and (b) suppress missing-screenshot suggestions when an existing image already covers the moment. Also add Groq as a new provider.

**Architecture:** Replace the flat provider whitelist with a per-model capability registry. Extend `ChatMessage` with image content blocks. Branch `DetectScreenshotGaps` on `client.Capabilities().Vision`: vision-on flow makes one or more **batched relevance vision calls** (≤5 images each) followed by **one verdict-enriched detection text call**; vision-off flow is unchanged. Findings render in `screenshots.md` with a new `## Image Issues` section.

**Tech Stack:** Go 1.26+, testify, testscript, golangci-lint. Bifrost SDK for LLM transport — Groq routes through Bifrost's OpenAI provider with a custom `BaseURL`. No new deps.

**Source design:** [`.plans/VISION_IMAGE_ANALYSIS_DESIGN.md`](./VISION_IMAGE_ANALYSIS_DESIGN.md).

---

## Engineer Onboarding

Before starting, skim:

- [`CLAUDE.md`](../CLAUDE.md) — TDD iron law (RED → GREEN → REFACTOR), 90% statement coverage, commit after every cycle, SCREAMING_SNAKE_CASE plans.
- [`internal/analyzer/screenshot_gaps.go`](../internal/analyzer/screenshot_gaps.go) — current text-only screenshot detector you'll be extending.
- [`internal/analyzer/bifrost_client.go`](../internal/analyzer/bifrost_client.go) lines 110–200 — provider switch and `completeOneTurn` you'll be extending for image content blocks.
- [`internal/cli/tier_validate.go`](../internal/cli/tier_validate.go) and [`internal/cli/llm_client.go`](../internal/cli/llm_client.go) — provider validation and tier construction you'll be replacing.

**Per-task discipline (the iron law restated):**
1. Write the failing test first.
2. Run it; confirm it fails for the *right* reason.
3. Write the minimal code to pass.
4. Run; confirm green.
5. Refactor with tests staying green.
6. Run `go test ./...`, `go build ./...`, `golangci-lint run`. All must be clean.
7. Commit with the format from CLAUDE.md.

Never skip steps 1–2. If you wrote production code first, delete it and start over.

---

## Task 1 — Per-Model Capability Registry

**Files:**
- Create: `internal/cli/capabilities.go`
- Create: `internal/cli/capabilities_test.go`

### Step 1.1: RED — write the failing test

Create `internal/cli/capabilities_test.go`:

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveCapabilities_ExactMatchWins(t *testing.T) {
	caps, ok := ResolveCapabilities("anthropic", "claude-haiku-4-5")
	assert.True(t, ok)
	assert.True(t, caps.ToolUse)
	assert.True(t, caps.Vision)
}

func TestResolveCapabilities_WildcardForSelfHosted(t *testing.T) {
	caps, ok := ResolveCapabilities("ollama", "anything-goes")
	assert.True(t, ok)
	assert.False(t, caps.ToolUse)
	assert.False(t, caps.Vision)
}

func TestResolveCapabilities_UnknownProviderReturnsFalse(t *testing.T) {
	_, ok := ResolveCapabilities("not-a-provider", "anything")
	assert.False(t, ok)
}

func TestResolveCapabilities_UnknownModelOnKnownProviderReturnsZero(t *testing.T) {
	caps, ok := ResolveCapabilities("anthropic", "claude-future-9-9")
	assert.True(t, ok)
	assert.False(t, caps.ToolUse)
	assert.False(t, caps.Vision)
}

func TestResolveCapabilities_GroqVisionModel(t *testing.T) {
	caps, ok := ResolveCapabilities("groq", "meta-llama/llama-4-scout-17b-16e-instruct")
	assert.True(t, ok)
	assert.True(t, caps.ToolUse)
	assert.True(t, caps.Vision)
}
```

### Step 1.2: Verify RED

```bash
go test ./internal/cli/ -run TestResolveCapabilities -v
```

Expected: build error — `ResolveCapabilities` undefined.

### Step 1.3: GREEN — minimal implementation

Create `internal/cli/capabilities.go`:

```go
package cli

// ModelCapabilities describes which optional LLM features a (provider, model)
// pair supports. Looked up via ResolveCapabilities at tier construction time
// and travels with the LLMClient so analysis branches without naming providers.
type ModelCapabilities struct {
	Provider string
	Model    string
	ToolUse  bool
	Vision   bool
}

// knownModels enumerates per-model capabilities for hosted providers.
// Model "*" is the wildcard for self-hosted providers (ollama, lmstudio)
// where the user picks the model and capabilities default to off.
//
// Adding a new model: add a row here. Validation falls back to "no
// capabilities" for unknown (provider, model) pairs on a known provider, so
// new models can be used immediately even before this table catches up.
var knownModels = []ModelCapabilities{
	{"anthropic", "claude-haiku-4-5", true, true},
	{"anthropic", "claude-sonnet-4-6", true, true},
	{"anthropic", "claude-opus-4-7", true, true},
	{"openai", "gpt-5", true, true},
	{"openai", "gpt-5-mini", true, true},
	{"openai", "gpt-4o", true, true},
	{"openai", "gpt-4o-mini", true, true},
	{"groq", "meta-llama/llama-4-scout-17b-16e-instruct", true, true},
	{"ollama", "*", false, false},
	{"lmstudio", "*", false, false},
}

// ResolveCapabilities returns the capability flags for (provider, model).
// The bool is true when the provider is recognized; for known providers with
// an unknown model, it returns a zero-value ModelCapabilities and true so
// the caller can run with no optional features rather than failing the run.
func ResolveCapabilities(provider, model string) (ModelCapabilities, bool) {
	var providerKnown bool
	for _, m := range knownModels {
		if m.Provider == provider {
			providerKnown = true
			if m.Model == model || m.Model == "*" {
				return m, true
			}
		}
	}
	if providerKnown {
		return ModelCapabilities{Provider: provider, Model: model}, true
	}
	return ModelCapabilities{}, false
}
```

### Step 1.4: Verify GREEN

```bash
go test ./internal/cli/ -run TestResolveCapabilities -v
```

All five tests pass.

### Step 1.5: Commit

```bash
git add internal/cli/capabilities.go internal/cli/capabilities_test.go
git commit -m "$(cat <<'EOF'
feat(cli): introduce per-model capability registry

- RED: TestResolveCapabilities_* covering exact match, wildcard,
  unknown provider, unknown model on known provider, and Groq.
- GREEN: ResolveCapabilities with knownModels table; wildcard
  Model "*" covers self-hosted providers.
- Status: 5 tests passing, build successful

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2 — Wire Registry into Tier Validation

**Files:**
- Modify: `internal/cli/tier_validate.go`
- Create: `internal/cli/tier_validate_test.go` (or append to existing)

### Step 2.1: RED — failing tests

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTierConfigs_RejectsUnknownProvider(t *testing.T) {
	err := validateTierConfigs("nope/foo", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
	assert.Contains(t, err.Error(), "nope")
}

func TestValidateTierConfigs_TypicalRequiresToolUse(t *testing.T) {
	err := validateTierConfigs("", "ollama/llama3", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool use")
	assert.Contains(t, err.Error(), "typical")
}

func TestValidateTierConfigs_AllowsGroqOnTypical(t *testing.T) {
	err := validateTierConfigs("", "groq/meta-llama/llama-4-scout-17b-16e-instruct", "")
	assert.NoError(t, err)
}

func TestValidateTierConfigs_AllowsUnknownModelOnKnownProvider(t *testing.T) {
	err := validateTierConfigs("anthropic/claude-future-9-9", "", "")
	assert.NoError(t, err)
}

func TestValidateTierConfigs_DefaultsAreValid(t *testing.T) {
	err := validateTierConfigs("", "", "")
	assert.NoError(t, err)
}
```

### Step 2.2: Verify RED

```bash
go test ./internal/cli/ -run TestValidateTierConfigs -v
```

`AllowsGroqOnTypical` will fail because `groq` is not in `isKnownProvider`. The unknown-model case may already pass — note the actual failure list before continuing.

### Step 2.3: GREEN — replace `isKnownProvider` and `providerSupportsToolUse`

Rewrite `internal/cli/tier_validate.go`:

```go
package cli

import "fmt"

const (
	defaultSmallTier   = "anthropic/claude-haiku-4-5"
	defaultTypicalTier = "anthropic/claude-sonnet-4-6"
	defaultLargeTier   = "anthropic/claude-opus-4-7"
)

// knownProviders returns the deduplicated provider list for "valid: ..."
// error messages. Built from knownModels so adding a provider only requires
// a row in the registry.
func knownProviders() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range knownModels {
		if _, ok := seen[m.Provider]; ok {
			continue
		}
		seen[m.Provider] = struct{}{}
		out = append(out, m.Provider)
	}
	return out
}

func validateTierConfigs(small, typical, large string) error {
	for _, tc := range []struct {
		name, raw string
		fallback  string
		needsTool bool
	}{
		{"small", small, defaultSmallTier, false},
		{"typical", typical, defaultTypicalTier, true},
		{"large", large, defaultLargeTier, false},
	} {
		s := tc.raw
		if s == "" {
			s = tc.fallback
		}
		provider, model, err := parseTierString(s)
		if err != nil {
			return fmt.Errorf("tier %q: %w", tc.name, err)
		}
		caps, ok := ResolveCapabilities(provider, model)
		if !ok {
			return fmt.Errorf("tier %q: unknown provider %q (valid: %v)", tc.name, provider, knownProviders())
		}
		if tc.needsTool && !caps.ToolUse {
			return fmt.Errorf("tier %q: model %q on provider %q does not support tool use; the drift investigator requires a tool-use-capable model", tc.name, model, provider)
		}
	}
	return nil
}
```

Hunt for any remaining callers of the deleted helpers:

```bash
grep -rn "isKnownProvider\|providerSupportsToolUse" internal/ cmd/
```

Fix every hit (write a test for any new behavior you uncover).

### Step 2.4: Verify GREEN

```bash
go test ./internal/cli/ -v
go build ./...
golangci-lint run
```

### Step 2.5: Commit

```bash
git add internal/cli/tier_validate.go internal/cli/tier_validate_test.go
git commit -m "$(cat <<'EOF'
feat(cli): drive tier validation from capability registry

- RED: TestValidateTierConfigs_* covering unknown provider,
  typical-needs-tool-use, Groq-on-typical, unknown-model-on-known-
  provider, defaults.
- GREEN: validateTierConfigs uses ResolveCapabilities; old
  isKnownProvider / providerSupportsToolUse helpers removed.
  Provider list rendered from knownModels for error messages.
- Status: tests passing, build successful

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3 — Plumb Capabilities Through `LLMClient`

**Files:**
- Modify: `internal/analyzer/client.go`
- Modify: `internal/analyzer/bifrost_client.go`
- Modify: any in-tree fakes — find with `grep -rln "func.*CompleteJSON" internal/`
- Test: `internal/analyzer/bifrost_client_capabilities_test.go` (new)

### Step 3.1: RED

```go
package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBifrostClient_CapabilitiesAreSetAtConstruction(t *testing.T) {
	caps := ModelCapabilities{Provider: "anthropic", Model: "claude-haiku-4-5", ToolUse: true, Vision: true}
	c, err := NewBifrostClientWithProvider("anthropic", "test-key", "claude-haiku-4-5", "", caps)
	assert.NoError(t, err)
	assert.Equal(t, caps, c.Capabilities())
}
```

### Step 3.2: Verify RED

```bash
go test ./internal/analyzer/ -run TestBifrostClient_Capabilities -v
```

Expected: `ModelCapabilities` undefined in `analyzer`, `Capabilities()` undefined, arity mismatch.

### Step 3.3: GREEN

In `internal/analyzer/client.go`:

```go
// ModelCapabilities mirrors cli.ModelCapabilities so analyzer code can branch
// on capabilities without importing cli (which would create a dependency
// cycle). Field set is identical.
type ModelCapabilities struct {
	Provider string
	Model    string
	ToolUse  bool
	Vision   bool
}

type LLMClient interface {
	Complete(ctx context.Context, prompt string) (string, error)
	CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error)
	Capabilities() ModelCapabilities
}
```

In `internal/analyzer/bifrost_client.go`:
- Add `caps ModelCapabilities` to `BifrostClient`.
- Add `func (c *BifrostClient) Capabilities() ModelCapabilities { return c.caps }`.
- Append `caps ModelCapabilities` to `NewBifrostClientWithProvider`'s parameter list and store it on the struct.

In `internal/cli/llm_client.go`:
- `buildTierClient` resolves `caps := ResolveCapabilities(provider, model)` (the cli type) and converts to `analyzer.ModelCapabilities` (same field set) when calling the constructor.

Walk every fake / mock that satisfies `LLMClient`:

```bash
grep -rln "CompleteJSON" internal/ | xargs grep -L "Capabilities()"
```

For each, add a `caps ModelCapabilities` field and:

```go
func (f *fakeName) Capabilities() ModelCapabilities { return f.caps }
```

### Step 3.4: Verify GREEN

```bash
go test ./... -v
go build ./...
golangci-lint run
```

### Step 3.5: Commit

```bash
git add -A
git commit -m "feat(analyzer): plumb model capabilities through LLMClient

- RED: TestBifrostClient_CapabilitiesAreSetAtConstruction.
- GREEN: LLMClient.Capabilities() returns analyzer.ModelCapabilities;
  BifrostClient stores caps at construction; buildTierClient
  resolves and threads them through.
- All in-tree fakes updated.
- Status: tests passing, build successful

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 4 — Add Groq Provider

**Files:**
- Modify: `internal/analyzer/bifrost_client.go` lines ~110–132 (`NewBifrostClientWithProvider`)
- Modify: `internal/cli/llm_client.go` `buildTierClient`
- Test: extend `internal/analyzer/bifrost_client_test.go` (or its sibling)

### Step 4.1: RED

```go
func TestNewBifrostClientWithProvider_GroqUsesOpenAIWithCustomBase(t *testing.T) {
	caps := ModelCapabilities{Provider: "groq", Model: "meta-llama/llama-4-scout-17b-16e-instruct", ToolUse: true, Vision: true}
	c, err := NewBifrostClientWithProvider("groq", "gsk_test", "meta-llama/llama-4-scout-17b-16e-instruct", "https://api.groq.com/openai", caps)
	require.NoError(t, err)
	assert.Equal(t, schemas.OpenAI, c.provider)
	assert.True(t, c.Capabilities().Vision)
}

func TestNewBifrostClientWithProvider_GroqRequiresBaseURL(t *testing.T) {
	_, err := NewBifrostClientWithProvider("groq", "gsk_test", "x", "", ModelCapabilities{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseURL")
}
```

### Step 4.2: Verify RED

```bash
go test ./internal/analyzer/ -run TestNewBifrostClientWithProvider_Groq -v
```

Expected: `unsupported Bifrost provider: "groq"`.

### Step 4.3: GREEN

In `bifrost_client.go`'s switch:

```go
case "groq":
    provider = schemas.OpenAI
    if baseURL == "" {
        return nil, fmt.Errorf("groq provider requires a baseURL")
    }
```

In `llm_client.go` `buildTierClient`:

```go
case "groq":
    apiKey = os.Getenv("GROQ_API_KEY")
    if apiKey == "" {
        return nil, nil, fmt.Errorf("GROQ_API_KEY not set")
    }
    bifrostProvider = "groq"
    baseURL = "https://api.groq.com/openai"
    counter = analyzer.NewTiktokenCounter()
```

### Step 4.4: Verify GREEN; commit

```bash
go test ./internal/analyzer/ ./internal/cli/ -v
go build ./...
golangci-lint run
git commit -am "feat(analyzer): add groq provider via Bifrost OpenAI compat path

- RED: TestNewBifrostClientWithProvider_Groq* covering provider
  routing and the required baseURL guard.
- GREEN: groq routes through schemas.OpenAI with
  baseURL=https://api.groq.com/openai; buildTierClient reads
  GROQ_API_KEY.
- Status: tests passing, build successful

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 5 — Image Content Blocks on `ChatMessage`

**Files:**
- Modify: `internal/analyzer/types.go`
- Test: `internal/analyzer/types_test.go` (new if missing)

### Step 5.1: RED

```go
package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChatMessage_ContentBlocksZeroValueIsNil(t *testing.T) {
	m := ChatMessage{Role: "user", Content: "hi"}
	assert.Nil(t, m.ContentBlocks)
}

func TestChatMessage_CanAttachImageURLBlock(t *testing.T) {
	m := ChatMessage{
		Role:    "user",
		Content: "What does this show?",
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockText, Text: "Below is a screenshot:"},
			{Type: ContentBlockImageURL, ImageURL: "https://example.com/dash.png"},
		},
	}
	assert.Len(t, m.ContentBlocks, 2)
	assert.Equal(t, ContentBlockImageURL, m.ContentBlocks[1].Type)
	assert.Equal(t, "https://example.com/dash.png", m.ContentBlocks[1].ImageURL)
}
```

### Step 5.2: GREEN

In `types.go`:

```go
type ContentBlockType string

const (
	ContentBlockText     ContentBlockType = "text"
	ContentBlockImageURL ContentBlockType = "image_url"
)

// ContentBlock is one element of a multimodal message body. When
// ChatMessage.ContentBlocks is non-empty, the Bifrost client renders the
// blocks instead of the flat Content string.
type ContentBlock struct {
	Type     ContentBlockType
	Text     string
	ImageURL string
}
```

Add to `ChatMessage`:

```go
// ContentBlocks, when non-empty, supplies a multimodal message body.
// The flat Content string is used only when ContentBlocks is empty.
ContentBlocks []ContentBlock
```

### Step 5.3: Commit

```bash
go test ./internal/analyzer/ -v && go build ./... && golangci-lint run
git commit -am "feat(analyzer): add image content blocks to ChatMessage

- RED: TestChatMessage_ContentBlocks* round-tripping image_url.
- GREEN: ContentBlock + ContentBlockType union; ChatMessage gains
  optional ContentBlocks slice. Empty preserves legacy path.
- Status: tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 6 — Bifrost Image-Block Marshaling

**Files:**
- Modify: `internal/analyzer/bifrost_client.go` (`completeOneTurn` user-message branch and the JSON path)
- Test: `internal/analyzer/bifrost_client_image_test.go` (new)

This is the most subtle task — read `completeOneTurn` lines 160–200 first.

### Step 6.1: RED — fake `bifrostRequester` that captures the request

```go
package analyzer

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureRequester struct {
	last *schemas.BifrostChatRequest
	resp *schemas.BifrostChatResponse
}

func (c *captureRequester) ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	c.last = req
	return c.resp, nil
}

func TestCompleteOneTurn_AnthropicRendersImageContentBlocks(t *testing.T) {
	cap := &captureRequester{resp: emptyAssistantResponse()}
	c := &BifrostClient{client: cap, provider: schemas.Anthropic, model: "claude-haiku-4-5"}
	msgs := []ChatMessage{{
		Role: "user",
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockText, Text: "Look at this:"},
			{Type: ContentBlockImageURL, ImageURL: "https://x.test/a.png"},
		},
	}}
	_, _ = c.completeOneTurn(context.Background(), msgs, nil)

	require.NotNil(t, cap.last)
	require.Len(t, cap.last.Messages, 1)
	blocks := cap.last.Messages[0].Content.ContentBlocks
	require.Len(t, blocks, 2)
	assert.Equal(t, schemas.ChatContentBlockTypeText, blocks[0].Type)
	assert.Equal(t, schemas.ChatContentBlockTypeImage, blocks[1].Type)
	// Assert the URL appears in whichever field the SDK uses for image URLs.
	// Adapt the assertion below based on the actual schemas.ChatContentBlock
	// shape (see go doc github.com/maximhq/bifrost/core/schemas.ChatContentBlock).
}

func TestCompleteOneTurn_OpenAICompatRendersImageContentBlocks(t *testing.T) {
	cap := &captureRequester{resp: emptyAssistantResponse()}
	c := &BifrostClient{client: cap, provider: schemas.OpenAI, model: "x"}
	msgs := []ChatMessage{{
		Role: "user",
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockImageURL, ImageURL: "https://x.test/b.png"},
		},
	}}
	_, _ = c.completeOneTurn(context.Background(), msgs, nil)
	require.NotNil(t, cap.last)
	blocks := cap.last.Messages[0].Content.ContentBlocks
	require.Len(t, blocks, 1)
	assert.Equal(t, schemas.ChatContentBlockTypeImage, blocks[0].Type)
}

func emptyAssistantResponse() *schemas.BifrostChatResponse {
	// Construct a minimally-valid response that completeOneTurn can translate
	// without panicking. Adapt to the SDK shape.
	return &schemas.BifrostChatResponse{}
}
```

### Step 6.2: Verify RED

```bash
go test ./internal/analyzer/ -run TestCompleteOneTurn_.*Image -v
```

### Step 6.3: GREEN

In `completeOneTurn`'s `case "user":`, when `len(m.ContentBlocks) > 0`, build the message via a new helper instead of `ContentStr`:

```go
case "user":
    bm.Role = schemas.ChatMessageRoleUser
    if len(m.ContentBlocks) > 0 {
        bm.Content = &schemas.ChatMessageContent{
            ContentBlocks: renderContentBlocks(c.provider, m.ContentBlocks),
        }
    } else if cacheable {
        bm.Content = anthropicCachedContent(m.Content)
    } else {
        bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
    }
```

```go
// renderContentBlocks translates the provider-neutral ContentBlock slice to
// Bifrost schema blocks. Image blocks are normalized by the SDK; confirm the
// exact field name against vendor/github.com/maximhq/bifrost/core/schemas/.
func renderContentBlocks(provider schemas.ModelProvider, blocks []ContentBlock) []schemas.ChatContentBlock {
    out := make([]schemas.ChatContentBlock, 0, len(blocks))
    for _, b := range blocks {
        switch b.Type {
        case ContentBlockText:
            text := b.Text
            out = append(out, schemas.ChatContentBlock{
                Type: schemas.ChatContentBlockTypeText,
                Text: &text,
            })
        case ContentBlockImageURL:
            out = append(out, makeImageBlock(provider, b.ImageURL))
        }
    }
    return out
}
```

Implement `makeImageBlock(provider, url)` using the actual SDK image-block fields. If both providers share one struct shape, the helper is unconditional.

Apply the same translation to whatever the structured-output path uses (search for `ContentStr` usages in this file).

### Step 6.4: Verify GREEN

```bash
go test ./internal/analyzer/ -v
go build ./...
golangci-lint run
```

### Step 6.5: Commit

```bash
git commit -am "feat(analyzer): render image content blocks via Bifrost

- RED: TestCompleteOneTurn_*Image asserting Anthropic and
  OpenAI/Groq paths produce ChatContentBlock entries with the URL
  preserved.
- GREEN: completeOneTurn translates ContentBlocks via
  renderContentBlocks/makeImageBlock; legacy ContentStr path
  preserved when ContentBlocks is empty.
- Status: tests passing, build successful

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 7 — Image Batching Helper

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Test: `internal/analyzer/screenshot_gaps_test.go` (extend or create)

### Step 7.1: RED

```go
func TestSplitImageBatches(t *testing.T) {
	ref := func(i int) imageRef { return imageRef{Src: fmt.Sprintf("img-%d.png", i)} }
	for _, tc := range []struct {
		name string
		n    int
		want []int // batch sizes
	}{
		{"empty", 0, nil},
		{"one", 1, []int{1}},
		{"exactly five", 5, []int{5}},
		{"six", 6, []int{5, 1}},
		{"twelve", 12, []int{5, 5, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refs := make([]imageRef, tc.n)
			for i := range refs {
				refs[i] = ref(i)
			}
			got := splitImageBatches(refs, 5)
			gotSizes := make([]int, 0, len(got))
			for _, b := range got {
				gotSizes = append(gotSizes, len(b))
			}
			assert.Equal(t, tc.want, gotSizes)
		})
	}
}
```

### Step 7.2: GREEN

```go
// splitImageBatches groups refs into chunks of size <= max, preserving order.
// Returns nil for empty input. max must be > 0.
func splitImageBatches(refs []imageRef, max int) [][]imageRef {
	if len(refs) == 0 || max <= 0 {
		return nil
	}
	out := make([][]imageRef, 0, (len(refs)+max-1)/max)
	for i := 0; i < len(refs); i += max {
		end := i + max
		if end > len(refs) {
			end = len(refs)
		}
		out = append(out, refs[i:end])
	}
	return out
}
```

### Step 7.3: Commit

```bash
git commit -am "feat(analyzer): add splitImageBatches helper

- RED: TestSplitImageBatches table covering 0, 1, 5, 6, 12.
- GREEN: ordered chunks of <= max.
- Status: tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 8 — Multimodal JSON Method + Relevance Pass

**Files:**
- Modify: `internal/analyzer/client.go` (interface)
- Modify: `internal/analyzer/bifrost_client.go` (implementation)
- Modify: `internal/analyzer/screenshot_gaps.go` (relevance pass)
- Test: `internal/analyzer/screenshot_gaps_relevance_test.go`

### Step 8.1: RED

```go
type fakeJSONClient struct {
	caps     ModelCapabilities
	jsonResp json.RawMessage
}

func (f *fakeJSONClient) Complete(ctx context.Context, prompt string) (string, error) { return "", nil }
func (f *fakeJSONClient) CompleteJSON(ctx context.Context, prompt string, _ JSONSchema) (json.RawMessage, error) {
	return f.jsonResp, nil
}
func (f *fakeJSONClient) CompleteJSONMultimodal(ctx context.Context, msgs []ChatMessage, _ JSONSchema) (json.RawMessage, error) {
	return f.jsonResp, nil
}
func (f *fakeJSONClient) Capabilities() ModelCapabilities { return f.caps }

func TestRelevancePass_ParsesImageIssuesAndVerdicts(t *testing.T) {
	resp := json.RawMessage(`{
	  "image_issues": [
	    {"index":"img-2","src":"b.png","reason":"shows dashboard, prose describes settings","suggested_action":"replace"}
	  ],
	  "verdicts": [
	    {"index":"img-1","matches":true},
	    {"index":"img-2","matches":false}
	  ]
	}`)
	client := &fakeJSONClient{
		caps:     ModelCapabilities{Vision: true},
		jsonResp: resp,
	}
	page := DocPage{URL: "https://x/p", Content: "..."}
	refs := []imageRef{{Src: "a.png"}, {Src: "b.png"}}

	issues, verdicts, err := relevancePass(context.Background(), client, page, refs)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "img-2", issues[0].Index)
	assert.Equal(t, "https://x/p", issues[0].PageURL)
	require.Len(t, verdicts, 2)
	assert.True(t, verdicts[0].Matches)
	assert.False(t, verdicts[1].Matches)
}

func TestRelevancePass_BatchesAtFiveImages(t *testing.T) {
	// 12 refs → 3 calls. Use a fake that counts calls.
}
```

### Step 8.2: Verify RED

```bash
go test ./internal/analyzer/ -run TestRelevancePass -v
```

### Step 8.3: GREEN

Add to `LLMClient`:

```go
CompleteJSONMultimodal(ctx context.Context, messages []ChatMessage, schema JSONSchema) (json.RawMessage, error)
```

Implement on `BifrostClient` as a thin wrapper over the existing JSON path that passes pre-built messages (with `ContentBlocks`) instead of building from a single prompt string. Update every fake.

In `screenshot_gaps.go`:

```go
type ImageIssue struct {
	PageURL         string `json:"page_url"`
	Index           string `json:"index"`
	Src             string `json:"src"`
	Reason          string `json:"reason"`
	SuggestedAction string `json:"suggested_action"`
}

type ImageVerdict struct {
	Index   string `json:"index"`
	Matches bool   `json:"matches"`
}

type relevancePassResponse struct {
	ImageIssues []ImageIssue   `json:"image_issues"`
	Verdicts    []ImageVerdict `json:"verdicts"`
}

var relevancePassSchema = JSONSchema{
	Name: "screenshot_image_relevance",
	Doc: json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "image_issues": {
	      "type": "array",
	      "items": {
	        "type": "object",
	        "properties": {
	          "index": {"type": "string"},
	          "src": {"type": "string"},
	          "reason": {"type": "string"},
	          "suggested_action": {"type": "string"}
	        },
	        "required": ["index","src","reason","suggested_action"],
	        "additionalProperties": false
	      }
	    },
	    "verdicts": {
	      "type": "array",
	      "items": {
	        "type": "object",
	        "properties": {
	          "index": {"type": "string"},
	          "matches": {"type": "boolean"}
	        },
	        "required": ["index","matches"],
	        "additionalProperties": false
	      }
	    }
	  },
	  "required": ["image_issues","verdicts"],
	  "additionalProperties": false
	}`),
}

func relevancePass(ctx context.Context, client LLMClient, page DocPage, refs []imageRef) ([]ImageIssue, []ImageVerdict, error) {
	var issues []ImageIssue
	var verdicts []ImageVerdict
	startIdx := 0
	for batchN, batch := range splitImageBatches(refs, 5) {
		prompt := buildRelevancePrompt(page, batch, startIdx)
		blocks := []ContentBlock{{Type: ContentBlockText, Text: prompt}}
		for _, r := range batch {
			blocks = append(blocks, ContentBlock{Type: ContentBlockImageURL, ImageURL: r.Src})
		}
		msg := ChatMessage{Role: "user", ContentBlocks: blocks, Content: prompt}
		raw, err := client.CompleteJSONMultimodal(ctx, []ChatMessage{msg}, relevancePassSchema)
		if err != nil {
			return nil, nil, fmt.Errorf("relevancePass batch %d: %w", batchN, err)
		}
		var resp relevancePassResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			log.Warnf("relevancePass: invalid JSON for %s batch %d: %v", page.URL, batchN, err)
			startIdx += len(batch)
			continue
		}
		for i := range resp.ImageIssues {
			resp.ImageIssues[i].PageURL = page.URL
		}
		issues = append(issues, resp.ImageIssues...)
		verdicts = append(verdicts, resp.Verdicts...)
		startIdx += len(batch)
	}
	return issues, verdicts, nil
}
```

Add `buildRelevancePrompt` with a `// PROMPT:` block describing index naming (e.g. `img-1`, `img-2`, … numbered globally so verdicts merge cleanly across batches), the relevance criterion, and the JSON shape.

### Step 8.4: Commit

```bash
git commit -am "feat(analyzer): vision relevance pass for docs images

- RED: TestRelevancePass_ParsesImageIssuesAndVerdicts and
  TestRelevancePass_BatchesAtFiveImages.
- GREEN: relevancePass batches refs (<=5/call), issues
  CompleteJSONMultimodal calls, merges issues + verdicts by index.
  New ImageIssue, ImageVerdict types; new relevancePassSchema and
  // PROMPT: block in buildRelevancePrompt.
- LLMClient gains CompleteJSONMultimodal; BifrostClient implements;
  in-tree fakes updated.
- Status: tests passing, build successful

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 9 — Verdict-Enriched Detection Prompt

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Test: `internal/analyzer/screenshot_gaps_test.go`

### Step 9.1: RED

```go
func TestBuildDetectionPromptWithVerdicts_AnnotatesImages(t *testing.T) {
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}, {Index: "img-2", Matches: false}}
	refs := []imageRef{{Src: "a.png", AltText: "Settings"}, {Src: "b.png", AltText: "Logs"}}
	prompt := buildDetectionPromptWithVerdicts("https://x/p", "content...", refs, verdicts)
	assert.Contains(t, prompt, "img-1")
	assert.Contains(t, prompt, "verdict: matches")
	assert.Contains(t, prompt, "verdict: does not match")
}

func TestBuildDetectionPromptWithVerdicts_NilVerdictsDelegateToLegacy(t *testing.T) {
	refs := []imageRef{{Src: "a.png"}}
	got := buildDetectionPromptWithVerdicts("https://x/p", "content...", refs, nil)
	want := buildScreenshotPrompt("https://x/p", "content...", buildCoverageMap(refs))
	assert.Equal(t, want, got)
}
```

### Step 9.2: GREEN

Implement `buildDetectionPromptWithVerdicts(pageURL, content string, refs []imageRef, verdicts []ImageVerdict) string`. When `verdicts == nil`, return `buildScreenshotPrompt(pageURL, content, buildCoverageMap(refs))`. Otherwise build a verdict-annotated coverage list and a `// PROMPT:` block that explicitly tells the model: "if any image with `verdict: matches` already covers the moment, do not flag it; report it under `suppressed_by_image` instead."

Update `screenshotGapsResponse` schema to include `suppressed_by_image: []ScreenshotGapItem` so the audit count works without a duplicate call.

### Step 9.3: Commit

```bash
git commit -am "feat(analyzer): verdict-enriched detection prompt

- RED: TestBuildDetectionPromptWithVerdicts_*.
- GREEN: buildDetectionPromptWithVerdicts annotates each image
  with its verdict and asks for both 'gaps' and
  'suppressed_by_image'. Nil verdicts delegate to legacy prompt.
- Schema extended to include suppressed_by_image.
- Status: tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 10 — Wire Vision Branch into `DetectScreenshotGaps`

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Modify: `internal/cli/analyze.go` (callers of the new return type)
- Test: `internal/analyzer/screenshot_gaps_integration_test.go`

### Step 10.1: RED

Build a fake `LLMClient` that scripts: 1+ `CompleteJSONMultimodal` calls (relevance pass, returning canned issues + verdicts) followed by 1 `CompleteJSON` call (detection pass, returning canned gaps + suppressed list). Then:

```go
func TestDetectScreenshotGaps_VisionBranchEmitsImageIssuesAndAuditStats(t *testing.T) {
	client := &scriptedClient{ /* see above */ }
	res, err := DetectScreenshotGaps(context.Background(), client, []DocPage{ /* 1 page with 6 images */ }, nil)
	require.NoError(t, err)
	assert.Len(t, res.ImageIssues, 1)
	assert.Len(t, res.AuditStats, 1)
	assert.True(t, res.AuditStats[0].VisionEnabled)
	assert.Equal(t, 2, res.AuditStats[0].RelevanceBatches)
	assert.Equal(t, 6, res.AuditStats[0].ImagesSeen)
}

func TestDetectScreenshotGaps_NonVisionBranchUnchanged(t *testing.T) {
	client := &fakeJSONClient{caps: ModelCapabilities{Vision: false}, jsonResp: json.RawMessage(`{"gaps":[]}`)}
	res, err := DetectScreenshotGaps(context.Background(), client, []DocPage{{URL: "https://x/p", Content: "..."}}, nil)
	require.NoError(t, err)
	assert.Empty(t, res.ImageIssues)
	require.Len(t, res.AuditStats, 1)
	assert.False(t, res.AuditStats[0].VisionEnabled)
}
```

### Step 10.2: GREEN

```go
type ScreenshotResult struct {
	MissingGaps []ScreenshotGap
	ImageIssues []ImageIssue
	AuditStats  []ScreenshotPageStats
}

type ScreenshotPageStats struct {
	PageURL            string
	VisionEnabled      bool
	RelevanceBatches   int
	ImagesSeen         int
	ImageIssues        int
	MissingScreenshots int
	MissingSuppressed  int
}

func DetectScreenshotGaps(ctx context.Context, client LLMClient, pages []DocPage, progress ScreenshotProgressFunc) (ScreenshotResult, error) {
	var result ScreenshotResult
	for i, page := range pages {
		stats := ScreenshotPageStats{PageURL: page.URL}
		refs := extractImages(page.Content)
		stats.ImagesSeen = len(refs)
		var verdicts []ImageVerdict
		if client.Capabilities().Vision && len(refs) > 0 {
			stats.VisionEnabled = true
			batches := splitImageBatches(refs, 5)
			stats.RelevanceBatches = len(batches)
			issues, vs, err := relevancePass(ctx, client, page, refs)
			if err != nil {
				return result, err
			}
			result.ImageIssues = append(result.ImageIssues, issues...)
			stats.ImageIssues = len(issues)
			verdicts = vs
		}
		// Detection pass — text only. With verdicts when vision ran.
		gaps, suppressed, err := detectionPass(ctx, client, page, refs, verdicts)
		if err != nil {
			return result, err
		}
		stats.MissingScreenshots = len(gaps)
		stats.MissingSuppressed = suppressed
		result.MissingGaps = append(result.MissingGaps, gaps...)
		result.AuditStats = append(result.AuditStats, stats)
		if progress != nil {
			progress(i+1, len(pages), page.URL)
		}
	}
	return result, nil
}
```

`detectionPass` is a small helper that wraps the existing CompleteJSON + parse logic, optionally calling `buildDetectionPromptWithVerdicts` instead of `buildScreenshotPrompt`.

Update `internal/cli/analyze.go` for the new return type — replace `screenshotGaps` slice references with `screenshotResult.MissingGaps` and pass the full result to the reporter.

### Step 10.3: Commit

```bash
git commit -am "feat(analyzer): branch DetectScreenshotGaps on vision capability

- RED: TestDetectScreenshotGaps_VisionBranch* + non-vision parity.
- GREEN: ScreenshotResult struct (MissingGaps, ImageIssues,
  AuditStats); vision-on path runs relevance pass + verdict-
  enriched detection; vision-off path unchanged. analyze.go
  updated for the new return type.
- Status: tests passing, build successful

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 11 — Reporter: `## Image Issues` Section

**Files:**
- Modify: the package containing `WriteScreenshots` (find with `grep -rn "func WriteScreenshots" internal/`)
- Test: same package

### Step 11.1: RED

```go
func TestWriteScreenshots_RendersImageIssuesSection(t *testing.T) {
	tmp := t.TempDir()
	res := analyzer.ScreenshotResult{
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x/p", VisionEnabled: true}},
		ImageIssues: []analyzer.ImageIssue{{
			PageURL: "https://x/p", Index: "img-1", Src: "b.png",
			Reason: "shows dashboard but prose describes settings",
			SuggestedAction: "replace",
		}},
	}
	require.NoError(t, WriteScreenshots(tmp, res))
	body, err := os.ReadFile(filepath.Join(tmp, "screenshots.md"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "## Image Issues")
	assert.Contains(t, string(body), "shows dashboard but prose describes settings")
}

func TestWriteScreenshots_VisionRanButNoIssues_RendersEmptyMarker(t *testing.T) {
	tmp := t.TempDir()
	res := analyzer.ScreenshotResult{
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x/p", VisionEnabled: true}},
	}
	require.NoError(t, WriteScreenshots(tmp, res))
	body, _ := os.ReadFile(filepath.Join(tmp, "screenshots.md"))
	assert.Contains(t, string(body), "## Image Issues")
	assert.Contains(t, string(body), "No image issues detected")
}

func TestWriteScreenshots_VisionDidNotRun_OmitsImageIssuesHeader(t *testing.T) {
	tmp := t.TempDir()
	res := analyzer.ScreenshotResult{
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x/p", VisionEnabled: false}},
	}
	require.NoError(t, WriteScreenshots(tmp, res))
	body, _ := os.ReadFile(filepath.Join(tmp, "screenshots.md"))
	assert.NotContains(t, string(body), "## Image Issues")
}
```

### Step 11.2: GREEN

Update `WriteScreenshots` to take `analyzer.ScreenshotResult`. Render `## Missing Screenshots` (existing logic), then conditionally render `## Image Issues`:

```go
visionRan := false
for _, s := range res.AuditStats {
    if s.VisionEnabled {
        visionRan = true
        break
    }
}
if visionRan {
    fmt.Fprintln(w, "## Image Issues")
    fmt.Fprintln(w)
    if len(res.ImageIssues) == 0 {
        fmt.Fprintln(w, "_No image issues detected._")
    } else {
        for _, ii := range res.ImageIssues {
            // render the per-issue block per the design
        }
    }
}
```

### Step 11.3: Commit

```bash
git commit -am "feat(reporter): render ## Image Issues in screenshots.md

- RED: TestWriteScreenshots_RendersImageIssuesSection,
  VisionRanButNoIssues, VisionDidNotRun.
- GREEN: WriteScreenshots accepts ScreenshotResult; renders
  ## Missing Screenshots followed by ## Image Issues. Header
  omitted when no page ran vision; rendered with placeholder
  when vision ran but found none.
- Status: tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 12 — Audit Log Line per Page

**Files:**
- Modify: `internal/cli/analyze.go` around the screenshot block (lines ~438–509)
- Test: scoped log-capture test

### Step 12.1: RED

```go
func TestAnalyze_EmitsScreenshotAuditLine(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// run analyze with a fake tiering whose Small() has Vision=true and
	// returns scripted responses, against a single fixture page.

	out := buf.String()
	assert.Contains(t, out, "page=https://example.com/p")
	assert.Contains(t, out, "vision=on")
	assert.Contains(t, out, "relevance_batches=")
	assert.Contains(t, out, "images_seen=")
	assert.Contains(t, out, "image_issues=")
	assert.Contains(t, out, "missing_screenshots=")
	assert.Contains(t, out, "missing_suppressed=")
}
```

### Step 12.2: GREEN

After the screenshot block in `analyze.go`, iterate `screenshotResult.AuditStats` and emit one `log.Infof` per page with the documented field set. Vision-off pages emit `vision=off` with relevance fields zeroed.

### Step 12.3: Commit

```bash
git commit -am "feat(cli): per-page screenshot audit log

- RED: TestAnalyze_EmitsScreenshotAuditLine.
- GREEN: log.Infof emits page-scoped audit line with vision flag
  and counts after each page completes.
- Status: tests passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 13 — `ftg doctor` Reports Resolved Capabilities

**Files:**
- Find with `grep -rn "doctor" cmd/ internal/cli/`
- Modify: that file
- Test: extend its existing tests

### Step 13.1: RED

```go
func TestDoctor_PrintsResolvedCapabilitiesPerTier(t *testing.T) {
	// Run doctor with --llm-small=anthropic/claude-haiku-4-5
	out := captureDoctorOutput(t)
	assert.Contains(t, out, "small: anthropic/claude-haiku-4-5 (tool_use=true vision=true)")
	assert.Contains(t, out, "typical:")
	assert.Contains(t, out, "large:")
}
```

### Step 13.2: GREEN

After existing checks (`mdfetch`, `hugo`, etc.), resolve capabilities for each tier via `ResolveCapabilities` and print one line per tier in the format above.

### Step 13.3: Commit.

---

## Task 14 — testscript End-to-End Scenarios

**Files:**
- Create: `cmd/find-the-gaps/testdata/vision-screenshot.txtar`
- Create: `cmd/find-the-gaps/testdata/vision-disabled-screenshot.txtar`

### Step 14.1: Pattern-match an existing scenario

```bash
ls cmd/find-the-gaps/testdata/*.txtar
```

Pick the closest existing scenario that runs the screenshot pass with a fake LLM server and copy its scaffolding.

### Step 14.2: Author scenarios

**`vision-screenshot.txtar`:**
- Fixture: 1 docs page with 12 `<img>` tags, one captioned `Settings page` but actually a dashboard.
- Fake LLM server: when receiving a `screenshot_image_relevance` schema request, return scripted responses for 3 batches (5+5+2). When receiving the detection schema request, return one gap with one entry under `suppressed_by_image`.
- Tier flag: `--llm-small=anthropic/claude-haiku-4-5` (capability registry resolves Vision=true).
- Assertions:
  - `screenshots.md` contains `## Image Issues` with the planted mismatch.
  - Stdout contains `relevance_batches=3 images_seen=12 image_issues=1`.
  - `## Missing Screenshots` shape unchanged from existing scenarios.

**`vision-disabled-screenshot.txtar`:**
- Same fixture.
- Tier flag: `--llm-small=ollama/llama3` (Vision=false via wildcard).
- Assertions:
  - `screenshots.md` does NOT contain `## Image Issues`.
  - Stdout contains `vision=off`.

### Step 14.3: Run

```bash
go test ./cmd/find-the-gaps/... -run TestScript -v
```

### Step 14.4: Commit.

---

## Task 15 — Docs

**Files:**
- Modify: `README.md` — Configuration section (`GROQ_API_KEY`); installation note about Groq being a hosted API (no new local install required).
- Modify: `CHANGELOG.md` — under Unreleased, add bullets for: capability registry, Groq provider, `## Image Issues` output, doctor capability lines.
- Modify: `.plans/VERIFICATION_PLAN.md` — add **Scenario 13: Vision-aware screenshot analysis** with three sub-cases per the design (anthropic/claude-haiku-4-5 vs ollama wildcard vs groq/llama-4-scout).

### Step 15.1: Edit and commit

```bash
git commit -am "docs: vision-aware screenshot analysis + Groq

- README: GROQ_API_KEY in Configuration.
- CHANGELOG: Unreleased entries for the capability registry,
  Groq provider, ## Image Issues section, doctor lines.
- VERIFICATION_PLAN: Scenario 13 with three sub-cases.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 16 — Final Verification

### Step 16.1: Coverage

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1
```

Each touched package ≥90% statement coverage.

### Step 16.2: Lint and build

```bash
golangci-lint run
go build ./...
```

### Step 16.3: PROGRESS.md

Per CLAUDE.md, document each task's status, tests passing, coverage achieved.

### Step 16.4: PR

Branch already named `vision-image-analysis`. Use a merge commit (no squash). PR description summarizes:
- New capability registry.
- New Groq provider.
- Vision-aware screenshot pipeline (relevance + verdict-enriched detection).
- New `## Image Issues` output section.
- Per-page audit log.
- Doctor capability output.

Do not push or open the PR without explicit user approval.

---

## Notes for the Engineer

**Groq endpoint detail.** Groq exposes an OpenAI-compatible API rooted at `https://api.groq.com/openai`. Bifrost's `schemas.OpenAI` provider appends `/v1/chat/completions` to `BaseURL` itself (see `bifrost/core/providers/openai/openai.go:741`), so the configured base must NOT include `/v1` — same convention `lmstudio` follows (see `bifrost_client.go:122`). The only differences for Groq are: a real bearer token (`GROQ_API_KEY`) and the public hosted endpoint.

**Image cap.** Groq enforces 5 images per request. Anthropic and OpenAI accept many more, but we standardize on ≤5/call across providers — it keeps the pipeline uniform, costs nothing meaningful on Anthropic/OpenAI for typical pages, and ensures Groq compatibility without a per-provider branch.

**Suppression accounting.** The detection prompt asks the model for two arrays — `gaps` (rendered to `screenshots.md`) and `suppressed_by_image` (counted into audit stats only). This avoids running detection twice to compute `missing_suppressed`.

**Order of operations.** Tasks 1–6 are foundation (no behavior change in screenshot output yet). Tasks 7–10 are the new pipeline. Tasks 11–13 are user-facing surface. Tasks 14–16 close the loop. Don't reorder — each task assumes the previous ones landed.

**TDD reminder.** If a test passes immediately after you write it, the test is wrong. Make it fail, watch it fail for the *right* reason, then make it pass. Per CLAUDE.md, violations require deleting and starting over.
