package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	geminiStreamingPath  = ":streamGenerateContent"
	geminiNonStreamPath  = ":generateContent"
	geminiHTTPTimeout    = 2 * time.Minute
	httpStatusClientErr  = 400
)

// Gemini implements the Provider interface using Gemini-compatible REST endpoints.
type Gemini struct {
	client   *http.Client
	baseURL  string
	apiKey   string
	authType string
}

// NewGemini constructs a Gemini provider using the provided configuration.
func NewGemini(cfg ProviderConfig) (*Gemini, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("providers/gemini: missing api key")
	}

	authType := strings.ToLower(strings.TrimSpace(cfg.AuthType))
	if authType == "" {
		authType = "api_key"
	}

	if authType != "api_key" && authType != "auth_token" {
		return nil, fmt.Errorf("providers/gemini: unsupported auth type %q", cfg.AuthType)
	}

	base := normalizeGeminiBaseURL(cfg.BaseURL)

	return &Gemini{
		client:   &http.Client{Timeout: geminiHTTPTimeout},
		baseURL:  base,
		apiKey:   apiKey,
		authType: authType,
	}, nil
}

func normalizeGeminiBaseURL(base string) string {
	b := strings.TrimSpace(base)
	if b == "" {
		return defaultGeminiBaseURL
	}

	b = strings.TrimRight(b, "/")
	if strings.HasSuffix(b, "/v1") || strings.HasSuffix(b, "/v1beta") || strings.HasSuffix(b, "/v1beta1") {
		return b
	}
	return b + "/v1beta"
}

// Chat sends a user prompt (with optional images) to a Gemini endpoint and streams responses if requested.
func (g *Gemini) Chat(ctx context.Context, opts ChatOptions) error {
	if g == nil || g.client == nil {
		return fmt.Errorf("providers/gemini: nil client")
	}

	if strings.TrimSpace(opts.Model) == "" {
		return fmt.Errorf("providers/gemini: model is required")
	}

	body, err := g.buildRequestBody(opts)
	if err != nil {
		return err
	}

	endpoint, err := g.buildEndpoint(opts.Model, opts.Stream)
	if err != nil {
		return err
	}

	onDelta := onDeltaOrNoop(opts.OnDelta)
	if opts.Stream {
		return g.stream(ctx, endpoint, body, onDelta)
	}
	return g.nonStream(ctx, endpoint, body, onDelta)
}

func (g *Gemini) buildRequestBody(opts ChatOptions) ([]byte, error) {
	contents, systemInstruction, err := g.buildContents(opts)
	if err != nil {
		return nil, err
	}

	req := geminiGenerateRequest{
		Contents: contents,
	}

	if systemInstruction != nil {
		req.SystemInstruction = systemInstruction
	}

	if cfg := buildGenerationConfig(opts); cfg != nil {
		req.GenerationConfig = cfg
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("providers/gemini: encode request: %w", err)
	}
	return buf.Bytes(), nil
}

func (g *Gemini) buildContents(opts ChatOptions) ([]geminiContent, *geminiContent, error) {
	// Pre-allocate: 1 for text prompt + N images
	parts := make([]geminiPart, 0, 1+len(opts.Images))
	if prompt := strings.TrimSpace(opts.Prompt); prompt != "" {
		parts = append(parts, geminiPart{Text: prompt})
	}

	for _, img := range opts.Images {
		if len(img) == 0 {
			continue
		}

		mime := http.DetectContentType(img)
		if !strings.HasPrefix(mime, "image/") {
			mime = "image/png"
		}

		parts = append(parts, geminiPart{
			InlineData: &geminiInlineData{
				MimeType: mime,
				Data:     base64.StdEncoding.EncodeToString(img),
			},
		})
	}

	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("providers/gemini: prompt or images are required")
	}

	contents := []geminiContent{
		{
			Role:  "user",
			Parts: parts,
		},
	}

	var systemInstruction *geminiContent
	if system := strings.TrimSpace(opts.SystemPrompt); system != "" {
		systemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: system}},
		}
	}
	return contents, systemInstruction, nil
}

func (g *Gemini) buildEndpoint(model string, stream bool) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("providers/gemini: model is required")
	}

	escapedModel := url.PathEscape(model)
	pathSuffix := geminiNonStreamPath
	query := ""

	if stream {
		pathSuffix = geminiStreamingPath
		query = "?alt=sse" // Required for SSE streaming format
	}

	base := strings.TrimRight(g.baseURL, "/")
	return fmt.Sprintf("%s/models/%s%s%s", base, escapedModel, pathSuffix, query), nil
}

func buildGenerationConfig(opts ChatOptions) *geminiGenerationConfig {
	cfg := &geminiGenerationConfig{}
	if opts.Temperature != 0 {
		cfg.Temperature = floatPtr(opts.Temperature)
	}

	if opts.TopP != 0 {
		cfg.TopP = floatPtr(opts.TopP)
	}

	if topK := extractTopK(opts.Options); topK != nil {
		cfg.TopK = topK
	}

	if opts.NoResponseFormat {
		if cfg.isEmpty() {
			return nil
		}
		return cfg
	}

	if len(opts.Format) > 0 {
		cfg.ResponseMimeType = "application/json"

		var schema any
		if json.Unmarshal(opts.Format, &schema) == nil {
			cfg.ResponseSchema = schema
		}
	} else {
		cfg.ResponseMimeType = "application/json"
	}

	if cfg.isEmpty() {
		return nil
	}
	return cfg
}

func extractTopK(opts map[string]any) *int {
	if len(opts) == 0 {
		return nil
	}

	if v, ok := opts["topK"]; ok {
		if i := toInt(v); i != nil {
			return i
		}
	}

	if v, ok := opts["top_k"]; ok {
		if i := toInt(v); i != nil {
			return i
		}
	}
	return nil
}

func toInt(v any) *int {
	switch t := v.(type) {
	case int:
		return intPtr(t)
	case int32:
		return intPtr(int(t))
	case int64:
		return intPtr(int(t))
	case float32:
		return intPtr(int(t))
	case float64:
		return intPtr(int(t))
	case json.Number:
		if i, err := strconv.Atoi(t.String()); err == nil {
			return intPtr(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return intPtr(i)
		}
	}
	return nil
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

func (cfg *geminiGenerationConfig) isEmpty() bool {
	if cfg == nil {
		return true
	}
	return cfg.Temperature == nil && cfg.TopP == nil && cfg.TopK == nil &&
		cfg.ResponseMimeType == "" && cfg.ResponseSchema == nil
}

func (g *Gemini) stream(
	ctx context.Context, endpoint string, body []byte, onDelta func(string, string) error,
) error {
	req, err := g.newRequest(ctx, endpoint, body)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "text/event-stream")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("providers/gemini: stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := checkGeminiResponse(resp); err != nil {
		return err
	}

	return g.readSSEStream(resp.Body, onDelta)
}

func (g *Gemini) readSSEStream(body io.Reader, onDelta func(string, string) error) error {
	reader := bufio.NewReader(body)

	var dataBuf bytes.Buffer

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return g.handleSSEReadError(err, &dataBuf, onDelta)
		}

		if done, err := g.processSSELine(line, &dataBuf, onDelta); done || err != nil {
			return err
		}
	}
}

func (g *Gemini) handleSSEReadError(
	err error, dataBuf *bytes.Buffer, onDelta func(string, string) error,
) error {
	if errors.Is(err, io.EOF) {
		if dataBuf.Len() == 0 {
			return nil
		}
		return g.dispatchSSEData(dataBuf.String(), onDelta)
	}
	return fmt.Errorf("providers/gemini: read stream: %w", err)
}

func (g *Gemini) processSSELine(
	line string, dataBuf *bytes.Buffer, onDelta func(string, string) error,
) (bool, error) {
	line = strings.TrimRight(line, "\r\n")

	if strings.HasPrefix(line, "data:") {
		segment := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		if dataBuf.Len() > 0 {
			dataBuf.WriteByte('\n')
		}

		dataBuf.WriteString(segment)
		return false, nil
	}

	if line == "" && dataBuf.Len() > 0 {
		if err := g.dispatchSSEData(dataBuf.String(), onDelta); err != nil {
			return true, err
		}

		dataBuf.Reset()
	}

	return false, nil
}

func (g *Gemini) dispatchSSEData(payload string, onDelta func(string, string) error) error {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" || trimmed == "[DONE]" {
		return nil
	}

	var chunk geminiGenerateResponse
	if err := json.Unmarshal([]byte(trimmed), &chunk); err != nil {
		return fmt.Errorf("providers/gemini: decode stream chunk: %w", err)
	}

	if chunk.Error != nil {
		return chunk.Error
	}
	return emitGeminiCandidates(chunk.Candidates, onDelta)
}

func (g *Gemini) nonStream(
	ctx context.Context, endpoint string, body []byte, onDelta func(string, string) error,
) error {
	req, err := g.newRequest(ctx, endpoint, body)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("providers/gemini: request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := checkGeminiResponse(resp); err != nil {
		return err
	}

	var out geminiGenerateResponse

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return fmt.Errorf("providers/gemini: decode response: %w", err)
	}

	return emitGeminiCandidates(out.Candidates, onDelta)
}

func (g *Gemini) newRequest(ctx context.Context, endpoint string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.attachAPIKey(endpoint), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("providers/gemini: build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", browserUserAgent)

	if g.authType == "auth_token" {
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
	}
	return req, nil
}

func (g *Gemini) attachAPIKey(endpoint string) string {
	if g.authType == "auth_token" {
		return endpoint
	}

	if strings.Contains(endpoint, "?") {
		return endpoint + "&key=" + url.QueryEscape(g.apiKey)
	}
	return endpoint + "?key=" + url.QueryEscape(g.apiKey)
}

func checkGeminiResponse(resp *http.Response) error {
	if resp.StatusCode < httpStatusClientErr {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)

	var apiErr geminiError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != nil {
		return apiErr.Error
	}

	return fmt.Errorf("providers/gemini: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func emitGeminiCandidates(candidates []geminiCandidate, onDelta func(string, string) error) error {
	for _, cand := range candidates {
		for _, part := range cand.Content.Parts {
			if part.Text == "" {
				continue
			}

			if err := onDelta(part.Text, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

type geminiGenerateRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"system_instruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerateResponse struct {
	Candidates []geminiCandidate   `json:"candidates"`
	Error      *geminiErrorPayload `json:"error"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiGenerationConfig struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"topP,omitempty"`
	TopK             *int     `json:"topK,omitempty"`
	ResponseMimeType string   `json:"responseMimeType,omitempty"`
	ResponseSchema   any      `json:"responseSchema,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiError struct {
	Error *geminiErrorPayload `json:"error"`
}

type geminiErrorPayload struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (e *geminiErrorPayload) Error() string {
	if e == nil {
		return ""
	}

	if e.Status != "" {
		return fmt.Sprintf("providers/gemini: %s (%d): %s", e.Status, e.Code, e.Message)
	}

	if e.Code != 0 {
		return fmt.Sprintf("providers/gemini: code %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("providers/gemini: %s", e.Message)
}
