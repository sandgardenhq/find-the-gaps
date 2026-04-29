package cli

import (
	"errors"
	"testing"
)

func TestPickProject_returnsSelected(t *testing.T) {
	projects := []Project{
		{Name: "alpha", SiteDir: "/x/alpha/site"},
		{Name: "beta", SiteDir: "/x/beta/site"},
	}
	original := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		return opts[1], nil
	}
	t.Cleanup(func() { huhSelectFn = original })

	got, err := pickProject(projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "beta" {
		t.Errorf("got %q, want beta", got.Name)
	}
}

func TestPickProject_propagatesCancel(t *testing.T) {
	projects := []Project{{Name: "alpha"}, {Name: "beta"}}
	original := huhSelectFn
	huhSelectFn = func(opts []Project) (Project, error) {
		return Project{}, errors.New("user cancelled")
	}
	t.Cleanup(func() { huhSelectFn = original })

	if _, err := pickProject(projects); err == nil {
		t.Error("expected error when underlying select cancels, got nil")
	}
}

func TestPickProject_emptyOpts_returnsError(t *testing.T) {
	if _, err := pickProject(nil); err == nil {
		t.Error("expected error for empty options, got nil")
	}
}
