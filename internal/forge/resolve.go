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
	// true, e.g. "docs-url is a forge URL; reading markdown from --repo on disk."
	Notice string
}

// Resolve decides how to ingest docsURL.
//
//   - When docsURL is not a forge URL (and forgeFlag is empty), Result.OnDisk is
//     false and the caller should crawl normally.
//   - When docsURL is a forge URL and --repo is a clone of the same repository,
//     Result.OnDisk is true with Pages populated.
//   - In every other forge case (no --repo, mismatched origin, wiki path, no
//     git, etc.), returns ErrForgeNotIngestable.
//
// forgeFlag is the value of --forge (empty when unset). When non-empty, host
// detection is bypassed and the URL's path is parsed as a forge URL.
func Resolve(docsURL, repoPath, forgeFlag string) (Result, error) {
	parsed, perr := url.Parse(docsURL)
	if perr != nil {
		return Result{}, fmt.Errorf("parse docs-url: %w", perr)
	}
	host := strings.ToLower(parsed.Hostname())
	if forgeFlag == "" && !IsForgeHost(host) {
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
		return Result{}, fmt.Errorf("%w: --repo origin is %s/%s/%s, docs-url targets %s/%s/%s",
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
		Notice: fmt.Sprintf("docs-url is a forge URL; reading markdown from %s on disk.", repoPath),
	}, nil
}
