package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

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
		TorrentURL: item.Link, // <link> is the .torrent download, <guid> the view page
		MagnetURI:  magnetURI,
		Seeders:    seeders,
		Leechers:   leechers,
		TotalSize:  size,
		WebsiteURL: item.Link,
	}
}

// parseSizeString parses sizes like "1.5 GiB", "700MiB" or "1.37GB". Sites
// mean binary units either way.
func parseSizeString(sizeStr string) int64 {
	m := reSize.FindStringSubmatch(strings.TrimSpace(sizeStr))
	if m == nil {
		return 0
	}
	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	multiplier := map[string]float64{"": 1, "B": 1, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40}[strings.ToUpper(m[2])]
	return int64(value * multiplier)
}

var reSize = regexp.MustCompile(`(?i)^([\d.]+)\s*([KMGT]?)i?B?$`)

func parseIntString(s string) int {
	val, _ := strconv.Atoi(strings.TrimSpace(s))
	return val
}

var searchHTTPClient = &http.Client{Timeout: 20 * time.Second}

// searchNyaa queries nyaa's RSS feed (or the mirror given with -nyaa)
func searchNyaa(query string) ([]Torrent, error) {
	apiURL := fmt.Sprintf("%s/?page=rss&q=%s&c=1_2&f=0", cfg.NyaaURL, url.QueryEscape(query))
	resp, err := searchHTTPClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}

	var rss NyaaRSS
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return nil, err
	}

	torrents := make([]Torrent, 0, len(rss.Channel.Items))
	for i, item := range rss.Channel.Items {
		t := item.toTorrent(i)
		t.Source = "nyaa"
		torrents = append(torrents, t)
	}
	return torrents, nil
}

// searchAnimeTosho queries the AnimeTosho JSON feed
func searchAnimeTosho(query string) ([]Torrent, error) {
	apiURL := fmt.Sprintf("%s/json?qx=1&q=%s", cfg.AnimeToshoURL, url.QueryEscape(query))
	resp, err := searchHTTPClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}

	var torrents []Torrent
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, err
	}
	for i := range torrents {
		torrents[i].Source = "animetosho"
	}
	return torrents, nil
}

// torrentSearchResultMsg carries combined results; err is set only when
// every source failed
type torrentSearchResultMsg struct {
	torrents []Torrent
	err      error
}

// performTorrentSearch searches every source for each distinct title (romaji
// titles match most release names, English ones catch the rest) and merges
// the results, dropping duplicates
func performTorrentSearch(titles ...string) tea.Cmd {
	return func() tea.Msg {
		type result struct {
			torrents []Torrent
			err      error
			source   string
		}

		var queries []string
		seen := map[string]bool{}
		for _, t := range titles {
			t = strings.TrimSpace(t)
			if t != "" && !seen[strings.ToLower(t)] {
				seen[strings.ToLower(t)] = true
				queries = append(queries, t)
			}
		}

		sources := enabledSources()
		results := make(chan result)
		for _, q := range queries {
			for _, src := range sources {
				go func(q string, src torrentSource) {
					t, err := src.search(q)
					results <- result{t, err, src.name}
				}(q, src)
			}
		}

		var combined []Torrent
		var errs []string
		dedupe := map[string]bool{}
		for range len(sources) * len(queries) {
			r := <-results
			if r.err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", r.source, r.err))
				continue
			}
			for _, t := range r.torrents {
				key := strings.ToLower(infoHashFromMagnet(t.MagnetURI))
				if key == "" {
					key = t.Source + "|" + t.Title
				}
				if dedupe[key] {
					continue
				}
				dedupe[key] = true
				combined = append(combined, t)
			}
		}

		for i := range combined {
			combined[i].ID = i
		}

		msg := torrentSearchResultMsg{torrents: combined}
		if len(combined) == 0 && len(errs) > 0 {
			msg.err = fmt.Errorf("%s", strings.Join(errs, "; "))
		}
		return msg
	}
}

// infoHashFromMagnet pulls the btih out of a magnet link
func infoHashFromMagnet(magnet string) string {
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		if h, ok := strings.CutPrefix(xt, "urn:btih:"); ok {
			return h
		}
	}
	return ""
}
