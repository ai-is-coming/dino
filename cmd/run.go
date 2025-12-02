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
	"sort"
	"strconv"

	"strings"

	"dino/internal/conf"
	"dino/internal/utils"

	termcolor "github.com/fatih/color"
	"github.com/kaptinlin/jsonrepair"
	"github.com/ollama/ollama/api"
	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"
)

var (
	stream    bool
	inputDir  string
	outputDir string
)

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
		if provider != "ollama" {
			return fmt.Errorf("unsupported provider: %s", provider)
		}

		termcolor.New(termcolor.FgGreen).Printf("provider: %s, model: %s, stream: %t, temperature: %s, top_p: %s\n", provider, model, stream, cfg.Temperature, cfg.TopP)

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

		// Batch image mode if an input directory is provided
		if effInput != "" {
			prompt := strings.TrimSpace(cfg.Prompt)
			if prompt == "" {
				return fmt.Errorf("config prompt is empty; set 'prompt' in the config file")
			}

			if effOutput == "" {
				effOutput = "outputs"
			}
			if err := os.MkdirAll(effOutput, 0o755); err != nil {
				return fmt.Errorf("create output dir: %w", err)
			}
			// also create subdir for annotated bbox outputs
			bboxDir := filepath.Join(effOutput, "bbox")
			if err := os.MkdirAll(bboxDir, 0o755); err != nil {
				return fmt.Errorf("create bbox output dir: %w", err)
			}
			// also create subdir for raw json outputs
			jsonDir := filepath.Join(effOutput, "json")
			if err := os.MkdirAll(jsonDir, 0o755); err != nil {
				return fmt.Errorf("create json output dir: %w", err)
			}

			client, err := api.ClientFromEnvironment()
			if err != nil {
				return err
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
				termcolor.New(termcolor.FgCyan).Printf("processing: %s\nprompt: \n%s\n\n", absImgPath, currentPrompt)
				// Load image bytes for Ollama chat images
				imgBytes, err := os.ReadFile(absImgPath)
				if err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "skip %s: read image: %v\n", absImgPath, err)
					continue
				}

				req := &api.ChatRequest{
					Model: model,
					Messages: []api.Message{
						{Role: "user", Content: currentPrompt, Images: []api.ImageData{api.ImageData(imgBytes)}},
					},
					Options: map[string]any{
						"temperature": temp,
						"top_p":       topP,
					},
				}
				if !stream {
					req.Stream = new(bool)
				}
				var sb strings.Builder
				respFunc := func(resp api.ChatResponse) error {
					sb.WriteString(resp.Message.Content)
					if stream && resp.Message.Thinking != "" {
						termcolor.New(termcolor.FgHiWhite).Printf("%s", resp.Message.Thinking)
					} else if resp.Message.Content != "" {
						termcolor.New(termcolor.FgHiWhite).Printf("%s", resp.Message.Content)
					}
					return nil
				}
				if err := client.Chat(ctx, req, respFunc); err != nil {
					termcolor.New(termcolor.FgRed).Fprintf(os.Stderr, "error generating for %s: %v\n", imgPath, err)
					continue
				}

				out := strings.TrimSpace(sb.String())

				// Attempt to repair invalid JSON (LLM outputs may be malformed)
				if repaired, err := jsonrepair.JSONRepair(out); err == nil && strings.TrimSpace(repaired) != "" {
					out = repaired
				} else if err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "warn %s: jsonrepair failed: %v\n", imgPath, err)
				}

				base := filepath.Base(imgPath)
				ext := filepath.Ext(base)
				name := strings.TrimSuffix(base, ext)

				// Save raw JSON output alongside results
				jsonPath := filepath.Join(jsonDir, name+".json")
				if err := os.WriteFile(jsonPath, []byte(out), 0o644); err != nil {
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
					Label string   `json:"label"`
					BBox  []string `json:"bbox"`
				}
				termcolor.New(termcolor.FgHiGreen).Printf("\nassistant response: %s\n", out)
				if err := json.Unmarshal([]byte(out), &dets); err != nil {
					termcolor.New(termcolor.FgYellow).Fprintf(os.Stderr, "skip %s: parse json: %v\n", base, err)
					continue
				}

				isQwen3VL := strings.HasPrefix(strings.ToLower(strings.Split(model, ":")[0]), "qwen3-vl")

				for _, d := range dets {
					label := d.Label
					x1, y1, x2, y2 := 0, 0, 0, 0
					if isQwen3VL {
						// Expect normalized bbox [x1, y1, x2, y2] as string array in 0..999
						if len(d.BBox) >= 4 {
							x1, y1, x2, y2 = utils.DenormalizeBbox999(d.BBox[0], d.BBox[1], d.BBox[2], d.BBox[3], bounds.Dx(), bounds.Dy())
						}
					} else {
						// Expect absolute pixel bbox as string array [x1, y1, x2, y2]
						if len(d.BBox) >= 4 {
							dx1, _ := decimal.NewFromString(d.BBox[0])
							dy1, _ := decimal.NewFromString(d.BBox[1])
							dx2, _ := decimal.NewFromString(d.BBox[2])
							dy2, _ := decimal.NewFromString(d.BBox[3])
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

					col := color.RGBA{0, 255, 0, 255} // green default
					if strings.ToLower(label) == "climb" {
						col = color.RGBA{255, 0, 0, 255} // red for climb
					}
					utils.DrawRect(dst, x1, y1, x2, y2, col, 3)
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
					_ = jpeg.Encode(outFile, dst, &jpeg.Options{Quality: 90})
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

		// Print the prompt that will be sent to the model
		termcolor.New(termcolor.FgCyan).Fprintln(os.Stderr, "prompt:")
		fmt.Fprintln(os.Stderr, prompt)

		client, err := api.ClientFromEnvironment()
		if err != nil {
			return err
		}

		req := &api.ChatRequest{
			Model: model,
			Messages: []api.Message{
				{Role: "user", Content: prompt},
			},
			Options: map[string]any{
				"temperature": temp,
				"top_p":       topP,
			},
		}
		if !stream {
			req.Stream = new(bool)
		}

		ctx := context.Background()
		if stream {
			respFunc := func(resp api.ChatResponse) error {
				fmt.Print(resp.Message.Content)
				return nil
			}
			if err := client.Chat(ctx, req, respFunc); err != nil {
				return err
			}
			fmt.Println()
			return nil
		}

		respFunc := func(resp api.ChatResponse) error {
			fmt.Println(resp.Message.Content)
			return nil
		}
		if err := client.Chat(ctx, req, respFunc); err != nil {
			return err
		}
		return nil
	},
}

func init() {
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
