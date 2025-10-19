package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Debug logging to file
func debugLog(msg string) {
	f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		f.WriteString(fmt.Sprintf("%s\n", msg))
	}
}

// Search Functions
type animeSearchResultMsg struct {
	anime      []Anime
	totalPages int
	page       int
}

type torrentSearchResultMsg []Torrent

func performAnimeSearch(query string, page int) tea.Cmd {
	return func() tea.Msg {
		variables := map[string]any{
			"search":  query,
			"page":    page,
			"perPage": 20,
		}

		requestBody := map[string]any{
			"query": `
			query ($search: String, $page: Int, $perPage: Int) {
				Page(page: $page, perPage: $perPage) {
					pageInfo {
						total
						perPage
						currentPage
						lastPage
						hasNextPage
					}
					media(search: $search, type: ANIME, sort: POPULARITY_DESC) {
						id
						title {
							romaji
							english
						}
						format
						status
						episodes
						averageScore
						season
						seasonYear
						coverImage {
							large
						}
						siteUrl
					}
				}
			}
			`,
			"variables": variables,
		}

		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return animeSearchResultMsg{anime: nil}
		}

		resp, err := http.Post("https://graphql.anilist.co", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return animeSearchResultMsg{anime: nil}
		}
		defer resp.Body.Close()

		var result AniListResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return animeSearchResultMsg{anime: nil}
		}

		return animeSearchResultMsg{
			anime:      result.Data.Page.Media,
			totalPages: result.Data.Page.PageInfo.LastPage,
			page:       page - 1,
		}
	}
}

// Search both AnimeTosho and Nyaa
func performTorrentSearch(query string) tea.Cmd {
<<<<<<< HEAD
	return func() tea.Msg {
		animetoshoChan := make(chan []Torrent, 1)
		nyaaChan := make(chan []Torrent, 1)

		// AnimeTosho search
		go func() {
			debugLog("Starting AnimeTosho search")
			apiURL := "https://feed.animetosho.org/json?qx=1&q=" + url.QueryEscape(query)
			resp, err := http.Get(apiURL)
			if err != nil {
				debugLog(fmt.Sprintf("AnimeTosho error: %v", err))
				animetoshoChan <- nil
				return
			}
			defer resp.Body.Close()

			var torrents []Torrent
			if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
				debugLog(fmt.Sprintf("AnimeTosho decode error: %v", err))
				animetoshoChan <- nil
				return
			}

			// Tag as animetosho source
			for i := range torrents {
				torrents[i].ID = i
				torrents[i].Source = "animetosho"
			}
			debugLog(fmt.Sprintf("AnimeTosho: got %d results", len(torrents)))
			animetoshoChan <- torrents
		}()


		go func() {
			debugLog("Starting Nyaa search")
			apiURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0", url.QueryEscape(query))
			debugLog(fmt.Sprintf("Nyaa URL: %s", apiURL))
			
			req, err := http.NewRequest("GET", apiURL, nil)
			if err != nil {
				debugLog(fmt.Sprintf("Nyaa request error: %v", err))
				nyaaChan <- nil
				return
			}
			
			// Add User-Agent
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				debugLog(fmt.Sprintf("Nyaa error: %v", err))
				nyaaChan <- nil
				return
			}
			defer resp.Body.Close()
			debugLog(fmt.Sprintf("Nyaa response status: %d", resp.StatusCode))

			torrents := parseNyaaRSS(resp, query)
			debugLog(fmt.Sprintf("Nyaa: got %d results", len(torrents)))
			nyaaChan <- torrents
		}()

		// Collect results
		animetoshoResults := <-animetoshoChan
		nyaaResults := <-nyaaChan

		// Combine
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

		fmt.Printf("Total combined results: %d\n", len(combined))
		return torrentSearchResultMsg(combined)
	}
}

// Helper to parse Nyaa RSS
func parseNyaaRSS(resp *http.Response, query string) []Torrent {
	// Read the raw response to see what we're dealing with
	var rss struct {
		Channel struct {
			Items []struct {
				Title    string `xml:"title"`
				Link     string `xml:"link"`
				GUID     string `xml:"guid"`
				Seeders  string `xml:"seeders"`
				Leechers string `xml:"leechers"`
				Size     string `xml:"size"`
				InfoHash string `xml:"infoHash"`
			} `xml:"item"`
		} `xml:"channel"`
	}

	decoder := xml.NewDecoder(resp.Body)
	decoder.Strict = false  // Be lenient with malformed XML
	
	if err := decoder.Decode(&rss); err != nil {
		debugLog(fmt.Sprintf("Nyaa parse error: %v", err))
		return nil
	}

	torrents := make([]Torrent, 0)
	for i, item := range rss.Channel.Items {
		if item.Title == "" {
			continue // Skip empty entries
		}
		t := Torrent{
			ID:         i,
			Title:      item.Title,
			Link:       item.Link,
			TorrentURL: item.GUID,
			Seeders:    parseIntString(item.Seeders),
			Leechers:   parseIntString(item.Leechers),
			TotalSize:  parseSizeString(item.Size),
			WebsiteURL: item.Link,
			Source:     "nyaa",
		}
		if item.InfoHash != "" {
			t.MagnetURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", item.InfoHash, url.QueryEscape(item.Title))
		}
		torrents = append(torrents, t)
	}
	debugLog(fmt.Sprintf("Parsed %d Nyaa torrents", len(torrents)))
	return torrents
}
=======
	return performCombinedSearch(query)
}
>>>>>>> 9a772087f77d87291857d66e3f1cfe594e0a7dc6
