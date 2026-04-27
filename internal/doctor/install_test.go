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
		Name:        "mdfetch",
		Binary:      "mdfetch",
		InstallHint: "npm install -g @sandgarden/mdfetch",
		InstallCmds: map[string][]string{
			"darwin": {"npm", "install", "-g", "@sandgarden/mdfetch"},
		},
	}}
	lookup := func(_ string) bool { return false }
	runner := func(_ context.Context, _, _ io.Writer, _ string, _ ...string) error { return nil }
	var stdout, stderr bytes.Buffer
	code := runInstall(context.Background(), tools, "linux", &stdout, &stderr, lookup, runner)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "mdfetch") {
		t.Errorf("stderr missing tool name; got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "npm install -g @sandgarden/mdfetch") {
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
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.0.0")
	writeFakeBin(t, dir, "hugo", "hugo v0.154.5+extended darwin/arm64")
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
