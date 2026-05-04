//go:build cachespike

package analyzer

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/require"
)

// TestBifrostUserBlockCacheControlEndToEnd verifies the only Bifrost
// caching path this project will use: cache_control on a user-message
// content block. After warming the cache and observing Anthropic's
// freshness-lag window, every subsequent identical request must read
// from cache.
//
// IMPORTANT — propagation lag:
//
//	Anthropic's cache requires several seconds for a fresh write to
//	become globally readable. Two calls in milliseconds may BOTH report
//	cache_write > 0 with cache_read = 0 — that is normal, not a bug.
//	This spike intentionally inserts a 10s delay after the first call
//	to clear the lag window.
//
// Run with: go test -tags=cachespike -run TestBifrostUserBlockCacheControlEndToEnd -v ./internal/analyzer/
func TestBifrostUserBlockCacheControlEndToEnd(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	account := &bifrostAccount{provider: schemas.Anthropic, apiKey: apiKey}
	client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: account})
	require.NoError(t, err)

	preamble := strings.Repeat("This is documentation that should be cached. ", 600)
	tail := "Now answer briefly: what is 2 + 2?"

	send := func(label string) *schemas.BifrostChatResponse {
		req := &schemas.BifrostChatRequest{
			Provider: schemas.Anthropic,
			Model:    "claude-sonnet-4-6",
			Input: []schemas.ChatMessage{{
				Role: schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{
					ContentBlocks: []schemas.ChatContentBlock{
						{
							Type:         schemas.ChatContentBlockTypeText,
							Text:         schemas.Ptr(preamble),
							CacheControl: &schemas.CacheControl{Type: schemas.CacheControlTypeEphemeral},
						},
						{
							Type: schemas.ChatContentBlockTypeText,
							Text: schemas.Ptr(tail),
						},
					},
				},
			}},
			Params: &schemas.ChatParameters{
				MaxCompletionTokens: schemas.Ptr(64),
			},
		}
		ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
		resp, bErr := client.ChatCompletionRequest(ctx, req)
		require.Nil(t, bErr, "%s: bifrost error: %+v", label, bErr)
		require.NotNil(t, resp.Usage)
		require.NotNil(t, resp.Usage.PromptTokensDetails)
		t.Logf("%s usage: cache_write=%d cache_read=%d",
			label,
			resp.Usage.PromptTokensDetails.CachedWriteTokens,
			resp.Usage.PromptTokensDetails.CachedReadTokens,
		)
		return resp
	}

	resp1 := send("call 1 (warmup)")
	// Either an immediate write (cold cache) or an immediate read (warm
	// from a prior run within TTL) is acceptable proof the path is wired.
	require.True(t,
		resp1.Usage.PromptTokensDetails.CachedWriteTokens > 0 ||
			resp1.Usage.PromptTokensDetails.CachedReadTokens > 0,
		"expected EITHER cache write or read on call 1; got %+v",
		resp1.Usage.PromptTokensDetails)

	// Wait long enough for Anthropic's freshness-lag window to close.
	t.Log("waiting 10s for cache propagation...")
	time.Sleep(10 * time.Second)

	resp2 := send("call 2 (after 10s)")
	require.Greater(t, resp2.Usage.PromptTokensDetails.CachedReadTokens, 0,
		"call 2 must read from cache after 10s settle; got %+v",
		resp2.Usage.PromptTokensDetails)

	resp3 := send("call 3 (immediate)")
	require.Greater(t, resp3.Usage.PromptTokensDetails.CachedReadTokens, 0,
		"call 3 must also read from cache; got %+v",
		resp3.Usage.PromptTokensDetails)
}
