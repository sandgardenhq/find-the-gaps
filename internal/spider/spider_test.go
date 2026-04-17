package spider

import (
	"os"
	"testing"
)

// fakeFetcher returns a Fetcher that writes content to the output file.
func fakeFetcher(content string) Fetcher {
	return func(rawURL, outputPath string) error {
		return os.WriteFile(outputPath, []byte(content), 0o644)
	}
}

func TestMdfetchFetcher_missingBinary_returnsError(t *testing.T) {
	// Point PATH to an empty dir so mdfetch is not found.
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	opts := Options{CacheDir: t.TempDir(), Workers: 1}
	fetch := MdfetchFetcher(opts)
	err := fetch("https://docs.example.com", t.TempDir()+"/out.md")
	if err == nil {
		t.Error("expected error when mdfetch is not on PATH")
	}
}
