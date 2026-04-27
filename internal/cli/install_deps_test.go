package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDepsCmd_AllPresent_ExitsZero(t *testing.T) {
	// Write fake binaries so RunInstall skips actual installs.
	dir := t.TempDir()
	for _, name := range []string{"mdfetch", "hugo"} {
		path := filepath.Join(dir, name)
		script := "#!/bin/sh\necho \"" + name + " 1.0.0\"\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"install-deps"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already installed") {
		t.Errorf("expected 'already installed' in output; got %q", stdout.String())
	}
}
