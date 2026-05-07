package updatecheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func mapEnv(m map[string]string) func(string) string {
	return func(key string) string {
		return m[key]
	}
}

func TestShouldSkip_HappyPath(t *testing.T) {
	skip, reason := ShouldSkip(GateInputs{
		Env:           mapEnv(nil),
		Version:       "v1.3.0",
		Command:       "analyze",
		StderrIsTTY:   true,
	})
	assert.False(t, skip, "default conditions should not skip; reason=%q", reason)
}

func TestShouldSkip_DevBuild(t *testing.T) {
	skip, reason := ShouldSkip(GateInputs{
		Env:         mapEnv(nil),
		Version:     "dev",
		Command:     "analyze",
		StderrIsTTY: true,
	})
	assert.True(t, skip)
	assert.Contains(t, reason, "dev")
}

func TestShouldSkip_EmptyVersion(t *testing.T) {
	skip, _ := ShouldSkip(GateInputs{
		Env:         mapEnv(nil),
		Version:     "",
		Command:     "analyze",
		StderrIsTTY: true,
	})
	assert.True(t, skip, "empty version is dev-equivalent and must skip")
}

func TestShouldSkip_QuietEnv(t *testing.T) {
	skip, reason := ShouldSkip(GateInputs{
		Env:         mapEnv(map[string]string{"FIND_THE_GAPS_QUIET": "1"}),
		Version:     "v1.3.0",
		Command:     "analyze",
		StderrIsTTY: true,
	})
	assert.True(t, skip)
	assert.Contains(t, reason, "QUIET")
}

func TestShouldSkip_DedicatedKillSwitch(t *testing.T) {
	skip, reason := ShouldSkip(GateInputs{
		Env:         mapEnv(map[string]string{"FIND_THE_GAPS_NO_UPDATE_CHECK": "1"}),
		Version:     "v1.3.0",
		Command:     "analyze",
		StderrIsTTY: true,
	})
	assert.True(t, skip)
	assert.Contains(t, reason, "NO_UPDATE_CHECK")
}

func TestShouldSkip_CIEnv(t *testing.T) {
	for _, val := range []string{"true", "1", "TRUE", "yes"} {
		skip, reason := ShouldSkip(GateInputs{
			Env:         mapEnv(map[string]string{"CI": val}),
			Version:     "v1.3.0",
			Command:     "analyze",
			StderrIsTTY: true,
		})
		assert.True(t, skip, "CI=%q should skip", val)
		assert.Contains(t, reason, "CI")
	}
}

func TestShouldSkip_CIEnvFalseyDoesNotSkip(t *testing.T) {
	for _, val := range []string{"", "false", "0", "no"} {
		skip, _ := ShouldSkip(GateInputs{
			Env:         mapEnv(map[string]string{"CI": val}),
			Version:     "v1.3.0",
			Command:     "analyze",
			StderrIsTTY: true,
		})
		assert.False(t, skip, "CI=%q should not skip", val)
	}
}

func TestShouldSkip_NonTTYStderr(t *testing.T) {
	skip, reason := ShouldSkip(GateInputs{
		Env:         mapEnv(nil),
		Version:     "v1.3.0",
		Command:     "analyze",
		StderrIsTTY: false,
	})
	assert.True(t, skip, "non-TTY stderr should skip to keep piped output clean")
	assert.Contains(t, reason, "tty")
}

func TestShouldSkip_TrivialCommands(t *testing.T) {
	for _, cmd := range []string{"version", "help", "completion", "__complete"} {
		skip, _ := ShouldSkip(GateInputs{
			Env:         mapEnv(nil),
			Version:     "v1.3.0",
			Command:     cmd,
			StderrIsTTY: true,
		})
		assert.True(t, skip, "command %q should skip the update check", cmd)
	}
}

func TestShouldSkip_RealCommandsRun(t *testing.T) {
	for _, cmd := range []string{"analyze", "render", "serve", "doctor"} {
		skip, reason := ShouldSkip(GateInputs{
			Env:         mapEnv(nil),
			Version:     "v1.3.0",
			Command:     cmd,
			StderrIsTTY: true,
		})
		assert.False(t, skip, "command %q should run the update check; reason=%q", cmd, reason)
	}
}
