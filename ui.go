package main

import (
	"fmt"
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
