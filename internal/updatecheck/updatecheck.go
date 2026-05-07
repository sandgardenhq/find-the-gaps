package updatecheck

import (
	"context"
	"time"

	"golang.org/x/mod/semver"
)

// CacheWindow is how long a cached check is considered fresh enough to skip
// the network. The plan calls for 24h.
const CacheWindow = 24 * time.Hour

// RunOptions bundles every input Run needs. Tests build it directly; the CLI
// builds it from the cobra context + os.Getenv + runtime.GOOS.
type RunOptions struct {
	CurrentVersion string
	Command        string
	StderrIsTTY    bool
	Env            func(string) string
	GOOS           string
	BrewOnPath     bool

	CachePath string
	BaseURL   string
	Timeout   time.Duration

	// Now lets tests inject deterministic time. Defaults to time.Now.
	Now func() time.Time
}

// Run executes the full update-check pipeline:
//
//  1. Gate: short-circuit on dev builds, opt-out env vars, trivial commands,
//     non-TTY stderr.
//  2. Cache: if a fresh entry exists, use it without hitting the network.
//  3. Fetch: hit GitHub for the latest tag.
//  4. Cache write: persist the freshly-fetched tag (even when up to date) so
//     we don't re-hit the API for the rest of the cache window.
//  5. Render: if remote is newer than current, return the platform-aware
//     notice. Otherwise return "".
//
// All network failures are swallowed and yield notice="", err=nil. Run is
// best-effort: a bad GitHub response must never break the user's command.
func Run(ctx context.Context, o RunOptions) (string, error) {
	if skip, _ := ShouldSkip(GateInputs{
		Env:         o.Env,
		Version:     o.CurrentVersion,
		Command:     o.Command,
		StderrIsTTY: o.StderrIsTTY,
	}); skip {
		return "", nil
	}

	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	currentTime := now()

	cached, _ := ReadCache(o.CachePath)
	if cached.IsFresh(currentTime, CacheWindow) {
		return decideNotice(o, cached.LatestVersion), nil
	}

	tag, err := FetchLatestTag(ctx, Fetcher{
		BaseURL:   o.BaseURL,
		UserAgent: "find-the-gaps/" + o.CurrentVersion,
		Timeout:   o.Timeout,
	})
	if err != nil {
		return "", nil
	}

	_ = WriteCache(o.CachePath, Cache{
		LastCheckedAt:         currentTime,
		LatestVersion:         tag,
		CurrentVersionAtCheck: o.CurrentVersion,
	})

	return decideNotice(o, tag), nil
}

// decideNotice compares current to latest and renders the notice if behind.
// Centralized here so the cache-hit and network paths use identical logic.
func decideNotice(o RunOptions, latest string) string {
	if latest == "" {
		return ""
	}
	if semver.Compare(o.CurrentVersion, latest) >= 0 {
		return ""
	}
	return RenderNotice(o.CurrentVersion, latest, o.GOOS, o.BrewOnPath)
}
