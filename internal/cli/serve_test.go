package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// servingURLRe matches the line `serving <dir> at http://host:port/`.
var servingURLRe = regexp.MustCompile(`http://[^\s]+`)

// safeBuffer is an io.Writer/Stringer protected by a mutex so the test
// goroutine can poll stdout while the server goroutine writes to it.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runServeAsync starts `ftg serve` with the given args under a cancelable
// context. It returns the stdout buffer, a cancel func, and a channel that
// receives the run() exit code when the server exits.
func runServeAsync(t *testing.T, args []string) (stdout *safeBuffer, cancel context.CancelFunc, done <-chan int) {
	t.Helper()
	stdout = &safeBuffer{}
	stderr := &safeBuffer{}
	ctx, cancelFn := context.WithCancel(context.Background())
	exitCh := make(chan int, 1)
	go func() {
		root := NewRootCmd()
		root.SetArgs(args)
		root.SetOut(stdout)
		root.SetErr(stderr)
		root.SetContext(ctx)
		exitCh <- errorToExitCode(root.Execute(), stderr)
	}()
	return stdout, cancelFn, exitCh
}

// waitForServingURL polls stdout until the `serving ... at http://...` line
// appears, then returns the parsed URL.
func waitForServingURL(t *testing.T, stdout *safeBuffer) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m := servingURLRe.FindString(stdout.String()); m != "" {
			return strings.TrimRight(m, "/")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("did not see serving URL within deadline; stdout=%q", stdout.String())
	return ""
}

func TestServe_missingSiteDir_returnsErrorWithHint(t *testing.T) {
	cacheBase := t.TempDir()
	repoParent := t.TempDir()
	repoDir := filepath.Join(repoParent, "neverAnalyzed")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	// Note: we deliberately do NOT create cacheBase/neverAnalyzed/site.

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"serve",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	expectedPath := filepath.Join(cacheBase, "neverAnalyzed", "site")
	if !strings.Contains(stderr.String(), expectedPath) {
		t.Errorf("stderr should name missing path %q, got %q", expectedPath, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ftg analyze") {
		t.Errorf("stderr should hint at 'ftg analyze', got %q", stderr.String())
	}
}

func TestServe_resolvesSiteDir_fromRepoAndCacheDir(t *testing.T) {
	cacheBase := t.TempDir()
	repoParent := t.TempDir()
	repoDir := filepath.Join(repoParent, "myproject")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	siteDir := filepath.Join(cacheBase, "myproject", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("hello from serve"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	t.Cleanup(cancel)

	url := waitForServingURL(t, stdout)
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "hello from serve") {
		t.Errorf("body = %q, want it to contain 'hello from serve'", string(body))
	}

	cancel()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatalf("serve did not exit within deadline")
	}
}
