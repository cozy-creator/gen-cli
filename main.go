package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	_ "golang.org/x/image/webp"
)

const falBaseURL = "https://fal.run"

// Models maps short names to their generation and edit paths
var models = map[string]struct {
	GenPath             string
	EditPath            string
	SupportsAutoImgSize bool   // Whether the model supports "auto" image_size
	SizeParamName       string // "image_size" or "aspect_ratio"
}{
	"z-turbo":         {"fal-ai/z-image/turbo", "", false, "image_size"},
	"qwen":            {"fal-ai/qwen-image", "fal-ai/qwen-image-edit-plus", false, "image_size"},
	"flux2-pro":       {"fal-ai/flux-2-pro", "fal-ai/flux-2-pro/edit", true, "image_size"},
	"flux2-flex":      {"fal-ai/flux-2-flex", "fal-ai/flux-2-flex/edit", true, "image_size"},
	"nano-banana":     {"fal-ai/nano-banana", "fal-ai/nano-banana/edit", true, "aspect_ratio"},
	"nano-banana-pro": {"fal-ai/nano-banana-pro", "fal-ai/nano-banana-pro/edit", true, "aspect_ratio"},
}

// Model aliases
var modelAliases = map[string]string{
	"flux2": "flux2-pro",
}

type ImageSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ImageRequest struct {
	Prompt              string      `json:"prompt"`
	ImageSize           interface{} `json:"image_size,omitempty"`   // string or ImageSize struct
	AspectRatio         string      `json:"aspect_ratio,omitempty"` // for nano-banana models
	OutputFormat        string      `json:"output_format,omitempty"`
	ImageURLs           []string    `json:"image_urls,omitempty"`
	Seed                *int        `json:"seed,omitempty"`
	EnableSafetyChecker bool        `json:"enable_safety_checker"`
}

type ImageOutput struct {
	URL         string `json:"url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"content_type"`
}

type ImageResponse struct {
	Images []ImageOutput `json:"images"`
	Seed   int           `json:"seed"`
}

var (
	model       string
	size        string
	format      string
	output      string
	seed        int
	inputImages []string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "gen [prompt]",
		Short: "Image Generator CLI",
		Long: `Generate and edit images using FAL AI models.

Requires FAL_KEY (checked in order: env var, ./.env, ~/.gen-cli/.env).
Images are saved to ~/.gen-cli/output/ by default.

If -i/--image flags are provided, automatically uses edit mode.
Otherwise, generates a new image from the prompt.

For FLUX models, reference multiple images using @image1, @image2, etc:
  - "@image1 wearing the outfit from @image2"
  - "combine the style of @image1 with @image2"

For flux2-flex, you can also use HEX color codes:
  - "a wall painted in color #2ECC71"
  - "the car in color #1A1A1A with accents in #FFD700"

Limits: flux2-pro supports up to 9 images (9MP total),
        flux2-flex supports up to 10 images (14MP total),
        nano-banana-pro supports up to 14 images.`,
		Args:                  cobra.MaximumNArgs(1),
		DisableAutoGenTag:     true,
		CompletionOptions:     cobra.CompletionOptions{DisableDefaultCmd: true},
		Example: `  gen "a cat in space"
  gen "cyberpunk city" -m flux2-pro -s 16:9
  gen "add sunglasses" -i photo.png
  gen "@image1 in the style of @image2" -i content.png -i style.png -m flux2-pro`,
		Run: runGenerate,
	}

	rootCmd.Flags().StringVarP(&model, "model", "m", "z-turbo", "Model to use")
	rootCmd.Flags().StringArrayVarP(&inputImages, "image", "i", nil, "Input image(s) for editing")
	rootCmd.Flags().StringVarP(&size, "size", "s", "", "Aspect ratio: 16:9, 4:3, 1:1, 3:4, 9:16 (default: 4:3 for gen, auto for edit)")
	rootCmd.Flags().StringVarP(&format, "format", "f", "png", "Output format (png, jpeg)")
	rootCmd.Flags().StringVarP(&output, "output", "o", "", "Output file path")
	rootCmd.Flags().IntVar(&seed, "seed", -1, "Seed for reproducibility")

	// Models subcommand
	modelsCmd := &cobra.Command{
		Use:     "models",
		Aliases: []string{"ls", "list"},
		Short:   "List available models",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Available Models:")
			fmt.Println()
			for name, info := range models {
				editSupport := "no edit"
				if info.EditPath != "" {
					editSupport = "supports edit"
				}
				// Check for aliases
				var aliases []string
				for alias, target := range modelAliases {
					if target == name {
						aliases = append(aliases, alias)
					}
				}
				aliasStr := ""
				if len(aliases) > 0 {
					aliasStr = fmt.Sprintf(" (alias: %s)", strings.Join(aliases, ", "))
				}
				fmt.Printf("  %-17s  %s%s\n", name, editSupport, aliasStr)
			}
			fmt.Println()
			fmt.Println("Use -i flag to enable edit mode (e.g., gen \"prompt\" -i image.png)")
		},
	}

	rootCmd.AddCommand(modelsCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getGenCLIDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".gen-cli")
	// Auto-create the directory if it doesn't exist
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func getAPIKey() string {
	// Check environment variable first
	if apiKey := os.Getenv("FAL_KEY"); apiKey != "" {
		return apiKey
	}

	// Try loading from .env in current directory
	_ = godotenv.Load()
	if apiKey := os.Getenv("FAL_KEY"); apiKey != "" {
		return apiKey
	}

	// Try loading from ~/.gen-cli/.env
	if genDir := getGenCLIDir(); genDir != "" {
		envPath := filepath.Join(genDir, ".env")
		_ = godotenv.Load(envPath)
		if apiKey := os.Getenv("FAL_KEY"); apiKey != "" {
			return apiKey
		}
	}

	fmt.Fprintln(os.Stderr, "Error: FAL_KEY not found")
	fmt.Fprintln(os.Stderr, "Set FAL_KEY environment variable or create ~/.gen-cli/.env")
	os.Exit(1)
	return ""
}

func getDefaultOutputPath(format string) string {
	genDir := getGenCLIDir()
	if genDir == "" {
		return fmt.Sprintf("generated_%d.%s", time.Now().Unix(), format)
	}

	outputDir := filepath.Join(genDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Sprintf("generated_%d.%s", time.Now().Unix(), format)
	}

	return filepath.Join(outputDir, fmt.Sprintf("generated_%d.%s", time.Now().Unix(), format))
}

func resolveModel(name string) string {
	if alias, ok := modelAliases[name]; ok {
		return alias
	}
	return name
}

func runGenerate(cmd *cobra.Command, args []string) {
	// If no prompt provided, show help
	if len(args) == 0 {
		cmd.Help()
		return
	}

	prompt := args[0]
	apiKey := getAPIKey()

	resolvedModel := resolveModel(model)
	info, ok := models[resolvedModel]
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: unknown model '%s'. Use 'gen models' to see available options.\n", model)
		os.Exit(1)
	}

	isEditMode := len(inputImages) > 0

	// Determine model path
	var modelPath string
	if isEditMode {
		if info.EditPath == "" {
			fmt.Fprintf(os.Stderr, "Error: model '%s' does not support editing.\n", model)
			os.Exit(1)
		}
		modelPath = info.EditPath
	} else {
		modelPath = info.GenPath
	}

	// Determine image size/aspect ratio
	var sizeValue string
	if size != "" && size != "auto" {
		sizeValue = size
	} else if isEditMode && info.SupportsAutoImgSize {
		sizeValue = "auto"
	} else if isEditMode && len(inputImages) > 0 {
		// Get dimensions from first input image and find closest preset
		width, height, err := getImageDimensions(inputImages[0])
		if err == nil {
			ratio := getClosestRatio(width, height)
			sizeValue = ratio
			fmt.Printf("Input image: %dx%d -> using %s\n", width, height, ratio)
		}
	} else if !isEditMode {
		sizeValue = "4:3"
	}

	// Build request
	req := ImageRequest{
		Prompt:       prompt,
		OutputFormat: format,
	}

	// Set the appropriate size parameter based on model
	if info.SizeParamName == "aspect_ratio" {
		// nano-banana models use aspect_ratio with ratio strings directly
		if sizeValue != "" {
			req.AspectRatio = sizeValue
		}
	} else {
		// Other models use image_size with preset names
		if sizeValue != "" && sizeValue != "auto" {
			req.ImageSize = parseSize(sizeValue)
		} else if sizeValue == "auto" {
			req.ImageSize = "auto"
		}
	}
	if seed >= 0 {
		req.Seed = &seed
	}

	// Handle input images for edit mode
	if isEditMode {
		var imageURLs []string
		for i, imgPath := range inputImages {
			dataURI, err := imageToDataURI(imgPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading image %d (%s): %v\n", i+1, imgPath, err)
				os.Exit(1)
			}
			imageURLs = append(imageURLs, dataURI)
		}
		req.ImageURLs = imageURLs
		fmt.Printf("Edit mode: %d input image(s)\n", len(imageURLs))
	}

	fmt.Printf("Using model: %s\n", modelPath)
	if sizeValue != "" {
		fmt.Printf("Requested size: %s\n", sizeValue)
	}

	startTime := time.Now()
	response, err := callFALAPI(apiKey, modelPath, req)
	elapsed := time.Since(startTime)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(response.Images) == 0 {
		fmt.Fprintln(os.Stderr, "Error: No images returned")
		os.Exit(1)
	}

	outPath := output
	if outPath == "" {
		outPath = getDefaultOutputPath(format)
	} else {
		// Check if output is a directory
		if info, err := os.Stat(outPath); err == nil && info.IsDir() {
			outPath = filepath.Join(outPath, fmt.Sprintf("generated_%d.%s", time.Now().Unix(), format))
		}
	}

	fmt.Println("Downloading image...")
	if err := downloadImage(response.Images[0].URL, outPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving image: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Image saved to: %s\n", outPath)
	if response.Images[0].Width > 0 {
		fmt.Printf("Dimensions: %dx%d\n", response.Images[0].Width, response.Images[0].Height)
	}
	fmt.Printf("Seed: %d\n", response.Seed)
	fmt.Printf("Time: %.1fs\n", elapsed.Seconds())
}

func callFALAPI(apiKey, modelPath string, req ImageRequest) (*ImageResponse, error) {
	url := fmt.Sprintf("%s/%s", falBaseURL, modelPath)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Key "+apiKey)

	done := make(chan bool)
	go showProgress(done)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(httpReq)

	done <- true
	fmt.Println()

	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try parsing as detailed error array
		var detailedErr struct {
			Detail []struct {
				Msg  string `json:"msg"`
				Type string `json:"type"`
			} `json:"detail"`
		}
		if json.Unmarshal(body, &detailedErr) == nil && len(detailedErr.Detail) > 0 {
			return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, detailedErr.Detail[0].Msg)
		}

		// Try parsing as simple error
		var simpleErr struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(body, &simpleErr) == nil && simpleErr.Detail != "" {
			return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, simpleErr.Detail)
		}

		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var imgResp ImageResponse
	if err := json.Unmarshal(body, &imgResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &imgResp, nil
}

func showProgress(done chan bool) {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	for {
		select {
		case <-done:
			fmt.Print("\r✓ Complete!          ")
			return
		default:
			fmt.Printf("\r%s Processing...", frames[i%len(frames)])
			i++
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func getImageDimensions(imagePath string) (int, int, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}

	return config.Width, config.Height, nil
}

// Maps ratio strings to API preset names (for image_size parameter)
var ratioToPreset = map[string]string{
	"9:16": "portrait_16_9",
	"3:4":  "portrait_4_3",
	"1:1":  "square_hd",
	"4:3":  "landscape_4_3",
	"16:9": "landscape_16_9",
}

// Ratios supported by aspect_ratio parameter (nano-banana models)
// These use the ratio string directly, no conversion needed
var aspectRatioSupported = map[string]bool{
	"21:9": true,
	"16:9": true,
	"3:2":  true,
	"4:3":  true,
	"5:4":  true,
	"1:1":  true,
	"4:5":  true,
	"3:4":  true,
	"2:3":  true,
	"9:16": true,
	"auto": true,
}

// Maps aspect ratios to preset names (for auto-detection from image dimensions)
var aspectPresets = []struct {
	Name  string
	Ratio float64 // width / height
}{
	{"portrait_16_9", 9.0 / 16.0},  // 0.5625
	{"portrait_4_3", 3.0 / 4.0},    // 0.75
	{"square_hd", 1.0},              // 1.0
	{"landscape_4_3", 4.0 / 3.0},   // 1.333
	{"landscape_16_9", 16.0 / 9.0}, // 1.778
}

// parseSize converts user-friendly size (ratio or preset) to API preset name
func parseSize(s string) string {
	// Check if it's a ratio like "16:9"
	if preset, ok := ratioToPreset[s]; ok {
		return preset
	}
	// Otherwise assume it's already a preset name or "auto"
	return s
}

func getClosestPreset(width, height int) string {
	if width == 0 || height == 0 {
		return "square_hd"
	}

	ratio := float64(width) / float64(height)

	// Find closest match
	closestPreset := "square_hd"
	closestDiff := 999.0

	for _, preset := range aspectPresets {
		diff := abs(ratio - preset.Ratio)
		if diff < closestDiff {
			closestDiff = diff
			closestPreset = preset.Name
		}
	}

	return closestPreset
}

func getClosestRatio(width, height int) string {
	preset := getClosestPreset(width, height)
	for ratio, p := range ratioToPreset {
		if p == preset {
			return ratio
		}
	}
	return "1:1"
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func imageToDataURI(imagePath string) (string, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(imagePath))
	var mimeType string
	switch ext {
	case ".png":
		mimeType = "image/png"
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".webp":
		mimeType = "image/webp"
	case ".gif":
		mimeType = "image/gif"
	default:
		mimeType = "application/octet-stream"
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
}

func downloadImage(url, outputPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}
