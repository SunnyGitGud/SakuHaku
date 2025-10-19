package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func fetchTrendingAnime() tea.Cmd {
	return func() tea.Msg {
		query := `
		query {
			Page(page: 1, perPage: 50) {
				media(type: ANIME, sort: TRENDING_DESC) {
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
		`

		result, err := makePublicRequest(query, nil)
		if err != nil {
			return userListMsg(nil)
		}

		var entries []UserAnimeEntry
		for _, anime := range result.Data.Page.Media {
			entries = append(entries, UserAnimeEntry{
				Media: anime,
			})
		}

		return userListMsg(entries)
	}
}

func fetchPopularThisSeason() tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		season, year := getCurrentSeason(now)

		query := `
		query ($season: MediaSeason, $year: Int) {
			Page(page: 1, perPage: 50) {
				media(type: ANIME, season: $season, seasonYear: $year, sort: POPULARITY_DESC) {
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
		`

		variables := map[string]interface{}{
			"season": season,
			"year":   year,
		}

		result, err := makePublicRequest(query, variables)
		if err != nil {
			return userListMsg(nil)
		}

		var entries []UserAnimeEntry
		for _, anime := range result.Data.Page.Media {
			entries = append(entries, UserAnimeEntry{
				Media: anime,
			})
		}

		return userListMsg(entries)
	}
}

func fetchTopRated() tea.Cmd {
	return func() tea.Msg {
		query := `
		query {
			Page(page: 1, perPage: 50) {
				media(type: ANIME, sort: SCORE_DESC) {
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
		`

		result, err := makePublicRequest(query, nil)
		if err != nil {
			return userListMsg(nil)
		}

		var entries []UserAnimeEntry
		for _, anime := range result.Data.Page.Media {
			entries = append(entries, UserAnimeEntry{
				Media: anime,
			})
		}

		return userListMsg(entries)
	}
}

func (m *model) fetchCurrentList() tea.Cmd {
	switch m.currentListType {
	case ListCurrentlyWatching:
		if m.accessToken == "" {
			return fetchTrendingAnime()
		}
		return fetchUserAnimeList(m.accessToken, m.userID, "CURRENT")
	case ListPlanToWatch:
		if m.accessToken == "" {
			return fetchTrendingAnime()
		}
		return fetchUserAnimeList(m.accessToken, m.userID, "PLANNING")
	case ListTrending:
		return fetchTrendingAnime()
	case ListPopularSeason:
		return fetchPopularThisSeason()
	case ListTopRated:
		return fetchTopRated()
	default:
		return nil
	}
}

func getCurrentSeason(t time.Time) (string, int) {
	month := t.Month()
	year := t.Year()

	switch {
	case month >= 1 && month <= 3:
		return "WINTER", year
	case month >= 4 && month <= 6:
		return "SPRING", year
	case month >= 7 && month <= 9:
		return "SUMMER", year
	default:
		return "FALL", year
	}
}

func makePublicRequest(query string, variables map[string]interface{}) (*AniListResponse, error) {
	requestBody := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post("https://graphql.anilist.co", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result AniListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
