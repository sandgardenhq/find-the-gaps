package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name     string
		ldflags  string
		buildVer string
		want     string
	}{
		{
			name:     "ldflags override wins over BuildInfo",
			ldflags:  "v1.2.3",
			buildVer: "v9.9.9",
			want:     "v1.2.3",
		},
		{
			name:     "ldflags override wins when BuildInfo is devel",
			ldflags:  "v1.2.3",
			buildVer: "(devel)",
			want:     "v1.2.3",
		},
		{
			name:     "BuildInfo used when ldflags is dev sentinel",
			ldflags:  "dev",
			buildVer: "v0.4.0",
			want:     "v0.4.0",
		},
		{
			name:     "BuildInfo pseudo-version is acceptable",
			ldflags:  "dev",
			buildVer: "v0.0.0-20260424123456-abcdef012345",
			want:     "v0.0.0-20260424123456-abcdef012345",
		},
		{
			name:     "fallback to dev when BuildInfo is (devel)",
			ldflags:  "dev",
			buildVer: "(devel)",
			want:     "dev",
		},
		{
			name:     "fallback to dev when BuildInfo is empty",
			ldflags:  "dev",
			buildVer: "",
			want:     "dev",
		},
		{
			name:     "fallback to dev when both are empty",
			ldflags:  "",
			buildVer: "",
			want:     "dev",
		},
		{
			name:     "empty ldflags treated as no override",
			ldflags:  "",
			buildVer: "v2.0.0",
			want:     "v2.0.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveVersion(tc.ldflags, tc.buildVer)
			if got != tc.want {
				t.Errorf("resolveVersion(%q, %q) = %q, want %q", tc.ldflags, tc.buildVer, got, tc.want)
			}
		})
	}
}

func TestNewRootCmd_Structure(t *testing.T) {
	root := NewRootCmd()

	if root.Use != "ftg" {
		t.Errorf("Use = %q, want %q", root.Use, "ftg")
	}
	if root.Short == "" {
		t.Error("Short description is empty")
	}
	if !strings.Contains(root.Long, "documentation") {
		t.Errorf("Long description should mention documentation, got %q", root.Long)
	}
	if root.Version == "" {
		t.Error("Version is empty")
	}

	want := map[string]bool{"doctor": false, "analyze": false, "serve": false}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestAnalyze_defaultRepo_scansCurrentDir(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"analyze"})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	// Running analyze with default "." should scan successfully (even if cwd is the repo root).
	// We only check it doesn't crash — the exact output depends on the working directory.
	_ = root.Execute()
}

func TestRun_HelpReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--help"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
}

func TestRun_AnalyzeReturnsZero(t *testing.T) {
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
}

func TestErrorToExitCode_Nil_ReturnsZero(t *testing.T) {
	var stderr bytes.Buffer
	if code := errorToExitCode(nil, &stderr); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", stderr.String())
	}
}

func TestErrorToExitCode_ExitCodeError_PropagatesCode(t *testing.T) {
	var stderr bytes.Buffer
	err := &ExitCodeError{Code: 42}
	if code := errorToExitCode(err, &stderr); code != 42 {
		t.Errorf("code = %d, want 42", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("ExitCodeError should not write to stderr, got %q", stderr.String())
	}
}

func TestErrorToExitCode_PlainError_PrintsAndReturnsOne(t *testing.T) {
	var stderr bytes.Buffer
	code := errorToExitCode(errors.New("boom"), &stderr)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Error: boom") {
		t.Errorf("stderr = %q, want it to contain 'Error: boom'", stderr.String())
	}
}

func TestExitCodeError_Error(t *testing.T) {
	e := &ExitCodeError{Code: 7}
	if !strings.Contains(e.Error(), "7") {
		t.Errorf("Error() = %q, want it to contain '7'", e.Error())
	}
}

func TestExecute_ReturnsInt(t *testing.T) {
	saved := append([]string{}, os.Args...)
	defer func() { os.Args = saved }()
	os.Args = []string{"ftg", "--help"}
	if code := Execute(); code != 0 {
		t.Errorf("Execute() = %d, want 0", code)
	}
}

func TestRootCmd_verboseFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--help"})
	if code != 0 {
		t.Fatalf("--help failed: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "--verbose") {
		t.Errorf("--verbose flag not in help output:\n%s", stdout.String())
	}
}

func TestRootCmd_verboseShorthand_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run(&stdout, &stderr, []string{"--help"})
	if !strings.Contains(stdout.String(), "-v") {
		t.Errorf("-v shorthand not in help output:\n%s", stdout.String())
	}
}

func TestRootCmd_verbose_acceptedWithoutError(t *testing.T) {
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--verbose", "analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Fatalf("--verbose flag rejected (code=%d): stderr=%q", code, stderr.String())
	}
}

// NOTE: do not add t.Parallel() to tests that call run() with --verbose — the global
// charmbracelet/log logger is shared state; PersistentPreRunE resets it per invocation
// but parallel execution would race on it.
func TestRun_verbose_showsDebugOutput(t *testing.T) {
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.InfoLevel)
	})
	// Runs analyze over an empty repo (no docs-url) with --verbose.
	// Expects at least one DEBUG line in stderr.
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--verbose", "analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "DEBU") {
		t.Errorf("expected DEBU lines in stderr with --verbose; got: %q", stderr.String())
	}
}

func TestRun_noVerbose_noDebugOutput(t *testing.T) {
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.InfoLevel)
	})
	// Same analyze call without --verbose must produce no DEBUG lines.
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "DEBU") {
		t.Errorf("expected no DEBU lines in stderr without --verbose; got: %q", stderr.String())
	}
}

func TestRun_verbose_doctorShowsDebugOutput(t *testing.T) {
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.InfoLevel)
	})
	// Running doctor --verbose must produce DEBU lines in stderr
	// regardless of whether mdfetch is installed.
	var stdout, stderr bytes.Buffer
	run(&stdout, &stderr, []string{"--verbose", "doctor"})
	// Do not assert exit code — tools may or may not be present in CI.
	if !strings.Contains(stderr.String(), "DEBU") {
		t.Errorf("expected DEBU lines in stderr with --verbose doctor; got: %q", stderr.String())
	}
}

func TestRun_noVerbose_infoLogsVisible(t *testing.T) {
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.InfoLevel)
	})
	// Info-level messages from the analyze pipeline must appear even without --verbose.
	// (Nothing currently triggers a Warn in the no-docs-url path, so just confirm
	// Info lines appear — the phase-start logs are Info.)
	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	// The "scanning repository" Info log must appear at default level.
	if !strings.Contains(stderr.String(), "scanning repository") {
		t.Errorf("expected 'scanning repository' info log in stderr; got: %q", stderr.String())
	}
}
