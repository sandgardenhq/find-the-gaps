package spider

import (
	"fmt"
	"os/exec"
	"strconv"
)

// Options configures the spider.
type Options struct {
	CacheDir string
	Workers  int // number of parallel mdfetch workers; default 5
	Timeout  int // mdfetch --timeout in ms; 0 = mdfetch default
	Retries  int // mdfetch --retries; 0 = mdfetch default
}

// Fetcher fetches rawURL and writes markdown to outputPath.
type Fetcher func(rawURL, outputPath string) error

// MdfetchFetcher returns a Fetcher that shells out to the mdfetch binary.
func MdfetchFetcher(opts Options) Fetcher {
	return func(rawURL, outputPath string) error {
		args := []string{rawURL, "-o", outputPath}
		if opts.Timeout > 0 {
			args = append(args, "--timeout", strconv.Itoa(opts.Timeout))
		}
		if opts.Retries > 0 {
			args = append(args, "--retries", strconv.Itoa(opts.Retries))
		}
		cmd := exec.Command("mdfetch", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("mdfetch %s: %w\n%s", rawURL, err, out)
		}
		return nil
	}
}
