package analyzer_test

import (
	"encoding/json"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocsFeatureMapRoundtrips(t *testing.T) {
	fm := analyzer.DocsFeatureMap{
		{Feature: "authentication", Pages: []string{"https://example.com/auth"}},
		{Feature: "search", Pages: []string{}},
	}
	data, err := json.Marshal(fm)
	require.NoError(t, err)

	var got analyzer.DocsFeatureMap
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, fm, got)
}
