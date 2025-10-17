package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type Torrent struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Link       string `json:"link"`
	TorrentURL string `json:"torrent_url"`
	MagnetURI  string `json:"magnet_uri"`
	Seeders    any    `json:"seeders"`
	Leechers   any    `json:"leechers"`
	TotalSize  int64  `json:"total_size"`
	WebsiteURL string `json:"website_url"`
}

func getTorrentList() []Torrent {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter search query: ")
	query, _ := reader.ReadString('\n')
	query = strings.TrimSpace(query)

	apiURL := fmt.Sprintf("https://feed.animetosho.org/json?qx=1&q=%s&page=1", url.QueryEscape(query))
	resp, err := http.Get(apiURL)
	if err != nil {
		fmt.Printf("failed to get resp: %s\n", err)
		return nil
	}
	defer resp.Body.Close()

	var torrents []Torrent
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		fmt.Printf("failed to decode: %v\n", err)
		return nil
	}

	if len(torrents) == 0 {
		fmt.Println("No results found.")
	}

	if len(torrents) > 20 {
		torrents = torrents[:20]
	}
	return torrents
}

// makes "text" clickable with "url"
func hyperlink(text, link string) string {
	if link == "" {
		return text
	}
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", link, text)
}

func toString(v any) string {
	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f", val)
	case int:
		return fmt.Sprintf("%d", val)
	case string:
		return val
	case nil:
		return "?"
	default:
		return fmt.Sprintf("%v", val)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
