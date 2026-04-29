package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunHTTPServer_servesAndShutsDown(t *testing.T) {
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout safeBuffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = runHTTPServer(ctx, &stdout, siteDir, "127.0.0.1:0", false)
	}()

	deadline := time.Now().Add(3 * time.Second)
	var url string
	for time.Now().Before(deadline) {
		if m := servingURLRe.FindString(stdout.String()); m != "" {
			url = strings.TrimRight(m, "/")
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if url == "" {
		t.Fatalf("never saw serving URL; out=%q", stdout.String())
	}

	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Contains(body, []byte("hi")) {
		t.Errorf("body = %q", body)
	}

	cancel()
	wg.Wait()
	if runErr != nil {
		t.Errorf("runHTTPServer returned %v on graceful shutdown", runErr)
	}
}
