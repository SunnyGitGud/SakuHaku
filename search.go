package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
	return performCombinedSearch(query)
}
