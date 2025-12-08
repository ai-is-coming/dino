package providers

import "encoding/json"

// ChatOptions describes parameters for a provider chat call.
// This is designed to be generic enough so other providers can adopt a similar shape.
type ChatOptions struct {
	// Required
	Model  string
	Prompt string

	// Optional controls
	Stream      bool
	Think       bool
	Temperature float64
	TopP        float64

	// Optional JSON schema/format control. If nil, defaults to "\"json\"".
	Format json.RawMessage

	// Optional vision inputs. If provided, they will be attached to the user message.
	Images [][]byte

	// Extra provider-specific options; values here override the derived ones.
	Options map[string]any

	// OnDelta callback for streaming/non-streaming responses; if nil, chunks are ignored.
	OnDelta func(content, thinking string) error
}

// Option is a functional option to build ChatOptions ergonomically.
type Option func(*ChatOptions)

// NewChatOptions constructs ChatOptions from required fields plus Option fns.
func NewChatOptions(model, prompt string, fns ...Option) ChatOptions {
	co := ChatOptions{Model: model, Prompt: prompt}

	for _, fn := range fns {
		if fn != nil {
			fn(&co)
		}
	}
	return co
}

// WithTemperature sets the sampling temperature.
func WithTemperature(v float64) Option { return func(c *ChatOptions) { c.Temperature = v } }

// WithTopP sets nucleus sampling probability.
func WithTopP(v float64) Option { return func(c *ChatOptions) { c.TopP = v } }

// WithStream toggles streaming.
func WithStream(b bool) Option { return func(c *ChatOptions) { c.Stream = b } }

// WithThink toggles reasoning/thinking capability if supported.
func WithThink(b bool) Option { return func(c *ChatOptions) { c.Think = b } }

// WithImages attaches one or more image bytes for multimodal input.
func WithImages(imgs ...[]byte) Option { return func(c *ChatOptions) { c.Images = imgs } }

// WithFormat sets the output format/schema raw message.
func WithFormat(raw json.RawMessage) Option { return func(c *ChatOptions) { c.Format = raw } }

// WithSchemaString sets the output format from a JSON string literal or object string.
func WithSchemaString(s string) Option { return func(c *ChatOptions) { c.Format = json.RawMessage(s) } }

// WithOptions merges extra provider-specific options; later calls override earlier keys.
func WithOptions(m map[string]any) Option {
	return func(c *ChatOptions) {
		if len(m) == 0 {
			return
		}

		if c.Options == nil {
			c.Options = map[string]any{}
		}

		for k, v := range m {
			c.Options[k] = v
		}
	}
}

// WithExtraOption sets a single provider-specific option key.
func WithExtraOption(k string, v any) Option {
	return func(c *ChatOptions) {
		if c.Options == nil {
			c.Options = map[string]any{}
		}

		c.Options[k] = v
	}
}

// WithOnDelta sets the streaming callback.
func WithOnDelta(fn func(content, thinking string) error) Option {
	return func(c *ChatOptions) { c.OnDelta = fn }
}
