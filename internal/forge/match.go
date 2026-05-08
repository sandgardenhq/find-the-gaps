package forge

import "strings"

// SameRepo reports whether docs and remote refer to the same forge repository.
// Host comparison is exact (already lowercased); owner/repo are case-insensitive.
func SameRepo(docs URL, remote Remote) bool {
	return docs.Host == remote.Host &&
		strings.EqualFold(docs.Owner, remote.Owner) &&
		strings.EqualFold(docs.Repo, remote.Repo)
}
