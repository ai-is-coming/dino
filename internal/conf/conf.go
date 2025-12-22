package conf

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

var k = koanf.New(".")

// Config holds all configuration for the application.
type Config struct {
	Provider         string   `koanf:"provider"`
	Model            string   `koanf:"model"`
	Stream           bool     `koanf:"stream"`
	Input            string   `koanf:"input"`
	Output           string   `koanf:"output"`
	Classes          []string `koanf:"classes"`
	Prompt           string   `koanf:"prompt"`
	SystemPrompt     string   `koanf:"systemPrompt"`
	NoResponseFormat bool     `koanf:"noResponseFormat"`
	Temperature      string   `koanf:"temperature"`
	TopP             string   `koanf:"topP"`
	Schema           string   `koanf:"schema"`
	APIKey           string   `koanf:"apiKey"`
	BaseURL          string   `koanf:"baseURL"`
	AuthType         string   `koanf:"authType"`  // "api_key" (default) or "auth_token"
	BboxScale        int      `koanf:"bboxScale"` // Scale for bbox normalization (e.g., 1000); 0 means no denormalization
}

// Init initializes the configuration from file and environment variables.
func Init(configFile string) error {
	// Load from config file if specified
	if configFile != "" {
		if err := k.Load(file.Provider(configFile), yaml.Parser()); err != nil {
			return fmt.Errorf("error loading config file: %w", err)
		}
	} else {
		// Try default locations if no explicit config file is provided
		candidates := []string{
			"conf.yaml",
			"./conf.yaml",
			"dino/conf.yaml",
			"./dino/conf.yaml",
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				// Best-effort load; ignore the error here to still allow ENV overrides
				_ = k.Load(file.Provider(p), yaml.Parser())

				break
			}
		}
	}

	k.Load(env.Provider(EnvPrefix, ".", func(s string) string {
		s = strings.TrimPrefix(s, EnvPrefix)
		s = strings.ToLower(s)
		return strings.Replace(s, "_", ".", -1)
	}), nil)
	return nil
}

// Load reads configuration from koanf.
func Load() (*Config, error) {
	var config Config

	if err := k.Unmarshal("", &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// Get returns the koanf instance for direct access.
func Get() *koanf.Koanf {
	return k
}
