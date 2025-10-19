package main

import (
	"encoding/xml"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Nyaa RSS Feed structures
type NyaaRSS struct {
	Channel NyaaChannel `xml:"channel"`
}

type NyaaChannel struct {
	Items []NyaaItem `xml:"item"`
}

type NyaaItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Seeders     string `xml:"seeders"`
	Leechers    string `xml:"leechers"`
	Downloads   string `xml:"downloads"`
	InfoHash    string `xml:"infoHash"`
	CategoryID  string `xml:"categoryId"`
	Category    string `xml:"category"`
	Size        string `xml:"size"`
	Description string `xml:"description"`
}

// Convert Nyaa item to our Torrent struct
func (item NyaaItem) toTorrent(index int) Torrent {
	// Parse size string like "1.5 GiB" to bytes
	size := parseSizeString(item.Size)
	
	// Parse seeders/leechers
	seeders := parseIntString(item.Seeders)
	leechers := parseIntString(item.Leechers)
	
	// Build magnet URI from infohash
	magnetURI := ""
	if item.InfoHash != "" {
		magnetURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=http://nyaa.tracker.wf:7777/announce&tr=udp://open.stealth.si:80/announce&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://tracker.torrent.eu.org:451/announce",
			item.InfoHash,
			url.QueryEscape(item.Title))
	}
	
	return Torrent{
		ID:         index,
		Title:      item.Title,
		Link:       item.Link,
		TorrentURL: item.GUID,
		MagnetURI:  magnetURI,
		Seeders:    seeders,
		Leechers:   leechers,
		TotalSize:  size,
		WebsiteURL: item.Link,
	}
}

func parseSizeString(sizeStr string) int64 {
	sizeStr = strings.TrimSpace(sizeStr)
	parts := strings.Fields(sizeStr)
	if len(parts) != 2 {
		return 0
	}
	
	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	
	unit := strings.ToUpper(parts[1])
	multiplier := int64(1)
	
	switch unit {
	case "KIB":
		multiplier = 1024
	case "MIB":
		multiplier = 1024 * 1024
	case "GIB":
		multiplier = 1024 * 1024 * 1024
	case "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	}
	
	return int64(value * float64(multiplier))
}

func parseIntString(s string) int {
	val, _ := strconv.Atoi(strings.TrimSpace(s))
	return val
}

// Perform Nyaa.si search
func performNyaaSearch(query string) tea.Cmd {
	return func() tea.Msg {
		// Nyaa.si RSS feed endpoint
		apiURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0", url.QueryEscape(query))
		
		resp, err := http.Get(apiURL)
		if err != nil {
			return torrentSearchResultMsg(nil)
		}
		defer resp.Body.Close()
		
		var rss NyaaRSS
		if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
			return torrentSearchResultMsg(nil)
		}
		
		// Convert to our Torrent struct
		torrents := make([]Torrent, 0, len(rss.Channel.Items))
		for i, item := range rss.Channel.Items {
			torrents = append(torrents, item.toTorrent(i))
		}
		
		return torrentSearchResultMsg(torrents)
	}
}

// Combined search from multiple sources
type combinedTorrentMsg struct {
	torrents []Torrent
	source   string
}

func performCombinedSearch(query string) tea.Cmd {
	return func() tea.Msg {
		// Search both AnimeTosho and Nyaa concurrently
		animetoshoChan := make(chan []Torrent, 1)
		nyaaChan := make(chan []Torrent, 1)
		
		// AnimeTosho search
		go func() {
			apiURL := fmt.Sprintf("https://feed.animetosho.org/json?qx=1&q=%s", url.QueryEscape(query))
			resp, err := http.Get(apiURL)
			if err != nil {
				animetoshoChan <- nil
				return
			}
			defer resp.Body.Close()
			
			var torrents []Torrent
			if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
				animetoshoChan <- nil
				return
			}
			
			// Tag source
			for i := range torrents {
				torrents[i].ID = i
				torrents[i].Source = "animetosho"
			}
			animetoshoChan <- torrents
		}()
		
		// Nyaa search
		go func() {
			apiURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0", url.QueryEscape(query))
			resp, err := http.Get(apiURL)
			if err != nil {
				nyaaChan <- nil
				return
			}
			defer resp.Body.Close()
			
			var rss NyaaRSS
			if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
				nyaaChan <- nil
				return
			}
			
			torrents := make([]Torrent, 0, len(rss.Channel.Items))
			for i, item := range rss.Channel.Items {
				torrents = append(torrents, item.toTorrent(i))
			}
			nyaaChan <- torrents
		}()
		
		// Collect results
		animetoshoResults := <-animetoshoChan
		nyaaResults := <-nyaaChan
		
		// Combine and deduplicate
		combined := make([]Torrent, 0)
		if animetoshoResults != nil {
			combined = append(combined, animetoshoResults...)
		}
		if nyaaResults != nil {
			combined = append(combined, nyaaResults...)
		}
		
		// Re-index
		for i := range combined {
			combined[i].ID = i
		}
		
		return torrentSearchResultMsg(combined)
	}
}
