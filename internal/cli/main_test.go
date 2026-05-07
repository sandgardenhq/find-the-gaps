package cli

import (
	"os"
	"testing"
)

// TestMain bypasses the external-tool precheck for every in-process test in
// this package. The unit tests here drive analyze and render through cached
// fixtures (prepareDocsCache, fake hugo via site.HugoBin, etc.), so insisting
// on real mdfetch/hugo binaries on $PATH would be busywork that fails on CI
// runners where the binaries aren't installed.
//
// The precheck itself is unit-tested in internal/doctor/precheck_test.go, and
// its wiring through analyze/render is exercised end-to-end in
// cmd/ftg/testdata/script/{analyze_missing_mdfetch,render_missing_hugo}.txtar.
func TestMain(m *testing.M) {
	requireExternalTools = noopRequireExternalTools
	os.Exit(m.Run())
}
