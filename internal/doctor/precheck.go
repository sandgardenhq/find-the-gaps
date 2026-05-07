package doctor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Precheck describes a set of external-tool requirements that a single command
// needs satisfied before it begins work.
type Precheck struct {
	// Command is the user-facing name of the calling command (e.g. "ftg analyze")
	// and is included in the error header.
	Command string
	// Tools is the list of tool names — each must match a Tool.Name in
	// RequiredTools — that must be on $PATH for the command to run. The order
	// is preserved in the error message.
	Tools []string
	// Suffix is appended after the per-tool block. Use it for command-specific
	// guidance like "Pass --no-site to skip Hugo."
	Suffix string
}

// Require returns nil when every tool in p.Tools is found on $PATH.
// When any tool is missing, it returns an error whose message lists each
// missing tool with its install hint, suitable for returning directly from
// cobra's RunE.
//
// Tool names that don't appear in RequiredTools are reported as a programmer
// error rather than silently skipped, so a typo at the call site fails loudly.
func Require(_ context.Context, p Precheck) error {
	required, err := resolveRequirements(p.Tools)
	if err != nil {
		return err
	}

	var missing []Tool
	for _, t := range required {
		if _, lookErr := exec.LookPath(t.Binary); lookErr != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s needs the following external tool(s):\n", p.Command)
	for _, t := range missing {
		fmt.Fprintf(&b, "\n  %s — not found on $PATH\n    install: %s\n", t.Name, t.InstallHint)
	}
	if p.Suffix != "" {
		fmt.Fprintf(&b, "\n%s\n", p.Suffix)
	}
	fmt.Fprintf(&b, "\nRun `ftg doctor` to verify after installing.")
	return errors.New(b.String())
}

func resolveRequirements(names []string) ([]Tool, error) {
	out := make([]Tool, 0, len(names))
	for _, name := range names {
		tool, ok := lookupRequired(name)
		if !ok {
			return nil, fmt.Errorf("doctor.Require: unknown tool %q (not in RequiredTools)", name)
		}
		out = append(out, tool)
	}
	return out, nil
}

func lookupRequired(name string) (Tool, bool) {
	for _, t := range RequiredTools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}
