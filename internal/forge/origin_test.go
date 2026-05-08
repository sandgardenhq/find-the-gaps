package forge

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--allow-empty", "-q", "-m", "init"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
}

func TestReadOrigin_present(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	cmd := exec.Command("git", "remote", "add", "origin", "https://github.com/foo/bar.git")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	got, err := ReadOrigin(dir)
	if err != nil {
		t.Fatalf("ReadOrigin: %v", err)
	}
	if got != "https://github.com/foo/bar.git" {
		t.Fatalf("got %q", got)
	}
}

func TestReadOrigin_missing(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	if _, err := ReadOrigin(dir); err == nil {
		t.Fatal("expected error when origin is unset")
	}
}

func TestReadOrigin_notARepo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	if _, err := ReadOrigin(dir); err == nil {
		t.Fatal("expected error for non-repo path")
	}
}
