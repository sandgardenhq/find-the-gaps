package analyzer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func anthropicResponse(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	}
}

func TestAnthropicClient_Complete_returnsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse("hello world"))
	}))
	defer srv.Close()

	c := newAnthropicClient(srv.URL, "test-key", "claude-sonnet-4-6")
	got, err := c.Complete(context.Background(), "ping")
	require.NoError(t, err)
	assert.Equal(t, "hello world", got)
}

func TestAnthropicClient_Complete_sendsModelAndPrompt(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse("ok"))
	}))
	defer srv.Close()

	c := newAnthropicClient(srv.URL, "key", "claude-opus-4-7")
	_, err := c.Complete(context.Background(), "my prompt")
	require.NoError(t, err)

	assert.Equal(t, "claude-opus-4-7", gotBody["model"])
	msgs := gotBody["messages"].([]any)
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].(map[string]any)["role"])
	assert.Equal(t, "my prompt", msgs[0].(map[string]any)["content"])
}

func TestAnthropicClient_Complete_serverError_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newAnthropicClient(srv.URL, "key", "claude-sonnet-4-6")
	_, err := c.Complete(context.Background(), "ping")
	assert.Error(t, err)
}

func TestAnthropicClient_Complete_emptyContent_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{}})
	}))
	defer srv.Close()

	c := newAnthropicClient(srv.URL, "key", "claude-sonnet-4-6")
	_, err := c.Complete(context.Background(), "ping")
	assert.Error(t, err)
}
