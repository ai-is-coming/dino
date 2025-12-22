package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	confOutput string
	confForce  bool
)

const (
	confDirPerm  = 0o755
	confFilePerm = 0o644
)

// confCmd generates a default conf.yaml in the current directory (or a specified path).
var confCmd = &cobra.Command{
	Use:   "conf",
	Short: "Generate default config file to conf.yaml",
	Long: "Generate a default configuration file to ./conf.yaml. " +
		"If the file already exists, it will not be overwritten by default; " +
		"use --force to overwrite.",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := confOutput
		if strings.TrimSpace(out) == "" {
			out = "conf.yaml"
		}

		if _, err := os.Stat(out); err == nil && !confForce {
			color.New(color.FgYellow).Fprintf(os.Stderr, "File already exists: %s (use --force to overwrite)\n", out)
			return fmt.Errorf("file exists: %s", out)
		}

		if dir := filepath.Dir(out); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, confDirPerm); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}

		if err := os.WriteFile(out, []byte(defaultConfYAML), confFilePerm); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}
		color.New(color.FgGreen).Printf("Generated default config: %s\n", out)
		return nil
	},
}

const defaultConfYAML = `provider: ollama
model: qwen3-vl:32b
# apiKey: your-api-key-here  # Required for openai/gemini provider, optional for ollama
# baseURL: https://api.openai.com  # Optional: custom API endpoint
# authType: api_key  # api_key (X-Api-Key header) or auth_token (Bearer token)
# noResponseFormat: false  # Set to true for APIs that don't support response_format
temperature: 0.6
topP: 0.95
stream: true
input: 'inputs'
output: 'outputs'
# Bbox scale for models that return normalized coordinates (e.g., qwen3-vl uses 1000)
# Set to 0 or omit for models that return absolute pixel coordinates
bboxScale: 1000
classes:
- person
- climb
systemPrompt: |
  You are a concise image assistant.
  Do not rotate or transform the image orientation.
prompt: |
  Analyze the image and detect only people (humans). Ignore all non-person objects.
  Output only a single valid JSON string and nothing else (no extra text, no code fences).
  Strictly follow these rules:
  - Return a single JSON array of detections (not wrapped in an object).
  - Each detection must include:
    - label: "person" for normal people; "climb" for people who are climbing
    - bbox: pixel coordinates ["x1", "y1", "x2", "y2"] as integers
  - Only include detections for people. If uncertain whether someone is climbing, use "person".
  - If no people are found, return [].
  - Output must be valid standard JSON: no comments, no trailing commas, no NaN/Infinity, and no extra keys.
  - Example output [{"label": "climb", "bbox": [100, 200, 120, 300]}, {"label": "person", "bbox": [400, 220, 460, 360]}]
` + `schema: '{"type":"array","items":{"type":"object","properties":{"label":{"type":"string"},` +
	`"bbox":{"type":"array","items":{"type":"number"}}},"required":["label","bbox"]}}'
`

func attachConfFlags() {
	confCmd.Flags().StringVarP(&confOutput, "output", "o", "", "output config file path (default ./conf.yaml)")
	confCmd.Flags().BoolVarP(&confForce, "force", "f", false, "overwrite existing config file")
}
