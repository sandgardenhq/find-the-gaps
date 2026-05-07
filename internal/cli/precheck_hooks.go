package cli

import (
	"context"

	"github.com/sandgardenhq/find-the-gaps/internal/doctor"
)

// requireExternalTools wraps doctor.Require so unit tests in this package can
// drive analyze and render without needing real mdfetch/hugo binaries on
// $PATH. Production code reaches it transparently; the binding is overridden
// in TestMain to a noop for in-process tests. End-to-end coverage of the
// precheck wiring lives in cmd/ftg/testdata/script/{analyze_missing_mdfetch,
// render_missing_hugo}.txtar, which exercise the real doctor.Require.
var requireExternalTools = doctor.Require

// noopRequireExternalTools is the test override target. Exported through the
// package-private hook above; tests assign this to bypass the precheck.
func noopRequireExternalTools(_ context.Context, _ doctor.Precheck) error {
	return nil
}
