package action

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runScript(t *testing.T, script string, args ...string) (stdout string, exitCode int) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{abs}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("script %s: %v\n%s", script, err, out.String())
	}
	return strings.TrimSpace(out.String()), exitCode
}

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{"semver tag", "v1.2.3", "v1.2.3"},
		{"semver with patch zero", "v0.1.0", "v0.1.0"},
		{"branch name falls back to latest", "main", "latest"},
		{"floating major falls back to latest", "v1", "latest"},
		{"empty ref falls back to latest", "", "latest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, code := runScript(t, "resolve-version.sh", tc.ref)
			if code != 0 {
				t.Fatalf("exit code %d, output %q", code, got)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
