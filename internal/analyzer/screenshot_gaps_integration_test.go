package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedClient is a minimal LLMClient that returns canned JSON payloads
// from CompleteJSONMultimodal (vision relevance pass) and CompleteJSON
// (text-only detection pass). The vision and detection counts are tracked
// independently so tests can assert that DetectScreenshotGaps issues the
// expected number of calls per branch.
//
// multimodalResps drives per-call responses for the relevance pass: call N
// returns multimodalResps[N]. When N exceeds the slice length the last entry
// is returned (so a one-element slice is broadcast across all batches). This
// lets a test thread one issue through one specific batch and empty results
// through the rest.
type scriptedClient struct {
	caps            ModelCapabilities
	multimodalResps []json.RawMessage
	detectionResp   json.RawMessage
	multimodalCalls atomic.Int64
	detectionCalls  atomic.Int64
	lastSchemaName  string
}

func (s *scriptedClient) Complete(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *scriptedClient) CompleteJSON(_ context.Context, _ string, schema JSONSchema) (json.RawMessage, error) {
	s.detectionCalls.Add(1)
	s.lastSchemaName = schema.Name
	return s.detectionResp, nil
}

func (s *scriptedClient) CompleteJSONMultimodal(_ context.Context, _ []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	idx := int(s.multimodalCalls.Add(1) - 1)
	s.lastSchemaName = schema.Name
	if idx >= len(s.multimodalResps) {
		idx = len(s.multimodalResps) - 1
	}
	return s.multimodalResps[idx], nil
}

func (s *scriptedClient) Capabilities() ModelCapabilities { return s.caps }

func TestDetectScreenshotGaps_VisionBranchEmitsImageIssuesAndAuditStats(t *testing.T) {
	// First batch (img-1..img-5) carries one mismatch on img-2; second batch
	// (img-6) is clean. Verdicts cover every image so the detection pass can
	// reason about coverage globally.
	batch1Resp := json.RawMessage(`{
	  "image_issues": [
	    {"index":"img-2","src":"img-1.png","reason":"shows dashboard, prose describes settings","suggested_action":"replace"}
	  ],
	  "verdicts": [
	    {"index":"img-1","matches":true},
	    {"index":"img-2","matches":false},
	    {"index":"img-3","matches":true},
	    {"index":"img-4","matches":true},
	    {"index":"img-5","matches":true}
	  ]
	}`)
	batch2Resp := json.RawMessage(`{
	  "image_issues": [],
	  "verdicts": [
	    {"index":"img-6","matches":true}
	  ]
	}`)
	detectionResp := json.RawMessage(`{
	  "gaps": [
	    {"quoted_passage":"Click Save.","should_show":"the Save modal","suggested_alt":"Save modal","insertion_hint":"after the Save paragraph"}
	  ],
	  "suppressed_by_image": [
	    {"quoted_passage":"View the dashboard.","should_show":"dashboard","suggested_alt":"Dashboard","insertion_hint":"after the dashboard paragraph"}
	  ]
	}`)
	client := &scriptedClient{
		caps:            ModelCapabilities{Vision: true},
		multimodalResps: []json.RawMessage{batch1Resp, batch2Resp},
		detectionResp:   detectionResp,
	}

	// Build a single page with 6 markdown images, distinct paragraphs to
	// keep ParagraphIndex stable.
	var b strings.Builder
	b.WriteString("# Page\n\n")
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&b, "Paragraph %d.\n\n![alt-%d](img-%d.png)\n\n", i, i, i)
	}
	pages := []DocPage{{URL: "https://x/p", Path: "/tmp/p.md", Content: b.String()}}

	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)

	// Image issues from relevance pass were collected and tagged with the
	// page URL so the reporter can attribute them.
	require.Len(t, res.ImageIssues, 1)
	assert.Equal(t, "img-2", res.ImageIssues[0].Index)
	assert.Equal(t, "https://x/p", res.ImageIssues[0].PageURL)

	// Detection pass produced one missing gap.
	require.Len(t, res.MissingGaps, 1)
	assert.Equal(t, "https://x/p", res.MissingGaps[0].PageURL)
	assert.Equal(t, "Click Save.", res.MissingGaps[0].QuotedPassage)

	// Audit stats reflect the vision branch.
	require.Len(t, res.AuditStats, 1)
	assert.True(t, res.AuditStats[0].VisionEnabled)
	// 6 images at max=5 → 2 batches.
	assert.Equal(t, 2, res.AuditStats[0].RelevanceBatches)
	assert.Equal(t, 6, res.AuditStats[0].ImagesSeen)
	assert.Equal(t, 1, res.AuditStats[0].ImageIssues)
	assert.Equal(t, 1, res.AuditStats[0].MissingScreenshots)
	assert.Equal(t, 1, res.AuditStats[0].MissingSuppressed)
	assert.Equal(t, "https://x/p", res.AuditStats[0].PageURL)

	// Call count: 2 vision calls + 1 detection call.
	assert.Equal(t, int64(2), client.multimodalCalls.Load())
	assert.Equal(t, int64(1), client.detectionCalls.Load())
}

func TestDetectScreenshotGaps_NonVisionBranchUnchanged(t *testing.T) {
	client := &fakeLLMClient{
		responses: []string{
			`{"gaps":[{"quoted_passage":"Run the command.","should_show":"Terminal showing output.","suggested_alt":"Terminal","insertion_hint":"after the command block"}]}`,
		},
	}
	pages := []DocPage{
		{URL: "https://example.com/a", Path: "/tmp/a.md", Content: "# A\n\nRun the command.\n"},
	}
	res, err := DetectScreenshotGaps(context.Background(), client, pages, nil)
	require.NoError(t, err)

	// Non-vision branch: no image issues, no relevance batches.
	assert.Empty(t, res.ImageIssues)
	require.Len(t, res.AuditStats, 1)
	assert.False(t, res.AuditStats[0].VisionEnabled)
	assert.Equal(t, 0, res.AuditStats[0].RelevanceBatches)
	assert.Equal(t, 0, res.AuditStats[0].ImageIssues)

	// Missing gaps populated as before.
	require.Len(t, res.MissingGaps, 1)
	assert.Equal(t, "https://example.com/a", res.MissingGaps[0].PageURL)
	assert.Equal(t, "Run the command.", res.MissingGaps[0].QuotedPassage)
	assert.Equal(t, 1, res.AuditStats[0].MissingScreenshots)
}
