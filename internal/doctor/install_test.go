package doctor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestRunInstall_AlreadyInstalled_Skips(t *testing.T) {
	tools := []Tool{{
		Name:   "mdfetch",
		Binary: "mdfetch",
		InstallCmds: map[string][]string{
			"darwin": {"npm", "install", "-g", "@sandgarden/mdfetch"},
		},
	}}
	lookup := func(binary string) bool { return binary == "mdfetch" }
	var ran []string
	runner := func(_ context.Context, _, _ io.Writer, name string, _ ...string) error {
		ran = append(ran, name)
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runInstall(context.Background(), tools, "darwin", &stdout, &stderr, lookup, runner)
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if len(ran) != 0 {
		t.Errorf("install command ran unexpectedly: %v", ran)
	}
	if !strings.Contains(stdout.String(), "already installed") {
		t.Errorf("stdout missing 'already installed'; got %q", stdout.String())
	}
}

func TestRunInstall_Missing_RunsInstallCmd(t *testing.T) {
	tools := []Tool{{
		Name:   "mdfetch",
		Binary: "mdfetch",
		InstallCmds: map[string][]string{
			"darwin": {"npm", "install", "-g", "@sandgarden/mdfetch"},
		},
	}}
	lookup := func(_ string) bool { return false }
	var ran [][]string
	runner := func(_ context.Context, _, _ io.Writer, name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...))
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runInstall(context.Background(), tools, "darwin", &stdout, &stderr, lookup, runner)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if len(ran) != 1 {
		t.Fatalf("expected 1 install command, got %d: %v", len(ran), ran)
	}
	want := []string{"npm", "install", "-g", "@sandgarden/mdfetch"}
	for i, a := range want {
		if ran[0][i] != a {
			t.Errorf("cmd arg[%d] = %q, want %q", i, ran[0][i], a)
		}
	}
}

func TestRunInstall_UnsupportedPlatform_ReturnsOne(t *testing.T) {
	tools := []Tool{{
		Name:        "ripgrep",
		Binary:      "rg",
		InstallHint: "brew install ripgrep",
		InstallCmds: map[string][]string{
			"darwin": {"brew", "install", "ripgrep"},
		},
	}}
	lookup := func(_ string) bool { return false }
	runner := func(_ context.Context, _, _ io.Writer, _ string, _ ...string) error { return nil }
	var stdout, stderr bytes.Buffer
	code := runInstall(context.Background(), tools, "linux", &stdout, &stderr, lookup, runner)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "ripgrep") {
		t.Errorf("stderr missing tool name; got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "brew install ripgrep") {
		t.Errorf("stderr missing install hint; got %q", stderr.String())
	}
}

func TestRunInstall_InstallFails_ReturnsOne(t *testing.T) {
	tools := []Tool{{
		Name:   "mdfetch",
		Binary: "mdfetch",
		InstallCmds: map[string][]string{
			"darwin": {"npm", "install", "-g", "@sandgarden/mdfetch"},
		},
	}}
	lookup := func(_ string) bool { return false }
	runner := func(_ context.Context, _, _ io.Writer, _ string, _ ...string) error {
		return fmt.Errorf("npm not found")
	}
	var stdout, stderr bytes.Buffer
	code := runInstall(context.Background(), tools, "darwin", &stdout, &stderr, lookup, runner)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "npm not found") {
		t.Errorf("stderr missing error; got %q", stderr.String())
	}
}

func TestRunInstall_PublicFunc_AllPresent_ReturnsZero(t *testing.T) {
	// RunInstall (public) uses real exec.LookPath. Write fake binaries so both
	// tools are found and no actual install commands run.
	dir := t.TempDir()
	writeFakeBin(t, dir, "rg", "ripgrep 14.0.0")
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.0.0")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := RunInstall(context.Background(), &stdout, &stderr)
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already installed") {
		t.Errorf("stdout missing 'already installed'; got %q", stdout.String())
	}
}

func TestDefaultRunner_RunsCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := defaultRunner(context.Background(), &stdout, io.Discard, "echo", "hello")
	if err != nil {
		t.Fatalf("defaultRunner returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "hello") {
		t.Errorf("expected stdout to contain 'hello', got %q", stdout.String())
	}
}

func TestRunInstall_MultipleTools_SkipsInstalledInstallsMissing(t *testing.T) {
	tools := []Tool{
		{
			Name: "ripgrep", Binary: "rg",
			InstallCmds: map[string][]string{"darwin": {"brew", "install", "ripgrep"}},
		},
		{
			Name: "mdfetch", Binary: "mdfetch",
			InstallCmds: map[string][]string{"darwin": {"npm", "install", "-g", "@sandgarden/mdfetch"}},
		},
	}
	lookup := func(binary string) bool { return binary == "rg" } // rg present, mdfetch missing
	var installed []string
	runner := func(_ context.Context, _, _ io.Writer, name string, args ...string) error {
		installed = append(installed, args[len(args)-1])
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runInstall(context.Background(), tools, "darwin", &stdout, &stderr, lookup, runner)
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if len(installed) != 1 || installed[0] != "@sandgarden/mdfetch" {
		t.Errorf("expected only mdfetch installed, got %v", installed)
	}
}
