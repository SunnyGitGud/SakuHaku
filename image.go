package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/disintegration/imaging"
	"github.com/lucasb-eyer/go-colorful"
)

// Cache directory for downloaded images
var cacheDir = getCacheDir()

func getCacheDir() string {
	homeDir, _ := os.UserHomeDir()
	cache := filepath.Join(homeDir, ".anilist_cli_cache")
	os.MkdirAll(cache, 0755)
	return cache
}

// Download and cache image
func downloadImage(url string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("empty URL")
	}

	// Create cache filename from URL
	filename := strings.ReplaceAll(url, "/", "_")
	filename = strings.ReplaceAll(filename, ":", "_")
	cachePath := filepath.Join(cacheDir, filename)

	// Check if already cached
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	// Download image
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Save to cache
	f, err := os.Create(cachePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return "", err
	}

	return cachePath, nil
}

// imageToString converts an image to a string representation using half-blocks
func imageToString(img image.Image) string {
	b := img.Bounds()
	imageWidth := b.Max.X
	h := b.Max.Y
	str := strings.Builder{}

	// Use half-block characters (▀) to get 2x vertical resolution
	for heightCounter := 0; heightCounter < h; heightCounter += 2 {
		// Render each column of pixels
		for x := 0; x < imageWidth; x++ {
			// Get top pixel color
			c1, _ := colorful.MakeColor(img.At(x, heightCounter))
			color1 := lipgloss.Color(c1.Hex())

			// Get bottom pixel color (or use top if we're at the last row)
			var c2 colorful.Color
			if heightCounter+1 < h {
				c2, _ = colorful.MakeColor(img.At(x, heightCounter+1))
			} else {
				c2 = c1
			}
			color2 := lipgloss.Color(c2.Hex())

			// Render half-block with foreground and background colors
			str.WriteString(lipgloss.NewStyle().
				Foreground(color1).
				Background(color2).
				Render("▀"))
		}

		str.WriteString("\n")
	}

	return str.String()
}

// renderImageToTerminal loads and renders an image from a file path
func renderImageToTerminal(imagePath string, maxWidth, maxHeight int) (string, error) {
	// Open image file
	f, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Decode image
	img, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}

	// Get original dimensions
	bounds := img.Bounds()
	imgWidth := bounds.Dx()
	imgHeight := bounds.Dy()

	aspectRatio := float64(imgWidth) / float64(imgHeight)

	// Calculate target dimensions
	// Each terminal character is roughly 1:2 (width:height)
	// Half-blocks give us double vertical resolution
	targetWidth := maxWidth
	targetHeight := int(float64(targetWidth) / aspectRatio) // Account for character aspect ratio

	// If height exceeds max, scale down
	if targetHeight/2 > maxHeight {
		targetHeight = maxHeight * 2
		targetWidth = int(float64(targetHeight) * aspectRatio)
	}

	// Ensure minimum size for quality
	if targetWidth < 20 {
		targetWidth = 20
	}
	if targetHeight < 20 {
		targetHeight = 20
	}

	// Resize image to exact target dimensions for best quality
	resizedImg := imaging.Resize(img, targetWidth, targetHeight, imaging.Lanczos)

	// Convert to string with colors
	return imageToString(resizedImg), nil
}

// getAnimePoster gets and renders an anime poster with dynamic sizing
func getAnimePoster(coverURL string, maxWidth, maxHeight int) string {
	if coverURL == "" {
		return generatePlaceholder(maxWidth, maxHeight)
	}

	// Download and cache
	imagePath, err := downloadImage(coverURL)
	if err != nil {
		return generatePlaceholder(maxWidth, maxHeight)
	}

	// Render with the specified dimensions
	rendered, err := renderImageToTerminal(imagePath, maxWidth, maxHeight)
	if err != nil {
		return generatePlaceholder(maxWidth, maxHeight)
	}

	return rendered
}

// generatePlaceholder creates a simple placeholder box
func generatePlaceholder(width, height int) string {
	var sb strings.Builder

	border := strings.Repeat("─", width)
	sb.WriteString("┌" + border + "┐\n")

	for i := 0; i < height-2; i++ {
		if i == height/2-1 {
			text := "NO IMAGE"
			padding := (width - len(text)) / 2
			if padding < 0 {
				padding = 0
			}
			spaces := width - padding - len(text)
			if spaces < 0 {
				spaces = 0
			}
			sb.WriteString("│" + strings.Repeat(" ", padding) + text + strings.Repeat(" ", spaces) + "│\n")
		} else {
			sb.WriteString("│" + strings.Repeat(" ", width) + "│\n")
		}
	}

	sb.WriteString("└" + border + "┘\n")
	return sb.String()
}

// clearImageCache removes all cached images
func clearImageCache() error {
	return os.RemoveAll(cacheDir)
}
