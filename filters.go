package main

import (
	"fmt"
	"sort"
)

// torrentSort is how torrent results are ordered
type torrentSort int

const (
	sortRelevance torrentSort = iota // as returned by the sites
	sortSeeders
	sortSize
	torrentSortCount
)

func (s torrentSort) String() string {
	switch s {
	case sortSeeders:
		return "seeders"
	case sortSize:
		return "size"
	}
	return "relevance"
}

// seederSteps are the minimum-seeder thresholds cycled with f
var seederSteps = []int{0, 1, 5, 10, 25, 50, 100}

func nextSeederStep(cur int) int {
	for _, s := range seederSteps {
		if s > cur {
			return s
		}
	}
	return 0
}

// applyTorrentFilters rebuilds the visible torrent list from allTorrents
func (m *model) applyTorrentFilters() {
	numbering := m.currentNumbering()
	filtered := make([]Torrent, 0, len(m.allTorrents))
	for _, t := range m.allTorrents {
		// Sites without swarm info (SubsPlease, TokyoTosho) aren't hidden by
		// the seeder filter: unknown isn't the same as few
		if n := seedersOf(t); n != seedersUnknown && n < m.minSeeders {
			continue
		}
		if m.epFilter > 0 && !numbering.toSeason(parseEpisode(t.Title)).Contains(m.epFilter) {
			continue
		}
		filtered = append(filtered, t)
	}

	switch m.torrentSort {
	case sortSeeders:
		sort.SliceStable(filtered, func(i, j int) bool { return seedersOf(filtered[i]) > seedersOf(filtered[j]) })
	case sortSize:
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].TotalSize > filtered[j].TotalSize })
	}

	m.torrents = filtered
	m.torrentPage = 0
	m.torrentCursor = 0
	m.selectedTorrents = make(map[int]struct{})
	if m.ready {
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoTop()
	}
}

// filterSummary describes the active filters for the torrent header
func (m *model) filterSummary() string {
	ep := "any episode"
	n := m.currentNumbering()
	if m.epFilter > 0 {
		ep = fmt.Sprintf("episode %d", m.epFilter)
		if n.Known && n.Offset > 0 {
			ep += fmt.Sprintf(" (or %d absolute)", m.epFilter+n.Offset)
		}
	}
	return fmt.Sprintf("Showing %d of %d · %s · ≥%d seeders · sort: %s   (e/E episode · f seeders · o sort)",
		len(m.torrents), len(m.allTorrents), ep, m.minSeeders, m.torrentSort)
}

// currentNumbering is the episode numbering of the anime whose torrents are shown
func (m *model) currentNumbering() episodeNumbering {
	if m.selectedAnime == nil {
		return episodeNumbering{}
	}
	numberingMu.Lock()
	defer numberingMu.Unlock()
	return numberingCache[m.selectedAnime.ID]
}
