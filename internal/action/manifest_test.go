package action

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestActionManifest_DeclaresExpectedInputs(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(repoRoot, "action.yml"))
	require.NoError(t, err, "action.yml must exist at repo root")

	var manifest struct {
		Name string `yaml:"name"`
		Runs struct {
			Using string `yaml:"using"`
		} `yaml:"runs"`
		Inputs map[string]struct {
			Required bool   `yaml:"required"`
			Default  string `yaml:"default"`
		} `yaml:"inputs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &manifest))

	require.Equal(t, "composite", manifest.Runs.Using, "must be a composite action")

	required := []string{"docs-url", "bifrost-api-key"}
	optional := []string{"create-issue", "skip-screenshot-check"}
	for _, k := range required {
		got, ok := manifest.Inputs[k]
		require.True(t, ok, "input %q missing", k)
		require.True(t, got.Required, "input %q must be required", k)
	}
	for _, k := range optional {
		_, ok := manifest.Inputs[k]
		require.True(t, ok, "input %q missing", k)
	}
}

func TestActionManifest_StepsWired(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(repoRoot, "action.yml"))
	require.NoError(t, err)

	var manifest struct {
		Runs struct {
			Steps []struct {
				Name string `yaml:"name"`
				If   string `yaml:"if"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &manifest))

	names := []string{}
	for _, s := range manifest.Runs.Steps {
		names = append(names, s.Name)
	}
	for _, want := range []string{
		"Resolve find-the-gaps version",
		"Install find-the-gaps",
		"Install mdfetch",
		"Run analyze",
		"Upload report artifact",
		"Update tracking issue",
	} {
		require.Contains(t, names, want, "missing step %q", want)
	}
}
