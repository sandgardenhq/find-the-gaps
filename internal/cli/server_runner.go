package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
)

// runHTTPServer binds a listener on addr, serves siteDir, prints the resolved
// URL to out, optionally opens the URL in the default browser, and blocks
// until ctx is canceled. Shared by `serve` and the post-`analyze` auto-open
// path.
func runHTTPServer(ctx context.Context, out io.Writer, siteDir, addr string, openBrowser bool) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           http.FileServer(http.Dir(siteDir)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	url := fmt.Sprintf("http://%s/", ln.Addr().String())
	_, _ = fmt.Fprintf(out, "serving %s at %s\n", siteDir, url)

	if openBrowser {
		if err := openInBrowser(url); err != nil {
			log.Warnf("could not open browser: %v", err)
		}
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	}
}
