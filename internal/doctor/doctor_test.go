package doctor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_AllPresent_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr)

	if code != 0 {
		t.Errorf("code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "mdfetch 1.2.3") {
		t.Errorf("stdout missing mdfetch version; stdout=%q", stdout.String())
	}
	if strings.Contains(stdout.String(), "ripgrep") || strings.Contains(stdout.String(), " rg ") {
		t.Errorf("stdout must not mention ripgrep/rg; got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty on success, got %q", stderr.String())
	}
}

func TestRun_MdfetchMissing_ReturnsOne(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr)

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "mdfetch") {
		t.Errorf("stderr should mention mdfetch, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "ripgrep") {
		t.Errorf("stderr must not mention ripgrep, got %q", stderr.String())
	}
}

func TestRun_VersionCommandFails_ReturnsOne(t *testing.T) {
	dir := t.TempDir()
	writeFailingBin(t, dir, "mdfetch")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr)

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "mdfetch") {
		t.Errorf("stderr should mention mdfetch failure, got %q", stderr.String())
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

func writeFailingBin(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nexit 2\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
