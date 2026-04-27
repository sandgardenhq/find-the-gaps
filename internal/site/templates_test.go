package site

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHugoConfigMirror(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:          "Find the Gaps — myrepo",
		Mode:           ModeMirror,
		ScreenshotsRan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`baseURL = "/"`,
		`theme = "hextra"`,
		`title = "Find the Gaps — myrepo"`,
		`[[menu.main]]`,
		`name = "Mapping"`,
		`name = "Gaps"`,
		`name = "Screenshots"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[taxonomies]") {
		t.Error("mirror mode must not declare taxonomies")
	}
}

func TestRenderHugoConfigOmitsScreenshotsWhenNotRan(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:          "x",
		Mode:           ModeMirror,
		ScreenshotsRan: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, `name = "Screenshots"`) {
		t.Errorf("Screenshots menu should be omitted; got:\n%s", got)
	}
}

func TestRenderHugoConfigExpandedHasTaxonomies(t *testing.T) {
	got, err := renderHugoConfig(hugoConfigData{
		Title:          "x",
		Mode:           ModeExpanded,
		ScreenshotsRan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`[taxonomies]`,
		`layer        = "layers"`,
		`status       = "statuses"`,
		`user_facing  = "user_facing"`,
		`name = "Features"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, `name = "Mapping"`) {
		t.Error("expanded mode should use Features, not Mapping")
	}
}

// suppress unused warning while we wire things up
var _ = time.Now
