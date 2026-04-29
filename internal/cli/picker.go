package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// huhSelectFn runs the real interactive picker. Tests swap it for a stub.
var huhSelectFn = func(opts []Project) (Project, error) {
	options := make([]huh.Option[string], len(opts))
	for i, p := range opts {
		options[i] = huh.NewOption(p.Name, p.Name)
	}
	var chosen string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Multiple analyzed projects found. Pick one to serve:").
				Options(options...).
				Value(&chosen),
		),
	)
	if err := form.Run(); err != nil {
		return Project{}, err
	}
	for _, p := range opts {
		if p.Name == chosen {
			return p, nil
		}
	}
	return Project{}, fmt.Errorf("internal: huh returned unknown project %q", chosen)
}

// pickProject prompts the user to choose one project from opts.
func pickProject(opts []Project) (Project, error) {
	if len(opts) == 0 {
		return Project{}, fmt.Errorf("pickProject: no options to choose from")
	}
	return huhSelectFn(opts)
}
