package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/disintegration/imaging"
	"github.com/muesli/termenv"
	_ "golang.org/x/image/webp"
)

// Cache directory for downloaded images
var cacheDir = getCacheDir()

func getCacheDir() string {
	homeDir, _ := os.UserHomeDir()
	cache := filepath.Join(homeDir, ".anilist_cli_cache")
	os.MkdirAll(cache, 0755)
	return cache
}

var imageHTTPClient = &http.Client{Timeout: 20 * time.Second}

// posterStore keeps decoded posters and finished renders in memory so moving
// the cursor never touches the disk or network on the UI goroutine.
type posterStore struct {
	mu       sync.Mutex
	decoded  map[string]image.Image
	rendered map[string]string
	inflight map[string]bool
	failed   map[string]time.Time
}

// posterRetryAfter is how long a failed poster waits before being retried
const posterRetryAfter = 30 * time.Second

// maxRenderedPosters bounds the render cache (window resizes add new sizes)
const maxRenderedPosters = 256

func (p *posterStore) recentlyFailed(url string) bool {
	at, ok := p.failed[url]
	return ok && time.Since(at) < posterRetryAfter
}

var posters = &posterStore{
	decoded:  make(map[string]image.Image),
	rendered: make(map[string]string),
	inflight: make(map[string]bool),
	failed:   make(map[string]time.Time),
}

// posterLoadedMsg is sent once a poster has been downloaded and decoded
type posterLoadedMsg struct {
	url string
	err error
}

// cachePathFor maps a URL to a stable, filesystem-safe cache file name
func cachePathFor(url string) string {
	sum := sha1.Sum([]byte(url))
	ext := strings.ToLower(filepath.Ext(url))
	if len(ext) > 5 || strings.ContainsAny(ext, "?&=/") {
		ext = ""
	}
	return filepath.Join(cacheDir, hex.EncodeToString(sum[:])+ext)
}

// Download and cache image
func downloadImage(url string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("empty URL")
	}

	cachePath := cachePathFor(url)
	if st, err := os.Stat(cachePath); err == nil && st.Size() > 0 {
		return cachePath, nil
	}

	resp, err := imageHTTPClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching %s: %s", url, resp.Status)
	}

	// Write to a temp file first so an interrupted download never leaves a
	// truncated image in the cache
	tmp, err := os.CreateTemp(cacheDir, "dl-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), cachePath); err != nil {
		return "", err
	}
	return cachePath, nil
}

func decodeImageFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// loadPoster downloads and decodes a poster in the background
func loadPoster(url string) tea.Cmd {
	posters.mu.Lock()
	if url == "" || posters.inflight[url] || posters.recentlyFailed(url) || posters.decoded[url] != nil {
		posters.mu.Unlock()
		return nil
	}
	posters.inflight[url] = true
	posters.mu.Unlock()

	return func() tea.Msg {
		path, err := downloadImage(url)
		var img image.Image
		if err == nil {
			img, err = decodeImageFile(path)
			if err != nil {
				// Corrupt cache entry, make sure the next run re-downloads it
				os.Remove(path)
			}
		}

		posters.mu.Lock()
		delete(posters.inflight, url)
		if err != nil {
			posters.failed[url] = time.Now()
		} else {
			delete(posters.failed, url)
			posters.decoded[url] = img
		}
		posters.mu.Unlock()
		return posterLoadedMsg{url: url, err: err}
	}
}

// posterCmds loads the given posters (skipping ones already loaded)
func posterCmds(urls ...string) tea.Cmd {
	var cmds []tea.Cmd
	for _, u := range urls {
		if cmd := loadPoster(u); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// fitCells returns the largest cell size (width, rows) an image with the given
// pixel dimensions can be drawn at inside maxWidth x maxRows while keeping its
// aspect ratio. Each cell shows two vertically stacked pixels via a half block,
// and a terminal cell is about twice as tall as it is wide, so one "pixel" is
// roughly square.
func fitCells(imgW, imgH, maxWidth, maxRows int) (int, int) {
	if imgW <= 0 || imgH <= 0 || maxWidth <= 0 || maxRows <= 0 {
		return 0, 0
	}
	aspect := float64(imgW) / float64(imgH)

	w := maxWidth
	rows := int(float64(w)/aspect/2 + 0.5)
	if rows > maxRows {
		rows = maxRows
		w = int(float64(rows*2)*aspect + 0.5)
	}
	return max(1, min(w, maxWidth)), max(1, rows)
}

// posterAspect is the usual AniList cover ratio, used to size placeholders
const posterAspectW, posterAspectH = 460, 650

// getAnimePoster returns the rendered poster if it is ready, or a placeholder of
// the same size while it loads. It never blocks on I/O.
func getAnimePoster(coverURL string, maxWidth, maxRows int) string {
	if coverURL == "" {
		w, h := fitCells(posterAspectW, posterAspectH, maxWidth, maxRows)
		return generatePlaceholder(w, h, "NO IMAGE")
	}

	key := coverURL + "|" + strconv.Itoa(maxWidth) + "x" + strconv.Itoa(maxRows)

	posters.mu.Lock()
	if s, ok := posters.rendered[key]; ok {
		posters.mu.Unlock()
		return s
	}
	img := posters.decoded[coverURL]
	failed := posters.recentlyFailed(coverURL)
	posters.mu.Unlock()

	if img == nil {
		w, h := fitCells(posterAspectW, posterAspectH, maxWidth, maxRows)
		label := "LOADING…"
		if failed {
			label = "NO IMAGE"
		}
		return generatePlaceholder(w, h, label)
	}

	s := renderImage(img, maxWidth, maxRows)

	posters.mu.Lock()
	if len(posters.rendered) >= maxRenderedPosters {
		posters.rendered = make(map[string]string)
	}
	posters.rendered[key] = s
	posters.mu.Unlock()
	return s
}

// renderImage scales img to fit in maxWidth x maxRows cells and renders it
// with half blocks
func renderImage(img image.Image, maxWidth, maxRows int) string {
	b := img.Bounds()
	w, rows := fitCells(b.Dx(), b.Dy(), maxWidth, maxRows)
	if w == 0 {
		return ""
	}
	// Lanczos gives the crispest result when shrinking large covers
	resized := imaging.Resize(img, w, rows*2, imaging.Lanczos)
	return imageToString(resized, colorProfile())
}

var (
	profileOnce sync.Once
	profile     termenv.Profile
)

// colorProfile picks the richest color mode the terminal supports. Anything we
// can't detect (e.g. output isn't a TTY yet) gets truecolor, which nearly every
// modern terminal handles.
func colorProfile() termenv.Profile {
	profileOnce.Do(func() {
		profile = lipgloss.ColorProfile()
		if profile == termenv.Ascii {
			profile = termenv.TrueColor
		}
		switch strings.ToLower(os.Getenv("COLORTERM")) {
		case "truecolor", "24bit":
			profile = termenv.TrueColor
		}
	})
	return profile
}

// imageToString converts an image to a string using upper half blocks: the
// foreground paints the top pixel and the background the bottom one. Escape
// sequences are written directly (rather than via a style per cell) and only
// when the color actually changes, which keeps the output small and fast.
func imageToString(img *image.NRGBA, p termenv.Profile) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	var sb strings.Builder
	sb.Grow(w * (h / 2) * 20)

	for y := 0; y < h; y += 2 {
		lastFg, lastBg := "", ""
		for x := 0; x < w; x++ {
			top := pixelHex(img, x, y)
			bottom := top
			if y+1 < h {
				bottom = pixelHex(img, x, y+1)
			}

			fg := p.Color(top)
			bg := p.Color(bottom)
			if fgSeq := fg.Sequence(false); fgSeq != lastFg {
				sb.WriteString("\x1b[" + fgSeq + "m")
				lastFg = fgSeq
			}
			if bgSeq := bg.Sequence(true); bgSeq != lastBg {
				sb.WriteString("\x1b[" + bgSeq + "m")
				lastBg = bgSeq
			}
			sb.WriteString("▀")
		}
		// Reset at the end of every line so colors never bleed into the
		// separator or following text
		sb.WriteString("\x1b[0m")
		if y+2 < h {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// pixelHex returns the pixel as #rrggbb, blending any transparency onto black
func pixelHex(img *image.NRGBA, x, y int) string {
	i := img.PixOffset(x+img.Rect.Min.X, y+img.Rect.Min.Y)
	r, g, b, a := uint32(img.Pix[i]), uint32(img.Pix[i+1]), uint32(img.Pix[i+2]), uint32(img.Pix[i+3])
	if a != 255 {
		r, g, b = r*a/255, g*a/255, b*a/255
	}
	const digits = "0123456789abcdef"
	return string([]byte{'#',
		digits[r>>4], digits[r&15],
		digits[g>>4], digits[g&15],
		digits[b>>4], digits[b&15],
	})
}

// renderImageToTerminal loads and renders an image from a file path
func renderImageToTerminal(imagePath string, maxWidth, maxHeight int) (string, error) {
	img, err := decodeImageFile(imagePath)
	if err != nil {
		return "", err
	}
	return renderImage(img, maxWidth, maxHeight), nil
}

// generatePlaceholder draws a width x height box (including the border) with a
// centered label
func generatePlaceholder(width, height int, label string) string {
	if width < 2 || height < 2 {
		return ""
	}
	inner := width - 2
	var sb strings.Builder

	border := strings.Repeat("─", inner)
	sb.WriteString("┌" + border + "┐\n")

	labelWidth := lipgloss.Width(label)
	for i := 0; i < height-2; i++ {
		if i == (height-2)/2 && labelWidth <= inner {
			left := (inner - labelWidth) / 2
			right := inner - left - labelWidth
			sb.WriteString("│" + strings.Repeat(" ", left) + label + strings.Repeat(" ", right) + "│\n")
		} else {
			sb.WriteString("│" + strings.Repeat(" ", inner) + "│\n")
		}
	}

	sb.WriteString("└" + border + "┘")
	return sb.String()
}

// clearImageCache removes all cached images
func clearImageCache() error {
	posters.mu.Lock()
	posters.decoded = make(map[string]image.Image)
	posters.rendered = make(map[string]string)
	posters.failed = make(map[string]time.Time)
	posters.mu.Unlock()
	return os.RemoveAll(cacheDir)
}
