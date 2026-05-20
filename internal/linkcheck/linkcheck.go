package linkcheck

import (
	"context"
	"net/url"
	"sort"
	"sync"
)

// Finding is one URL's row in the report. Pages are sorted ascending for
// deterministic output.
type Finding struct {
	URL         string   `json:"url"`
	FinalURL    string   `json:"final_url,omitempty"`
	StatusChain []int    `json:"status_chain,omitempty"`
	ErrorType   string   `json:"error_type,omitempty"`
	Detail      string   `json:"detail,omitempty"`
	Pages       []string `json:"pages"`
}

// Report groups Findings by bucket. Each list is sorted by len(Pages) desc,
// tiebreak alphabetic by URL.
type Report struct {
	Broken []Finding `json:"broken"`
	Auth   []Finding `json:"auth_required"`
}

// Options configures Run.
type Options struct {
	Links          map[string][]string
	Checker        Checker
	Cache          *Cache
	Workers        int
	PerHostWorkers int
}

// Run probes every URL in opts.Links and returns the assembled Report.
func Run(ctx context.Context, opts Options) (Report, error) {
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	if opts.PerHostWorkers <= 0 {
		opts.PerHostWorkers = 4
	}

	type job struct {
		url string
	}
	jobs := make(chan job)
	results := make(chan Result, len(opts.Links))

	var (
		hostSemMu sync.Mutex
		hostSem   = map[string]chan struct{}{}
	)
	acquire := func(host string) {
		hostSemMu.Lock()
		sem, ok := hostSem[host]
		if !ok {
			sem = make(chan struct{}, opts.PerHostWorkers)
			hostSem[host] = sem
		}
		hostSemMu.Unlock()
		sem <- struct{}{}
	}
	release := func(host string) {
		hostSemMu.Lock()
		sem := hostSem[host]
		hostSemMu.Unlock()
		<-sem
	}

	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if opts.Cache != nil {
					if hit, ok := opts.Cache.Get(j.url); ok {
						results <- hit
						continue
					}
				}
				u, err := url.Parse(j.url)
				if err != nil {
					results <- Result{URL: j.url, Bucket: BucketBroken, ErrorType: "bad_url", Detail: err.Error()}
					continue
				}
				acquire(u.Host)
				r := opts.Checker.Check(ctx, j.url)
				release(u.Host)
				// Skip the cache when ctx was canceled (SIGINT, deadline).
				// Otherwise interrupted probes get classified as Broken/network
				// and silently become false-broken findings on the next run.
				if opts.Cache != nil && ctx.Err() == nil {
					opts.Cache.Put(r)
				}
				results <- r
			}
		}()
	}

	go func() {
		defer close(jobs)
		for u := range opts.Links {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{url: u}:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var rep Report
	for r := range results {
		pages := append([]string(nil), opts.Links[r.URL]...)
		sort.Strings(pages)
		f := Finding{
			URL:         r.URL,
			FinalURL:    r.FinalURL,
			StatusChain: r.StatusChain,
			ErrorType:   r.ErrorType,
			Detail:      r.Detail,
			Pages:       pages,
		}
		switch r.Bucket {
		case BucketBroken:
			rep.Broken = append(rep.Broken, f)
		case BucketAuth:
			rep.Auth = append(rep.Auth, f)
		}
	}

	sortFindings(rep.Broken)
	sortFindings(rep.Auth)
	return rep, nil
}

func sortFindings(xs []Finding) {
	sort.SliceStable(xs, func(i, j int) bool {
		if len(xs[i].Pages) != len(xs[j].Pages) {
			return len(xs[i].Pages) > len(xs[j].Pages)
		}
		return xs[i].URL < xs[j].URL
	})
}
