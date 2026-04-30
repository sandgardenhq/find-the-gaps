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
		Name:   "hugo",
		Binary: "hugo",
		InstallCmds: map[string][]string{
			"darwin": {"brew", "install", "hugo"},
		},
	}}
	lookup := func(binary string) bool { return binary == "hugo" }
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

func TestRunInstall_UpgradeTrue_RunsEvenWhenPresent(t *testing.T) {
	tools := []Tool{{
		Name:    "mdfetch",
		Binary:  "mdfetch",
		Upgrade: true,
		InstallCmds: map[string][]string{
			"darwin": {"npm", "install", "-g", "@sandgarden/mdfetch@latest"},
		},
	}}
	lookup := func(binary string) bool { return binary == "mdfetch" } // present
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
		t.Fatalf("expected install command to run for Upgrade=true tool, got %d invocations: %v", len(ran), ran)
	}
	want := []string{"npm", "install", "-g", "@sandgarden/mdfetch@latest"}
	for i, a := range want {
		if ran[0][i] != a {
			t.Errorf("cmd arg[%d] = %q, want %q", i, ran[0][i], a)
		}
	}
}

func TestRequiredTools_MdfetchUpgradesToLatest(t *testing.T) {
	var mdfetch *Tool
	for i := range RequiredTools {
		if RequiredTools[i].Name == "mdfetch" {
			mdfetch = &RequiredTools[i]
			break
		}
	}
	if mdfetch == nil {
		t.Fatal("mdfetch entry missing from RequiredTools")
	}
	if !mdfetch.Upgrade {
		t.Error("mdfetch RequiredTools entry must have Upgrade=true so install-deps re-runs npm and pulls the latest published version")
	}
	for goos, cmd := range mdfetch.InstallCmds {
		joined := strings.Join(cmd, " ")
		if !strings.Contains(joined, "@sandgarden/mdfetch@latest") {
			t.Errorf("mdfetch InstallCmds[%q] = %q, want it to pin @latest", goos, joined)
		}
	}
	if !strings.Contains(mdfetch.InstallHint, "@sandgarden/mdfetch@latest") {
		t.Errorf("mdfetch InstallHint = %q, want it to pin @latest", mdfetch.InstallHint)
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
	// mdfetch has Upgrade=true, so install-deps re-runs `npm install -g ...`
	// even when mdfetch is already on PATH. Provide a fake npm that succeeds
	// so the test exercises the upgrade path without needing real npm.
	writeFakeBin(t, dir, "npm", "")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := RunInstall(context.Background(), &stdout, &stderr)
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, stderr.String())
	}
	// hugo still uses skip-when-present, so the substring should appear.
	if !strings.Contains(stdout.String(), "already installed") {
		t.Errorf("stdout missing 'already installed' (expected for hugo); got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Installing mdfetch") {
		t.Errorf("stdout missing 'Installing mdfetch' (expected because mdfetch.Upgrade=true); got %q", stdout.String())
	}
}

func TestRequiredTools_HugoCoversDarwinAndLinux(t *testing.T) {
	var hugo *Tool
	for i := range RequiredTools {
		if RequiredTools[i].Name == "hugo" {
			hugo = &RequiredTools[i]
			break
		}
	}
	if hugo == nil {
		t.Fatal("hugo entry missing from RequiredTools")
	}
	for _, goos := range []string{"darwin", "linux"} {
		cmd, ok := hugo.InstallCmds[goos]
		if !ok {
			t.Errorf("hugo InstallCmds missing entry for %q", goos)
			continue
		}
		if len(cmd) == 0 {
			t.Errorf("hugo InstallCmds[%q] is empty", goos)
		}
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
