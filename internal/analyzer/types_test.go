package analyzer_test

import (
	"encoding/json"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeFeature_JSONRoundtrip(t *testing.T) {
	f := analyzer.CodeFeature{
		Name:        "CLI command routing",
		Description: "Provides top-level command structure.",
		Layer:       "cli",
		UserFacing:  true,
	}
	data, err := json.Marshal(f)
	require.NoError(t, err)

	var got analyzer.CodeFeature
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, f, got)
}

func TestCodeFeature_UserFacingFalse_JSONRoundtrip(t *testing.T) {
	f := analyzer.CodeFeature{
		Name:        "token batching",
		Description: "Splits symbol indexes into token-budget-sized chunks.",
		Layer:       "analysis engine",
		UserFacing:  false,
	}
	data, err := json.Marshal(f)
	require.NoError(t, err)

	var got analyzer.CodeFeature
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, f, got)
}
