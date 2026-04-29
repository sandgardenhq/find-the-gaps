package ignore

import (
	"strings"
	"testing"

	gitignore "github.com/sabhiram/go-gitignore"
)

func TestDefaults_everyLineCompiles(t *testing.T) {
	for i, line := range strings.Split(defaultsContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if gi := gitignore.CompileIgnoreLines(trimmed); gi == nil {
			t.Errorf("line %d %q: CompileIgnoreLines returned nil", i+1, line)
		}
	}
}

func TestDefaults_skipsRepresentativeFiles(t *testing.T) {
	cases := []struct {
		path  string
		isDir bool
	}{
		{"node_modules", true},
		{"node_modules/foo.js", false},
		{"vendor/x/y.go", false},
		{"__pycache__/bar.pyc", false},
		{"dist/main.js", false},
		{"target/debug/foo", false},
		{".idea", true},
		{"package-lock.json", false},
		{"go.sum", false},
		{"Cargo.lock", false},
		{"foo_test.go", false},
		{"bar.test.ts", false},
		{"BazTest.java", false},
		{"api.pb.go", false},
		{"models_pb2.py", false},
		{"bundle.min.js", false},
		{"logo.png", false},
		{"data.zip", false},
		{".DS_Store", false},
		{"app.log", false},
	}
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, c := range cases {
		got := m.Match(c.path, c.isDir)
		if !got.Skip {
			t.Errorf("%s should be skipped by defaults; got %+v", c.path, got)
		}
		if got.Reason != "defaults" {
			t.Errorf("%s reason = %q, want defaults", c.path, got.Reason)
		}
	}
}

func TestDefaults_keepsRepresentativeFiles(t *testing.T) {
	keeps := []string{
		"main.go",
		"README.md",
		"docs/intro.md",
		"examples/quickstart.go",
		"package.json",
		"go.mod",
		"pyproject.toml",
		"Cargo.toml",
		"src/lib/foo.ts",
	}
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range keeps {
		if got := m.Match(p, false); got.Skip {
			t.Errorf("%s should NOT be skipped; got %+v", p, got)
		}
	}
}
