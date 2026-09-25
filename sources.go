package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// torrentSource is a site we can search for releases
type torrentSource struct {
	id     string // used in -sources and Torrent.Source
	name   string // for error messages
	badge  string // shown next to results
	search func(query string) ([]Torrent, error)
}

// allSources lists every supported site in the order results are shown
func allSources() []torrentSource {
	return []torrentSource{
		{"animetosho", "AnimeTosho", "📦", searchAnimeTosho},
		{"nyaa", "nyaa (" + cfg.NyaaURL + ")", "🐱", searchNyaa},
		{"subsplease", "SubsPlease", "🍥", searchSubsPlease},
		{"tokyotosho", "TokyoTosho", "🗼", searchTokyoTosho},
	}
}

// enabledSources is allSources filtered by -sources
func enabledSources() []torrentSource {
	var out []torrentSource
	for _, s := range allSources() {
		if len(cfg.Sources) == 0 || contains(cfg.Sources, s.id) {
			out = append(out, s)
		}
	}
	return out
}

func sourceBadge(id string) string {
	for _, s := range allSources() {
		if s.id == id {
			return s.badge
		}
	}
	return "📦"
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// getOK fetches a URL and fails on non-200 responses
func getOK(u string) (io.ReadCloser, error) {
	resp, err := searchHTTPClient.Get(u)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return resp.Body, nil
}

// SubsPlease ------------------------------------------------------------------

// subsPleaseRelease is one entry of SubsPlease's search API. Their RSS feeds
// only list the latest releases and can't be searched, so we use the same
// JSON API their website's search box calls.
type subsPleaseRelease struct {
	Show      string `json:"show"`
	Episode   string `json:"episode"`
	Downloads []struct {
		Res    string `json:"res"`
		Magnet string `json:"magnet"`
	} `json:"downloads"`
	ReleaseDate string `json:"release_date"`
}

func searchSubsPlease(query string) ([]Torrent, error) {
	body, err := getOK(fmt.Sprintf("%s/api/?f=search&tz=UTC&s=%s", cfg.SubsPleaseURL, url.QueryEscape(query)))
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	// No matches comes back as [] rather than {}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var releases map[string]subsPleaseRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, err
	}

	// Map order is random; newest episode first like the other sites
	keys := make([]string, 0, len(releases))
	for k := range releases {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := releases[keys[i]], releases[keys[j]]
		if a.ReleaseDate != b.ReleaseDate {
			return a.ReleaseDate > b.ReleaseDate
		}
		return keys[i] > keys[j]
	})

	var torrents []Torrent
	for _, k := range keys {
		r := releases[k]
		// Highest resolution first
		sort.Slice(r.Downloads, func(i, j int) bool {
			a, _ := strconv.Atoi(r.Downloads[i].Res)
			b, _ := strconv.Atoi(r.Downloads[j].Res)
			return a > b
		})
		for _, d := range r.Downloads {
			if d.Magnet == "" {
				continue
			}
			torrents = append(torrents, Torrent{
				Title:     fmt.Sprintf("[SubsPlease] %s - %s (%sp)", r.Show, r.Episode, d.Res),
				MagnetURI: d.Magnet,
				Source:    "subsplease",
				// The API has no size or swarm info
				TotalSize: magnetSize(d.Magnet),
			})
		}
	}
	return torrents, nil
}

// magnetSize reads the optional xl (exact length) parameter of a magnet
func magnetSize(magnet string) int64 {
	u, err := url.Parse(magnet)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(u.Query().Get("xl"), 10, 64)
	return n
}

// TokyoTosho ------------------------------------------------------------------

type tokyoToshoRSS struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Category    string `xml:"category"`
	} `xml:"channel>item"`
}

var (
	reTTMagnet = regexp.MustCompile(`href="(magnet:[^"]+)"`)
	reTTSize   = regexp.MustCompile(`(?i)Size:\s*([\d.]+\s*[KMGT]i?B)`)
)

// searchTokyoTosho uses TokyoTosho's search RSS, anime category only
func searchTokyoTosho(query string) ([]Torrent, error) {
	body, err := getOK(fmt.Sprintf("%s/rss.php?terms=%s&type=1", cfg.TokyoToshoURL, url.QueryEscape(query)))
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var rss tokyoToshoRSS
	if err := xml.NewDecoder(body).Decode(&rss); err != nil {
		return nil, err
	}

	var torrents []Torrent
	for _, it := range rss.Items {
		t := Torrent{
			Title:      strings.TrimSpace(it.Title),
			TorrentURL: strings.TrimSpace(it.Link),
			Source:     "tokyotosho",
		}
		if m := reTTMagnet.FindStringSubmatch(it.Description); m != nil {
			t.MagnetURI = html.UnescapeString(m[1])
		}
		if m := reTTSize.FindStringSubmatch(it.Description); m != nil {
			t.TotalSize = parseSizeString(m[1])
		}
		if t.MagnetURI == "" && t.TorrentURL == "" {
			continue
		}
		torrents = append(torrents, t)
	}
	return torrents, nil
}

// sizeLabel formats a release size, "?" when the site didn't say
func sizeLabel(n int64) string {
	if n <= 0 {
		return "?"
	}
	return formatBytes(n)
}
