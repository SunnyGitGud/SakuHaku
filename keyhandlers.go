package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
)

func (m *model) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Login mode
	if m.mode == ModeLogin {
		switch msg.String() {
		case "l":
			m.loading = true
			m.loadingMsg = "Opening browser for authentication..."
			return tea.Batch(m.spinner.Tick, startOAuthFlow())
		case "s":
			m.mode = ModeAnimeSearch
			m.viewport.SetContent(m.renderContent())
			return nil
		case "q", "ctrl+c":
			return tea.Quit
		}
		return nil
	}

	// Handle search mode
	if m.searchMode {
		switch msg.String() {
		case "esc":
			m.searchMode = false
			m.searchInput = ""
			return nil
		case "enter":
			if m.searchInput != "" {
				m.searchMode = false
				m.animeQuery = m.searchInput
				m.searchInput = ""
				m.mode = ModeAnimeSearch
				m.loading = true
				m.loadingMsg = "Searching anime..."
				return tea.Batch(m.spinner.Tick, performAnimeSearch(m.animeQuery, 1))
			}
			m.searchMode = false
			m.searchInput = ""
			return nil
		case "backspace":
			if len(m.searchInput) > 0 {
				m.searchInput = m.searchInput[:len(m.searchInput)-1]
			}
			return nil
		default:
			if len(msg.String()) == 1 {
				m.searchInput += msg.String()
			}
		}
		return nil
	}

	// Anything but a repeat press cancels a pending confirmation
	key := msg.String()
	if key != "q" && key != "esc" {
		m.confirmQuit = false
	}
	if key != "X" {
		m.confirmDelete = ""
	}

	// Common keys
	switch key {
	case "ctrl+c":
		return tea.Quit
	case "q":
		return m.quit()
	case "D":
		if m.mode != ModeDownloads {
			m.prevMode = m.mode
			m.mode = ModeDownloads
			m.refreshDownloads()
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
			return m.startTicking()
		}
		return nil
	case "esc":
		if m.mode == ModeDownloads {
			m.mode = m.prevMode
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
			return nil
		}
		if m.mode == ModeTorrents {
			if m.accessToken != "" {
				m.mode = ModeUserList
			} else {
				m.mode = ModeAnimeSearch
			}
			m.selectedAnime = nil
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
			return nil
		} else if m.mode == ModeAnimeSearch && m.accessToken != "" {
			m.mode = ModeUserList
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
			return nil
		}
		return m.quit()
	case "s":
		if m.mode == ModeUserList || m.mode == ModeAnimeSearch {
			m.searchMode = true
			m.searchInput = ""
		}
		return nil
	case "r":
		// Refresh current list
		if m.mode == ModeUserList && m.accessToken != "" {
			m.loading = true
			m.loadingMsg = "Refreshing list..."
			return tea.Batch(m.spinner.Tick, m.fetchCurrentList())
		}
		return nil
	case "tab":
		// Cycle through list types
		if m.mode == ModeUserList {
			m.currentListType = (m.currentListType + 1) % 5
			m.loading = true
			m.loadingMsg = fmt.Sprintf("Loading %s...", m.currentListType.String())
			return tea.Batch(m.spinner.Tick, m.fetchCurrentList())
		}
		return nil
	case "L":
		// Logout (capital L)
		if m.accessToken != "" {
			homeDir, _ := os.UserHomeDir()
			tokenPath := fmt.Sprintf("%s/%s", homeDir, tokenFile)
			os.Remove(tokenPath)
			m.accessToken = ""
			m.username = ""
			m.userID = 0
			m.mode = ModeLogin
			m.loginMsg = "Logged out. Press 'l' to login or 's' to browse"
		}
		return nil
	}

	// Mode-specific keys
	switch m.mode {
	case ModeUserList:
		return m.handleUserListKeys(msg)
	case ModeAnimeSearch:
		return m.handleAnimeKeys(msg)
	case ModeTorrents:
		return m.handleTorrentKeys(msg)
	case ModeDownloads:
		return m.handleDownloadKeys(msg)
	}

	return nil
}

func (m *model) handleUserListKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if m.userEntryCursor > 0 {
			m.userEntryCursor--
			m.viewport.SetContent(m.renderContent())
		}
	case "down", "j":
		if m.userEntryCursor < len(m.userEntries)-1 {
			m.userEntryCursor++
			m.viewport.SetContent(m.renderContent())
		}
	case "enter":
		if m.userEntryCursor < len(m.userEntries) {
			entry := m.userEntries[m.userEntryCursor]
			m.selectedAnime = &entry.Media
			title := entry.Media.Title.English
			if title == "" {
				title = entry.Media.Title.Romaji
			}
			m.loading = true
			m.loadingMsg = "looking for torrets..."
			return tea.Batch(m.spinner.Tick, performTorrentSearch(title))
		}
	}
	return nil
}

func (m *model) handleAnimeKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "n":
		if m.animePage < m.animeTotalPages-1 {
			return performAnimeSearch(m.animeQuery, m.animePage+2)
		}
	case "p":
		if m.animePage > 0 {
			return performAnimeSearch(m.animeQuery, m.animePage)
		}
	case "up", "k":
		if m.animeCursor > 0 {
			m.animeCursor--
			m.viewport.SetContent(m.renderContent())
		}
	case "down", "j":
		if m.animeCursor < len(m.anime)-1 {
			m.animeCursor++
			m.viewport.SetContent(m.renderContent())
		}
	case "enter":
		if m.animeCursor < len(m.anime) {
			m.selectedAnime = &m.anime[m.animeCursor]
			title := m.selectedAnime.Title.English
			if title == "" {
				title = m.selectedAnime.Title.Romaji
			}
			return performTorrentSearch(title)
		}
	}
	return nil
}

func (m *model) handleTorrentKeys(msg tea.KeyMsg) tea.Cmd {
	perPage := 20
	visibleTorrents := m.visibleTorrents(perPage)

	switch msg.String() {
	case "n":
		if m.torrentPage < m.totalTorrentPages(perPage)-1 {
			m.torrentPage++
			m.torrentCursor = 0
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
		}
	case "p":
		if m.torrentPage > 0 {
			m.torrentPage--
			m.torrentCursor = 0
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
		}
	case "up", "k":
		if m.torrentCursor > 0 {
			m.torrentCursor--
			m.viewport.SetContent(m.renderContent())
			m.ensureCursorVisible(3)
		}
	case "down", "j":
		if m.torrentCursor < len(visibleTorrents)-1 {
			m.torrentCursor++
			m.viewport.SetContent(m.renderContent())
			m.ensureCursorVisible(3)
		}
	case "enter":
		actualIndex := m.torrentPage*perPage + m.torrentCursor
		if actualIndex < len(m.torrents) {
			source := m.torrents[actualIndex].source()
			if source == "" || m.torrentClient == nil {
				m.statusMsg = "Can't stream this torrent (no magnet link or torrent client unavailable)"
				return nil
			}
			m.loading = true
			m.loadingMsg = "Fetching torrent metadata..."
			return tea.Batch(m.spinner.Tick, m.startTorrentStream(source), m.startTicking())
		}
		return nil
	case "d":
		// Download everything marked with Space, or the torrent under the cursor
		indexes := make([]int, 0, len(m.selectedTorrents))
		for i := range m.selectedTorrents {
			indexes = append(indexes, i)
		}
		if len(indexes) == 0 {
			indexes = append(indexes, m.torrentPage*perPage+m.torrentCursor)
		}
		return m.queueDownloads(indexes)
	case " ":
		actualIndex := m.torrentPage*perPage + m.torrentCursor
		if _, ok := m.selectedTorrents[actualIndex]; ok {
			delete(m.selectedTorrents, actualIndex)
		} else {
			m.selectedTorrents[actualIndex] = struct{}{}
		}
		m.viewport.SetContent(m.renderContent())
	}
	return nil
}

func (m *model) ensureCursorVisible(lineHeight int) {
	var cursorY int
	switch m.mode {
	case ModeUserList:
		cursorY = m.userEntryCursor * lineHeight
	case ModeAnimeSearch:
		cursorY = m.animeCursor * lineHeight
	case ModeTorrents:
		cursorY = m.torrentCursor * lineHeight
	case ModeDownloads:
		cursorY = downloadsHeaderLines + m.downloadCursor*lineHeight
	}

	if cursorY < m.viewport.YOffset {
		m.viewport.YOffset = cursorY
	}

	if cursorY > m.viewport.YOffset+m.viewport.Height-lineHeight {
		m.viewport.YOffset = cursorY - m.viewport.Height + lineHeight
	}

	if m.viewport.YOffset < 0 {
		m.viewport.YOffset = 0
	}
	if m.viewport.YOffset > m.viewport.TotalLineCount()-m.viewport.Height {
		m.viewport.YOffset = max(0, m.viewport.TotalLineCount()-m.viewport.Height)
	}
}

// queueDownloads starts full downloads for the given torrent result indexes
func (m *model) queueDownloads(indexes []int) tea.Cmd {
	if m.torrentClient == nil {
		m.statusMsg = "Torrent client unavailable"
		return nil
	}
	var cmds []tea.Cmd
	for _, i := range indexes {
		if i < 0 || i >= len(m.torrents) {
			continue
		}
		if source := m.torrents[i].source(); source != "" {
			cmds = append(cmds, m.torrentClient.AddAsync(source, tc.ModeDownload))
		}
	}
	if len(cmds) == 0 {
		m.statusMsg = "Nothing to download"
		return nil
	}
	m.selectedTorrents = make(map[int]struct{})
	m.viewport.SetContent(m.renderContent())
	m.statusMsg = fmt.Sprintf("Queued %d download(s), press D to view", len(cmds))
	return tea.Batch(append(cmds, m.startTicking())...)
}

func (m *model) handleDownloadKeys(msg tea.KeyMsg) tea.Cmd {
	if len(m.downloads) == 0 || m.torrentClient == nil {
		return nil
	}
	m.downloadCursor = min(m.downloadCursor, len(m.downloads)-1)
	selected := m.downloads[m.downloadCursor]

	rerender := func() {
		m.refreshDownloads()
		m.viewport.SetContent(m.renderContent())
		m.ensureCursorVisible(downloadEntryLines)
	}

	switch msg.String() {
	case "up", "k":
		if m.downloadCursor > 0 {
			m.downloadCursor--
			rerender()
		}
	case "down", "j":
		if m.downloadCursor < len(m.downloads)-1 {
			m.downloadCursor++
			rerender()
		}
	case "enter":
		t, err := m.torrentClient.Torrent(selected.InfoHash)
		if err != nil {
			m.statusMsg = err.Error()
			return nil
		}
		if t.Info() == nil {
			m.statusMsg = "Still fetching metadata, try again in a moment"
			return nil
		}
		vid := tc.GetLargestVideoFile(t)
		if vid == nil {
			m.statusMsg = "No video file in this torrent"
			return nil
		}
		m.streamURL = m.torrentClient.ServeTorrentEpisode(t, vid.DisplayPath())
		m.statusMsg = "Streaming " + vid.DisplayPath()
		return openVideoPlayer(m.streamURL)
	case " ", "p":
		paused, err := m.torrentClient.TogglePause(selected.InfoHash)
		if err != nil {
			m.statusMsg = err.Error()
		} else if paused {
			m.statusMsg = "Paused " + selected.Name
		} else {
			m.statusMsg = "Resumed " + selected.Name
		}
		rerender()
	case "d":
		if err := m.torrentClient.StartDownload(selected.InfoHash); err != nil {
			m.statusMsg = err.Error()
		} else {
			m.statusMsg = "Downloading all of " + selected.Name
		}
		rerender()
	case "x":
		if err := m.torrentClient.Remove(selected.InfoHash, false); err != nil {
			m.statusMsg = err.Error()
		} else {
			m.statusMsg = "Removed " + selected.Name + " (files kept)"
		}
		rerender()
	case "X":
		if m.confirmDelete != selected.InfoHash {
			m.confirmDelete = selected.InfoHash
			m.statusMsg = "Press X again to remove and DELETE the files of " + selected.Name
			return nil
		}
		m.confirmDelete = ""
		if err := m.torrentClient.Remove(selected.InfoHash, true); err != nil {
			m.statusMsg = err.Error()
		} else {
			m.statusMsg = "Deleted " + selected.Name
		}
		rerender()
	case "o":
		dir := m.torrentClient.DownloadDir
		if selected.Path != "" {
			if st, err := os.Stat(selected.Path); err == nil && st.IsDir() {
				dir = selected.Path
			}
		}
		if err := browser.OpenFile(dir); err != nil {
			m.statusMsg = "Couldn't open folder: " + err.Error()
		}
	}
	return nil
}

// quit exits, but asks for a second press while downloads are still running
func (m *model) quit() tea.Cmd {
	if m.torrentClient != nil && m.torrentClient.ActiveCount() > 0 && !m.confirmQuit {
		m.confirmQuit = true
		m.statusMsg = "Downloads are still running, press q again to quit"
		return nil
	}
	return tea.Quit
}
