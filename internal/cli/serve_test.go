package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
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

func TestServe_open_invokesOpener(t *testing.T) {
	cacheBase := t.TempDir()
	repoParent := t.TempDir()
	repoDir := filepath.Join(repoParent, "openproj")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	siteDir := filepath.Join(cacheBase, "openproj", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	gotURL := make(chan string, 1)
	original := openInBrowser
	openInBrowser = func(url string) error {
		gotURL <- url
		return nil
	}
	t.Cleanup(func() { openInBrowser = original })

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
		"--open",
	})
	t.Cleanup(cancel)

	url := waitForServingURL(t, stdout)

	select {
	case got := <-gotURL:
		if got != url+"/" && got != url {
			t.Errorf("opener received URL %q, want %q (or with trailing slash)", got, url)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("opener was never invoked")
	}

	cancel()
	<-done
}

func TestServe_noOpenFlag_doesNotInvokeOpener(t *testing.T) {
	cacheBase := t.TempDir()
	repoParent := t.TempDir()
	repoDir := filepath.Join(repoParent, "noopen")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	siteDir := filepath.Join(cacheBase, "noopen", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	called := make(chan struct{}, 1)
	original := openInBrowser
	openInBrowser = func(url string) error {
		called <- struct{}{}
		return nil
	}
	t.Cleanup(func() { openInBrowser = original })

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	t.Cleanup(cancel)

	_ = waitForServingURL(t, stdout)

	// Give the (incorrectly wired) opener a brief window to fire if it's going to.
	select {
	case <-called:
		t.Error("opener was called even though --open was not passed")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	<-done
}

func TestServe_shutdownOnContextCancel(t *testing.T) {
	cacheBase := t.TempDir()
	repoParent := t.TempDir()
	repoDir := filepath.Join(repoParent, "shutdownproj")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	siteDir := filepath.Join(cacheBase, "shutdownproj", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("ok"), 0o644); err != nil {
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
		t.Fatalf("liveness GET: %v", err)
	}
	_ = resp.Body.Close()

	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d, want 0 on graceful shutdown", code)
		}
	case <-time.After(6 * time.Second):
		t.Fatalf("serve did not exit within 6s of context cancel")
	}

	// After shutdown the listener is closed, so a follow-up GET must fail.
	client := &http.Client{Timeout: 1 * time.Second}
	if resp, err := client.Get(url + "/"); err == nil {
		_ = resp.Body.Close()
		t.Errorf("server still responding after cancel; expected dial error")
	}
}

func TestBrowserOpenerArgs_perOS(t *testing.T) {
	const url = "http://example.test/"
	tests := []struct {
		goos     string
		wantName string
		wantArgs []string
	}{
		{"darwin", "open", []string{url}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", url}},
		{"linux", "xdg-open", []string{url}},
		{"freebsd", "xdg-open", []string{url}},
	}
	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			name, args := browserOpenerArgs(tc.goos, url)
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Errorf("args = %v, want %v", args, tc.wantArgs)
			}
		})
	}
}

func TestServe_addrInUse_returnsListenError(t *testing.T) {
	cacheBase := t.TempDir()
	repoParent := t.TempDir()
	repoDir := filepath.Join(repoParent, "addrcoll")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	siteDir := filepath.Join(cacheBase, "addrcoll", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	defer func() { _ = hold.Close() }()
	addr := hold.Addr().String()

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"serve",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--addr", addr,
	})
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero when --addr is in use; stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "listen on") {
		t.Errorf("stderr should explain the listen failure, got %q", stderr.String())
	}
}

func TestServe_open_logsWarnOnOpenerError(t *testing.T) {
	cacheBase := t.TempDir()
	repoParent := t.TempDir()
	repoDir := filepath.Join(repoParent, "openerr")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	siteDir := filepath.Join(cacheBase, "openerr", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	original := openInBrowser
	openInBrowser = func(url string) error {
		return errors.New("simulated browser failure")
	}
	t.Cleanup(func() { openInBrowser = original })

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
		"--open",
	})
	t.Cleanup(cancel)

	url := waitForServingURL(t, stdout)

	// Server must remain up despite opener failure.
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET after opener error: %v", err)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d, want 0 (opener failure must not abort serve)", code)
		}
	case <-time.After(6 * time.Second):
		t.Fatalf("serve did not exit within deadline")
	}
}

func TestServe_addrFlag_defaultsTo8080(t *testing.T) {
	cmd := newServeCmd()
	flag := cmd.Flags().Lookup("addr")
	if flag == nil {
		t.Fatal("--addr flag is not defined")
	}
	if got, want := flag.DefValue, "127.0.0.1:8080"; got != want {
		t.Errorf("--addr default = %q, want %q", got, want)
	}
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

// forceInteractive sets the test override so the picker code path runs under
// `go test` (which is otherwise non-TTY).
func forceInteractive(t *testing.T, on bool) {
	t.Helper()
	prev := testInteractiveOverride
	v := on
	testInteractiveOverride = &v
	t.Cleanup(func() { testInteractiveOverride = prev })
}

func TestServe_multipleProjects_invokesPickerAndServesChoice(t *testing.T) {
	cacheBase := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		siteDir := filepath.Join(cacheBase, name, "site")
		if err := os.MkdirAll(siteDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("hello "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	originalPicker := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		for _, p := range opts {
			if p.Name == "beta" {
				return p, nil
			}
		}
		return Project{}, errors.New("beta not in options")
	}
	t.Cleanup(func() { huhSelectFn = originalPicker })
	forceInteractive(t, true)

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	t.Cleanup(cancel)

	url := waitForServingURL(t, stdout)
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "hello beta") {
		t.Errorf("served wrong project: body=%q", body)
	}

	cancel()
	<-done
}

func TestServe_singleProject_skipsPicker(t *testing.T) {
	cacheBase := t.TempDir()
	siteDir := filepath.Join(cacheBase, "solo", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("solo"), 0o644); err != nil {
		t.Fatal(err)
	}

	called := false
	originalPicker := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		called = true
		return opts[0], nil
	}
	t.Cleanup(func() { huhSelectFn = originalPicker })
	forceInteractive(t, true)

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	t.Cleanup(cancel)

	_ = waitForServingURL(t, stdout)
	if called {
		t.Error("picker was called even though only one project exists")
	}

	cancel()
	<-done
}

func TestServe_noProjects_errorsWithHint(t *testing.T) {
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	if code == 0 {
		t.Fatalf("exit 0, want non-zero; stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "ftg analyze") {
		t.Errorf("stderr should hint at `ftg analyze`, got %q", stderr.String())
	}
}

func TestServe_projectFlag_shortCircuitsPicker(t *testing.T) {
	cacheBase := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(cacheBase, name, "site"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cacheBase, name, "site", "index.html"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	called := false
	originalPicker := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		called = true
		return opts[0], nil
	}
	t.Cleanup(func() { huhSelectFn = originalPicker })

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--project", "beta",
		"--addr", "127.0.0.1:0",
	})
	t.Cleanup(cancel)

	url := waitForServingURL(t, stdout)
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "beta") {
		t.Errorf("served wrong project: body=%q", body)
	}
	if called {
		t.Error("picker called despite --project being set")
	}

	cancel()
	<-done
}

func TestServe_multipleProjects_nonInteractive_errorsWithList(t *testing.T) {
	cacheBase := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(cacheBase, name, "site"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cacheBase, name, "site", "index.html"), []byte("ok"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	forceInteractive(t, false)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--addr", "127.0.0.1:0",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr.String())
	}
	for _, want := range []string{"--project", "alpha", "beta"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %q", want, stderr.String())
		}
	}
}

func TestServe_repoAndProject_mutuallyExclusive(t *testing.T) {
	cacheBase := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cacheBase, "alpha", "site"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--repo", "/tmp/whatever",
		"--project", "alpha",
		"--addr", "127.0.0.1:0",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Errorf("stderr should explain --repo and --project conflict, got %q", stderr.String())
	}
}

func TestServe_projectFlag_missingProject_errors(t *testing.T) {
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"serve",
		"--cache-dir", cacheBase,
		"--project", "ghost",
		"--addr", "127.0.0.1:0",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr.String())
	}
	expected := filepath.Join(cacheBase, "ghost", "site")
	if !strings.Contains(stderr.String(), expected) {
		t.Errorf("stderr should name missing path %q, got %q", expected, stderr.String())
	}
}

// `ftg serve --repo .` must resolve the site at <cache>/<repo>/site, not
// <cache>/site. Without absolute-path resolution, filepath.Base(".") is "."
// and filepath.Join collapses the project segment.
func TestServe_relativeRepoDot_resolvesAbsoluteBasename(t *testing.T) {
	cacheBase := t.TempDir()
	repoParent := t.TempDir()
	repoDir := filepath.Join(repoParent, "relrepo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	siteDir := filepath.Join(cacheBase, "relrepo", "site")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("hello from relrepo"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	prevCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevCWD) })
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	stdout, cancel, done := runServeAsync(t, []string{
		"serve",
		"--repo", ".",
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
	if !strings.Contains(string(body), "hello from relrepo") {
		t.Errorf("body = %q, want it to contain 'hello from relrepo' (proves serve resolved <cache>/relrepo/site, not <cache>/site)", string(body))
	}

	cancel()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatalf("serve did not exit within deadline")
	}
}
