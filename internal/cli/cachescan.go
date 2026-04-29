package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Project is one analyzed repo whose Hugo site lives under cacheDir/<Name>/site.
type Project struct {
	Name    string
	SiteDir string
}

// ListAnalyzedProjects returns every immediate subdirectory of cacheDir that
// contains a `site` subdirectory. A non-existent cacheDir is treated as "no
// analyzed projects" (not an error) so the caller can produce one helpful
// message.
func ListAnalyzedProjects(cacheDir string) ([]Project, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		siteDir := filepath.Join(cacheDir, e.Name(), "site")
		info, err := os.Stat(siteDir)
		if err != nil || !info.IsDir() {
			continue
		}
		out = append(out, Project{Name: e.Name(), SiteDir: siteDir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
