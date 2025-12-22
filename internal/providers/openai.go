package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// OpenAI implements the Provider interface using the official OpenAI Go SDK.
type OpenAI struct {
	client openai.Client
}

const browserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// NewOpenAI constructs an OpenAI provider from ProviderConfig.
func NewOpenAI(cfg ProviderConfig) (*OpenAI, error) {
	var opts []option.RequestOption

	// Auth: set standard Authorization header and (optionally) a proxy-friendly X-Api-Key.
	if strings.TrimSpace(cfg.APIKey) != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
		// Some OpenAI-compatible gateways expect X-Api-Key instead of Authorization.
		opts = append(opts, option.WithHeader("X-Api-Key", cfg.APIKey))
	}

	// BaseURL: the SDK expects a base like https://.../v1
	if b := strings.TrimSpace(cfg.BaseURL); b != "" {
		b = strings.TrimRight(b, "/")
		if !strings.HasSuffix(b, "/v1") {
			b += "/v1"
		}

		opts = append(opts, option.WithBaseURL(b))
	}

	// Spoof a browser UA for compatibility with some proxies/gateways.
	opts = append(opts, option.WithHeader("User-Agent", browserUserAgent))

	c := openai.NewClient(opts...)
	return &OpenAI{client: c}, nil
}

// Chat calls the Chat Completions API with optional vision inputs and streaming.
func (o *OpenAI) Chat(ctx context.Context, opts ChatOptions) error {
	if o == nil {
		return fmt.Errorf("providers/openai: nil client")
	}

	// Build content parts: text + optional images (as data URLs)
	parts := []openai.ChatCompletionContentPartUnionParam{
		openai.TextContentPart(strings.TrimSpace(opts.Prompt)),
	}

	for _, img := range opts.Images {
		mime := http.DetectContentType(img)
		if !strings.HasPrefix(mime, "image/") {
			mime = "image/png"
		}

		dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img)
		parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: dataURL}))
	}

	messages := make([]openai.ChatCompletionMessageParamUnion, 0, MaxMessageRoleCount)
	if system := strings.TrimSpace(opts.SystemPrompt); system != "" {
		messages = append(messages, openai.ChatCompletionMessageParamUnion{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: openai.String(system),
				},
			},
		})
	}

	messages = append(messages, openai.ChatCompletionMessageParamUnion{
		OfUser: &openai.ChatCompletionUserMessageParam{
			Content: openai.ChatCompletionUserMessageParamContentUnion{
				OfArrayOfContentParts: parts,
			},
		},
	})

	params := openai.ChatCompletionNewParams{
		Messages:    messages,
		Model:       shared.ChatModel(opts.Model),
		Temperature: openai.Float(opts.Temperature),
		TopP:        openai.Float(opts.TopP),
	}

	if !opts.NoResponseFormat {
		params.ResponseFormat = toResponseFormat(opts.Format)
	}

	onDelta := onDeltaOrNoop(opts.OnDelta)

	if opts.Stream {
		return o.handleStreamingChat(ctx, params, onDelta)
	}

	resp, err := o.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return formatOpenAIAPIError("providers/openai: chat completion failed", err)
	}

	if len(resp.Choices) == 0 {
		return fmt.Errorf("providers/openai: empty choices")
	}
	return onDelta(strings.TrimSpace(resp.Choices[0].Message.Content), "")
}

func (o *OpenAI) handleStreamingChat(
	ctx context.Context,
	params openai.ChatCompletionNewParams,
	onDelta func(string, string) error,
) error {
	stream := o.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	for stream.Next() {
		ch := stream.Current()

		if len(ch.Choices) == 0 {
			continue
		}

		content := strings.TrimSpace(ch.Choices[0].Delta.Content)
		if content == "" {
			continue
		}

		if err := onDelta(content, ""); err != nil {
			return err
		}
	}

	if err := stream.Err(); err != nil {
		return formatOpenAIAPIError("providers/openai: streaming chat completion failed", err)
	}

	return nil
}

// toResponseFormat converts a raw Format value into an OpenAI ResponseFormat union.
func toResponseFormat(raw json.RawMessage) openai.ChatCompletionNewParamsResponseFormatUnion {
	// Default to JSON object mode if nothing provided
	if len(raw) == 0 {
		return openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}
	}

	trim := strings.TrimSpace(string(raw))
	if trim == `"json"` || trim == `json` {
		return openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}
	}

	// If it's a valid JSON object, treat it as a schema
	var schema any
	if json.Unmarshal(raw, &schema) == nil {
		return openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "dino_schema",
					Strict: openai.Bool(true),
					Schema: schema,
				},
			},
		}
	}
	// Fallback to JSON object mode
	return openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
	}
}

func formatOpenAIAPIError(context string, err error) error {
	if err == nil {
		return nil
	}

	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return wrapNonOpenAIError(context, err)
	}

	detail := buildOpenAIAPIErrorDetail(context, apiErr)
	if !strings.HasSuffix(detail, "\n") {
		detail += "\n"
	}

	return fmt.Errorf("%s%w", detail, err)
}

func wrapNonOpenAIError(context string, err error) error {
	if context == "" {
		return err
	}

	return fmt.Errorf("%s: %w", context, err)
}

func buildOpenAIAPIErrorDetail(context string, apiErr *openai.Error) string {
	var buf bytes.Buffer

	buf.WriteString(resolveOpenAIErrorLabel(context))
	buf.WriteByte('\n')

	if status := extractStatusInfo(apiErr); status != "" {
		buf.WriteString(status)
	}

	buf.WriteString(formatHeaders(extractResponseHeader(apiErr)))
	buf.WriteString(formatResponseBody(apiErr))

	return buf.String()
}

func resolveOpenAIErrorLabel(context string) string {
	if context != "" {
		return context
	}

	return "providers/openai: api error"
}

func extractResponseHeader(apiErr *openai.Error) http.Header {
	if apiErr == nil || apiErr.Response == nil {
		return nil
	}

	return apiErr.Response.Header
}

func formatResponseBody(apiErr *openai.Error) string {
	var buf bytes.Buffer

	buf.WriteString("body:\n")

	if apiErr == nil || apiErr.Response == nil || apiErr.Response.Body == nil {
		buf.WriteString("  <empty>\n")
		return buf.String()
	}

	body, readErr := readResponseBody(apiErr.Response.Body)
	if readErr != nil {
		fmt.Fprintf(&buf, "  <error reading body: %v>\n", readErr)
		return buf.String()
	}

	apiErr.Response.Body = io.NopCloser(strings.NewReader(body))
	if body == "" {
		buf.WriteString("  <empty>\n")
		return buf.String()
	}

	buf.WriteString(body)

	if !strings.HasSuffix(body, "\n") {
		buf.WriteByte('\n')
	}

	return buf.String()
}

func extractStatusInfo(apiErr *openai.Error) string {
	if apiErr == nil || apiErr.StatusCode == 0 {
		return ""
	}

	statusText := http.StatusText(apiErr.StatusCode)
	if statusText != "" {
		return fmt.Sprintf("status: %d %s\n", apiErr.StatusCode, statusText)
	}

	return fmt.Sprintf("status: %d\n", apiErr.StatusCode)
}

func formatHeaders(header http.Header) string {
	var buf strings.Builder
	buf.WriteString("headers:\n")

	if len(header) == 0 {
		buf.WriteString("  <none>\n")
		return buf.String()
	}

	headerKeys := make([]string, 0, len(header))

	for key := range header {
		headerKeys = append(headerKeys, key)
	}

	sort.Strings(headerKeys)

	for _, key := range headerKeys {
		values := header[key]
		if len(values) == 0 {
			fmt.Fprintf(&buf, "  %s:\n", key)
			continue
		}

		for _, value := range values {
			fmt.Fprintf(&buf, "  %s: %s\n", key, value)
		}
	}

	return buf.String()
}

func readResponseBody(body io.ReadCloser) (string, error) {
	if body == nil {
		return "", nil
	}

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		_ = body.Close()
		return "", err
	}

	if err := body.Close(); err != nil {
		return "", err
	}

	return string(bodyBytes), nil
}
