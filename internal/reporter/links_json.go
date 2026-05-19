package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

// WriteLinksJSON writes <dir>/links.json. All three top-level keys are always
// present; empty buckets render as `[]`.
func WriteLinksJSON(dir string, rep linkcheck.Report) error {
	type out struct {
		Broken     []linkcheck.Finding `json:"broken"`
		Auth       []linkcheck.Finding `json:"auth_required"`
		Redirected []linkcheck.Finding `json:"redirected"`
	}
	o := out{Broken: rep.Broken, Auth: rep.Auth, Redirected: rep.Redirected}
	if o.Broken == nil {
		o.Broken = []linkcheck.Finding{}
	}
	if o.Auth == nil {
		o.Auth = []linkcheck.Finding{}
	}
	if o.Redirected == nil {
		o.Redirected = []linkcheck.Finding{}
	}
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "links.json"), b, 0o644)
}
