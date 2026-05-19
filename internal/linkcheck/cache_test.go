package linkcheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "links-cache.json")

	c := NewCache(path)
	if err := c.Load(); err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if _, ok := c.Get("https://a/"); ok {
		t.Fatalf("expected empty cache miss")
	}
	c.Put(Result{URL: "https://a/", Bucket: BucketOK, CheckedAt: time.Now()})
	if err := c.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	c2 := NewCache(path)
	if err := c2.Load(); err != nil {
		t.Fatalf("load populated: %v", err)
	}
	got, ok := c2.Get("https://a/")
	if !ok {
		t.Fatalf("expected hit after reload")
	}
	if got.Bucket != BucketOK {
		t.Fatalf("got bucket=%v, want OK", got.Bucket)
	}
}

func TestCache_LoadMissingFileIsNotAnError(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "missing.json"))
	if err := c.Load(); err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if _, ok := c.Get("https://a/"); ok {
		t.Fatalf("expected empty cache")
	}
}

func TestCache_PutIsAtomicAcrossGoroutines(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "cache.json"))
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			c.Put(Result{URL: "https://a/", Bucket: BucketOK})
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		_, _ = c.Get("https://a/")
	}
	<-done
	if _, ok := c.Get("https://a/"); !ok {
		t.Fatalf("expected entry to be set")
	}
}

func TestCache_FlushIsAtomicReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	c := NewCache(path)
	c.Put(Result{URL: "https://a/", Bucket: BucketOK})
	if err := c.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	for i := 0; i < 5; i++ {
		c.Put(Result{URL: "https://a/", Bucket: BucketBroken})
		if err := c.Flush(); err != nil {
			t.Fatalf("flush #%d: %v", i, err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat after flush #%d: %v", i, err)
		}
	}
}
