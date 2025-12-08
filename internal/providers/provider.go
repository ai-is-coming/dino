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

// New returns a Provider implementation based on the given name.
// Currently supports: "ollama". You can extend this switch to add more providers.
func New(name string) (Provider, error) {
	s := strings.ToLower(strings.TrimSpace(name))
	switch s {
	case "", "ollama":
		return NewOllamaFromEnv()
	default:
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
}
