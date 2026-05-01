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
	// jsonResponseQueues maps JSONSchema.Name to a FIFO of responses; each
	// CompleteJSON call pops the next entry. When exhausted, the last entry
	// is repeated (matches Complete semantics). Takes precedence over
	// jsonResponses when both are set for the same schema.
	jsonResponseQueues map[string][]json.RawMessage
	// jsonSchemas captures schemas passed to CompleteJSON in call order so
	// tests can assert the right schema reached the client.
	jsonSchemas []analyzer.JSONSchema

	// caps is returned from Capabilities(). Zero-value means no optional
	// features; tests that need vision or tool-use override it explicitly.
	caps analyzer.ModelCapabilities
}

func (f *fakeClient) Capabilities() analyzer.ModelCapabilities { return f.caps }

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
	callIdx := len(f.jsonSchemas)
	f.jsonSchemas = append(f.jsonSchemas, schema)
	f.callCount++
	if f.forcedErr != nil {
		return nil, f.forcedErr
	}
	if queue, ok := f.jsonResponseQueues[schema.Name]; ok && len(queue) > 0 {
		idx := callIdx
		if idx >= len(queue) {
			idx = len(queue) - 1
		}
		return queue[idx], nil
	}
	raw, ok := f.jsonResponses[schema.Name]
	if !ok {
		return nil, fmt.Errorf("fakeClient: no canned CompleteJSON response for schema %q", schema.Name)
	}
	return raw, nil
}

// CompleteJSONMultimodal reuses the same schema-keyed canned response map as
// CompleteJSON. The fake doesn't otherwise inspect the messages slice; tests
// that need to assert ContentBlocks structure should use a more specialized
// fake (see screenshot_gaps_relevance_test.go's fakeJSONClient).
func (f *fakeClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, schema analyzer.JSONSchema) (json.RawMessage, error) {
	callIdx := len(f.jsonSchemas)
	f.jsonSchemas = append(f.jsonSchemas, schema)
	f.callCount++
	if f.forcedErr != nil {
		return nil, f.forcedErr
	}
	if queue, ok := f.jsonResponseQueues[schema.Name]; ok && len(queue) > 0 {
		idx := callIdx
		if idx >= len(queue) {
			idx = len(queue) - 1
		}
		return queue[idx], nil
	}
	raw, ok := f.jsonResponses[schema.Name]
	if !ok {
		return nil, fmt.Errorf("fakeClient: no canned CompleteJSONMultimodal response for schema %q", schema.Name)
	}
	return raw, nil
}
