package spider

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
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

type crawlResult struct {
	rawURL   string
	filePath string
	links    []string
	err      error
}

// Crawl fetches startURL and every same-host link discovered in fetched pages.
// It returns a map of URL → absolute cache file path.
// fetch is called once per URL; use MdfetchFetcher for production.
func Crawl(startURL string, opts Options, fetch Fetcher) (map[string]string, error) {
	if opts.Workers <= 0 {
		opts.Workers = 5
	}
	if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
		return nil, err
	}
	idx, err := LoadIndex(opts.CacheDir)
	if err != nil {
		return nil, err
	}

	_, err = url.Parse(startURL)
	if err != nil {
		return nil, err
	}

	pages := idx.All()
	visited := make(map[string]bool, len(pages))
	for u := range pages {
		visited[u] = true
	}

	jobs := make(chan string)
	results := make(chan crawlResult)

	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rawURL := range jobs {
				results <- doFetch(rawURL, opts.CacheDir, fetch, idx)
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	inFlight := 0
	enqueue := func(rawURL string) {
		if !visited[rawURL] {
			visited[rawURL] = true
			inFlight++
			go func() { jobs <- rawURL }()
		}
	}

	enqueue(startURL)
	if inFlight == 0 {
		close(jobs)
		return pages, nil
	}

	for res := range results {
		inFlight--
		if res.err != nil {
			// log and continue — a single failed page does not abort the crawl
			_ = res.err
		} else {
			pages[res.rawURL] = res.filePath
			for _, link := range res.links {
				enqueue(link)
			}
		}
		if inFlight == 0 {
			close(jobs)
		}
	}

	return pages, nil
}

func doFetch(rawURL, cacheDir string, fetch Fetcher, idx *Index) crawlResult {
	filename := URLToFilename(rawURL)
	filePath := filepath.Join(cacheDir, filename)

	if err := fetch(rawURL, filePath); err != nil {
		return crawlResult{rawURL: rawURL, err: err}
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return crawlResult{rawURL: rawURL, err: err}
	}

	pageURL, _ := url.Parse(rawURL)
	links := ExtractLinks(string(content), pageURL)

	if err := idx.Record(rawURL, filename); err != nil {
		return crawlResult{rawURL: rawURL, err: err}
	}

	return crawlResult{rawURL: rawURL, filePath: filePath, links: links}
}
