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
	filtered := make([]Torrent, 0, len(m.allTorrents))
	for _, t := range m.allTorrents {
		if seedersOf(t) < m.minSeeders {
			continue
		}
		if m.epFilter > 0 && !parseEpisode(t.Title).Contains(m.epFilter) {
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
	if m.epFilter > 0 {
		ep = fmt.Sprintf("episode %d", m.epFilter)
	}
	return fmt.Sprintf("Showing %d of %d · %s · ≥%d seeders · sort: %s   (e/E episode · f seeders · o sort)",
		len(m.torrents), len(m.allTorrents), ep, m.minSeeders, m.torrentSort)
}
