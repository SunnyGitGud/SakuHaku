package main

import (
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// Absolute episode numbering
//
// AniList has one entry per season, numbered from 1, but many releases number
// sequels continuously: Jujutsu Kaisen season 2 episode 5 is often released as
// "Jujutsu Kaisen - 29". We work out the offset (the episode count of all
// earlier TV seasons) by walking the PREQUEL relations, then map release
// numbers back to the season's own numbering.

// maxPrequelHops bounds the walk (One Piece-style franchises are one entry,
// long chains are things like Gintama)
const maxPrequelHops = 15

// episodeNumbering is what we know about a show's numbering
type episodeNumbering struct {
	Offset int  // episodes in earlier seasons, 0 if none/unknown
	Total  int  // episodes in this season, 0 if unknown (still airing)
	Known  bool // Offset is reliable
}

// toSeason converts a release's episode info to this season's numbering.
// A number counts as absolute when it's beyond this season's own episodes
// (or, while airing, beyond the offset) and maps into the season's range.
func (n episodeNumbering) toSeason(info EpisodeInfo) EpisodeInfo {
	if !n.Known || n.Offset == 0 {
		return info
	}
	conv := func(ep int) int {
		if ep <= n.Offset {
			return ep
		}
		if n.Total > 0 && ep <= n.Total {
			return ep // fits the season as is: season-relative numbering
		}
		rel := ep - n.Offset
		if n.Total > 0 && rel > n.Total {
			return ep // past this season too, leave it alone
		}
		return rel
	}
	switch {
	case info.Episode > 0:
		info.Episode = conv(info.Episode)
	case info.Batch && info.From > 0:
		from, to := conv(info.From), conv(info.To)
		if from <= to {
			info.From, info.To = from, to
		}
	}
	return info
}

// isAbsolute reports whether the release number was mapped, for labels
func (n episodeNumbering) isAbsolute(info EpisodeInfo) bool {
	return n.toSeason(info) != info
}

// episodeNumberingMsg carries the result of the prequel walk
type episodeNumberingMsg struct {
	mediaID   int
	numbering episodeNumbering
	err       error
}

var (
	numberingMu    sync.Mutex
	numberingCache = map[int]episodeNumbering{}
)

type relationsResponse struct {
	Data struct {
		Media struct {
			ID        int  `json:"id"`
			Episodes  *int `json:"episodes"`
			Relations struct {
				Edges []struct {
					RelationType string `json:"relationType"`
					Node         struct {
						ID       int    `json:"id"`
						Type     string `json:"type"`
						Format   string `json:"format"`
						Episodes *int   `json:"episodes"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"relations"`
		} `json:"Media"`
	} `json:"data"`
}

// seasonFormats are the formats that count as earlier seasons; movies, OVAs
// and specials are numbered separately by release groups
var seasonFormats = map[string]bool{"TV": true, "TV_SHORT": true, "ONA": true}

// fetchEpisodeNumbering walks the prequel chain of an anime
func fetchEpisodeNumbering(mediaID int) tea.Cmd {
	return func() tea.Msg {
		numberingMu.Lock()
		if n, ok := numberingCache[mediaID]; ok {
			numberingMu.Unlock()
			return episodeNumberingMsg{mediaID: mediaID, numbering: n}
		}
		numberingMu.Unlock()

		n, err := walkPrequels(mediaID)
		if err == nil {
			numberingMu.Lock()
			numberingCache[mediaID] = n
			numberingMu.Unlock()
		}
		return episodeNumberingMsg{mediaID: mediaID, numbering: n, err: err}
	}
}

func walkPrequels(mediaID int) (episodeNumbering, error) {
	const query = `
	query ($id: Int) {
		Media(id: $id, type: ANIME) {
			id
			episodes
			relations {
				edges {
					relationType
					node {
						id
						type
						format
						episodes
					}
				}
			}
		}
	}`

	var n episodeNumbering
	seen := map[int]bool{mediaID: true}
	current := mediaID
	for hop := 0; hop <= maxPrequelHops; hop++ {
		var resp relationsResponse
		if err := anilistQuery("", query, map[string]any{"id": current}, &resp); err != nil {
			return episodeNumbering{}, err
		}
		media := resp.Data.Media
		if hop == 0 && media.Episodes != nil {
			n.Total = *media.Episodes
		}

		next := 0
		for _, e := range media.Relations.Edges {
			if e.RelationType != "PREQUEL" || e.Node.Type != "ANIME" || !seasonFormats[e.Node.Format] || seen[e.Node.ID] {
				continue
			}
			if e.Node.Episodes == nil {
				// A prequel with an unknown length: can't know the offset
				return episodeNumbering{Total: n.Total}, nil
			}
			n.Offset += *e.Node.Episodes
			next = e.Node.ID
			break
		}
		if next == 0 {
			n.Known = true
			return n, nil
		}
		seen[next] = true
		current = next
	}
	return episodeNumbering{Total: n.Total}, fmt.Errorf("prequel chain longer than %d seasons", maxPrequelHops)
}
