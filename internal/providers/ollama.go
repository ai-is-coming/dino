package providers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ollama/ollama/api"
)

// Ollama wraps an Ollama API client.
// Use NewOllamaFromEnv() to construct from environment variables (OLLAMA_HOST, etc.).
type Ollama struct {
	client *api.Client
}

// NewOllama creates a provider from an existing client instance.
func NewOllama(client *api.Client) *Ollama {
	return &Ollama{client: client}
}

// NewOllamaFromEnv creates a provider using api.ClientFromEnvironment().
func NewOllamaFromEnv() (*Ollama, error) {
	c, err := api.ClientFromEnvironment()
	if err != nil {
		return nil, err
	}
	return &Ollama{client: c}, nil
}

// toAPIImages converts [][]byte to []api.ImageData.
func toAPIImages(imgs [][]byte) []api.ImageData {
	if len(imgs) == 0 {
		return nil
	}

	out := make([]api.ImageData, 0, len(imgs))
	for _, b := range imgs {
		out = append(out, api.ImageData(b))
	}
	return out
}

// mergeOptions builds the options map, only setting non-zero values and letting user options override.
func mergeOptions(opts ChatOptions) map[string]any {
	m := map[string]any{
		"enable_thinking": opts.Think,
	}
	if opts.Temperature != 0 {
		m["temperature"] = opts.Temperature
	}

	if opts.TopP != 0 {
		m["top_p"] = opts.TopP
	}

	for k, v := range opts.Options {
		m[k] = v
	}
	return m
}

// ensureFormat returns the provided format or a default JSON indicator.
func ensureFormat(f json.RawMessage) json.RawMessage {
	if len(f) == 0 {
		return json.RawMessage(`"json"`)
	}
	return f
}

// streamPtr returns nil for streaming (default) or a pointer to false to disable.
func streamPtr(stream bool) *bool {
	if stream {
		return nil
	}

	v := false
	return &v
}

// onDeltaOrNoop ensures a non-nil callback.
func onDeltaOrNoop(fn func(content, thinking string) error) func(string, string) error {
	if fn == nil {
		return func(string, string) error { return nil }
	}
	return fn
}

// Chat performs a chat completion against Ollama.
// onDelta is invoked for each response chunk; in non-stream mode it may be called once with the full content.
// The callback receives the assistant content and (optionally) the model's thinking chunk if present.
func (o *Ollama) Chat(ctx context.Context, opts ChatOptions) error {
	if o == nil || o.client == nil {
		return ErrNilClient
	}

	messages := make([]api.Message, 0, MaxMessageRoleCount)
	if system := strings.TrimSpace(opts.SystemPrompt); system != "" {
		messages = append(messages, api.Message{Role: "system", Content: system})
	}

	userMsg := api.Message{Role: "user", Content: opts.Prompt}
	if imgs := toAPIImages(opts.Images); len(imgs) > 0 {
		userMsg.Images = imgs
	}

	messages = append(messages, userMsg)

	merged := mergeOptions(opts)
	format := ensureFormat(opts.Format)

	req := &api.ChatRequest{
		Model:    opts.Model,
		Messages: messages,
		Think:    &api.ThinkValue{Value: opts.Think},
		Options:  merged,
		Format:   format,
		Stream:   streamPtr(opts.Stream),
	}

	respFunc := func(resp api.ChatResponse) error {
		onDelta := onDeltaOrNoop(opts.OnDelta)
		return onDelta(resp.Message.Content, resp.Message.Thinking)
	}
	return o.client.Chat(ctx, req, respFunc)
}

// ErrNilClient is returned when the provider is used without a valid client.
var ErrNilClient = errors.New("providers/ollama: nil client")
