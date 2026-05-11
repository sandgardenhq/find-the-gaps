package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchLatestTag_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/sandgardenhq/find-the-gaps/releases/latest", r.URL.Path)
		assert.Contains(t, r.Header.Get("User-Agent"), "find-the-gaps")
		assert.Contains(t, r.Header.Get("Accept"), "application/vnd.github+json")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.4.2","name":"v1.4.2"}`))
	}))
	defer srv.Close()

	tag, err := FetchLatestTag(context.Background(), Fetcher{
		BaseURL:   srv.URL,
		UserAgent: "find-the-gaps/v1.3.0",
		Timeout:   2 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "v1.4.2", tag)
}

func TestFetchLatestTag_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := FetchLatestTag(context.Background(), Fetcher{
		BaseURL:   srv.URL,
		UserAgent: "find-the-gaps/v1.3.0",
		Timeout:   2 * time.Second,
	})
	require.Error(t, err)
}

func TestFetchLatestTag_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := FetchLatestTag(context.Background(), Fetcher{
		BaseURL:   srv.URL,
		UserAgent: "find-the-gaps/v1.3.0",
		Timeout:   2 * time.Second,
	})
	require.Error(t, err)
}

func TestFetchLatestTag_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not valid`))
	}))
	defer srv.Close()

	_, err := FetchLatestTag(context.Background(), Fetcher{
		BaseURL:   srv.URL,
		UserAgent: "find-the-gaps/v1.3.0",
		Timeout:   2 * time.Second,
	})
	require.Error(t, err)
}

func TestFetchLatestTag_EmptyTagIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":""}`))
	}))
	defer srv.Close()

	_, err := FetchLatestTag(context.Background(), Fetcher{
		BaseURL:   srv.URL,
		UserAgent: "find-the-gaps/v1.3.0",
		Timeout:   2 * time.Second,
	})
	require.Error(t, err)
}

func TestFetchLatestTag_HonorsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"tag_name":"v1.4.2"}`))
	}))
	defer srv.Close()

	start := time.Now()
	_, err := FetchLatestTag(context.Background(), Fetcher{
		BaseURL:   srv.URL,
		UserAgent: "find-the-gaps/v1.3.0",
		Timeout:   25 * time.Millisecond,
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 150*time.Millisecond, "fetch should bail out near the timeout, not wait for the slow server")
}

func TestFetchLatestTag_ContextCancellationBeatsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := FetchLatestTag(ctx, Fetcher{
		BaseURL:   srv.URL,
		UserAgent: "find-the-gaps/v1.3.0",
		Timeout:   2 * time.Second,
	})
	require.Error(t, err)
	// Either context.Canceled directly or wrapped — accept either shape.
	assert.True(t,
		strings.Contains(err.Error(), "context canceled") ||
			strings.Contains(err.Error(), "canceled"),
		"expected cancellation error, got: %v", err)
}
