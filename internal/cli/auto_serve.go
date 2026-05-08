package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// autoServeDecision is the gating outcome for whether `analyze` auto-launches
// the local preview server when it finishes successfully. Pure data so tests
// can exercise every branch without spinning up a real listener.
type autoServeDecision struct {
	Serve  bool
	Reason string // skip reason when Serve == false; empty otherwise
}

// decideAutoServe applies the auto-serve gating rules. Order matters:
// "nothing was built" is reported before any opt-out so users see the most
// informative skip reason first.
func decideAutoServe(noSite, noServe, interactive bool, env func(string) string) autoServeDecision {
	if noSite {
		return autoServeDecision{Reason: "no-site"}
	}
	if noServe {
		return autoServeDecision{Reason: "no-serve"}
	}
	if env("FIND_THE_GAPS_QUIET") == "1" {
		return autoServeDecision{Reason: "quiet"}
	}
	if env("CI") != "" {
		return autoServeDecision{Reason: "ci"}
	}
	if !interactive {
		return autoServeDecision{Reason: "non-interactive"}
	}
	return autoServeDecision{Serve: true}
}

// runAutoServe runs the local preview server on an ephemeral port, opens the
// rendered site in the user's default browser, and blocks until SIGINT or
// SIGTERM. Used by `analyze` after a successful run.
func runAutoServe(ctx context.Context, out io.Writer, siteDir string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, _ = fmt.Fprintln(out, "starting local preview (press Ctrl+C to stop)")
	return runHTTPServer(ctx, out, siteDir, "127.0.0.1:0", true)
}
