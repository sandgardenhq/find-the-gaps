package action

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeGh writes a bash stub at <dir>/gh that:
//  1. records its argv to <dir>/gh.args (one line per call), and
//  2. when invoked as `gh release download <tag> --repo <repo> --pattern <asset> --dir <out>`,
//     drops a tarball containing the file `ftg` into <out>/<asset>.
func writeFakeGh(t *testing.T, dir string) {
	t.Helper()

	tarballPath := filepath.Join(dir, "fake-asset.tar.gz")
	writeFakeTarball(t, tarballPath, "ftg", "#!/bin/sh\necho fake-ftg\n")

	stub := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
echo "$@" >> %q
if [[ "${1:-}" == "release" && "${2:-}" == "download" ]]; then
  shift 2
  tag="$1"; shift
  out=""
  pattern=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --pattern) pattern="$2"; shift 2 ;;
      --dir)     out="$2"; shift 2 ;;
      --repo)    shift 2 ;;
      *)         shift ;;
    esac
  done
  cp %q "${out}/${pattern}"
fi
`, filepath.Join(dir, "gh.args"), tarballPath)

	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakeTarball(t *testing.T, path, name, content string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallBinary_UsesGhReleaseDownload(t *testing.T) {
	stubDir := t.TempDir()
	writeFakeGh(t, stubDir)

	dest := t.TempDir()
	scriptPath, err := filepath.Abs(filepath.Join("scripts", "install-binary.sh"))
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath,
		"sandgardenhq/find-the-gaps",
		"v0.1.1",
		"find-the-gaps_v0.1.1_linux-amd64.tar.gz",
		dest,
	)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("install-binary.sh failed: %v\n%s", err, out.String())
	}

	argsLog, err := os.ReadFile(filepath.Join(stubDir, "gh.args"))
	if err != nil {
		t.Fatalf("gh stub never invoked: %v", err)
	}
	got := string(argsLog)
	for _, want := range []string{
		"release download",
		"v0.1.1",
		"--repo sandgardenhq/find-the-gaps",
		"--pattern find-the-gaps_v0.1.1_linux-amd64.tar.gz",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("gh args missing %q\nfull args: %s", want, got)
		}
	}

	bin := filepath.Join(dest, "ftg")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("expected extracted binary at %s: %v", bin, err)
	}
}

func TestInstallBinary_RequiresAllArgs(t *testing.T) {
	scriptPath, err := filepath.Abs(filepath.Join("scripts", "install-binary.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", scriptPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit when no args passed; output: %s", out.String())
	}
}
