package analyzer

import (
	"context"
	"encoding/json"
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
	    {"index":"img-2","src":"b.png","reason":"shows dashboard, prose describes settings","suggested_action":"replace"}
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
