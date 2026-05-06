package site

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
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
		if err := materializeExpanded(srcDir, contentDir, in, opts); err != nil {
			return err
		}
	}
	return nil
}

func materializeExpanded(srcDir, contentDir string, in Inputs, opts BuildOptions) error {
	_ = srcDir // reserved for future per-mode assets

	// Resolve slugs first; subsequent renders use the same map.
	names := make([]string, 0, len(in.Mapping))
	for _, e := range in.Mapping {
		names = append(names, e.Feature.Name)
	}
	slugs := resolveSlugs(names)

	// docFeatures set for documented status.
	docFeatures := map[string]bool{}
	for _, f := range in.AllDocFeatures {
		docFeatures[f] = true
	}

	// driftByFeature for embedded drift sections.
	driftByFeature := map[string][]driftIssue{}
	for _, d := range in.Drift {
		for _, i := range d.Issues {
			driftByFeature[d.Feature] = append(driftByFeature[d.Feature], driftIssue{
				Page:           i.Page,
				Issue:          i.Issue,
				Priority:       i.Priority,
				PriorityReason: i.PriorityReason,
			})
		}
	}

	// docPagesByFeature from DocsMap.
	docPagesByFeature := map[string][]string{}
	for _, e := range in.DocsMap {
		docPagesByFeature[e.Feature] = e.Pages
	}

	featuresDir := filepath.Join(contentDir, "features")
	if err := os.MkdirAll(featuresDir, 0o755); err != nil {
		return err
	}

	// per-feature pages
	rows := make([]featureRow, 0, len(in.Mapping))
	for _, e := range in.Mapping {
		slug := slugs[e.Feature.Name]
		if slug == "" {
			continue // skip features whose name reduces to empty slug
		}
		documented := docFeatures[e.Feature.Name]
		featureDrift := driftByFeature[e.Feature.Name]
		page, err := renderFeature(featureData{
			Name:         e.Feature.Name,
			Description:  e.Feature.Description,
			Layer:        e.Feature.Layer,
			UserFacing:   e.Feature.UserFacing,
			Documented:   documented,
			Files:        e.Files,
			Symbols:      e.Symbols,
			DocURLs:      docPagesByFeature[e.Feature.Name],
			Drift:        featureDrift,
			DriftBuckets: bucketDrift(featureDrift),
		})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(featuresDir, slug+".md"), []byte(page), 0o644); err != nil {
			return err
		}
		rows = append(rows, featureRow{
			Slug: slug, Name: e.Feature.Name, Layer: e.Feature.Layer,
			UserFacing: e.Feature.UserFacing, Documented: documented,
			FileCount: len(e.Files), DriftCount: len(driftByFeature[e.Feature.Name]),
		})
	}

	// features index
	idx, err := renderFeaturesIndex(featuresIndexData{Rows: rows})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(featuresDir, "_index.md"), []byte(idx), 0o644); err != nil {
		return err
	}

	// gaps with linked feature names — read raw gaps.md and rewrite feature names.
	gapsBody, err := os.ReadFile(filepath.Join(opts.ProjectDir, "gaps.md"))
	if err != nil {
		return fmt.Errorf("read gaps.md: %w", err)
	}
	rewritten := linkFeatureNames(string(gapsBody), slugs)
	gapsFM := "+++\ntitle = \"Gaps\"\nweight = 20\n+++\n\n"
	stripped := stripLeadingH1([]byte(rewritten))
	if err := os.WriteFile(filepath.Join(contentDir, "gaps.md"), append([]byte(gapsFM), stripped...), 0o644); err != nil {
		return err
	}

	// per-docs-page screenshot pages
	if in.ScreenshotsRan {
		ssDir := filepath.Join(contentDir, "screenshots")
		if err := os.MkdirAll(ssDir, 0o755); err != nil {
			return err
		}
		// group by page (preserve first-seen order for determinism)
		byPage := map[string][]screenshotGap{}
		var order []string
		seen := map[string]bool{}
		for _, g := range in.Screenshots {
			if !seen[g.PageURL] {
				seen[g.PageURL] = true
				order = append(order, g.PageURL)
			}
			byPage[g.PageURL] = append(byPage[g.PageURL], screenshotGap{
				Quoted: g.QuotedPassage, ShouldShow: g.ShouldShow, Alt: g.SuggestedAlt, Insert: g.InsertionHint,
				Priority: g.Priority, PriorityReason: g.PriorityReason,
			})
		}
		pageSlugs := resolveSlugs(order)
		for _, url := range order {
			body, err := renderScreenshotPage(screenshotPageData{
				PageURL: url,
				Title:   url,
				Gaps:    byPage[url],
				Buckets: bucketScreenshotGaps(byPage[url]),
			})
			if err != nil {
				return err
			}
			slug := pageSlugs[url]
			if slug == "" {
				slug = "page"
			}
			if err := os.WriteFile(filepath.Join(ssDir, slug+".md"), []byte(body), 0o644); err != nil {
				return err
			}
		}
		// also write a section index for screenshots — frontmatter title is
		// the page heading, so no body H1. When the vision pass produced
		// image issues, render them as `## Image Issues` into the index so
		// expanded-mode sites match mirror mode (which picks the section up
		// transitively by reading screenshots.md verbatim).
		var ssIdx strings.Builder
		ssIdx.WriteString("+++\ntitle = \"Screenshots\"\nweight = 30\n+++\n")
		if len(in.ImageIssues) > 0 {
			ssIdx.WriteString("\n## Image Issues\n\n")
			// Outer grouping by priority (Large -> Medium -> Small, empty
			// buckets omitted), inner grouping by page (first-occurrence
			// order). Mirrors the reporter's screenshots.md layout.
			for _, p := range []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall} {
				var bucket []analyzer.ImageIssue
				for _, ii := range in.ImageIssues {
					if ii.Priority == p {
						bucket = append(bucket, ii)
					}
				}
				if len(bucket) == 0 {
					continue
				}
				switch p {
				case analyzer.PriorityLarge:
					ssIdx.WriteString("### Large\n\n")
				case analyzer.PriorityMedium:
					ssIdx.WriteString("### Medium\n\n")
				case analyzer.PrioritySmall:
					ssIdx.WriteString("### Small\n\n")
				}
				seenPage := map[string]bool{}
				var pageOrder []string
				byPage := map[string][]analyzer.ImageIssue{}
				for _, ii := range bucket {
					if !seenPage[ii.PageURL] {
						seenPage[ii.PageURL] = true
						pageOrder = append(pageOrder, ii.PageURL)
					}
					byPage[ii.PageURL] = append(byPage[ii.PageURL], ii)
				}
				for _, page := range pageOrder {
					for _, ii := range byPage[page] {
						fmt.Fprintf(&ssIdx, "- **Page:** %s\n", ii.PageURL)
						fmt.Fprintf(&ssIdx, "  **Image:** ![%s](%s)\n", ii.Index, ii.Src)
						fmt.Fprintf(&ssIdx, "  **Issue:** %s\n", ii.Reason)
						fmt.Fprintf(&ssIdx, "  **Suggested action:** %s\n", ii.SuggestedAction)
						if ii.PriorityReason != "" {
							fmt.Fprintf(&ssIdx, "  **Why:** %s\n", ii.PriorityReason)
						}
						ssIdx.WriteString("\n")
					}
				}
			}
		}
		if err := os.WriteFile(filepath.Join(ssDir, "_index.md"), []byte(ssIdx.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// stripLeadingH1 returns body with a leading CommonMark H1 line (`# ...\n`)
// and any immediately-following blank lines removed. If body does not start
// with `# `, it is returned unchanged. The frontmatter wrapper supplies the
// page heading on the website, so the redundant H1 in the standalone reporter
// output is dropped during materialize.
func stripLeadingH1(body []byte) []byte {
	if len(body) < 2 || body[0] != '#' || body[1] != ' ' {
		return body
	}
	_, rest, _ := bytes.Cut(body, []byte{'\n'})
	for len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}
	return rest
}

// linkFeatureNames replaces quoted feature-name occurrences in body with
// quoted markdown links to /features/<slug>/. Only names from the slugs map
// are rewritten; everything else is left untouched.
func linkFeatureNames(body string, slugs map[string]string) string {
	out := body
	for name, slug := range slugs {
		if name == "" || slug == "" {
			continue
		}
		quoted := "\"" + name + "\""
		linked := "\"[" + name + "](/features/" + slug + "/)\""
		out = strings.ReplaceAll(out, quoted, linked)
	}
	return out
}

func materializeMirror(srcDir, contentDir string, in Inputs, opts BuildOptions) error {
	_ = srcDir // reserved for future per-mode assets

	// mapping.md — rendered from structured Inputs so the website's mapping
	// page can use sub-heading lists without disturbing the standalone
	// reporter output at <projectDir>/mapping.md.
	mappingBody, err := renderMappingPage(buildMappingPageData(in))
	if err != nil {
		return err
	}
	mappingFM := "+++\ntitle = \"Mapping\"\nweight = 10\n+++\n\n"
	if err := os.WriteFile(filepath.Join(contentDir, "mapping.md"), []byte(mappingFM+mappingBody), 0o644); err != nil {
		return err
	}

	// gaps.md — read raw, strip the standalone reporter's leading `# Gaps
	// Found` H1, and wrap.
	gapsBody, err := os.ReadFile(filepath.Join(opts.ProjectDir, "gaps.md"))
	if err != nil {
		return fmt.Errorf("read gaps.md: %w", err)
	}
	gapsFM := "+++\ntitle = \"Gaps\"\nweight = 20\n+++\n\n"
	if err := os.WriteFile(filepath.Join(contentDir, "gaps.md"), append([]byte(gapsFM), stripLeadingH1(gapsBody)...), 0o644); err != nil {
		return err
	}

	if in.ScreenshotsRan {
		// screenshots.md — read raw, strip the standalone reporter's leading
		// `# Missing Screenshots` H1, and wrap. Mirrors gaps.md handling so any
		// section the reporter writes (including `## Image Issues`) flows into
		// the rendered site without a duplicate template.
		ssBody, err := os.ReadFile(filepath.Join(opts.ProjectDir, "screenshots.md"))
		if err != nil {
			return fmt.Errorf("read screenshots.md: %w", err)
		}
		ssFM := "+++\ntitle = \"Screenshots\"\nweight = 30\n+++\n\n"
		if err := os.WriteFile(filepath.Join(contentDir, "screenshots.md"),
			append([]byte(ssFM), stripLeadingH1(ssBody)...), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// buildMappingPageData converts analyzer Inputs into the view shape consumed
// by the mapping_page template. The DocsMap is the source of truth for
// "Documented on" — per-feature names from AnalyzePage are not used.
func buildMappingPageData(in Inputs) mappingPageData {
	docPagesByFeature := map[string][]string{}
	for _, e := range in.DocsMap {
		docPagesByFeature[e.Feature] = e.Pages
	}
	features := make([]mappingFeature, 0, len(in.Mapping))
	for _, e := range in.Mapping {
		pages := docPagesByFeature[e.Feature.Name]
		features = append(features, mappingFeature{
			Name:        e.Feature.Name,
			Description: e.Feature.Description,
			Layer:       e.Feature.Layer,
			UserFacing:  e.Feature.UserFacing,
			Documented:  len(pages) > 0,
			Files:       e.Files,
			Symbols:     e.Symbols,
			DocURLs:     pages,
		})
	}
	return mappingPageData{
		Summary:  in.Summary.Description,
		Features: features,
	}
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
