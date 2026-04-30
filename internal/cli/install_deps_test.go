package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDepsCmd_InstallsHugo(t *testing.T) {
	cmd := newInstallDepsCmd()
	if !strings.Contains(cmd.Long, "hugo") {
		t.Errorf("install-deps Long description should mention hugo; got %q", cmd.Long)
	}
	if !strings.Contains(cmd.Short, "hugo") {
		t.Errorf("install-deps Short description should mention hugo; got %q", cmd.Short)
	}
}

func TestInstallDepsCmd_AllPresent_ExitsZero(t *testing.T) {
	// Write fake binaries on PATH. mdfetch has Upgrade=true so install-deps
	// always re-runs `npm install -g @sandgarden/mdfetch@latest` to pull the
	// newest published version; the fake npm makes that path succeed.
	dir := t.TempDir()
	for _, name := range []string{"mdfetch", "hugo", "npm"} {
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
		t.Errorf("expected 'already installed' (for hugo) in output; got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Installing mdfetch") {
		t.Errorf("expected 'Installing mdfetch' (Upgrade=true forces re-install) in output; got %q", stdout.String())
	}
}
