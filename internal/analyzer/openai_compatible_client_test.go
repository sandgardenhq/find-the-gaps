package analyzer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestOpenAICompatibleClient_Complete_ReturnsContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "pong"}},
			},
		})
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "")
	got, err := client.Complete(context.Background(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	if got != "pong" {
		t.Errorf("expected pong, got %q", got)
	}
}

func TestOpenAICompatibleClient_ServerError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "")
	_, err := client.Complete(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestOpenAICompatibleClient_EmptyChoices_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "")
	_, err := client.Complete(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestOpenAICompatibleClient_ImplementsLLMClient(t *testing.T) {
	var _ analyzer.LLMClient = analyzer.NewOpenAICompatibleClient("http://localhost", "model", "")
}

func TestOpenAICompatibleClient_ImplementsToolLLMClient(t *testing.T) {
	var _ analyzer.ToolLLMClient = analyzer.NewOpenAICompatibleClient("http://localhost", "model", "")
}

func TestOpenAICompatibleClient_CompleteWithTools_ReturnsAssistantMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello"}},
			},
		})
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "")
	msg, err := client.CompleteWithTools(context.Background(), []analyzer.ChatMessage{
		{Role: "user", Content: "hi"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", msg.Role)
	}
	if msg.Content != "hello" {
		t.Errorf("expected content 'hello', got %q", msg.Content)
	}
}

func TestOpenAICompatibleClient_CompleteWithTools_ServerError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "")
	_, err := client.CompleteWithTools(context.Background(), []analyzer.ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}

func TestOpenAICompatibleClient_CompleteWithTools_BadJSON_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "")
	_, err := client.CompleteWithTools(context.Background(), []analyzer.ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

func TestOpenAICompatibleClient_CompleteWithTools_EmptyChoices_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "")
	_, err := client.CompleteWithTools(context.Background(), []analyzer.ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for empty choices, got nil")
	}
}

func TestOpenAICompatibleClient_WithAPIKey_SendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}},
			},
		})
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "secret-key")
	_, err := client.Complete(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("expected Authorization header 'Bearer secret-key', got %q", gotAuth)
	}
}

func TestOpenAICompatibleClient_BadJSON_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json at all"))
	}))
	defer srv.Close()

	client := analyzer.NewOpenAICompatibleClient(srv.URL, "test-model", "")
	_, err := client.Complete(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error for bad JSON response")
	}
}
