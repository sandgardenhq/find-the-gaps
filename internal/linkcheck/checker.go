package linkcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Bucket classifies a probe outcome.
type Bucket int

const (
	BucketOK Bucket = iota
	BucketBroken
	BucketAuth
	BucketRedirected
)

// Result is the outcome of probing a single URL.
type Result struct {
	URL         string    `json:"url"`
	FinalURL    string    `json:"final_url,omitempty"`
	StatusChain []int     `json:"status_chain,omitempty"`
	ErrorType   string    `json:"error_type,omitempty"`
	Detail      string    `json:"detail,omitempty"`
	Bucket      Bucket    `json:"bucket"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Checker probes a URL and returns its Result.
type Checker interface {
	Check(ctx context.Context, url string) Result
}

// HTTPChecker is the production Checker. Zero-value fields take production
// defaults via NewHTTPChecker.
type HTTPChecker struct {
	Client       *http.Client
	UserAgent    string
	RetryBackoff time.Duration
}

// NewHTTPChecker builds an HTTPChecker with sensible defaults.
func NewHTTPChecker(client *http.Client, userAgent string) *HTTPChecker {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPChecker{
		Client:       client,
		UserAgent:    userAgent,
		RetryBackoff: time.Second,
	}
}

var errRedirectLoop = errors.New("redirect loop or hop cap exceeded")

// Check probes one URL.
func (c *HTTPChecker) Check(ctx context.Context, target string) Result {
	res := Result{URL: target, CheckedAt: time.Now()}

	chain, finalURL, err := c.do(ctx, "HEAD", target)
	if shouldFallbackToGET(chain, err) {
		chain, finalURL, err = c.do(ctx, "GET", target)
	}
	if shouldRetry(chain, err) {
		select {
		case <-ctx.Done():
		case <-time.After(c.RetryBackoff):
		}
		method := "HEAD"
		chain, finalURL, err = c.do(ctx, method, target)
		if shouldFallbackToGET(chain, err) {
			chain, finalURL, err = c.do(ctx, "GET", target)
		}
	}

	res.StatusChain = chain
	res.FinalURL = finalURL
	classify(&res, err)
	return res
}

// do issues one request. The CheckRedirect closure captures every redirect
// hop's status code so the caller can see the full chain.
func (c *HTTPChecker) do(ctx context.Context, method, target string) (chain []int, finalURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, "", err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	// Per-call client clone so concurrent invocations don't fight over
	// CheckRedirect. The Transport is shared (safe for concurrent use).
	client := &http.Client{
		Transport: c.Client.Transport,
		Timeout:   c.Client.Timeout,
		Jar:       c.Client.Jar,
	}
	var hops []int
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errRedirectLoop
		}
		// r.Response is the response that triggered this redirect — the
		// previous-hop's request in via does NOT carry it.
		if r.Response != nil {
			hops = append(hops, r.Response.StatusCode)
		}
		return nil
	}

	resp, doErr := client.Do(req)
	if resp != nil {
		hops = append(hops, resp.StatusCode)
		finalURL = resp.Request.URL.String()
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	// http.Client wraps redirect errors in *url.Error; unwrap so the caller
	// sees the raw errRedirectLoop sentinel.
	if doErr != nil {
		var urlErr *url.Error
		if errors.As(doErr, &urlErr) && errors.Is(urlErr.Err, errRedirectLoop) {
			doErr = errRedirectLoop
		}
	}
	return hops, finalURL, doErr
}

func shouldFallbackToGET(chain []int, err error) bool {
	if err != nil {
		return false
	}
	if len(chain) == 0 {
		return false
	}
	last := chain[len(chain)-1]
	return last == http.StatusMethodNotAllowed || last == http.StatusNotImplemented
}

func shouldRetry(chain []int, err error) bool {
	if err != nil {
		if errors.Is(err, errRedirectLoop) || errors.Is(err, context.Canceled) {
			return false
		}
		return true
	}
	if len(chain) == 0 {
		return false
	}
	last := chain[len(chain)-1]
	return last >= 500 && last <= 599
}

func classify(res *Result, err error) {
	if err != nil {
		res.Bucket = BucketBroken
		switch {
		case errors.Is(err, errRedirectLoop):
			res.ErrorType = "redirect_loop"
			res.Detail = "redirect loop or hop cap exceeded"
		case isTimeout(err):
			res.ErrorType = "timeout"
			res.Detail = "request timed out"
		case isDNS(err):
			res.ErrorType = "dns"
			res.Detail = "DNS lookup failed"
		case isTLS(err):
			res.ErrorType = "tls"
			res.Detail = "TLS verification failed"
		case isConnRefused(err):
			res.ErrorType = "connection_refused"
			res.Detail = "connection refused"
		default:
			res.ErrorType = "network"
			res.Detail = err.Error()
		}
		return
	}
	if len(res.StatusChain) == 0 {
		res.Bucket = BucketBroken
		res.ErrorType = "network"
		res.Detail = "no response"
		return
	}
	last := res.StatusChain[len(res.StatusChain)-1]
	switch {
	case last == 401 || last == 403:
		res.Bucket = BucketAuth
		res.Detail = fmt.Sprintf("HTTP %d %s", last, http.StatusText(last))
	case last >= 200 && last < 300:
		if len(res.StatusChain) > 1 && res.FinalURL != "" && res.FinalURL != res.URL {
			res.Bucket = BucketRedirected
			res.Detail = fmt.Sprintf("redirected to %s", res.FinalURL)
		} else {
			res.Bucket = BucketOK
		}
	case last >= 500 && last <= 599:
		res.Bucket = BucketBroken
		res.ErrorType = "http_5xx"
		res.Detail = fmt.Sprintf("HTTP %d %s", last, http.StatusText(last))
	case last == 404:
		res.Bucket = BucketBroken
		res.ErrorType = "http_404"
		res.Detail = "HTTP 404 Not Found"
	default:
		res.Bucket = BucketBroken
		res.ErrorType = fmt.Sprintf("http_%d", last)
		res.Detail = fmt.Sprintf("HTTP %d %s", last, http.StatusText(last))
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}

func isDNS(err error) bool {
	var dns *net.DNSError
	return errors.As(err, &dns)
}

func isTLS(err error) bool {
	return strings.Contains(err.Error(), "tls:") || strings.Contains(err.Error(), "x509:")
}

func isConnRefused(err error) bool {
	return strings.Contains(err.Error(), "connection refused")
}
