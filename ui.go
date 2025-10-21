package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
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
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
)

// Bubble Tea Implementation
func initialModel() *model {
	client := tc.NewTorrentClient(tc.ClientName, "8888")
	if err := client.Init(); err != nil {
		fmt.Printf("Failed to initialize torrent client: %v", err)
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	m := &model{
		mode:             ModeLogin,
		selectedTorrents: make(map[int]struct{}),
		loginMsg:         "Press 'l' to login with AniList or 's' to browse without login",
		torrentClient:    client,
		spinner:          s,
		loading:          false,
	}

	// Try to load saved token
	if token, username, userID, err := loadSavedToken(); err == nil {
		m.accessToken = token
		m.username = username
		m.userID = userID
		m.mode = ModeUserList
		m.loginMsg = fmt.Sprintf("Welcome back, %s!", username)
		m.loading = true
		m.loadingMsg = "Loading your anime list..."
	}

	return m
}

func (m *model) Init() tea.Cmd {
	// If we have a token, fetch user list immediately
	if m.accessToken != "" && m.userID != 0 {
		return tea.Batch(m.spinner.Tick, fetchUserAnimeList(m.accessToken, m.userID, "CURRENT"))
	}
	return m.spinner.Tick
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	if m.loading {
		var spinnerCmd tea.Cmd
		m.spinner, spinnerCmd = m.spinner.Update(msg)
		if spinnerCmd != nil {
			cmds = append(cmds, spinnerCmd)
		}
	}

	switch msg := msg.(type) {
	case tc.TorrentAddedMsg:
		m.loading = false
		if msg.Error != nil {
			m.loginMsg = fmt.Sprintf("Error adding torrent: %v", msg.Error)
			return m, nil
		}
		m.activeTorrent = msg.Torrent

		vidfile := tc.GetLargestVideoFile(msg.Torrent)
		if vidfile != nil {
			m.streamURL = m.torrentClient.ServeTorrentEpisode(msg.Torrent, vidfile.DisplayPath())
			return m, openVideoPlayer(m.streamURL)
		} else {
			msg.Torrent.DownloadAll()
			m.streamURL = m.torrentClient.ServeTorrent(msg.Torrent)
			return m, tea.Batch(
				openVideoPlayer(m.streamURL),
				tc.TickProgress(),
			)
		}
	case tc.TorrentProgressMsg:
		if m.activeTorrent != nil {
			stats := m.activeTorrent.Stats()
			m.downloadProgress = float64(stats.BytesReadData.Int64()) / float64(m.activeTorrent.Length()) * 100

			// Continue ticking for progress updates
			if m.downloadProgress < 100 {
				return m, tc.TickProgress()
			}
		}
		return m, nil

	case authSuccessMsg:
		m.accessToken = msg.token
		m.username = msg.username
		m.userID = msg.userID
		m.mode = ModeUserList
		m.loading = true
		m.loadingMsg = "Loading your anime list..."
		m.loginMsg = fmt.Sprintf("Logged in as %s! Loading your anime list...", m.username)

		if err := saveToken(msg.token, msg.username, msg.userID); err != nil {
			m.loginMsg = fmt.Sprintf("Logged in but failed to save token: %v", err)
		}

		if !m.ready {
			m.viewport = viewport.New(80, 24)
			m.ready = true
		}

		return m, tea.Batch(m.spinner.Tick, fetchUserAnimeList(m.accessToken, m.userID, "CURRENT"))

	case authErrorMsg:
		m.loginMsg = fmt.Sprintf("Login failed: %v\nPress 'l' to retry or 's' to browse without login", msg.err)
		return m, nil

	case userListMsg:
		m.loading = false
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
		m.loading = false
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
		m.loading = false
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

		return m, nil

	case tea.KeyMsg:
		cmd = m.handleKey(msg)
		if cmd != nil {
			return m, cmd
		}
		var spinnerCmd tea.Cmd
		m.spinner, spinnerCmd = m.spinner.Update(msg)

		var viewportCmd tea.Cmd
		m.viewport, viewportCmd = m.viewport.Update(msg)

		return m, tea.Batch(spinnerCmd, viewportCmd)
	}

	var viewportCmd tea.Cmd
	m.viewport, viewportCmd = m.viewport.Update(msg)
	if viewportCmd != nil {
		cmds = append(cmds, viewportCmd)
	}

	// Return all commands
	return m, tea.Batch(cmds...)
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

		// Re-render content with new dimensions (images will auto-resize)
		m.viewport.SetContent(m.renderContent())
	}
}

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

func (m *model) renderView() string {
	if m.loading {
		return fmt.Sprintf("\n\n   %s %s\n\n", m.spinner.View(), m.loadingMsg)
	}
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
		return "No anime found."
	}

	// Get selected entry for right panel
	var selectedEntry *UserAnimeEntry
	if m.userEntryCursor < len(m.userEntries) {
		selectedEntry = &m.userEntries[m.userEntryCursor]
	}

	// Calculate dynamic sizes based on viewport
	leftWidth := m.viewport.Width / 2
	rightWidth := m.viewport.Width - leftWidth - 3

	imageWidth := m.viewport.Width / 3
	imageHeight := m.viewport.Height / 2 // Not really used, aspect ratio handles it

	// Left panel - list
	var leftPanel strings.Builder

	// List header
	listTitle := m.currentListType.String()
	if m.username != "" && (m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch) {
		leftPanel.WriteString(fmt.Sprintf("👤 %s's %s\n\n", m.username, listTitle))
	} else {
		leftPanel.WriteString(fmt.Sprintf("📺 %s\n\n", listTitle))
	}

	for i, entry := range m.userEntries {
		cursor := "  "
		if m.userEntryCursor == i {
			cursor = "▶ "
		}

		title := entry.Media.Title.English
		if title == "" {
			title = entry.Media.Title.Romaji
		}

		// Truncate title based on left panel width
		maxTitleWidth := leftWidth - 15
		if maxTitleWidth < 20 {
			maxTitleWidth = 20
		}
		if len(title) > maxTitleWidth {
			title = title[:maxTitleWidth-3] + "..."
		}

		// Show different info based on list type
		var info string
		if m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch {
			episodes := "?"
			if entry.Media.Episodes != nil {
				episodes = fmt.Sprintf("%d", *entry.Media.Episodes)
			}
			progress := fmt.Sprintf("%d/%s", entry.Progress, episodes)
			info = fmt.Sprintf(" (%s)", progress)
		} else {
			if entry.Media.Score != nil {
				info = fmt.Sprintf(" ⭐%d%%", *entry.Media.Score)
			}
		}

		line := fmt.Sprintf("%s%s%s\n", cursor, title, info)
		leftPanel.WriteString(line)
	}

	// Right panel - details
	var rightPanel strings.Builder
	if selectedEntry != nil {
		// Render poster with dynamic sizing
		if selectedEntry.Media.CoverImage.Large != "" {
			poster := getAnimePoster(selectedEntry.Media.CoverImage.Large, imageWidth, imageHeight)
			rightPanel.WriteString(poster)
			rightPanel.WriteString("\n")
		}

		// Title
		title := selectedEntry.Media.Title.English
		if title == "" {
			title = selectedEntry.Media.Title.Romaji
		}

		// Wrap title if too long for right panel
		wrappedTitle := wrapText(title, rightWidth)
		rightPanel.WriteString(fmt.Sprintf("📺 %s\n\n", wrappedTitle))

		// Show different details based on list type
		if m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch {
			// User's personal data
			episodes := "?"
			if selectedEntry.Media.Episodes != nil {
				episodes = fmt.Sprintf("%d", *selectedEntry.Media.Episodes)
			}
			rightPanel.WriteString(fmt.Sprintf("Progress: %d/%s\n", selectedEntry.Progress, episodes))

			if selectedEntry.Score > 0 {
				rightPanel.WriteString(fmt.Sprintf("Your Score: ⭐ %.1f/10\n", selectedEntry.Score))
			}

			rightPanel.WriteString(fmt.Sprintf("Status: %s\n", selectedEntry.Status))

			if selectedEntry.UpdatedAt > 0 {
				rightPanel.WriteString(fmt.Sprintf("Updated: %s\n", formatRelativeTime(selectedEntry.UpdatedAt)))
			}
			rightPanel.WriteString("\n")
		}

		// Anime details
		score := "N/A"
		if selectedEntry.Media.Score != nil {
			score = fmt.Sprintf("%d%%", *selectedEntry.Media.Score)
		}

		rightPanel.WriteString(fmt.Sprintf("Format: %s\n", selectedEntry.Media.Format))
		rightPanel.WriteString(fmt.Sprintf("Avg Score: %s\n", score))

		episodes := "?"
		if selectedEntry.Media.Episodes != nil {
			episodes = fmt.Sprintf("%d", *selectedEntry.Media.Episodes)
		}
		rightPanel.WriteString(fmt.Sprintf("Episodes: %s\n", episodes))

		if selectedEntry.Media.Season != "" {
			year := ""
			if selectedEntry.Media.SeasonYear != nil {
				year = fmt.Sprintf(" %d", *selectedEntry.Media.SeasonYear)
			}
			rightPanel.WriteString(fmt.Sprintf("Season: %s%s\n", selectedEntry.Media.Season, year))
		}

		if selectedEntry.Media.SiteURL != "" {
			rightPanel.WriteString(fmt.Sprintf("\n🔗 %s\n", hyperlink("AniList", selectedEntry.Media.SiteURL)))
		}
	}

	// Combine panels side by side
	return combinePanels(leftPanel.String(), rightPanel.String(), m.viewport.Width)
}

func (m *model) renderAnimeContent() string {
	if len(m.anime) == 0 {
		return "No anime found."
	}

	// Get selected anime for right panel
	var selectedAnime *Anime
	if m.animeCursor < len(m.anime) {
		selectedAnime = &m.anime[m.animeCursor]
	}

	// Calculate dynamic sizes
	leftWidth := m.viewport.Width / 2
	rightWidth := m.viewport.Width - leftWidth - 3

	// Image dimensions - MAXIMUM SIZE for best quality
	imageWidth := rightWidth - 4
	if imageWidth > 100 {
		imageWidth = 100
	}
	if imageWidth < 40 {
		imageWidth = 40
	}

	imageHeight := m.viewport.Height - 15
	if imageHeight > 60 {
		imageHeight = 60
	}
	if imageHeight < 30 {
		imageHeight = 30
	}

	// Left panel - list
	var leftPanel strings.Builder
	leftPanel.WriteString("📺 Anime Search Results\n\n")

	for i, a := range m.anime {
		cursor := "  "
		if m.animeCursor == i {
			cursor = "▶ "
		}

		title := a.Title.English
		if title == "" {
			title = a.Title.Romaji
		}

		// Truncate based on left width
		maxLen := leftWidth - 20
		if maxLen < 20 {
			maxLen = 20
		}
		if len(title) > maxLen {
			title = title[:maxLen-3] + "..."
		}

		score := "N/A"
		if a.Score != nil {
			score = fmt.Sprintf("%d%%", *a.Score)
		}

		line := fmt.Sprintf("%s%s (⭐ %s)\n", cursor, title, score)
		leftPanel.WriteString(line)
	}

	// Right panel - details with image
	var rightPanel strings.Builder
	if selectedAnime != nil {
		// Render poster with dynamic sizing
		if selectedAnime.CoverImage.Large != "" {
			poster := getAnimePoster(selectedAnime.CoverImage.Large, imageWidth, imageHeight)
			rightPanel.WriteString(poster)
			rightPanel.WriteString("\n")
		}

		// Title
		title := selectedAnime.Title.English
		if title == "" {
			title = selectedAnime.Title.Romaji
		}
		wrappedTitle := wrapText(title, rightWidth)
		rightPanel.WriteString(fmt.Sprintf("📺 %s\n\n", wrappedTitle))

		// Details
		episodes := "?"
		if selectedAnime.Episodes != nil {
			episodes = fmt.Sprintf("%d", *selectedAnime.Episodes)
		}

		score := "N/A"
		if selectedAnime.Score != nil {
			score = fmt.Sprintf("%d%%", *selectedAnime.Score)
		}

		year := "?"
		if selectedAnime.SeasonYear != nil {
			year = fmt.Sprintf("%d", *selectedAnime.SeasonYear)
		}

		rightPanel.WriteString(fmt.Sprintf("Format: %s\n", selectedAnime.Format))
		rightPanel.WriteString(fmt.Sprintf("Episodes: %s\n", episodes))
		rightPanel.WriteString(fmt.Sprintf("Score: ⭐ %s\n", score))
		rightPanel.WriteString(fmt.Sprintf("Season: %s %s\n", selectedAnime.Season, year))
		rightPanel.WriteString(fmt.Sprintf("Status: %s\n\n", selectedAnime.Status))

		if selectedAnime.SiteURL != "" {
			rightPanel.WriteString(fmt.Sprintf("🔗 %s\n", hyperlink("AniList", selectedAnime.SiteURL)))
		}
	}

	// Combine panels side by side
	return combinePanels(leftPanel.String(), rightPanel.String(), m.viewport.Width)
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

		// Show source badge
		sourceBadge := "📦"
		if t.Source == "nyaa" {
			sourceBadge = "🐱"
		}

		line := fmt.Sprintf("%s [%s] %s %s\n   💾 %s | 🌱 %s | 🧲 %s | 📤 %s\n\n",
			cursor, checked, sourceBadge, t.Title,
			formatBytes(t.TotalSize),
			toString(t.Seeders),
			toString(t.Leechers),
			hyperlink("magnet", t.MagnetURI))
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

func combinePanels(left, right string, totalWidth int) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	// Calculate panel widths
	leftWidth := totalWidth / 2
	// rightWidth := totalWidth - leftWidth - 3 // Account for separator

	var result strings.Builder

	maxLines := max(len(leftLines), len(rightLines))
	for i := 0; i < maxLines; i++ {
		// Left panel
		leftLine := ""
		if i < len(leftLines) {
			leftLine = leftLines[i]
		}

		// Remove ANSI codes for length calculation
		leftDisplayLen := visualLength(leftLine)

		// Truncate if too long
		if leftDisplayLen > leftWidth {
			leftLine = truncateToWidth(leftLine, leftWidth-3) + "..."
			leftDisplayLen = leftWidth
		}

		// Pad to width
		padding := leftWidth - leftDisplayLen
		if padding < 0 {
			padding = 0
		}

		result.WriteString(leftLine)
		result.WriteString(strings.Repeat(" ", padding))
		result.WriteString(" │ ")

		// Right panel
		if i < len(rightLines) {
			result.WriteString(rightLines[i])
		}
		result.WriteString("\n")
	}

	return result.String()
}

// Calculate visual length (excluding ANSI codes)
func visualLength(s string) int {
	inEscape := false
	length := 0

	for _, r := range s {
		if r == '\033' {
			inEscape = true
		} else if inEscape {
			if r == 'm' || r == '\\' {
				inEscape = false
			}
		} else {
			length++
		}
	}

	return length
}

// Truncate string to visual width
func truncateToWidth(s string, width int) string {
	inEscape := false
	length := 0
	result := strings.Builder{}

	for _, r := range s {
		if r == '\033' {
			inEscape = true
			result.WriteRune(r)
		} else if inEscape {
			result.WriteRune(r)
			if r == 'm' || r == '\\' {
				inEscape = false
			}
		} else {
			if length >= width {
				break
			}
			result.WriteRune(r)
			length++
		}
	}

	return result.String()
}

func (m *model) footerView() string {
	var pageInfo string
	if m.searchMode {
		pageInfo = "Enter to search | Esc to cancel"
	} else {
		switch m.mode {
		case ModeUserList:
			pageInfo = fmt.Sprintf("%d anime | Tab: switch list |s: search | r: refresh | L: logout | Enter: torrents | q: quit", len(m.userEntries))
		case ModeAnimeSearch:
			pageInfo = fmt.Sprintf("Page %d/%d | s: search | n/p: page | Enter: torrents | Esc: back | q: quit",
				m.animePage+1, m.animeTotalPages)
		case ModeTorrents:
			perPage := 20
			startIdx := m.torrentPage*perPage + 1
			endIdx := min(startIdx+len(m.visibleTorrents(perPage))-1, len(m.torrents))
			pageInfo = fmt.Sprintf("Page %d/%d | %d-%d of %d | Space/Enter: select | Esc: back | q: quit",
				m.torrentPage+1, m.totalTorrentPages(perPage), startIdx, endIdx, len(m.torrents))
			// Add streaming status if active
			if m.activeTorrent != nil && m.downloadProgress < 100 {
				pageInfo += fmt.Sprintf(" | Downloading: %.1f%%", m.downloadProgress)
			} else if m.streamURL != "" {
				pageInfo += " | Streaming active"
			}
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

func wrapText(text string, width int) string {
	if len(text) <= width {
		return text
	}

	var result strings.Builder
	words := strings.Fields(text)
	lineLen := 0

	for i, word := range words {
		wordLen := len(word)

		if lineLen+wordLen+1 > width {
			result.WriteString("\n")
			lineLen = 0
		} else if i > 0 {
			result.WriteString(" ")
			lineLen++
		}

		result.WriteString(word)
		lineLen += wordLen
	}

	return result.String()
}
