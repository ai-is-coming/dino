package conf

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

var k = koanf.New(".")

// Config holds all configuration for the application.
type Config struct {
	Provider string `koanf:"provider"`
	Model    string `koanf:"model"`
}

// Init initializes the configuration from file and environment variables.
func Init(configFile string) error {
	// Load from config file if specified
	if configFile != "" {
		if err := k.Load(file.Provider(configFile), yaml.Parser()); err != nil {
			return fmt.Errorf("error loading config file: %w", err)
		}
	}

	k.Load(env.Provider(EnvPrefix, ".", func(s string) string {
		return strings.Replace(strings.ToLower(
			strings.TrimPrefix(s, EnvPrefix)), "_", ".", -1)
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
