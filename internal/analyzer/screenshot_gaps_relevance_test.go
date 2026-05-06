package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeJSONClient is a minimal LLMClient that returns a single canned JSON
// payload from CompleteJSONMultimodal. It also captures the messages each
// call received so tests can assert prompt + image-block structure.
type fakeJSONClient struct {
	caps     ModelCapabilities
	jsonResp json.RawMessage

	calls       atomic.Int64
	lastMsgs    []ChatMessage
	lastSchema  JSONSchema
	allMessages [][]ChatMessage
	allSchemas  []JSONSchema
}

func (f *fakeJSONClient) Complete(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (f *fakeJSONClient) CompleteJSON(_ context.Context, _ string, _ JSONSchema) (json.RawMessage, error) {
	return f.jsonResp, nil
}

func (f *fakeJSONClient) CompleteJSONMultimodal(_ context.Context, msgs []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	f.calls.Add(1)
	f.lastMsgs = msgs
	f.lastSchema = schema
	// Snapshot a copy so later calls don't mutate captured slices.
	dup := make([]ChatMessage, len(msgs))
	copy(dup, msgs)
	f.allMessages = append(f.allMessages, dup)
	f.allSchemas = append(f.allSchemas, schema)
	return f.jsonResp, nil
}

func (f *fakeJSONClient) Capabilities() ModelCapabilities { return f.caps }

// TestRelevancePass_ParsesImageIssuesAndVerdicts pins the merging behavior:
// the canned LLM response carries one image issue and two verdicts, and the
// returned slices must reflect that. PageURL must be back-filled on each
// issue so downstream rendering can attribute the finding to its source page.
func TestRelevancePass_ParsesImageIssuesAndVerdicts(t *testing.T) {
	resp := json.RawMessage(`{
	  "image_issues": [
	    {"index":"img-2","src":"b.png","reason":"shows dashboard, prose describes settings","suggested_action":"replace","priority":"medium","priority_reason":"test stub"}
	  ],
	  "verdicts": [
	    {"index":"img-1","matches":true},
	    {"index":"img-2","matches":false}
	  ]
	}`)
	client := &fakeJSONClient{
		caps:     ModelCapabilities{Vision: true},
		jsonResp: resp,
	}
	page := DocPage{URL: "https://x/p", Content: "..."}
	refs := []imageRef{{Src: "a.png"}, {Src: "b.png"}}

	issues, verdicts, err := relevancePass(context.Background(), client, page, refs)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "img-2", issues[0].Index)
	assert.Equal(t, "https://x/p", issues[0].PageURL)
	assert.Equal(t, "b.png", issues[0].Src)
	assert.Equal(t, "replace", issues[0].SuggestedAction)
	require.Len(t, verdicts, 2)
	assert.Equal(t, "img-1", verdicts[0].Index)
	assert.True(t, verdicts[0].Matches)
	assert.Equal(t, "img-2", verdicts[1].Index)
	assert.False(t, verdicts[1].Matches)

	// Schema must be the relevance pass schema and the message must carry
	// content blocks: 1 text block + N image blocks (one per ref in the batch).
	assert.Equal(t, "screenshot_image_relevance", client.lastSchema.Name)
	require.Len(t, client.lastMsgs, 1)
	require.NotEmpty(t, client.lastMsgs[0].ContentBlocks)
	textBlocks := 0
	imageBlocks := 0
	for _, b := range client.lastMsgs[0].ContentBlocks {
		switch b.Type {
		case ContentBlockText:
			textBlocks++
		case ContentBlockImageURL:
			imageBlocks++
		}
	}
	assert.Equal(t, 1, textBlocks, "exactly one prompt text block per call")
	assert.Equal(t, len(refs), imageBlocks, "one image block per ref in the batch")
}

// TestRelevancePass_NormalizesLiteralEscapeSequences guards the same model
// quirk fixed in #50 for the detection pass: vision models occasionally write
// `\n` (the two-character escape sequence) as text inside a JSON string value
// rather than emitting an actual newline. Without normalization, the literal
// `\n` leaks into screenshots.md and the rendered Hugo image-issues page,
// where it shows up as a backslash-n instead of a paragraph break. Reason and
// SuggestedAction are both rendered, so both must be normalized.
func TestRelevancePass_NormalizesLiteralEscapeSequences(t *testing.T) {
	resp := json.RawMessage(`{
	  "image_issues": [
	    {"index":"img-1","src":"a.png","reason":"line one.\\nline two.","suggested_action":"step 1.\\nstep 2.","priority":"medium","priority_reason":"test stub"}
	  ],
	  "verdicts": [
	    {"index":"img-1","matches":false}
	  ]
	}`)
	client := &fakeJSONClient{
		caps:     ModelCapabilities{Vision: true},
		jsonResp: resp,
	}
	page := DocPage{URL: "https://x/p", Content: "..."}
	refs := []imageRef{{Src: "a.png"}}

	issues, _, err := relevancePass(context.Background(), client, page, refs)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "line one.\nline two.", issues[0].Reason,
		"literal `\\n` from the model should be converted to real newlines in Reason")
	assert.Equal(t, "step 1.\nstep 2.", issues[0].SuggestedAction,
		"literal `\\n` from the model should be converted to real newlines in SuggestedAction")
}

// TestRelevancePass_BatchesAtFiveImages pins the 5-image-per-call cap.
// Twelve refs must produce three calls (5 + 5 + 2). The fake counts calls
// via atomic.Int64 so the assertion is order-free.
func TestRelevancePass_BatchesAtFiveImages(t *testing.T) {
	client := &fakeJSONClient{
		caps:     ModelCapabilities{Vision: true},
		jsonResp: json.RawMessage(`{"image_issues":[],"verdicts":[]}`),
	}
	page := DocPage{URL: "https://x/p", Content: "..."}
	refs := make([]imageRef, 12)
	for i := range refs {
		refs[i] = imageRef{Src: fmt.Sprintf("img-%d.png", i+1)}
	}

	_, _, err := relevancePass(context.Background(), client, page, refs)
	require.NoError(t, err)
	assert.Equal(t, int64(3), client.calls.Load(), "12 refs at max=5 must trigger 3 batches")

	// Per-batch image counts: 5, 5, 2 in order.
	require.Len(t, client.allMessages, 3)
	wantSizes := []int{5, 5, 2}
	for i, msgs := range client.allMessages {
		require.Len(t, msgs, 1, "each call sends one message")
		images := 0
		for _, b := range msgs[0].ContentBlocks {
			if b.Type == ContentBlockImageURL {
				images++
			}
		}
		assert.Equal(t, wantSizes[i], images, "batch %d image count", i)
	}
}

// flakyJSONClient errors on the first CompleteJSONMultimodal call and returns
// the canned JSON on subsequent calls. Used to verify relevancePass is
// fail-open: a batch-level error (e.g. Bifrost cannot download an image URL)
// should be logged and skipped, not propagated, so the rest of the run
// continues. Without this, one unreachable image src on one page aborts the
// whole analyze.
type flakyJSONClient struct {
	caps     ModelCapabilities
	jsonResp json.RawMessage
	calls    atomic.Int64
}

func (f *flakyJSONClient) Complete(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (f *flakyJSONClient) CompleteJSON(_ context.Context, _ string, _ JSONSchema) (json.RawMessage, error) {
	return f.jsonResp, nil
}

func (f *flakyJSONClient) CompleteJSONMultimodal(_ context.Context, _ []ChatMessage, _ JSONSchema) (json.RawMessage, error) {
	n := f.calls.Add(1)
	if n == 1 {
		return nil, errors.New("bifrost CompleteJSON: Unable to download the file. Please verify the URL and try again")
	}
	return f.jsonResp, nil
}

func (f *flakyJSONClient) Capabilities() ModelCapabilities { return f.caps }

// TestRelevancePass_FailOpenOnBatchError pins the contract that a batch-level
// LLM error does NOT abort the page or the run. With 8 refs (two batches of
// 5+3) and the first call erroring, relevancePass must still call the second
// batch, return its issues, and return nil error.
func TestRelevancePass_FailOpenOnBatchError(t *testing.T) {
	resp := json.RawMessage(`{
	  "image_issues": [
	    {"index":"img-6","src":"f.png","reason":"mismatch","suggested_action":"replace","priority":"medium","priority_reason":"test stub"}
	  ],
	  "verdicts": [
	    {"index":"img-6","matches":false}
	  ]
	}`)
	client := &flakyJSONClient{
		caps:     ModelCapabilities{Vision: true},
		jsonResp: resp,
	}
	page := DocPage{URL: "https://x/p", Content: "..."}
	refs := make([]imageRef, 8)
	for i := range refs {
		refs[i] = imageRef{Src: fmt.Sprintf("img-%d.png", i+1)}
	}

	issues, _, err := relevancePass(context.Background(), client, page, refs)
	require.NoError(t, err, "batch errors must be logged and skipped, not returned")
	assert.Equal(t, int64(2), client.calls.Load(), "second batch must still be issued after first errors")
	require.Len(t, issues, 1, "issues from the surviving batch must be preserved")
	assert.Equal(t, "img-6", issues[0].Index)
}
