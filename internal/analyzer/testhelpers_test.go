package analyzer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// errBoom is a sentinel error for forced-error test cases.
var errBoom = errors.New("boom")

// fakeClient is a test double for analyzer.LLMClient.
type fakeClient struct {
	responses       []string // popped in order; last entry repeated when exhausted
	callCount       int
	forcedErr       error
	receivedPrompts []string

	// jsonResponses maps JSONSchema.Name to a canned json.RawMessage response.
	// When CompleteJSON is called and the schema name isn't in the map, the
	// fake returns an error so tests notice missing wiring.
	jsonResponses map[string]json.RawMessage
	// jsonSchemas captures schemas passed to CompleteJSON in call order so
	// tests can assert the right schema reached the client.
	jsonSchemas []analyzer.JSONSchema
}

func (f *fakeClient) Complete(_ context.Context, prompt string) (string, error) {
	f.receivedPrompts = append(f.receivedPrompts, prompt)
	if f.forcedErr != nil {
		return "", f.forcedErr
	}
	if len(f.responses) == 0 {
		return "", nil
	}
	idx := f.callCount
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	} else {
		f.callCount++
	}
	return f.responses[idx], nil
}

func (f *fakeClient) CompleteJSON(_ context.Context, prompt string, schema analyzer.JSONSchema) (json.RawMessage, error) {
	f.receivedPrompts = append(f.receivedPrompts, prompt)
	f.jsonSchemas = append(f.jsonSchemas, schema)
	if f.forcedErr != nil {
		return nil, f.forcedErr
	}
	raw, ok := f.jsonResponses[schema.Name]
	if !ok {
		return nil, fmt.Errorf("fakeClient: no canned CompleteJSON response for schema %q", schema.Name)
	}
	return raw, nil
}
