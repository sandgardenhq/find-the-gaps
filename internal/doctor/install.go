package doctor

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// RunInstall installs any RequiredTools not already present on $PATH.
// It uses the install command for the current platform and streams
// installer output directly to stdout/stderr.
func RunInstall(ctx context.Context, stdout, stderr io.Writer) int {
	lookup := func(binary string) bool {
		_, err := exec.LookPath(binary)
		return err == nil
	}
	return runInstall(ctx, RequiredTools, runtime.GOOS, stdout, stderr, lookup, defaultRunner)
}

func runInstall(
	ctx context.Context,
	tools []Tool,
	goos string,
	stdout, stderr io.Writer,
	lookup func(binary string) bool,
	runner func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error,
) int {
	exitCode := 0
	for _, t := range tools {
		if lookup(t.Binary) {
			_, _ = fmt.Fprintf(stdout, "%-8s already installed, skipping.\n", t.Name)
			continue
		}
		cmdArgs, ok := t.InstallCmds[goos]
		if !ok {
			_, _ = fmt.Fprintf(stderr, "%s: no automated install available on %s.\n  install manually: %s\n",
				t.Name, goos, t.InstallHint)
			exitCode = 1
			continue
		}
		_, _ = fmt.Fprintf(stdout, "Installing %s...\n", t.Name)
		if err := runner(ctx, stdout, stderr, cmdArgs[0], cmdArgs[1:]...); err != nil {
			_, _ = fmt.Fprintf(stderr, "Failed to install %s: %v\n  try manually: %s\n",
				t.Name, err, t.InstallHint)
			exitCode = 1
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%s installed.\n", t.Name)
	}
	return exitCode
}

func defaultRunner(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
