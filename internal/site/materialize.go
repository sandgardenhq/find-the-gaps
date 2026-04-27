package site

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// materialize writes a Hugo source tree into srcDir based on the inputs and options.
// srcDir must exist and be empty.
func materialize(srcDir string, in Inputs, opts BuildOptions) error {
	// 1. theme
	if err := extractEmbedFS(themeFS, "assets/theme/hextra", filepath.Join(srcDir, "themes", "hextra")); err != nil {
		return fmt.Errorf("extract theme: %w", err)
	}

	// 2. hugo.toml
	cfg, err := renderHugoConfig(hugoConfigData{
		Title:          "Find the Gaps — " + opts.ProjectName,
		Mode:           opts.Mode,
		ScreenshotsRan: in.ScreenshotsRan,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(srcDir, "hugo.toml"), []byte(cfg), 0o644); err != nil {
		return err
	}

	// 3. content/_index.md (home)
	contentDir := filepath.Join(srcDir, "content")
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		return err
	}
	home, err := renderHome(buildHomeData(in, opts))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(contentDir, "_index.md"), []byte(home), 0o644); err != nil {
		return err
	}

	// 4. mode-specific content
	switch opts.Mode {
	case ModeMirror:
		if err := materializeMirror(srcDir, contentDir, in, opts); err != nil {
			return err
		}
	case ModeExpanded:
		// Phase 4 follow-up tasks add this branch.
		return fmt.Errorf("expanded mode not yet wired in materialize")
	}
	return nil
}

func materializeMirror(srcDir, contentDir string, in Inputs, opts BuildOptions) error {
	type sec struct {
		src, dst, title string
		weight          int
	}
	secs := []sec{
		{"mapping.md", "mapping.md", "Mapping", 10},
		{"gaps.md", "gaps.md", "Gaps", 20},
	}
	if in.ScreenshotsRan {
		secs = append(secs, sec{"screenshots.md", "screenshots.md", "Screenshots", 30})
	}
	for _, s := range secs {
		body, err := os.ReadFile(filepath.Join(opts.ProjectDir, s.src))
		if err != nil {
			return fmt.Errorf("read %s: %w", s.src, err)
		}
		fm := fmt.Sprintf("+++\ntitle = %q\nweight = %d\n+++\n\n", s.title, s.weight)
		if err := os.WriteFile(filepath.Join(contentDir, s.dst), []byte(fm+string(body)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func buildHomeData(in Inputs, opts BuildOptions) homeData {
	undoc := 0
	docFeatures := map[string]bool{}
	for _, f := range in.AllDocFeatures {
		docFeatures[f] = true
	}
	for _, e := range in.Mapping {
		if len(e.Files) > 0 && !docFeatures[e.Feature.Name] && e.Feature.UserFacing {
			undoc++
		}
	}
	return homeData{
		ProjectName:           opts.ProjectName,
		GeneratedAt:           opts.GeneratedAt,
		Summary:               in.Summary.Description,
		FeatureCount:          len(in.Mapping),
		UndocumentedUserCount: undoc,
		DriftCount:            len(in.Drift),
		ScreenshotGapCount:    len(in.Screenshots),
		ScreenshotsRan:        in.ScreenshotsRan,
		Mode:                  opts.Mode,
	}
}

// extractEmbedFS copies an embedded subtree to a destination directory on disk.
func extractEmbedFS(efs fs.FS, root, dst string) error {
	return fs.WalkDir(efs, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, root)
		rel = strings.TrimPrefix(rel, "/")
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := fs.ReadFile(efs, p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}
