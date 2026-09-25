package main

import (
	"fmt"
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
	err        error
}

func performAnimeSearch(query string, page int) tea.Cmd {
	return func() tea.Msg {
		variables := map[string]any{
			"search":  query,
			"page":    page,
			"perPage": 20,
		}

		query := fmt.Sprintf(`
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
						%s
					}
				}
			}`, mediaFields)

		result, err := makePublicRequest(query, variables)
		if err != nil {
			return animeSearchResultMsg{err: err, page: page - 1}
		}

		return animeSearchResultMsg{
			anime:      result.Data.Page.Media,
			totalPages: result.Data.Page.PageInfo.LastPage,
			page:       page - 1,
		}
	}
}
