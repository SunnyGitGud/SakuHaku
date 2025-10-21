package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
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

	// Common keys
	switch msg.String() {
	case "ctrl+c", "q":
		return tea.Quit
	case "esc":
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
		return tea.Quit
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
	}

	return nil
}

func (m *model) handleUserListKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if m.userEntryCursor > 0 {
			m.userEntryCursor--
			m.viewport.SetContent(m.renderContent())
			m.ensureCursorVisible(4)
		}
	case "down", "j":
		if m.userEntryCursor < len(m.userEntries)-1 {
			m.userEntryCursor++
			m.viewport.SetContent(m.renderContent())
			m.ensureCursorVisible(4)
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
			m.ensureCursorVisible(4)
		}
	case "down", "j":
		if m.animeCursor < len(m.anime)-1 {
			m.animeCursor++
			m.viewport.SetContent(m.renderContent())
			m.ensureCursorVisible(4)
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
			selectedTorrent := m.torrents[actualIndex]
			m.loading = true
			m.loadingMsg = "Adding Torrent"
			return tea.Batch(m.spinner.Tick, m.startTorrentStream(selectedTorrent.MagnetURI))
		}
		return nil
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
