package site

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThemeFSContainsHugoTomlSchema(t *testing.T) {
	t.Parallel()

	info, err := fs.Stat(themeFS, "assets/theme/hextra/theme.toml")
	require.NoError(t, err, "theme.toml must be embedded in themeFS")
	require.False(t, info.IsDir(), "theme.toml must be a file, not a directory")
	require.Greater(t, info.Size(), int64(0), "theme.toml must be non-empty")
}

func TestTemplatesFSExists(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(templatesFS, "assets/templates")
	require.NoError(t, err, "templatesFS must contain assets/templates directory")
	// Directory exists; contents may be empty or contain only .gitkeep.
	_ = entries
}

// TestBaseofRendersFTGDisclaimerBelowNavbar locks in two invariants:
//  1. The disclaimer banner partial is included in the baseof template, so
//     every page rendered by Hugo carries it.
//  2. It is rendered AFTER the navbar partial so it appears just below the
//     site header (not above it, which would put it above the masthead).
func TestBaseofRendersFTGDisclaimerBelowNavbar(t *testing.T) {
	t.Parallel()

	raw, err := fs.ReadFile(themeFS, "assets/theme/hextra/layouts/baseof.html")
	require.NoError(t, err)
	body := string(raw)

	navbarIdx := strings.Index(body, `partial "navbar.html"`)
	require.GreaterOrEqual(t, navbarIdx, 0, "baseof must include the navbar partial")

	disclaimerIdx := strings.Index(body, `partial "ftg-disclaimer.html"`)
	require.GreaterOrEqual(t, disclaimerIdx, 0, "baseof must include the ftg-disclaimer partial")
	require.Greater(t, disclaimerIdx, navbarIdx, "ftg-disclaimer must render after the navbar so it sits below the header")
}

// TestFTGCustomFooterContent pins the Doc Holiday footer that renders at the
// bottom of every page via Hextra's `custom/footer.html` slot. The slot is
// wired in by Hextra's stock `footer.html`, so populating this file is the
// only thing needed to put the block on every page.
func TestFTGCustomFooterContent(t *testing.T) {
	t.Parallel()

	raw, err := fs.ReadFile(themeFS, "assets/theme/hextra/layouts/_partials/custom/footer.html")
	require.NoError(t, err)
	body := string(raw)

	// Exact marketing copy — each load-bearing sentence is pinned so future
	// edits are deliberate.
	require.Contains(t, body, "Find the Gaps is brought to you by Doc Holiday.")
	require.Contains(t, body, "Use FTG to identify places where documentation can improve; buy Doc Holiday to ensure it never deviates again.")
	require.Contains(t, body, "Email")
	require.Contains(t, body, "to get started, or visit")

	// Email must be a mailto link, doc.holiday must link to the homepage.
	require.Contains(t, body, `<a href="mailto:support@doc.holiday">support@doc.holiday</a>`,
		"support email must be a mailto: link")
	require.Contains(t, body, `href="https://doc.holiday"`,
		"`doc.holiday` must link to https://doc.holiday")

	// External marketing link should open in a new tab so the report stays
	// open in the reader's primary tab.
	require.Contains(t, body, `target="_blank"`)
	require.Contains(t, body, `rel="noopener"`)

	// The icon is rendered inline (no asset-pipeline dependency). The brand
	// fills are hex-coded and unique to the Doc Holiday mark — pin one of
	// them so a copy/paste regression that drops the SVG is caught.
	require.Contains(t, body, "<svg",
		"footer must include the Doc Holiday icon as inline SVG")
	require.Contains(t, body, "#1BB7D1",
		"icon must keep the Doc Holiday cyan brand color")
}

// TestFTGDisclaimerPartialContent verifies the disclaimer partial carries the
// exact wording the product team approved. The phrasing is product copy —
// changes should be deliberate, so the test pins each load-bearing sentence.
func TestFTGDisclaimerPartialContent(t *testing.T) {
	t.Parallel()

	raw, err := fs.ReadFile(themeFS, "assets/theme/hextra/layouts/_partials/ftg-disclaimer.html")
	require.NoError(t, err)
	body := string(raw)

	require.Contains(t, body, `<strong class="ftg-disclaimer-label">Disclaimer:</strong>`,
		"`Disclaimer:` must be emphasised so it reads as the lead-in")
	require.Contains(t, body, "Find the Gaps (FTG) compares code to documentation and exposes areas of under-documentation, drift, missing screenshots, etc.")
	require.Contains(t, body, "It uses LLMs to work its magic.")
	require.Contains(t, body, "LLMs make mistakes.")
	require.Contains(t, body, "Some output may be incorrect, and if so, please forgive us")

	// The trailing ":)" emoji on the first paragraph was removed — the
	// follow-up sentence now carries the only smile so the lead-in reads
	// less flippant against an actual disclaimer.
	require.NotContains(t, body, "please forgive us :)",
		"trailing :) on the LLM-fallibility sentence was removed")

	// Support copy must remain inline, with the email rendered as a mailto
	// link so it is one click to fire off a support email.
	require.Contains(t, body, "Any questions or issues?")
	require.Contains(t, body, "Please reach out to")
	require.Contains(t, body, `<a href="mailto:support@doc.holiday">support@doc.holiday</a>`,
		"support email must be a mailto: link, not a plain string")
	require.Contains(t, body, "We want to help :)")

	// The disclaimer + support copy now share a single paragraph — no <p>
	// break between "please forgive us." and "Any questions or issues?".
	// The two phrases must therefore appear inside the same <p>...</p>
	// block, and there must be exactly one <p> in the partial.
	require.Equal(t, 1, strings.Count(body, "<p>"),
		"banner must have a single <p>; disclaimer and support copy share one paragraph")
	pStart := strings.Index(body, "<p>")
	pEnd := strings.Index(body, "</p>")
	require.GreaterOrEqual(t, pStart, 0)
	require.GreaterOrEqual(t, pEnd, pStart)
	para := body[pStart:pEnd]
	require.Contains(t, para, "please forgive us")
	require.Contains(t, para, "Any questions or issues?",
		"`Any questions or issues?` must live in the same paragraph as the disclaimer (no line break)")
}
