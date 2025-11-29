# gen

A minimal CLI for generating and editing images using [FAL AI](https://fal.ai).

## Install

```bash
go install github.com/cozy-creator/gen@latest
```

If `gen` is not found after install, add Go's bin directory to your PATH:

```bash
echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.bashrc
source ~/.bashrc
```

## Setup

Get your API key from [fal.ai](https://fal.ai) and configure it:

```bash
mkdir -p ~/.gen-cli
echo "FAL_KEY=your_api_key_here" > ~/.gen-cli/.env
```

Or set it as an environment variable:

```bash
export FAL_KEY=your_api_key_here
```

## Usage

```bash
# Generate an image
gen "a cat in space"

# Use a specific model
gen "cyberpunk city" -m flux2-pro

# Edit an image (auto-detected via -i flag)
gen "add sunglasses" -i photo.png

# Combine multiple images (FLUX models)
gen "@image1 in the style of @image2" -i content.png -i style.png -m flux2

# Specify output path
gen "a mountain landscape" -o landscape.png

# List available models
gen models
```

## File Locations

```
~/.gen-cli/
├── .env          # FAL_KEY=your_api_key
└── output/       # Generated images (default output)
```

## Models

| Model | Edit Support |
|-------|--------------|
| z-turbo (default) | no |
| qwen | yes |
| flux2-pro (alias: flux2) | yes |
| flux2-flex | yes |
| nano-banana | yes |
| nano-banana-pro | yes |

## Flags

- `-m, --model` - Model to use (default: z-turbo)
- `-i, --image` - Input image(s) for editing (can specify multiple)
- `-s, --size` - Aspect ratio: 16:9, 4:3, 1:1, 3:4, 9:16 (default: 4:3 for gen, auto for edit)
- `-f, --format` - Output format: png, jpeg (default: png)
- `-o, --output` - Output file path
- `--seed` - Seed for reproducibility
