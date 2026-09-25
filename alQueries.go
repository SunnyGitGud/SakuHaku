package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// anilistEndpoint is a var so tests can point it at a fake server
var anilistEndpoint = "https://graphql.anilist.co"

// listPerPage is the page size for the browsable (non user) lists
const listPerPage = 50

var anilistHTTPClient = &http.Client{Timeout: 20 * time.Second}

// mediaFields is the set of Media fields every query asks for
const mediaFields = `
	id
	title {
		romaji
		english
	}
	format
	status
	episodes
	duration
	averageScore
	season
	seasonYear
	genres
	description(asHtml: false)
	studios(isMain: true) {
		nodes {
			name
		}
	}
	nextAiringEpisode {
		episode
		airingAt
	}
	coverImage {
		extraLarge
		large
	}
	siteUrl
`

// listPageMsg carries one page of a browsable list (trending, top rated...)
type listPageMsg struct {
	listType ListType
	entries  []UserAnimeEntry
	page     int
	lastPage int
	hasNext  bool
	err      error
}

// anilistRequest runs a GraphQL query, authenticated when token is set
func anilistRequest(token, query string, variables map[string]any) (*AniListResponse, error) {
	var result AniListResponse
	err := anilistQuery(token, query, variables, &result)
	return &result, err
}

// anilistQuery runs a GraphQL query (authenticated when token is set) and
// decodes the response into out
func anilistQuery(token, query string, variables map[string]any, out any) error {
	jsonData, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", anilistEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := anilistHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var errs struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &errs); err != nil {
		return fmt.Errorf("AniList: %s: %w", resp.Status, err)
	}
	if len(errs.Errors) > 0 {
		msgs := make([]string, len(errs.Errors))
		for i, e := range errs.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("AniList: %s", strings.Join(msgs, "; "))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AniList: %s", resp.Status)
	}
	return json.Unmarshal(body, out)
}

func makePublicRequest(query string, variables map[string]any) (*AniListResponse, error) {
	return anilistRequest("", query, variables)
}

func makeAuthenticatedRequest(token, query string, variables map[string]any) (*AniListResponse, error) {
	return anilistRequest(token, query, variables)
}

// fetchMediaPage loads one page of a browsable list
func fetchMediaPage(listType ListType, page int) tea.Cmd {
	return func() tea.Msg {
		args := "type: ANIME, sort: TRENDING_DESC"
		variables := map[string]any{"page": page, "perPage": listPerPage}
		varDecl := "$page: Int, $perPage: Int"

		switch listType {
		case ListPopularSeason:
			season, year := getCurrentSeason(time.Now())
			variables["season"] = season
			variables["year"] = year
			varDecl += ", $season: MediaSeason, $year: Int"
			args = "type: ANIME, season: $season, seasonYear: $year, sort: POPULARITY_DESC"
		case ListTopRated:
			args = "type: ANIME, sort: SCORE_DESC"
		}

		query := fmt.Sprintf(`
		query (%s) {
			Page(page: $page, perPage: $perPage) {
				pageInfo {
					currentPage
					lastPage
					hasNextPage
				}
				media(%s) {
					%s
				}
			}
		}`, varDecl, args, mediaFields)

		result, err := makePublicRequest(query, variables)
		msg := listPageMsg{listType: listType, page: page, err: err}
		if err != nil {
			return msg
		}

		for _, anime := range result.Data.Page.Media {
			msg.entries = append(msg.entries, UserAnimeEntry{Media: anime})
		}
		msg.lastPage = max(1, result.Data.Page.PageInfo.LastPage)
		msg.hasNext = result.Data.Page.PageInfo.HasNextPage
		return msg
	}
}

func fetchTrendingAnime() tea.Cmd     { return fetchMediaPage(ListTrending, 1) }
func fetchPopularThisSeason() tea.Cmd { return fetchMediaPage(ListPopularSeason, 1) }
func fetchTopRated() tea.Cmd          { return fetchMediaPage(ListTopRated, 1) }

// isBrowsableList reports whether the current list is a paginated public list
// rather than the user's own
func (m *model) isBrowsableList() bool {
	switch m.currentListType {
	case ListTrending, ListPopularSeason, ListTopRated:
		return true
	}
	// Without a login the personal lists fall back to trending
	return m.accessToken == ""
}

// fetchCurrentList loads the first page of the current list
func (m *model) fetchCurrentList() tea.Cmd {
	return m.fetchListPage(1)
}

func (m *model) fetchListPage(page int) tea.Cmd {
	switch m.currentListType {
	case ListCurrentlyWatching:
		if m.accessToken == "" {
			return fetchMediaPage(ListTrending, page)
		}
		return fetchUserAnimeList(m.accessToken, m.userID, "CURRENT")
	case ListPlanToWatch:
		if m.accessToken == "" {
			return fetchMediaPage(ListTrending, page)
		}
		return fetchUserAnimeList(m.accessToken, m.userID, "PLANNING")
	case ListTrending, ListPopularSeason, ListTopRated:
		return fetchMediaPage(m.currentListType, page)
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

// updateProgressMsg reports the result of saving progress to AniList
type updateProgressMsg struct {
	mediaID  int
	title    string
	progress int
	status   string
	skipped  bool // the list already had this episode (or later) watched
	err      error
}

// updateAniListProgress marks episode as watched. It re-reads the user's list
// entry first so re-watching an old episode never lowers their progress. The
// status becomes COMPLETED after the last episode, stays REPEATING during a
// rewatch and is CURRENT otherwise.
func updateAniListProgress(token string, mediaID int, title string, episode int, totalEpisodes *int) tea.Cmd {
	return func() tea.Msg {
		msg := updateProgressMsg{mediaID: mediaID, title: title, progress: episode}

		current, err := makeAuthenticatedRequest(token, `
		query ($id: Int) {
			Media(id: $id) {
				mediaListEntry {
					progress
					status
				}
			}
		}`, map[string]any{"id": mediaID})
		if err != nil {
			msg.err = err
			return msg
		}

		status := "CURRENT"
		if e := current.Data.Media.MediaListEntry; e != nil {
			if e.Progress >= episode {
				msg.skipped = true
				msg.status = e.Status
				return msg
			}
			if e.Status == "REPEATING" {
				status = "REPEATING"
			}
		}
		if totalEpisodes != nil && *totalEpisodes > 0 && episode >= *totalEpisodes {
			status = "COMPLETED"
		}
		msg.status = status

		_, msg.err = makeAuthenticatedRequest(token, `
		mutation ($mediaId: Int, $progress: Int, $status: MediaListStatus) {
			SaveMediaListEntry(mediaId: $mediaId, progress: $progress, status: $status) {
				id
				progress
				status
			}
		}`, map[string]any{
			"mediaId":  mediaID,
			"progress": episode,
			"status":   status,
		})
		return msg
	}
}

// fetchAnimeByID loads one anime's details
func fetchAnimeByID(id int) (*Anime, error) {
	var resp struct {
		Data struct {
			Media Anime `json:"Media"`
		} `json:"data"`
	}
	query := fmt.Sprintf(`query ($id: Int) { Media(id: $id, type: ANIME) { %s } }`, mediaFields)
	if err := anilistQuery("", query, map[string]any{"id": id}, &resp); err != nil {
		return nil, err
	}
	if resp.Data.Media.ID == 0 {
		return nil, fmt.Errorf("anime %d not found", id)
	}
	return &resp.Data.Media, nil
}
