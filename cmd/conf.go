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

// confCmd generates a default conf.yaml in the current directory (or a specified path).
var confCmd = &cobra.Command{
	Use:   "conf",
	Short: "Generate default config file to conf.yaml",
	Long:  "Generate a default configuration file to ./conf.yaml. If the file already exists, it will not be overwritten by default; use --force to overwrite.",
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
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}

		if err := os.WriteFile(out, []byte(defaultConfYAML), 0644); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}
		color.New(color.FgGreen).Printf("Generated default config: %s\n", out)
		return nil
	},
}

const defaultConfYAML = `provider: ollama
model: qwen3-vl:32b
temperature: 0.6
topP: 0.95
stream: true
input: 'inputs'
output: 'outputs'
classes:
- person
- climb
prompt: |
  You are a concise image assistant. 
  - You must not output any reasoning, explanation, analysis, or thinking.
  - Never reveal chain-of-thought.
  - Only output the final answer directly.
  - If the user asks “why” or “how”, still respond as briefly as possible without revealing reasoning.
  - Keep answers short and direct.

  Analyze the image and detect only people (humans). Ignore all non-person objects.
  Output only a single valid JSON string and nothing else (no extra text, no code fences).
  Strictly follow these rules:
  - Return a single JSON array of detections (not wrapped in an object).
  - Each detection must include:
    - label: "person" for normal people; "climb" for people who are climbing
    - bbox: pixel coordinates ["x1", "y1", "x2", "y2"] as strings
  - Only include detections for people. If uncertain whether someone is climbing, use "person".
  - If no people are found, return [].
  - Output must be valid standard JSON: no comments, no trailing commas, no NaN/Infinity, and no extra keys.
  - Example output [{"label": "climb", "bbox": ["100", "200", "120", "300"]}, {"label": "person", "bbox": ["400", "220", "460", "360"]}]
`

func init() {
	confCmd.Flags().StringVarP(&confOutput, "output", "o", "", "output config file path (default ./conf.yaml)")
	confCmd.Flags().BoolVarP(&confForce, "force", "f", false, "overwrite existing config file")
}
