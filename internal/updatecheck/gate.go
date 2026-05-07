package updatecheck

// GateInputs is the set of facts ShouldSkip needs to decide whether to run the
// update check. Kept as a struct so callers (the CLI wiring + tests) build it
// the same way and the function stays pure.
type GateInputs struct {
	// Env returns environment variable values. Tests pass a map-backed
	// function; the CLI passes os.Getenv.
	Env func(string) string
	// Version is the resolved current ftg version (e.g. "v1.3.0" or "dev").
	Version string
	// Command is the cobra subcommand name (e.g. "analyze", "doctor").
	Command string
	// StderrIsTTY is whether the process's stderr is a terminal. When false
	// the user is piping or redirecting, so we stay quiet.
	StderrIsTTY bool
}

// trivialCommands is the set of commands we never run the check for. Cobra's
// auto-generated commands (version, help, completion, __complete) live here
// because they are scripted or trivially fast.
var trivialCommands = map[string]bool{
	"version":    true,
	"help":       true,
	"completion": true,
	"__complete": true,
	"":           true, // bare `ftg` falls through to help
}

// truthyCI matches the values commonly used to indicate a CI environment.
// "false", "0", "no", and "" are explicitly NOT treated as truthy so that
// developers who happen to have CI=false in their shells do not get the check
// suppressed locally.
func truthyCI(v string) bool {
	switch v {
	case "true", "TRUE", "True", "1", "yes", "YES", "Yes":
		return true
	}
	return false
}

// ShouldSkip returns true (with a short reason) when the update check must not
// run. The order of checks is intentional: cheap, definitely-skip rules come
// first so we short-circuit before any work happens.
func ShouldSkip(in GateInputs) (bool, string) {
	if trivialCommands[in.Command] {
		return true, "trivial command: " + in.Command
	}
	if in.Version == "" || in.Version == "dev" {
		return true, "dev build"
	}
	if v := in.Env("FIND_THE_GAPS_NO_UPDATE_CHECK"); v != "" && v != "0" && v != "false" {
		return true, "FIND_THE_GAPS_NO_UPDATE_CHECK is set"
	}
	if v := in.Env("FIND_THE_GAPS_QUIET"); v != "" && v != "0" && v != "false" {
		return true, "FIND_THE_GAPS_QUIET is set"
	}
	if truthyCI(in.Env("CI")) {
		return true, "CI environment"
	}
	if !in.StderrIsTTY {
		return true, "stderr is not a tty"
	}
	return false, ""
}
