package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	var statusMsg string
	client := tc.NewTorrentClient(tc.ClientName, "8888")
	client.SetDownloadDir(defaultDownloadDir())
	if err := client.Init(); err != nil {
		statusMsg = fmt.Sprintf("Torrent client unavailable: %v", err)
		client = nil
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
		statusMsg:        statusMsg,
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

// defaultDownloadDir is where full downloads (and stream buffers) are kept.
// Override with SAKUHAKU_DOWNLOAD_DIR.
func defaultDownloadDir() string {
	dir := os.Getenv("SAKUHAKU_DOWNLOAD_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, "Downloads", "SakuHaku")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.wantPosters = m.wantPosters[:0]
	model, cmd := m.update(msg)
	// Rendering may have asked for posters that aren't loaded yet
	if len(m.wantPosters) > 0 {
		cmd = tea.Batch(cmd, posterCmds(m.wantPosters...))
	}
	return model, cmd
}

// startTicking starts the once-a-second download stats refresh if it isn't running
func (m *model) startTicking() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tc.TickProgress()
}

func (m *model) refreshDownloads() {
	if m.torrentClient == nil {
		m.downloads = nil
		return
	}
	m.torrentClient.Refresh()
	m.downloads = m.torrentClient.Downloads()
	if m.downloadCursor >= len(m.downloads) {
		m.downloadCursor = max(0, len(m.downloads)-1)
	}
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case posterLoadedMsg:
		if m.ready && (m.mode == ModeUserList || m.mode == ModeAnimeSearch) {
			m.viewport.SetContent(m.renderContent())
		}
		return m, nil

	case tc.TorrentAddedMsg:
		if msg.Mode == tc.ModeStream {
			m.loading = false
		}
		m.refreshDownloads()
		if m.mode == ModeDownloads {
			m.viewport.SetContent(m.renderContent())
		}
		if msg.Error != nil {
			m.statusMsg = fmt.Sprintf("Error adding torrent: %v", msg.Error)
			return m, nil
		}

		if msg.Mode == tc.ModeDownload {
			m.statusMsg = fmt.Sprintf("Downloading %s", msg.Torrent.Name())
			return m, nil
		}

		vidfile := tc.GetLargestVideoFile(msg.Torrent)
		if vidfile == nil {
			m.torrentClient.StartDownload(msg.Torrent.InfoHash().HexString())
			m.statusMsg = "No video file found in torrent, downloading it instead (D to view)"
			return m, nil
		}
		m.streamURL = m.torrentClient.ServeTorrentEpisode(msg.Torrent, vidfile.DisplayPath())
		m.statusMsg = "Streaming " + vidfile.DisplayPath()
		return m, openVideoPlayer(m.streamURL)

	case tc.TorrentProgressMsg:
		m.ticking = false
		m.refreshDownloads()
		if m.mode == ModeDownloads && m.ready {
			m.viewport.SetContent(m.renderContent())
		}
		if len(m.downloads) > 0 {
			return m, m.startTicking()
		}
		return m, nil

	case videoPlayerOpenedMsg:
		if msg.player == "browser" {
			m.statusMsg = "No video player found (install mpv), opened stream in browser"
		} else {
			m.statusMsg = "Playing in " + msg.player
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
		case ModeDownloads:
			title = titleStyle.Render("⬇ Download Manager")
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
			pageInfo = fmt.Sprintf("%d anime | Tab: switch list | s: search | r: refresh | D: downloads | L: logout | Enter: torrents | q: quit", len(m.userEntries))
		case ModeAnimeSearch:
			pageInfo = fmt.Sprintf("Page %d/%d | s: search | n/p: page | Enter: torrents | D: downloads | Esc: back | q: quit",
				m.animePage+1, m.animeTotalPages)
		case ModeTorrents:
			perPage := 20
			startIdx := m.torrentPage*perPage + 1
			endIdx := min(startIdx+len(m.visibleTorrents(perPage))-1, len(m.torrents))
			pageInfo = fmt.Sprintf("Page %d/%d | %d-%d of %d | Enter: stream | d: download | Space: mark | D: downloads | Esc: back",
				m.torrentPage+1, m.totalTorrentPages(perPage), startIdx, endIdx, len(m.torrents))
		case ModeDownloads:
			pageInfo = "Enter: watch | Space: pause | d: keep | x: remove | X: delete files | o: folder | Esc: back"
		}
		if summary := m.downloadSummary(); summary != "" && m.mode != ModeDownloads {
			pageInfo = summary + " | " + pageInfo
		}
	}

	// Keep the footer on one line: a wrapped footer pushes the viewport off screen
	width := m.viewport.Width
	pageInfo = ansi.Truncate(pageInfo, max(0, width-4), "…")
	info := infoStyle.Render(pageInfo)

	status := ""
	if m.statusMsg != "" {
		status = ansi.Truncate(" "+m.statusMsg+" ", max(0, width-lipgloss.Width(info)-2), "…")
	}
	line := status + strings.Repeat("─", max(0, width-lipgloss.Width(info)-lipgloss.Width(status)))
	return lipgloss.JoinHorizontal(lipgloss.Center, line, info)
}

// downloadSummary is a short "⬇ 2 · 3.1 MB/s" note for the footer
func (m *model) downloadSummary() string {
	active := 0
	var rate float64
	for _, d := range m.downloads {
		if d.State != tc.StateCompleted && d.State != tc.StatePaused {
			active++
			rate += d.DownRate
		}
	}
	if active == 0 {
		return ""
	}
	return fmt.Sprintf("⬇ %d · %s", active, tc.FormatSpeed(int64(rate)))
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
