package reporter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

// WriteLinksJSON writes <dir>/links.json. Both top-level keys are always
// present; empty buckets render as `[]`.
func WriteLinksJSON(dir string, rep linkcheck.Report) error {
	type out struct {
		Broken []linkcheck.Finding `json:"broken"`
		Auth   []linkcheck.Finding `json:"auth_required"`
	}
	o := out{Broken: rep.Broken, Auth: rep.Auth}
	if o.Broken == nil {
		o.Broken = []linkcheck.Finding{}
	}
	if o.Auth == nil {
		o.Auth = []linkcheck.Finding{}
	}
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "links.json"), b, 0o644)
}

// ReadLinksJSON loads a previously-persisted links.json from dir. Returns
// (zero, false, nil) when the file is missing — callers treat that as
// "the link check was not run for this project" and render an empty Dead
// Links report. Returns a non-nil error only when the file exists but
// cannot be read or parsed.
func ReadLinksJSON(dir string) (linkcheck.Report, bool, error) {
	path := filepath.Join(dir, "links.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return linkcheck.Report{}, false, nil
	}
	if err != nil {
		return linkcheck.Report{}, false, err
	}
	var in struct {
		Broken []linkcheck.Finding `json:"broken"`
		Auth   []linkcheck.Finding `json:"auth_required"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return linkcheck.Report{}, false, err
	}
	return linkcheck.Report{Broken: in.Broken, Auth: in.Auth}, true, nil
}
