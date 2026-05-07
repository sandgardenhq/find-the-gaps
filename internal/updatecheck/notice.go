// Package updatecheck checks GitHub Releases for a newer version of ftg and
// renders a platform-aware upgrade notice. See .plans/UPDATE_CHECK_ON_STARTUP.md.
package updatecheck

import (
	"fmt"
	"strings"
)

const (
	brewLine = "  brew upgrade sandgardenhq/tap/find-the-gaps"
	goLine   = "  go install github.com/sandgardenhq/find-the-gaps/cmd/find-the-gaps@latest"
)

// RenderNotice formats the upgrade message printed on stderr after the command
// finishes. goos is runtime.GOOS. brewOnPath is whether the `brew` binary is
// available on the user's PATH; it only changes ordering on Linux.
func RenderNotice(currentVersion, latestVersion, goos string, brewOnPath bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "A new version of ftg is available: %s (you have %s)\n\n",
		latestVersion, currentVersion)

	switch goos {
	case "windows":
		b.WriteString("To upgrade with Go:\n")
		b.WriteString(goLine + "\n\n")
	case "darwin":
		b.WriteString("To upgrade on macOS or Linux with Homebrew:\n")
		b.WriteString(brewLine + "\n\n")
		b.WriteString("Or with Go:\n")
		b.WriteString(goLine + "\n\n")
	case "linux":
		if brewOnPath {
			b.WriteString("To upgrade with Homebrew:\n")
			b.WriteString(brewLine + "\n\n")
			b.WriteString("Or with Go:\n")
			b.WriteString(goLine + "\n\n")
		} else {
			b.WriteString("To upgrade with Go:\n")
			b.WriteString(goLine + "\n\n")
			b.WriteString("Or with Homebrew:\n")
			b.WriteString(brewLine + "\n\n")
		}
	default:
		b.WriteString("To upgrade with Go:\n")
		b.WriteString(goLine + "\n\n")
	}

	fmt.Fprintf(&b, "Release notes: https://github.com/sandgardenhq/find-the-gaps/releases/tag/%s\n",
		latestVersion)

	return b.String()
}
