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

	termimg "github.com/blacktop/go-termimg"
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

// Render image to terminal with true color
func renderImageToTerminal(imagePath string, width, height int) (string, error) {
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
	
	// Use go-termimg to render the decoded image
	output, err := termimg.Render(img)
	if err != nil {
		return "", err
	}
	
	return output, nil
}

// Get rendered anime poster with true color
func getAnimePoster(coverURL string, width, height int) string {
	if coverURL == "" {
		return generatePlaceholder(width, height)
	}
	
	// Download and cache
	imagePath, err := downloadImage(coverURL)
	if err != nil {
		return generatePlaceholder(width, height)
	}
	
	// Render with true color
	rendered, err := renderImageToTerminal(imagePath, width, height)
	if err != nil {
		return generatePlaceholder(width, height)
	}
	
	return rendered
}

// Generate ASCII placeholder for missing images
func generatePlaceholder(width, height int) string {
	var sb strings.Builder
	border := strings.Repeat("─", width)
	
	sb.WriteString("┌" + border + "┐\n")
	for i := 0; i < height-2; i++ {
		if i == height/2-1 {
			text := "NO IMAGE"
			padding := (width - len(text)) / 2
			sb.WriteString("│" + strings.Repeat(" ", padding) + text + strings.Repeat(" ", width-padding-len(text)) + "│\n")
		} else {
			sb.WriteString("│" + strings.Repeat(" ", width) + "│\n")
		}
	}
	sb.WriteString("└" + border + "┘\n")
	
	return sb.String()
}

// Clear image cache
func clearImageCache() error {
	return os.RemoveAll(cacheDir)
}