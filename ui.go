package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderRight(true).
			Padding(0, 1)
	infoStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderLeft(true).
			Padding(0, 1)
)

// Bubble Tea Implementation
func initialModel() *model {
	m := &model{
		mode:             ModeLogin,
		selectedTorrents: make(map[int]struct{}),
		loginMsg:         "Press 'l' to login with AniList or 's' to browse without login",
	}

	// Try to load saved token
	if token, username, userID, err := loadSavedToken(); err == nil {
		m.accessToken = token
		m.username = username
		m.userID = userID
		m.mode = ModeUserList
		m.loginMsg = fmt.Sprintf("Welcome back, %s!", username)
	}

	return m
}

func (m *model) Init() tea.Cmd {
	// If we have a token, fetch user list immediately
	if m.accessToken != "" && m.userID != 0 {
		return fetchUserAnimeList(m.accessToken, m.userID, "CURRENT")
	}
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case authSuccessMsg:
		m.accessToken = msg.token
		m.username = msg.username
		m.userID = msg.userID
		m.mode = ModeUserList
		m.loginMsg = fmt.Sprintf("Logged in as %s! Loading your anime list...", m.username)

		if err := saveToken(msg.token, msg.username, msg.userID); err != nil {
			m.loginMsg = fmt.Sprintf("Logged in but failed to save token: %v", err)
		}

		if !m.ready {
			m.viewport = viewport.New(80, 24)
			m.ready = true
		}

		return m, fetchUserAnimeList(m.accessToken, m.userID, "CURRENT")

	case authErrorMsg:
		m.loginMsg = fmt.Sprintf("Login failed: %v\nPress 'l' to retry or 's' to browse without login", msg.err)
		return m, nil

	case userListMsg:
		m.mode = ModeUserList
		m.userEntries = []UserAnimeEntry(msg)

		if !m.ready {
			m.viewport = viewport.New(80, 24)
			m.ready = true
		}

		content := m.renderContent()
		m.viewport.SetContent(content)
		m.viewport.GotoTop()

		return m, nil

	case animeSearchResultMsg:
		m.anime = msg.anime
		m.animeCursor = 0
		m.animePage = msg.page
		m.animeTotalPages = msg.totalPages
		if m.ready {
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
		}
		return m, nil

	case torrentSearchResultMsg:
		m.mode = ModeTorrents
		m.torrents = []Torrent(msg)
		m.torrentCursor = 0
		m.torrentPage = 0
		m.selectedTorrents = make(map[int]struct{})
		if m.ready {
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.handleWindowResize(msg)

	case tea.KeyMsg:
		cmd = m.handleKey(msg)
		if cmd != nil {
			return m, cmd
		}
	}

	var viewportCmd tea.Cmd
	m.viewport, viewportCmd = m.viewport.Update(msg)
	return m, viewportCmd
}

func (m *model) View() string {
	return m.renderView()
}

// UI Handlers
func (m *model) handleWindowResize(msg tea.WindowSizeMsg) {
	headerHeight := lipgloss.Height(m.headerView())
	footerHeight := lipgloss.Height(m.footerView())
	verticalMargin := headerHeight + footerHeight

	if !m.ready {
		m.viewport = viewport.New(msg.Width, msg.Height-verticalMargin)
		m.viewport.YPosition = headerHeight
		m.viewport.SetContent(m.renderContent())
		m.ready = true
	} else {
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - verticalMargin
	}
}

func (m *model) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Login mode
	if m.mode == ModeLogin {
		switch msg.String() {
		case "l":
			m.loginMsg = "Opening browser for authentication..."
			return startOAuthFlow()
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
				return performAnimeSearch(m.animeQuery, 1)
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
		if m.mode == ModeUserList && m.accessToken != "" {
			return fetchUserAnimeList(m.accessToken, m.userID, "CURRENT")
		}
		return nil
	case "L":
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
			return performTorrentSearch(title)
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
	case "enter", " ":
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

func (m *model) renderView() string {
	if m.mode == ModeLogin {
		return fmt.Sprintf("\n\n  🎬 AniList Torrent Browser\n\n  %s\n\n", m.loginMsg)
	}

	if !m.ready {
		return "\n  Loading..."
	}
	return fmt.Sprintf("%s\n%s\n%s",
		m.headerView(),
		m.viewport.View(),
		m.footerView())
}

// View Components
func (m *model) renderContent() string {
	switch m.mode {
	case ModeUserList:
		return m.renderUserListContent()
	case ModeAnimeSearch:
		return m.renderAnimeContent()
	case ModeTorrents:
		return m.renderTorrentContent()
	}
	return ""
}

func (m *model) renderUserListContent() string {
	if len(m.userEntries) == 0 {
		return "No anime in your list."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👤 %s's Continue Watching\n\n", m.username))

	for i, entry := range m.userEntries {
		cursor := " "
		if m.userEntryCursor == i {
			cursor = ">"
		}

		title := entry.Media.Title.English
		if title == "" {
			title = entry.Media.Title.Romaji
		}

		episodes := "?"
		if entry.Media.Episodes != nil {
			episodes = fmt.Sprintf("%d", *entry.Media.Episodes)
		}

		progress := fmt.Sprintf("%d/%s", entry.Progress, episodes)

		line := fmt.Sprintf("%s %s\n   📺 Progress: %s | ⭐ %.1f | 📅 Updated: %s\n\n",
			cursor, title, progress, entry.Score, entry.UpdatedAt)
		sb.WriteString(line)
	}
	return sb.String()
}

func (m *model) renderAnimeContent() string {
	if len(m.anime) == 0 {
		return "No anime found."
	}

	var sb strings.Builder
	for i, a := range m.anime {
		cursor := " "
		if m.animeCursor == i {
			cursor = ">"
		}

		title := a.Title.English
		if title == "" {
			title = a.Title.Romaji
		}

		episodes := "?"
		if a.Episodes != nil {
			episodes = fmt.Sprintf("%d", *a.Episodes)
		}

		score := "N/A"
		if a.Score != nil {
			score = fmt.Sprintf("%d%%", *a.Score)
		}

		year := ""
		if a.SeasonYear != nil {
			year = fmt.Sprintf("%d", *a.SeasonYear)
		}

		line := fmt.Sprintf("%s %s\n   📺 %s | 🎬 %s eps | ⭐ %s | 📅 %s %s\n\n",
			cursor, title, a.Format, episodes, score, a.Season, year)
		sb.WriteString(line)
	}
	return sb.String()
}

func (m *model) renderTorrentContent() string {
	perPage := 20
	visible := m.visibleTorrents(perPage)

	if len(visible) == 0 {
		return "No torrents found for this anime."
	}

	var sb strings.Builder

	if m.selectedAnime != nil {
		title := m.selectedAnime.Title.English
		if title == "" {
			title = m.selectedAnime.Title.Romaji
		}
		sb.WriteString(fmt.Sprintf("🎬 Torrents for: %s\n\n", title))
	}

	for i, t := range visible {
		cursor := " "
		if m.torrentCursor == i {
			cursor = ">"
		}

		actualIndex := m.torrentPage*perPage + i
		checked := " "
		if _, ok := m.selectedTorrents[actualIndex]; ok {
			checked = "x"
		}

		line := fmt.Sprintf("%s [%s] %s\n   💾 %s | 🌱 %s | 🧲 %s\n\n",
			cursor, checked, t.Title, formatBytes(t.TotalSize), toString(t.Seeders), hyperlink("magnet", t.MagnetURI))
		sb.WriteString(line)
	}
	return sb.String()
}

func (m *model) headerView() string {
	var title string
	if m.searchMode {
		title = titleStyle.Render(fmt.Sprintf("Search Anime: %s_", m.searchInput))
	} else {
		switch m.mode {
		case ModeUserList:
			title = titleStyle.Render(fmt.Sprintf("👤 %s's List", m.username))
		case ModeAnimeSearch:
			title = titleStyle.Render("🔍 Browse Anime")
		case ModeTorrents:
			title = titleStyle.Render("📦 Torrent Results")
		}
	}
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(title)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, line)
}

func (m *model) footerView() string {
	var pageInfo string
	if m.searchMode {
		pageInfo = "Enter to search | Esc to cancel"
	} else {
		switch m.mode {
		case ModeUserList:
			pageInfo = fmt.Sprintf("%d anime | s: search | r: refresh | L: logout | Enter: torrents | q: quit", len(m.userEntries))
		case ModeAnimeSearch:
			pageInfo = fmt.Sprintf("Page %d/%d | s: search | n/p: page | Enter: torrents | Esc: back | q: quit",
				m.animePage+1, m.animeTotalPages)
		case ModeTorrents:
			perPage := 20
			startIdx := m.torrentPage*perPage + 1
			endIdx := min(startIdx+len(m.visibleTorrents(perPage))-1, len(m.torrents))
			pageInfo = fmt.Sprintf("Page %d/%d | %d-%d of %d | Space/Enter: select | Esc: back | q: quit",
				m.torrentPage+1, m.totalTorrentPages(perPage), startIdx, endIdx, len(m.torrents))
		}
	}

	info := infoStyle.Render(pageInfo)
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)))
	return lipgloss.JoinHorizontal(lipgloss.Center, line, info)
}

// Pagination Helpers
func (m *model) totalTorrentPages(perPage int) int {
	if len(m.torrents) == 0 {
		return 1
	}
	return (len(m.torrents) + perPage - 1) / perPage
}

func (m *model) visibleTorrents(perPage int) []Torrent {
	start := m.torrentPage * perPage
	end := min(start+perPage, len(m.torrents))
	if start >= len(m.torrents) {
		return nil
	}
	return m.torrents[start:end]
}
