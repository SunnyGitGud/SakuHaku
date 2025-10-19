package main

import "github.com/charmbracelet/bubbles/viewport"

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
}

type ViewMode int

const (
	ModeAnime ViewMode = iota
	ModeTorrents
)

type model struct {
	// Common
	mode        ViewMode
	ready       bool
	viewport    viewport.Model
	searchMode  bool
	searchInput string

	// Anime mode
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
}
