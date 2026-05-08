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
	makeURL := func(rel string) string {
		return fmt.Sprintf("https://%s/%s/%s/blob/%s/%s",
			host, owner, name, ref, filepath.ToSlash(rel))
	}
	root := filepath.Join(repo, sub)
	return walkDocs(repo, root, makeURL)
}

// WalkLocal returns a map from file:// URL → absolute file path for every
// documentation file under root. When root names a single file it returns a
// one-entry map.
func WalkLocal(root string) (map[string]string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve abs path %s: %w", root, err)
	}
	base := abs
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		base = filepath.Dir(abs)
	}
	makeURL := func(rel string) string {
		return "file://" + filepath.Join(base, rel)
	}
	return walkDocs(base, abs, makeURL)
}

// walkDocs walks the filesystem rooted at root, filters to documentation
// extensions, skips common non-source dirs, and synthesizes URLs via makeURL.
// rel paths in makeURL are relative to baseDir, which lets callers control the
// prefix without changing the walker. When root names a single file rather
// than a directory, exactly one entry is returned.
func walkDocs(baseDir, root string, makeURL func(rel string) string) (map[string]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}

	out := make(map[string]string)
	add := func(absPath string) {
		ext := strings.ToLower(filepath.Ext(absPath))
		if _, ok := docExtensions[ext]; !ok {
			return
		}
		rel, err := filepath.Rel(baseDir, absPath)
		if err != nil {
			return
		}
		out[makeURL(filepath.ToSlash(rel))] = absPath
	}

	if !info.IsDir() {
		add(root)
		return out, nil
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		add(path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
