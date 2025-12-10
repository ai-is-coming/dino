package providers

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Claude wraps an Anthropic Claude API client.
type Claude struct {
	client *anthropic.Client
}

// defaultUserAgent is the User-Agent header to use for API requests (Chrome on macOS).
const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// defaultMaxTokens is the default max tokens for Claude responses.
const defaultMaxTokens = int64(4096)

// NewClaude creates a Claude provider.
// apiKey: the API key or auth token to use.
// baseURL: optional custom API endpoint.
// authType: "api_key" uses X-Api-Key header (default), "auth_token" uses Authorization: Bearer header.
func NewClaude(apiKey, baseURL, authType string) (*Claude, error) {
	var opts []option.RequestOption

	if apiKey != "" {
		switch authType {
		case "auth_token", "bearer":
			opts = append(opts, option.WithAuthToken(apiKey))
		default: // "api_key" or empty
			opts = append(opts, option.WithAPIKey(apiKey))
		}
	}

	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	// Override User-Agent to avoid being blocked by some API proxies
	opts = append(opts, option.WithHeader("User-Agent", defaultUserAgent))
	client := anthropic.NewClient(opts...)

	return &Claude{client: &client}, nil
}

// toClaudeImageBlocks converts [][]byte to anthropic image content blocks.
func toClaudeImageBlocks(imgs [][]byte) []anthropic.ContentBlockParamUnion {
	if len(imgs) == 0 {
		return nil
	}

	out := make([]anthropic.ContentBlockParamUnion, 0, len(imgs))
	for _, b := range imgs {
		// Encode image as base64
		encoded := base64.StdEncoding.EncodeToString(b)
		// Default to JPEG, could be improved with proper detection
		out = append(out, anthropic.NewImageBlockBase64(string(anthropic.Base64ImageSourceMediaTypeImageJPEG), encoded))
	}
	return out
}

// Chat performs a chat completion against Claude.
// onDelta is invoked for each response chunk; in non-stream mode it may be called once with the full content.
// The callback receives the assistant content and (optionally) the model's thinking chunk if present.
func (c *Claude) Chat(ctx context.Context, opts ChatOptions) error {
	if c == nil {
		return ErrClaudeNilClient
	}

	// Build message contents
	var contents []anthropic.ContentBlockParamUnion

	// Add images first if present
	if imgBlocks := toClaudeImageBlocks(opts.Images); len(imgBlocks) > 0 {
		contents = append(contents, imgBlocks...)
	}

	// Add text prompt
	contents = append(contents, anthropic.NewTextBlock(opts.Prompt))

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(contents...),
	}

	onDelta := onDeltaOrNoop(opts.OnDelta)

	if opts.Stream {
		return c.chatStreaming(ctx, messages, opts, onDelta)
	}

	return c.chatNonStreaming(ctx, messages, opts, onDelta)
}

func (c *Claude) chatStreaming(
	ctx context.Context,
	messages []anthropic.MessageParam,
	opts ChatOptions,
	onDelta func(content, thinking string) error,
) error {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(opts.Model),
		Messages:  messages,
		MaxTokens: defaultMaxTokens,
	}

	if opts.Temperature != 0 {
		params.Temperature = anthropic.Float(opts.Temperature)
	}

	if opts.TopP != 0 {
		params.TopP = anthropic.Float(opts.TopP)
	}

	stream := c.client.Messages.NewStreaming(ctx, params)
	for stream.Next() {
		event := stream.Current()

		switch eventVariant := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			switch deltaVariant := eventVariant.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				_ = onDelta(deltaVariant.Text, "")
			case anthropic.ThinkingDelta:
				_ = onDelta("", deltaVariant.Thinking)
			}
		}
	}

	return stream.Err()
}

func (c *Claude) chatNonStreaming(
	ctx context.Context,
	messages []anthropic.MessageParam,
	opts ChatOptions,
	onDelta func(content, thinking string) error,
) error {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(opts.Model),
		Messages:  messages,
		MaxTokens: defaultMaxTokens,
	}

	if opts.Temperature != 0 {
		params.Temperature = anthropic.Float(opts.Temperature)
	}

	if opts.TopP != 0 {
		params.TopP = anthropic.Float(opts.TopP)
	}

	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return err
	}

	// Call onDelta with full content
	for _, content := range resp.Content {
		switch block := content.AsAny().(type) {
		case anthropic.TextBlock:
			if err := onDelta(block.Text, ""); err != nil {
				return err
			}
		case anthropic.ThinkingBlock:
			if err := onDelta("", block.Thinking); err != nil {
				return err
			}
		}
	}

	return nil
}

// ErrClaudeNilClient is returned when the provider is used without a valid client.
var ErrClaudeNilClient = errors.New("providers/claude: nil client")
