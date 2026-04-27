package site

import "embed"

//go:embed assets/theme/hextra
var themeFS embed.FS

//go:embed all:assets/templates
var templatesFS embed.FS
