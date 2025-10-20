package main

import (
	"github.com/anacrolix/torrent"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
)

// ----- Models -----
type Anime struct {
	ID          int    `json:"id"`
	Title       Title  `json:"title"`
	Format      string `json:"format"`
	Status      string `json:"status"`
	Episodes    *int   `json:"episodes"`
	Score       *int   `json:"averageScore"`
	Season      string `json:"season"`
	SeasonYear  *int   `json:"seasonYear"`
	Description string `json:"description"`
	CoverImage  struct {
		Large string `json:"large"`
	} `json:"coverImage"`
	SiteURL string `json:"siteUrl"`
}

type Title struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
}

type UserAnimeEntry struct {
	ID        int     `json:"id"`
	Status    string  `json:"status"`
	Progress  int     `json:"progress"`
	Score     float64 `json:"score"`
	Media     Anime   `json:"media"`
	UpdatedAt int64   `json:"updatedAt"`
}

type AniListResponse struct {
	Data struct {
		Page struct {
			PageInfo struct {
				Total       int  `json:"total"`
				PerPage     int  `json:"perPage"`
				CurrentPage int  `json:"currentPage"`
				LastPage    int  `json:"lastPage"`
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
			Media []Anime `json:"media"`
		} `json:"Page"`
		MediaListCollection struct {
			Lists []struct {
				Name    string           `json:"name"`
				Entries []UserAnimeEntry `json:"entries"`
			} `json:"lists"`
		} `json:"MediaListCollection"`
		Viewer struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"Viewer"`
	} `json:"data"`
}

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
	Source     string `json:"source"`
}

type ViewMode int

const (
	ModeLogin ViewMode = iota
	ModeUserList
	ModeAnimeSearch
	ModeTorrents
)

type model struct {
	// Auth
	accessToken string
	username    string
	userID      int

	// Common
	mode        ViewMode
	ready       bool
	viewport    viewport.Model
	searchMode  bool
	searchInput string
	loginMsg    string

	// List type tracking
	currentListType ListType

	// User list mode
	userEntries     []UserAnimeEntry
	userEntryCursor int

	// Anime search mode
	anime           []Anime
	animeCursor     int
	animePage       int
	animeTotalPages int
	animeQuery      string

	// Torrent mode
	torrents         []Torrent
	torrentCursor    int
	torrentPage      int
	selectedTorrents map[int]struct{}
	selectedAnime    *Anime

	// Torrent client
	torrentClient    *tc.TorrentClient
	activeTorrent    *torrent.Torrent
	streamURL        string
	downloadProgress float64

	//spinner
	spinner    spinner.Model
	loading    bool
	loadingMsg string
}

type ListType int

const (
	ListCurrentlyWatching ListType = iota
	ListPlanToWatch
	ListTrending
	ListPopularSeason
	ListTopRated
)

func (lt ListType) String() string {
	switch lt {
	case ListCurrentlyWatching:
		return "Currently Watching"
	case ListPlanToWatch:
		return "Plan to Watch"
	case ListTrending:
		return "Trending Now"
	case ListPopularSeason:
		return "Popular This Season"
	case ListTopRated:
		return "Top Rated"
	default:
		return "Unknown"
	}
}
