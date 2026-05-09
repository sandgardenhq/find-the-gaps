package cli

import (
	"bytes"
	"strings"
	"testing"
)

// emptyEnv is an env(string)->string lookup that returns "" for everything.
func emptyEnv(string) string { return "" }

// envWith returns a lookup that maps the given key to value, "" otherwise.
func envWith(key, value string) func(string) string {
	return func(k string) string {
		if k == key {
			return value
		}
		return ""
	}
}

func TestDecideAutoServe_serveByDefaultWhenInteractive(t *testing.T) {
	d := decideAutoServe(false /*noSite*/, false /*noServe*/, true /*interactive*/, emptyEnv)
	if !d.Serve {
		t.Errorf("Serve = false, want true; reason=%q", d.Reason)
	}
}

func TestDecideAutoServe_skipsWhenNoSite(t *testing.T) {
	d := decideAutoServe(true, false, true, emptyEnv)
	if d.Serve {
		t.Errorf("Serve = true, want false (no-site)")
	}
	if d.Reason != "no-site" {
		t.Errorf("Reason = %q, want %q", d.Reason, "no-site")
	}
}

func TestDecideAutoServe_skipsWhenNoServeFlag(t *testing.T) {
	d := decideAutoServe(false, true, true, emptyEnv)
	if d.Serve {
		t.Errorf("Serve = true, want false (no-serve)")
	}
	if d.Reason != "no-serve" {
		t.Errorf("Reason = %q, want %q", d.Reason, "no-serve")
	}
}

func TestDecideAutoServe_skipsWhenQuietEnvSet(t *testing.T) {
	d := decideAutoServe(false, false, true, envWith("FIND_THE_GAPS_QUIET", "1"))
	if d.Serve {
		t.Errorf("Serve = true, want false (quiet)")
	}
	if d.Reason != "quiet" {
		t.Errorf("Reason = %q, want %q", d.Reason, "quiet")
	}
}

func TestDecideAutoServe_skipsWhenCIEnvSet(t *testing.T) {
	d := decideAutoServe(false, false, true, envWith("CI", "true"))
	if d.Serve {
		t.Errorf("Serve = true, want false (CI)")
	}
	if d.Reason != "ci" {
		t.Errorf("Reason = %q, want %q", d.Reason, "ci")
	}
}

func TestDecideAutoServe_skipsWhenNonInteractive(t *testing.T) {
	d := decideAutoServe(false, false, false, emptyEnv)
	if d.Serve {
		t.Errorf("Serve = true, want false (non-interactive)")
	}
	if d.Reason != "non-interactive" {
		t.Errorf("Reason = %q, want %q", d.Reason, "non-interactive")
	}
}

// noSite takes precedence over every other gate so that "nothing was built"
// is the most informative skip reason a user sees.
func TestDecideAutoServe_noSiteWinsOverOtherSkips(t *testing.T) {
	d := decideAutoServe(true, true, false, envWith("CI", "true"))
	if d.Reason != "no-site" {
		t.Errorf("Reason = %q, want %q (no-site must take precedence)", d.Reason, "no-site")
	}
}

func TestAnalyze_noServeFlag_appearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, []string{"analyze", "--help"}); code != 0 {
		t.Fatalf("--help failed: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "--no-serve") {
		t.Errorf("--no-serve flag not in help output:\n%s", stdout.String())
	}
}

func TestAnalyze_noServeFlag_defaultsFalse(t *testing.T) {
	cmd := newAnalyzeCmd()
	f := cmd.Flags().Lookup("no-serve")
	if f == nil {
		t.Fatal("missing --no-serve flag")
	}
	if f.DefValue != "false" {
		t.Errorf("--no-serve default = %q, want \"false\"", f.DefValue)
	}
}
