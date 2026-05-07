package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultBaseURL is the GitHub API base used in production. Tests override
// this with an httptest.Server URL.
const DefaultBaseURL = "https://api.github.com"

// Fetcher carries the inputs FetchLatestTag needs. Kept as a struct so the
// CLI passes the same shape tests do.
type Fetcher struct {
	BaseURL   string
	UserAgent string
	Timeout   time.Duration
}

// FetchLatestTag hits GitHub's /releases/latest endpoint and returns the
// `tag_name` field. Any non-2xx, malformed body, or timeout returns an error;
// the orchestrator swallows it (best-effort behavior).
func FetchLatestTag(ctx context.Context, f Fetcher) (string, error) {
	if f.BaseURL == "" {
		f.BaseURL = DefaultBaseURL
	}

	ctx, cancel := context.WithTimeout(ctx, f.Timeout)
	defer cancel()

	url := f.BaseURL + "/repos/sandgardenhq/find-the-gaps/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("github releases/latest: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", errors.New("github releases/latest: empty tag_name")
	}
	return payload.TagName, nil
}
