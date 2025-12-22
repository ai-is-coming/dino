package providers

import (
	"context"
	"fmt"
	"strings"
)

// Provider defines the common interface all model providers should implement.
// Chat should call onDelta for each content chunk (and optionally thinking chunks).
// Implementations may call onDelta once when not streaming.
type Provider interface {
	Chat(ctx context.Context, opts ChatOptions) error
}

// ProviderConfig contains common configuration for all providers.
type ProviderConfig struct {
	APIKey   string
	BaseURL  string
	AuthType string // "api_key" (default) or "auth_token"
}

// New returns a Provider implementation based on the given name.
// Supports: "ollama", "openai", "gemini".
func New(name string, cfg ProviderConfig) (Provider, error) {
	s := strings.ToLower(strings.TrimSpace(name))
	switch s {
	case "", "ollama":
		return NewOllamaFromEnv()
	case "openai":
		return NewOpenAI(cfg)
	case "gemini":
		return NewGemini(cfg)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
}
