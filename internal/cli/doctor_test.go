package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_Doctor_AllPresent_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "rg", "ripgrep 99.0.0")
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != 0 {
		t.Errorf("code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ripgrep 99.0.0") {
		t.Errorf("stdout missing ripgrep version; got %q", stdout.String())
	}
}

func TestRun_Doctor_Missing_ReturnsOne(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != 1 {
		t.Errorf("code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "ripgrep") {
		t.Errorf("stderr should mention ripgrep, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "Error:") {
		t.Errorf("ExitCodeError should not print 'Error:' preamble, got %q", stderr.String())
	}
}

func writeFakeBin(t *testing.T, dir, name, versionLine string) {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho \"" + versionLine + "\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
