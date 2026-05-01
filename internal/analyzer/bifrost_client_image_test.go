package analyzer

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emptyAssistantResponse returns a minimally-valid BifrostChatResponse that
// completeOneTurn can translate without panicking. The choice carries an
// empty assistant text content; the caller of completeOneTurn does not
// inspect the response in these tests — only the captured request.
func emptyAssistantResponse() *schemas.BifrostChatResponse {
	empty := ""
	return &schemas.BifrostChatResponse{
		Choices: []schemas.BifrostResponseChoice{
			makeToolChoice(&schemas.ChatMessageContent{ContentStr: &empty}, nil),
		},
	}
}

// TestCompleteOneTurn_AnthropicRendersImageContentBlocks asserts that when a
// user ChatMessage carries ContentBlocks (a text block followed by an image
// block), completeOneTurn translates them into the matching Bifrost
// schemas.ChatContentBlock slice on the wire request — text block as
// ChatContentBlockTypeText with Text populated, image block as
// ChatContentBlockTypeImage with ImageURLStruct.URL preserved verbatim. This
// is the Anthropic provider lane.
func TestCompleteOneTurn_AnthropicRendersImageContentBlocks(t *testing.T) {
	fake := &fakeBifrostRequester{resp: emptyAssistantResponse()}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")

	const imageURL = "https://example.com/dash.png"
	_, err := client.completeOneTurn(context.Background(),
		[]ChatMessage{
			{
				Role: "user",
				ContentBlocks: []ContentBlock{
					{Type: ContentBlockText, Text: "Below is a screenshot:"},
					{Type: ContentBlockImageURL, ImageURL: imageURL},
				},
			},
		},
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fake.lastRequest)
	require.Len(t, fake.lastRequest.Input, 1)

	msg := fake.lastRequest.Input[0]
	require.NotNil(t, msg.Content)
	assert.Nil(t, msg.Content.ContentStr,
		"multimodal user message must be promoted to ContentBlocks; ContentStr must be nil")
	require.Len(t, msg.Content.ContentBlocks, 2)

	// Block 0: text.
	textBlock := msg.Content.ContentBlocks[0]
	assert.Equal(t, schemas.ChatContentBlockTypeText, textBlock.Type)
	require.NotNil(t, textBlock.Text)
	assert.Equal(t, "Below is a screenshot:", *textBlock.Text)
	assert.Nil(t, textBlock.ImageURLStruct, "text block must not carry image data")

	// Block 1: image URL.
	imageBlock := msg.Content.ContentBlocks[1]
	assert.Equal(t, schemas.ChatContentBlockTypeImage, imageBlock.Type)
	assert.Nil(t, imageBlock.Text, "image block must not carry text")
	require.NotNil(t, imageBlock.ImageURLStruct)
	assert.Equal(t, imageURL, imageBlock.ImageURLStruct.URL)
}

// TestCompleteOneTurn_OpenAICompatRendersImageContentBlocks asserts the same
// translation on the OpenAI provider lane (which Groq also rides on via a
// custom BaseURL). Bifrost normalizes image_url blocks across providers, so
// the wire shape is identical to the Anthropic lane.
func TestCompleteOneTurn_OpenAICompatRendersImageContentBlocks(t *testing.T) {
	fake := &fakeBifrostRequester{resp: emptyAssistantResponse()}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "gpt-4o-mini")

	const imageURL = "https://cdn.example.com/figure-12.png"
	_, err := client.completeOneTurn(context.Background(),
		[]ChatMessage{
			{
				Role: "user",
				ContentBlocks: []ContentBlock{
					{Type: ContentBlockText, Text: "Does this image match the doc passage?"},
					{Type: ContentBlockImageURL, ImageURL: imageURL},
				},
			},
		},
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fake.lastRequest)
	require.Len(t, fake.lastRequest.Input, 1)

	msg := fake.lastRequest.Input[0]
	require.NotNil(t, msg.Content)
	assert.Nil(t, msg.Content.ContentStr,
		"multimodal user message must be promoted to ContentBlocks; ContentStr must be nil")
	require.Len(t, msg.Content.ContentBlocks, 2)

	textBlock := msg.Content.ContentBlocks[0]
	assert.Equal(t, schemas.ChatContentBlockTypeText, textBlock.Type)
	require.NotNil(t, textBlock.Text)
	assert.Equal(t, "Does this image match the doc passage?", *textBlock.Text)

	imageBlock := msg.Content.ContentBlocks[1]
	assert.Equal(t, schemas.ChatContentBlockTypeImage, imageBlock.Type)
	require.NotNil(t, imageBlock.ImageURLStruct)
	assert.Equal(t, imageURL, imageBlock.ImageURLStruct.URL)

	// Guardrail: no Anthropic-only cache_control leaks onto the OpenAI lane.
	assert.Nil(t, textBlock.CacheControl, "OpenAI lane must not carry cache_control")
	assert.Nil(t, imageBlock.CacheControl, "OpenAI lane must not carry cache_control")
}

// TestCompleteOneTurn_ContentBlocksTakePrecedenceOverContentStr asserts that
// when a user ChatMessage carries both Content (string) and ContentBlocks,
// the ContentBlocks path wins — the wire request must NOT include ContentStr,
// otherwise downstream providers may send the flat text and silently drop
// the image.
func TestCompleteOneTurn_ContentBlocksTakePrecedenceOverContentStr(t *testing.T) {
	fake := &fakeBifrostRequester{resp: emptyAssistantResponse()}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "gpt-4o-mini")

	_, err := client.completeOneTurn(context.Background(),
		[]ChatMessage{
			{
				Role:    "user",
				Content: "ignored fallback text",
				ContentBlocks: []ContentBlock{
					{Type: ContentBlockText, Text: "real text"},
					{Type: ContentBlockImageURL, ImageURL: "https://example.com/x.png"},
				},
			},
		},
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, fake.lastRequest)
	require.Len(t, fake.lastRequest.Input, 1)

	msg := fake.lastRequest.Input[0]
	require.NotNil(t, msg.Content)
	assert.Nil(t, msg.Content.ContentStr,
		"ContentBlocks must win over Content; ContentStr must be nil")
	require.Len(t, msg.Content.ContentBlocks, 2)
	require.NotNil(t, msg.Content.ContentBlocks[0].Text)
	assert.Equal(t, "real text", *msg.Content.ContentBlocks[0].Text)
}
