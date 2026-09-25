package main

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
)

// ----- Models -----
type Anime struct {
	ID          int      `json:"id"`
	Title       Title    `json:"title"`
	Format      string   `json:"format"`
	Status      string   `json:"status"`
	Episodes    *int     `json:"episodes"`
	Score       *int     `json:"averageScore"`
	Season      string   `json:"season"`
	SeasonYear  *int     `json:"seasonYear"`
	Description string   `json:"description"`
	Duration    *int     `json:"duration"`
	Genres      []string `json:"genres"`
	Studios     struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"studios"`
	NextAiringEpisode *struct {
		Episode  int   `json:"episode"`
		AiringAt int64 `json:"airingAt"`
	} `json:"nextAiringEpisode"`
	CoverImage struct {
		ExtraLarge string `json:"extraLarge"`
		Large      string `json:"large"`
	} `json:"coverImage"`
	SiteURL string `json:"siteUrl"`
}

// PosterURL returns the highest resolution cover available. Downscaling a
// large source gives a much sharper terminal render than upscaling a small one.
func (a *Anime) PosterURL() string {
	if a.CoverImage.ExtraLarge != "" {
		return a.CoverImage.ExtraLarge
	}
	return a.CoverImage.Large
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
		Media struct {
			MediaListEntry *struct {
				Progress int    `json:"progress"`
				Status   string `json:"status"`
			} `json:"mediaListEntry"`
		} `json:"Media"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
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

// source is what to hand the torrent client: the magnet if we have one,
// otherwise the .torrent URL
func (t Torrent) source() string {
	if t.MagnetURI != "" {
		return t.MagnetURI
	}
	return t.TorrentURL
}

type ViewMode int

const (
	ModeLogin ViewMode = iota
	ModeUserList
	ModeAnimeSearch
	ModeTorrents
	ModeDownloads
	ModeStreaming
)

type model struct {
	// Auth
	accessToken string
	username    string
	userID      int

	// Common
	mode        ViewMode
	ready       bool
	termWidth   int
	termHeight  int
	viewport    viewport.Model
	searchMode  bool
	searchInput string
	loginMsg    string

	// List type tracking
	currentListType ListType

	// User list mode
	userEntries     []UserAnimeEntry
	userEntryCursor int
	listPage        int // 1-based page of a browsable list
	listLastPage    int
	listHasNext     bool

	// Anime search mode
	anime           []Anime
	animeCursor     int
	animePage       int
	animeTotalPages int
	animeQuery      string

	// Torrent mode. allTorrents holds the search results, torrents the
	// filtered and sorted view of them that is shown and indexed into.
	allTorrents      []Torrent
	epFilter         int // 0 = any episode
	pendingEpFilter  int // applied when the next results arrive
	minSeeders       int
	torrentSort      torrentSort
	epInputMode      bool
	epInput          string
	selectedEntry    *UserAnimeEntry // list entry torrents were opened from, if any
	torrents         []Torrent
	torrentCursor    int
	torrentPage      int
	selectedTorrents map[int]struct{}
	selectedAnime    *Anime
	torrentsFrom     ViewMode // list the torrent search was started from

	// Split view (list + poster) scroll position
	listOffset int

	// Posters the last render wanted; fetched in the background after Update
	wantPosters []string

	// Torrent client / download manager
	torrentClient  *tc.TorrentClient
	streamURL      string
	playback       *playback                 // episode being streamed
	streamFrom     ViewMode                  // screen to return to from streaming
	torrentCtx     map[string]*streamContext // anime context per info hash
	downloads      []tc.DownloadInfo
	downloadCursor int
	prevMode       ViewMode
	ticking        bool
	confirmDelete  string // info hash awaiting a second X to delete files
	statusMsg      string
	confirmQuit    bool

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
