package forge

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var docExtensions = map[string]struct{}{
	".md":       {},
	".markdown": {},
	".mdx":      {},
	".rst":      {},
	".adoc":     {},
	".asciidoc": {},
}

// Walk returns a map from synthesized forge URL → absolute file path for every
// documentation file under repo/sub. When sub names a single file it returns a
// one-entry map. ref is the branch name baked into synthesized URLs.
func Walk(repo, sub, ref, host, owner, name string) (map[string]string, error) {
	if ref == "" {
		ref = "main"
	}
	root := filepath.Join(repo, sub)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}

	out := make(map[string]string)
	add := func(rel string) {
		ext := strings.ToLower(filepath.Ext(rel))
		if _, ok := docExtensions[ext]; !ok {
			return
		}
		url := fmt.Sprintf("https://%s/%s/%s/blob/%s/%s",
			host, owner, name, ref, filepath.ToSlash(rel))
		out[url] = filepath.Join(repo, rel)
	}

	if !info.IsDir() {
		// Single-file URL.
		add(sub)
		return out, nil
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip common build/dependency dirs even without .gitignore plumbing.
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		add(rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
