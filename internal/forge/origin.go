package forge

import (
	"fmt"
	"os/exec"
	"strings"
)

// ReadOrigin returns the URL of the "origin" remote in the git repo at dir.
// Returns an error when dir is not a git repo or origin is not configured.
func ReadOrigin(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin (%s): %w: %s",
			dir, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
