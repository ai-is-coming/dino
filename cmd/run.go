package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"dino/internal/conf"
	"dino/internal/providers"
	"dino/internal/utils"

	termcolor "github.com/fatih/color"
	"github.com/kaptinlin/jsonrepair"
	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"
)

var (
	stream    bool
	inputDir  string
	outputDir string
)

var (
	thinkTagRegexp     = regexp.MustCompile(`(?s)<think>.*?</think>`)
	markdownFenceRegex = regexp.MustCompile("(?i)```(?:json)?")
)

// Common constants to avoid magic numbers in code and satisfy linters.
const (
	permDir       = 0o755
	permFile      = 0o644
	rectThickness = 3
	bgAlpha       = 200
	jpegQuality   = 90
	bboxMinLen    = 4
)

// defaultPalette defines a set of distinct colors assigned to classes by order.
var defaultPalette = []color.RGBA{
	{0, 255, 0, 255},     // green
	{255, 0, 0, 255},     // red
	{0, 0, 255, 255},     // blue
	{255, 255, 0, 255},   // yellow
	{255, 0, 255, 255},   // magenta
	{0, 255, 255, 255},   // cyan
	{255, 165, 0, 255},   // orange
	{128, 0, 128, 255},   // purple
	{255, 105, 180, 255}, // pink
	{0, 128, 0, 255},     // dark green
}

// colorForLabel returns a deterministic color for a given label.
// - If the label exists in cfg.Classes (case-insensitive), color is assigned by its index.
// - Otherwise, a stable hash of the label selects a color from the palette.
func colorForLabel(label string, classes []string) color.RGBA {
	if strings.TrimSpace(label) == "" {
		return color.RGBA{255, 255, 255, 255} // white for empty label
	}

	lower := strings.ToLower(strings.TrimSpace(label))
	for i, c := range classes {
		if strings.ToLower(strings.TrimSpace(c)) == lower {
			return defaultPalette[i%len(defaultPalette)]
		}
	}
	// fallback: stable hash by summing bytes, deterministic across runs
	sum := 0
	for i := 0; i < len(lower); i++ {
		sum += int(lower[i])
	}
	return defaultPalette[sum%len(defaultPalette)]
}

// runCmd proxies to the configured provider. For provider=ollama, it calls the Ollama HTTP API
//
//	POST $OLLAMA_HOST/api/generate with stream=true/false
var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Run the configured provider/model",
	Long:  "Run the configured provider/model. If provider is 'ollama', calls the Ollama HTTP API (/api/generate).",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := conf.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
		model := strings.TrimSpace(cfg.Model)
		if provider == "" || model == "" {
			termcolor.New(termcolor.FgRed).Fprintln(os.Stderr, "provider/model not configured")
			termcolor.New(termcolor.FgHiBlack).Fprintln(os.Stderr, "Set via env DINO_PROVIDER and DINO_MODEL or a config file")
			return fmt.Errorf("missing configuration")
		}
		termcolor.New(termcolor.FgGreen).Printf(
			"provider: %s, model: %s, stream: %t, temperature: %s, top_p: %s, bboxScale: %d\n",
			provider, model, stream, cfg.Temperature, cfg.TopP, cfg.BboxScale,
		)

		// parse temperature/top_p from string config into float64 with defaults
		temp := 0.2
		if s := strings.TrimSpace(cfg.Temperature); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				temp = v
			}
		}
		topP := 0.9
		if s := strings.TrimSpace(cfg.TopP); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				topP = v
			}
		}

		systemPrompt := strings.TrimSpace(cfg.SystemPrompt)
		var bboxHint string
		if cfg.BboxScale > 0 {
			bboxHint = fmt.Sprintf(
				"Image coordinates must be normalized to %dx%d space. Ensure bbox coordinates are between 0 and %d.",
				cfg.BboxScale, cfg.BboxScale, cfg.BboxScale,
			)
		}
		switch {
		case systemPrompt != "" && bboxHint != "":
			systemPrompt = systemPrompt + "\n" + bboxHint
		case systemPrompt == "" && bboxHint != "":
			systemPrompt = bboxHint
		}

		logPrompts := func(system, user string) {
			if s := strings.TrimSpace(system); s != "" {
				termcolor.New(termcolor.FgGreen).Printf("system prompt:\n%s\n\n", s)
			}
			termcolor.New(termcolor.FgCyan).Printf("user prompt:\n%s\n\n", user)
		}

		// Resolve effective input/output and stream from flags vs config
		effInput := strings.TrimSpace(inputDir)
		effOutput := strings.TrimSpace(outputDir)
		if !cmd.Flags().Changed("input") && strings.TrimSpace(cfg.Input) != "" {
			effInput = strings.TrimSpace(cfg.Input)
		}
		if !cmd.Flags().Changed("output") && strings.TrimSpace(cfg.Output) != "" {
			effOutput = strings.TrimSpace(cfg.Output)
		}
		if !cmd.Flags().Changed("stream") {
			stream = cfg.Stream

		}
		// Initialize provider via factory
		p, err := providers.New(provider, providers.ProviderConfig{
			APIKey:   cfg.APIKey,
			BaseURL:  cfg.BaseURL,
			AuthType: cfg.AuthType,
		})
		if err != nil {
			return err
		}

		// Batch image mode if an input directory is provided
		if effInput != "" {
			prompt := strings.TrimSpace(cfg.Prompt)
			if prompt == "" {
				return fmt.Errorf("config prompt is empty; set 'prompt' in the config file")
			}

			if effOutput == "" {
				effOutput = "outputs"
			}
			if err := os.MkdirAll(effOutput, permDir); err != nil {
				return fmt.Errorf("create output dir: %w", err)
			}
			// also create subdir for annotated bbox outputs
			bboxDir := filepath.Join(effOutput, "bbox")
			if err := os.MkdirAll(bboxDir, permDir); err != nil {
				return fmt.Errorf("create bbox output dir: %w", err)
			}
			// also create subdir for raw json outputs
			jsonDir := filepath.Join(effOutput, "json")
			if err := os.MkdirAll(jsonDir, permDir); err != nil {
				return fmt.Errorf("create json output dir: %w", err)
			}

			entries, err := os.ReadDir(effInput)
			if err != nil {
				return fmt.Errorf("read input dir: %w", err)
			}
			var imgs []string
			for _, de := range entries {
				if de.IsDir() {
					continue
				}
				p := filepath.Join(effInput, de.Name())
				if utils.IsImageFile(p) {
					imgs = append(imgs, p)
				}
			}
			sort.Strings(imgs)
			if len(imgs) == 0 {
				return fmt.Errorf("no images found in %s", effInput)
			}

			ctx := context.Background()
			for _, imgPath := range imgs {
				absImgPath, err := filepath.Abs(imgPath)
				if err != nil {
					return fmt.Errorf("failed to get absolute path for %s: %w", imgPath, err)
				}
				// Use chat message images instead of appending image path to the prompt
				currentPrompt := prompt
				termcolor.New(termcolor.FgCyan).Printf("processing: %s\n", absImgPath)
				// Load image bytes for Ollama chat images
				imgBytes, err := os.ReadFile(absImgPath)
				if err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "skip %s: read image: %v\n", absImgPath, err)

					continue
				}

				// Determine output format from config schema (if provided), else JSON mode
				format := json.RawMessage(`"json"`)
				if s := strings.TrimSpace(cfg.Schema); s != "" {
					if json.Valid([]byte(s)) {
						format = json.RawMessage(s)
					} else {
						termcolor.New(termcolor.FgYellow).Fprintln(
							os.Stderr,
							"warn: invalid schema in conf.yaml; falling back to JSON mode",
						)
					}
				}

				// Build chat options using functional options
				var sb strings.Builder
				opts := providers.NewChatOptions(
					model, currentPrompt,
					providers.WithStream(stream),
					providers.WithTemperature(temp),
					providers.WithTopP(topP),
					providers.WithImages(imgBytes),
					providers.WithFormat(format),
					providers.WithNoResponseFormat(cfg.NoResponseFormat),
					providers.WithSystemPrompt(systemPrompt),
					providers.WithOnDelta(func(content, thinking string) error {
						if stream && thinking != "" {
							termcolor.New(termcolor.FgHiWhite).Printf("%s", thinking)
						} else if content != "" {
							termcolor.New(termcolor.FgHiWhite).Printf("%s", content)
							sb.WriteString(content)
						}
						return nil
					}),
				)

				logPrompts(systemPrompt, currentPrompt)
				if err := p.Chat(ctx, opts); err != nil {
					termcolor.New(termcolor.FgRed).Fprintf(os.Stderr, "error generating for %s: %v\n", imgPath, err)

					continue
				}

				out := cleanLLMOutput(sb.String())

				// Attempt to repair invalid JSON (LLM outputs may be malformed)
				if repaired, err := jsonrepair.JSONRepair(out); err == nil && strings.TrimSpace(repaired) != "" {
					out = repaired
				} else if err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "warn %s: jsonrepair failed: %v\n", imgPath, err)
				}

				// Compact JSON output before saving, but keep original if parsing fails.
				var rawJSON any
				if err := json.Unmarshal([]byte(out), &rawJSON); err == nil {
					if compact, err := json.Marshal(rawJSON); err == nil {
						out = string(compact)
					}
				}

				base := filepath.Base(imgPath)
				ext := filepath.Ext(base)
				name := strings.TrimSuffix(base, ext)

				// Save raw JSON output alongside results
				jsonPath := filepath.Join(jsonDir, name+".json")
				if err := os.WriteFile(jsonPath, []byte(out), permFile); err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "warn %s: write json: %v\n", base, err)
				}

				// Load and prepare image for drawing
				imgFile, err := os.Open(imgPath)
				if err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "skip %s: open image: %v\n", base, err)

					continue
				}
				img, _, err := image.Decode(imgFile)
				_ = imgFile.Close()
				if err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "skip %s: decode image: %v\n", base, err)

					continue
				}
				bounds := img.Bounds()
				dst := image.NewRGBA(bounds)
				imagedraw.Draw(dst, bounds, img, bounds.Min, imagedraw.Src)

				// Parse detections JSON
				var dets []struct {
					Label string    `json:"label"`
					BBox  []float64 `json:"bbox"`
				}
				termcolor.New(termcolor.FgHiGreen).Printf("\nassistant response: %s\n", out)
				if err := json.Unmarshal([]byte(out), &dets); err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "skip %s: parse json: %v\n", base, err)

					continue
				}

				for _, d := range dets {
					label := d.Label
					x1, y1, x2, y2 := 0, 0, 0, 0
					if cfg.BboxScale > 0 {
						// Expect normalized bbox [x1, y1, x2, y2] in 0..bboxScale (floats or ints)
						if len(d.BBox) >= bboxMinLen {
							x1, y1, x2, y2 = utils.DenormalizeBbox(
								strconv.FormatFloat(d.BBox[0], 'f', -1, 64),
								strconv.FormatFloat(d.BBox[1], 'f', -1, 64),
								strconv.FormatFloat(d.BBox[2], 'f', -1, 64),
								strconv.FormatFloat(d.BBox[3], 'f', -1, 64),
								bounds.Dx(), bounds.Dy(),
								cfg.BboxScale,
							)
						}
					} else {
						// Expect absolute pixel bbox as floats/ints [x1, y1, x2, y2]
						if len(d.BBox) >= bboxMinLen {
							dx1 := decimal.NewFromFloat(d.BBox[0])
							dy1 := decimal.NewFromFloat(d.BBox[1])
							dx2 := decimal.NewFromFloat(d.BBox[2])
							dy2 := decimal.NewFromFloat(d.BBox[3])
							x1 = int(dx1.IntPart())
							y1 = int(dy1.IntPart())
							x2 = int(dx2.IntPart())
							y2 = int(dy2.IntPart())
						}
					}

					// normalize and clamp
					if x1 > x2 {
						x1, x2 = x2, x1
					}
					if y1 > y2 {
						y1, y2 = y2, y1
					}
					x1 = utils.Clamp(x1, bounds.Min.X, bounds.Max.X-1)
					x2 = utils.Clamp(x2, bounds.Min.X, bounds.Max.X-1)
					y1 = utils.Clamp(y1, bounds.Min.Y, bounds.Max.Y-1)
					y2 = utils.Clamp(y2, bounds.Min.Y, bounds.Max.Y-1)

					col := colorForLabel(label, cfg.Classes)
					utils.DrawRect(dst, x1, y1, x2, y2, col, rectThickness)
					// draw label text on a colored background near the top-left corner of the box
					bg := color.RGBA{R: col.R, G: col.G, B: col.B, A: bgAlpha}
					utils.DrawLabel(dst, x1, y1, label, color.RGBA{255, 255, 255, 255}, bg)
				}

				// Save annotated image to outputs/bbox with same base name & extension
				outImgPath := filepath.Join(bboxDir, base)
				outFile, err := os.Create(outImgPath)
				if err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "skip %s: create out: %v\n", base, err)

					continue
				}
				switch strings.ToLower(ext) {
				case ".jpg", ".jpeg":
					_ = jpeg.Encode(outFile, dst, &jpeg.Options{Quality: jpegQuality})
				case ".png":
					_ = png.Encode(outFile, dst)
				default:
					// Fallback to PNG if format not directly supported
					_ = outFile.Close()
					outImgPath = filepath.Join(bboxDir, name+".png")
					outFile, err = os.Create(outImgPath)
					if err == nil {
						_ = png.Encode(outFile, dst)
					}
				}
				_ = outFile.Close()
				termcolor.New(termcolor.FgGreen).Printf("saved %s\n\n", outImgPath)
			}
			return nil
		}

		// Fallback to original text prompt mode (no input directory)
		prompt, err := buildPrompt(args)
		if err != nil {
			return err
		}

		// Determine output format from config schema (if provided), else JSON mode
		format := json.RawMessage(`"json"`)
		if s := strings.TrimSpace(cfg.Schema); s != "" {
			if json.Valid([]byte(s)) {
				format = json.RawMessage(s)
			} else {
				termcolor.New(termcolor.FgYellow).Fprintln(
					os.Stderr,
					"warn: invalid schema in conf.yaml; falling back to JSON mode",
				)
			}
		}

		ctx := context.Background()
		if stream {
			// Build chat options with streaming callback via functional option
			opts := providers.NewChatOptions(
				model, prompt,
				providers.WithStream(true),
				providers.WithTemperature(temp),
				providers.WithTopP(topP),
				providers.WithFormat(format),
				providers.WithNoResponseFormat(cfg.NoResponseFormat),
				providers.WithSystemPrompt(systemPrompt),
				providers.WithOnDelta(func(content, thinking string) error {
					if content != "" {
						fmt.Print(content)
					}
					return nil
				}),
			)
			logPrompts(systemPrompt, prompt)
			if err := p.Chat(ctx, opts); err != nil {
				return err
			}
			fmt.Println()
			return nil
		}

		// Non-streaming: print the final content with newline
		opts := providers.NewChatOptions(
			model, prompt,
			providers.WithStream(false),
			providers.WithTemperature(temp),
			providers.WithTopP(topP),
			providers.WithFormat(format),
			providers.WithNoResponseFormat(cfg.NoResponseFormat),
			providers.WithSystemPrompt(systemPrompt),
			providers.WithOnDelta(func(content, thinking string) error {
				if content != "" {
					fmt.Println(content)
				}
				return nil
			}),
		)
		logPrompts(systemPrompt, prompt)
		if err := p.Chat(ctx, opts); err != nil {
			return err
		}
		return nil
	},
}

func attachRunFlags() {
	runCmd.Flags().BoolVar(&stream, "stream", true, "stream responses (ollama)")
	runCmd.Flags().StringVarP(&inputDir, "input", "i", "", "input folder containing images")
	runCmd.Flags().StringVarP(&outputDir, "output", "o", "", "output folder to save results")
}

func buildPrompt(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	// if no args, read from stdin (allow piping)
	fi, err := os.Stdin.Stat()
	if err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		b, _ := io.ReadAll(os.Stdin)

		s := strings.TrimSpace(string(b))
		if s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("no prompt provided: pass as arguments or pipe via stdin")
}

// cleanLLMOutput strips provider-specific wrappers (think tags and code fences) before JSON parsing.
func cleanLLMOutput(s string) string {
	cleaned := thinkTagRegexp.ReplaceAllString(s, "")
	cleaned = markdownFenceRegex.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}
