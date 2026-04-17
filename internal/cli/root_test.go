package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestNewRootCmd_Structure(t *testing.T) {
	root := NewRootCmd()

	if root.Use != "find-the-gaps" {
		t.Errorf("Use = %q, want %q", root.Use, "find-the-gaps")
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

	want := map[string]bool{"doctor": false, "analyze": false}
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

func TestDoctorStub_ReturnsNotYetImplemented(t *testing.T) {
	err := runSubcommand(t, "doctor")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error = %q, want it to contain 'not yet implemented'", err.Error())
	}
}

func TestAnalyzeStub_ReturnsNotYetImplemented(t *testing.T) {
	err := runSubcommand(t, "analyze")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error = %q, want it to contain 'not yet implemented'", err.Error())
	}
}

func runSubcommand(t *testing.T, args ...string) error {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	return root.Execute()
}

func TestRun_HelpReturnsZero(t *testing.T) {
	var stderr bytes.Buffer
	code := run(&stderr, []string{"--help"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
}

func TestRun_DoctorReturnsOne(t *testing.T) {
	var stderr bytes.Buffer
	code := run(&stderr, []string{"doctor"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not yet implemented") {
		t.Errorf("stderr = %q, want it to contain 'not yet implemented'", stderr.String())
	}
}

func TestExecute_ReturnsInt(t *testing.T) {
	// Smoke test: Execute() reads os.Args; preserve and restore.
	// A bare invocation with no extra args should exit zero (prints help by default isn't set,
	// but root cmd without subcommand returns nil because there's no RunE on root).
	saved := []string{}
	saved = append(saved, os.Args...)
	defer func() { os.Args = saved }()
	os.Args = []string{"find-the-gaps", "--help"}
	if code := Execute(); code != 0 {
		t.Errorf("Execute() = %d, want 0", code)
	}
}
