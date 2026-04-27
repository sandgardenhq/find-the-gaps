package site

import (
	"bytes"
	"fmt"
	"io/fs"
	"text/template"
)

// hugoConfigData drives renderHugoConfig.
//
// Mode is translated into the Expanded boolean flag at render time so that
// templates can compare against named semantics (`{{ if .Expanded }}`) rather
// than rely on integer values of the Mode iota — which would silently break
// if Mode constants are ever reordered.
type hugoConfigData struct {
	Title          string
	Mode           Mode
	ScreenshotsRan bool
}

// hugoConfigView is the data shape templates actually see. It carries the
// derived Expanded flag so templates never branch on raw Mode values.
type hugoConfigView struct {
	Title          string
	Expanded       bool
	ScreenshotsRan bool
}

// tmpl is the parsed embedded template set. Parsing happens once at package
// init; if the embedded templates fail to parse, that is a programmer error
// and we panic so it surfaces at startup rather than at first use.
var tmpl = mustParseTemplates(templatesFS)

func mustParseTemplates(efs fs.FS) *template.Template {
	t, err := parseTemplates(efs)
	if err != nil {
		panic(fmt.Sprintf("parse embedded templates: %v", err))
	}
	return t
}

func parseTemplates(efs fs.FS) (*template.Template, error) {
	return template.New("site").Funcs(template.FuncMap{
		// add helpers here as needed
	}).ParseFS(efs, "assets/templates/*.tmpl")
}

func renderHugoConfig(data hugoConfigData) (string, error) {
	view := hugoConfigView{
		Title:          data.Title,
		Expanded:       data.Mode == ModeExpanded,
		ScreenshotsRan: data.ScreenshotsRan,
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "hugo.toml.tmpl", view); err != nil {
		return "", fmt.Errorf("render hugo.toml: %w", err)
	}
	return buf.String(), nil
}
