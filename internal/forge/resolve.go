package forge

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrForgeNotIngestable is returned when the docs URL points at a forge but the
// on-disk shortcut cannot be used. Callers should print a message and exit
// non-zero.
var ErrForgeNotIngestable = errors.New("forge URL is not ingestable on disk")

// Result is the outcome of forge resolution.
type Result struct {
	// OnDisk is true when the caller should skip the spider crawl and use Pages
	// directly. False means the docs URL was not a forge URL — the caller should
	// continue with its normal crawl path.
	OnDisk bool
	// Pages is the synthesized url→filepath map populated when OnDisk is true.
	Pages map[string]string
	// Notice is a human-readable line the caller should print when OnDisk is
	// true, e.g. "--docs is a forge URL; reading markdown from --repo on disk."
	Notice string
}

// hasURLScheme reports whether s starts with http:// or https://. Anything
// else is treated as a local filesystem path.
func hasURLScheme(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// Resolve decides how to ingest docsURL.
//
//   - When docsURL is empty, scans repoPath on disk and emits file:// URLs.
//   - When docsURL is a non-URL string (no http://, https:// scheme), treats
//     it as a local path and scans there with file:// URLs.
//   - When docsURL is a forge URL and --repo is a clone of the same repository,
//     Result.OnDisk is true with synthesized forge URLs.
//   - When docsURL is any other URL, Result.OnDisk is false; the caller crawls.
//   - In every forge-URL failure case (no --repo, mismatched origin, wiki
//     path, no git, etc.), returns ErrForgeNotIngestable.
func Resolve(docsURL, repoPath string) (Result, error) {
	if docsURL == "" {
		if repoPath == "" {
			return Result{}, fmt.Errorf("no --docs provided and --repo is empty")
		}
		pages, err := WalkLocal(repoPath)
		if err != nil {
			return Result{}, fmt.Errorf("walk repo for docs: %w", err)
		}
		return Result{
			OnDisk: true,
			Pages:  pages,
			Notice: fmt.Sprintf("no --docs provided; reading markdown from %s on disk.", repoPath),
		}, nil
	}
	if !hasURLScheme(docsURL) {
		pages, err := WalkLocal(docsURL)
		if err != nil {
			return Result{}, fmt.Errorf("walk --docs path: %w", err)
		}
		return Result{
			OnDisk: true,
			Pages:  pages,
			Notice: fmt.Sprintf("reading markdown from %s on disk.", docsURL),
		}, nil
	}
	parsed, perr := url.Parse(docsURL)
	if perr != nil {
		return Result{}, fmt.Errorf("parse --docs URL: %w", perr)
	}
	host := strings.ToLower(parsed.Hostname())
	if !IsForgeHost(host) {
		return Result{OnDisk: false}, nil
	}

	purl, err := ParseURL(docsURL)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrForgeNotIngestable, err)
	}
	if purl.IsWiki {
		return Result{}, fmt.Errorf("%w: wiki URL %s", ErrForgeNotIngestable, docsURL)
	}
	if repoPath == "" {
		return Result{}, fmt.Errorf("%w: --repo not provided", ErrForgeNotIngestable)
	}
	originURL, err := ReadOrigin(repoPath)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrForgeNotIngestable, err)
	}
	remote, err := NormalizeRemote(originURL)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrForgeNotIngestable, err)
	}
	if !SameRepo(purl, remote) {
		return Result{}, fmt.Errorf("%w: --repo origin is %s/%s/%s, --docs targets %s/%s/%s",
			ErrForgeNotIngestable,
			remote.Host, remote.Owner, remote.Repo,
			purl.Host, purl.Owner, purl.Repo)
	}

	pages, err := Walk(repoPath, purl.Subpath, purl.Ref, purl.Host, purl.Owner, purl.Repo)
	if err != nil {
		return Result{}, fmt.Errorf("walk on-disk docs: %w", err)
	}
	return Result{
		OnDisk: true,
		Pages:  pages,
		Notice: fmt.Sprintf("--docs is a forge URL; reading markdown from %s on disk.", repoPath),
	}, nil
}
