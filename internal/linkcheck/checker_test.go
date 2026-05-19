package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

func TestChecker_2xxIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := NewHTTPChecker(newClient(), "test-ua")
	got := c.Check(context.Background(), srv.URL)
	if got.Bucket != BucketOK {
		t.Fatalf("bucket=%v, want OK; result=%+v", got.Bucket, got)
	}
	if got.ErrorType != "" {
		t.Fatalf("ErrorType=%q, want empty", got.ErrorType)
	}
}

func TestChecker_404IsBroken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), srv.URL)
	if got.Bucket != BucketBroken {
		t.Fatalf("bucket=%v, want Broken", got.Bucket)
	}
	if got.ErrorType != "http_404" {
		t.Fatalf("ErrorType=%q, want http_404", got.ErrorType)
	}
	if !strings.Contains(got.Detail, "404") {
		t.Fatalf("Detail=%q, want it to mention 404", got.Detail)
	}
}

func TestChecker_401And403GoToAuthBucket(t *testing.T) {
	for _, code := range []int{401, 403} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), srv.URL)
		srv.Close()
		if got.Bucket != BucketAuth {
			t.Fatalf("code=%d bucket=%v, want Auth", code, got.Bucket)
		}
	}
}

func TestChecker_RedirectToFinalURLIsRedirectedBucket(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/moved", http.StatusMovedPermanently)
	}))
	defer origin.Close()

	got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), origin.URL)
	if got.Bucket != BucketRedirected {
		t.Fatalf("bucket=%v, want Redirected; result=%+v", got.Bucket, got)
	}
	if got.FinalURL == "" || got.FinalURL == origin.URL {
		t.Fatalf("FinalURL=%q, want it to differ from origin", got.FinalURL)
	}
	if len(got.StatusChain) < 2 {
		t.Fatalf("StatusChain=%v, want at least [3xx, 200]", got.StatusChain)
	}
}

func TestChecker_HEAD405FallsBackToGET(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method == "HEAD" {
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), srv.URL)
	if got.Bucket != BucketOK {
		t.Fatalf("bucket=%v, want OK after GET fallback; result=%+v", got.Bucket, got)
	}
	if hits.Load() < 2 {
		t.Fatalf("hits=%d, want at least 2 (HEAD then GET)", hits.Load())
	}
}

func TestChecker_5xxRetriesOnceThenReportsBroken(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c := NewHTTPChecker(newClient(), "test-ua")
	c.RetryBackoff = 10 * time.Millisecond
	got := c.Check(context.Background(), srv.URL)
	if got.Bucket != BucketBroken {
		t.Fatalf("bucket=%v, want Broken", got.Bucket)
	}
	if got.ErrorType != "http_5xx" {
		t.Fatalf("ErrorType=%q, want http_5xx", got.ErrorType)
	}
	if hits.Load() < 2 {
		t.Fatalf("hits=%d, want at least 2 (initial + retry)", hits.Load())
	}
}

func TestChecker_4xxDoesNotRetry(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := NewHTTPChecker(newClient(), "test-ua")
	c.RetryBackoff = 10 * time.Millisecond
	_ = c.Check(context.Background(), srv.URL)
	if hits.Load() != 1 {
		t.Fatalf("hits=%d, want exactly 1 (no retry on 4xx)", hits.Load())
	}
}

func TestChecker_RedirectLoopIsBroken(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+r.URL.Path+"/x", http.StatusFound)
	}))
	defer srv.Close()

	got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), srv.URL+"/")
	if got.Bucket != BucketBroken {
		t.Fatalf("bucket=%v, want Broken", got.Bucket)
	}
	if got.ErrorType != "redirect_loop" {
		t.Fatalf("ErrorType=%q, want redirect_loop", got.ErrorType)
	}
}

func TestChecker_UnreachableHostIsBroken(t *testing.T) {
	c := NewHTTPChecker(newClient(), "test-ua")
	c.RetryBackoff = 10 * time.Millisecond
	got := c.Check(context.Background(), "http://127.0.0.1:1/")
	if got.Bucket != BucketBroken {
		t.Fatalf("bucket=%v, want Broken", got.Bucket)
	}
	if got.ErrorType != "connection_refused" && got.ErrorType != "dns" && got.ErrorType != "timeout" && got.ErrorType != "network" {
		t.Fatalf("ErrorType=%q, want connection_refused/dns/timeout/network", got.ErrorType)
	}
}

func TestChecker_SendsUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	_ = NewHTTPChecker(newClient(), "find-the-gaps/test").Check(context.Background(), srv.URL)
	if !strings.Contains(got, "find-the-gaps/test") {
		t.Fatalf("UA=%q, want it to contain find-the-gaps/test", got)
	}
}
