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
	"github.com/sunnygitgud/sakuhaku/discord"
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
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// Bubble Tea Implementation
func initialModel() *model {
	var statusMsg string
	client := tc.NewTorrentClient(tc.ClientName, "8888")
	client.SetDownloadDir(defaultDownloadDir())
	client.HTTPProxy = proxyFunc()
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
		loginMsg:         "Press 'l' to login with AniList, 's' to browse without login or 'J' to join a watch-together room",
		torrentClient:    client,
		spinner:          s,
		loading:          false,
		statusMsg:        statusMsg,
		torrentCtx:       make(map[string]*streamContext),
	}
	if cfg.DiscordClientID != "" && !cfg.NoDiscord {
		m.presence = discord.New(cfg.DiscordClientID)
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
	var join tea.Cmd
	if cfg.JoinLink != "" {
		join = m.startJoin(cfg.JoinLink)
	}
	if m.accessToken != "" && m.userID != 0 {
		return tea.Batch(m.spinner.Tick, fetchUserAnimeList(m.accessToken, m.userID, "CURRENT"), join)
	}
	return tea.Batch(m.spinner.Tick, join)
}

// defaultDownloadDir is where full downloads (and stream buffers) are kept.
// Override with -download-dir or SAKUHAKU_DOWNLOAD_DIR.
func defaultDownloadDir() string {
	dir := cfg.DownloadDir
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
	if m.fitViewport() {
		m.viewport.SetContent(m.renderContent())
	}
	if presence := m.updatePresence(); presence != nil {
		cmd = tea.Batch(cmd, presence)
	}
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

		if ctx, ok := msg.Tag.(*streamContext); ok && ctx != nil {
			m.torrentCtx[msg.Torrent.InfoHash().HexString()] = ctx
		}

		if msg.Mode == tc.ModeDownload {
			m.statusMsg = fmt.Sprintf("Downloading %s", msg.Torrent.Name())
			return m, nil
		}

		ctx, _ := msg.Tag.(*streamContext)
		return m, m.playTorrent(msg.Torrent, ctx)

	case tc.TorrentProgressMsg:
		m.ticking = false
		m.refreshDownloads()
		var trackCmd tea.Cmd
		if pb := m.playback; pb != nil && pb.Running {
			pb.readStatus()
			trackCmd = m.maybeTrack(pb, false)
		}
		if (m.mode == ModeDownloads || m.mode == ModeStreaming) && m.ready {
			m.viewport.SetContent(m.renderContent())
		}
		if len(m.downloads) > 0 || (m.playback != nil && m.playback.Running) {
			return m, tea.Batch(trackCmd, m.startTicking())
		}
		return m, trackCmd

	case playerStartedMsg:
		msg.apply()
		if msg.pb.invite != nil {
			m.loading = false
		}
		switch {
		case msg.pb.Player == "browser":
			m.statusMsg = "No video player found (install mpv), opened the stream in your browser"
		case msg.err != nil:
			m.statusMsg = "Couldn't start player: " + msg.err.Error()
		default:
			m.statusMsg = "Playing in " + msg.pb.Player
			if msg.pb.ResumedFrom > 0 {
				m.statusMsg += " (resumed at " + formatClock(msg.pb.ResumedFrom) + ")"
			}
		}
		if m.mode == ModeStreaming {
			m.viewport.SetContent(m.renderContent())
		}
		if msg.err != nil || msg.pb.proc == nil {
			return m, nil
		}
		if msg.pb.invite != nil {
			if msg.pb.Player != "mpv" {
				m.statusMsg = "Watch together needs mpv, playing on your own"
				return m, waitForPlayer(msg.pb)
			}
			m.statusMsg = "Connecting to the room..."
			return m, tea.Batch(waitForPlayer(msg.pb), joinRoom(msg.pb))
		}
		return m, waitForPlayer(msg.pb)

	case playerExitedMsg:
		pb := msg.pb
		pb.Running = false
		pb.readStatus()
		history.save(pb)
		trackCmd := m.maybeTrack(pb, false)
		if pb == m.playback {
			trackCmd = tea.Batch(trackCmd, m.closeRoom())
		}
		pb.cleanup()
		if pb == m.playback {
			m.statusMsg = "Player closed"
			if pb.Pos > 0 && !pb.EOF {
				m.statusMsg += " at " + formatClock(pb.Pos) + ", w to resume"
			}
		}
		if m.mode == ModeStreaming {
			m.viewport.SetContent(m.renderContent())
		}
		return m, trackCmd

	case joinResolvedMsg, roomStartedMsg, roomStatusMsg:
		cmd, _ := m.handleRoomMsg(msg)
		return m, cmd

	case presenceMsg:
		// Discord not running is normal; mention a failure once, not every update
		if msg.err != nil && !m.presenceWarned {
			m.presenceWarned = true
			m.statusMsg = "Discord presence unavailable: " + msg.err.Error()
		}
		if msg.err != nil {
			m.presenceKey = "" // try again on the next change
		}
		return m, nil

	case updateProgressMsg:
		text := ""
		switch {
		case msg.err != nil:
			text = "AniList update failed: " + msg.err.Error()
		case msg.skipped:
			text = fmt.Sprintf("AniList already has episode %d of %s as watched", msg.progress, msg.title)
		default:
			text = fmt.Sprintf("✓ AniList: %s episode %d watched (%s)", msg.title, msg.progress, strings.ToLower(msg.status))
			m.applyLocalProgress(msg.mediaID, msg.progress, msg.status)
		}
		m.statusMsg = text
		if pb := m.playback; pb != nil && pb.Anime != nil && pb.Anime.ID == msg.mediaID {
			pb.TrackMsg = text
		}
		if m.ready {
			m.viewport.SetContent(m.renderContent())
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

	case listPageMsg:
		m.loading = false
		if msg.err != nil {
			m.statusMsg = "Couldn't load list: " + msg.err.Error()
			return m, nil
		}
		m.mode = ModeUserList
		m.userEntries = msg.entries
		m.userEntryCursor = 0
		m.listOffset = 0
		m.listPage = msg.page
		m.listLastPage = msg.lastPage
		m.listHasNext = msg.hasNext

		if !m.ready {
			m.viewport = viewport.New(80, 24)
			m.ready = true
		}
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoTop()
		return m, nil

	case userListMsg:
		m.loading = false
		m.mode = ModeUserList
		m.userEntries = []UserAnimeEntry(msg)
		m.userEntryCursor = min(m.userEntryCursor, max(0, len(m.userEntries)-1))
		m.listPage, m.listLastPage, m.listHasNext = 0, 0, false

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
		if msg.err != nil {
			m.statusMsg = "Search failed: " + msg.err.Error()
			return m, nil
		}
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
		if msg.err != nil {
			m.statusMsg = "Torrent search failed: " + msg.err.Error() + " (try -nyaa with a mirror or -proxy)"
		}
		m.mode = ModeTorrents
		m.allTorrents = msg.torrents
		m.epFilter = m.pendingEpFilter
		m.pendingEpFilter = 0
		m.applyTorrentFilters()
		if m.epFilter > 0 && len(m.torrents) == 0 && len(m.allTorrents) > 0 {
			m.statusMsg = fmt.Sprintf("Nothing found for episode %d, showing all", m.epFilter)
			m.epFilter = 0
			m.applyTorrentFilters()
		} else if m.epFilter > 0 {
			m.statusMsg = fmt.Sprintf("Showing your next episode (%d), E to show all", m.epFilter)
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
	m.termWidth, m.termHeight = msg.Width, msg.Height
	if !m.ready {
		m.viewport = viewport.New(msg.Width, 1)
		m.ready = true
	}
	m.viewport.Width = msg.Width
	m.fitViewport()
	// Re-render content with new dimensions (images will auto-resize)
	m.viewport.SetContent(m.renderContent())
}

// fitViewport sizes the viewport to what the header and footer leave free.
// Their heights differ between screens (the login screen has no title), so
// this runs on every update rather than only on resize.
func (m *model) fitViewport() bool {
	if !m.ready || m.termHeight == 0 {
		return false
	}
	headerHeight := lipgloss.Height(m.headerView())
	footerHeight := lipgloss.Height(m.footerView())
	h := max(1, m.termHeight-headerHeight-footerHeight)
	if h == m.viewport.Height && m.viewport.YPosition == headerHeight {
		return false
	}
	m.viewport.Height = h
	m.viewport.YPosition = headerHeight
	return true
}

func (m *model) headerView() string {
	var title string
	if m.searchMode {
		title = titleStyle.Render(fmt.Sprintf("Search Anime: %s_", m.searchInput))
	} else if m.joinInputMode {
		link := m.joinInput
		if w := m.viewport.Width - 30; w > 10 && len(link) > w {
			link = "…" + link[len(link)-w:]
		}
		title = titleStyle.Render(fmt.Sprintf("Paste room link: %s_", link))
	} else if m.epInputMode {
		title = titleStyle.Render(fmt.Sprintf("Episode (empty = any): %s_", m.epInput))
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
		case ModeStreaming:
			title = titleStyle.Render("▶ Now Streaming")
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
			if m.listPage > 0 {
				pageInfo = fmt.Sprintf("Page %d/%d | n/p: page | ", m.listPage, max(m.listPage, m.listLastPage)) + pageInfo
			}
		case ModeAnimeSearch:
			pageInfo = fmt.Sprintf("Page %d/%d | s: search | n/p: page | Enter: torrents | D: downloads | Esc: back | q: quit",
				m.animePage+1, m.animeTotalPages)
		case ModeTorrents:
			perPage := 20
			startIdx := m.torrentPage*perPage + 1
			endIdx := min(startIdx+len(m.visibleTorrents(perPage))-1, len(m.torrents))
			pageInfo = fmt.Sprintf("Page %d/%d | %d-%d of %d | Enter: stream | d: download | Space: mark | n/p: page | D: downloads | Esc: back",
				m.torrentPage+1, m.totalTorrentPages(perPage), startIdx, endIdx, len(m.torrents))
		case ModeStreaming:
			pageInfo = "w: (re)open player | s: stop | W: watch together | c: copy link | m: mark watched | d: keep | Esc: back"
		case ModeDownloads:
			pageInfo = "Enter: watch | Space: pause | d: keep | x: remove | X: delete files | o: folder | Esc: back"
		}
		if summary := m.downloadSummary(); summary != "" && m.mode != ModeDownloads && m.mode != ModeStreaming {
			pageInfo = summary + " | " + pageInfo
		}
	}

	// Keep the footer on one line: a wrapped footer pushes the viewport off screen
	width := m.viewport.Width
	pageInfo = ansi.Truncate(pageInfo, max(0, width-4), "…")
	info := infoStyle.Render(pageInfo)
	line := strings.Repeat("─", max(0, width-lipgloss.Width(info)))
	footer := lipgloss.JoinHorizontal(lipgloss.Center, line, info)

	// The status line is always there (blank when idle) so the footer height,
	// and with it the viewport size, never changes
	status := ""
	if m.statusMsg != "" {
		status = statusStyle.Render(ansi.Truncate(" "+m.statusMsg, max(0, width), "…"))
	}
	return status + "\n" + footer
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

// applyLocalProgress mirrors a successful AniList update in the loaded list so
// the UI doesn't need a refetch
func (m *model) applyLocalProgress(mediaID, progress int, status string) {
	for i := range m.userEntries {
		if m.userEntries[i].Media.ID == mediaID && m.userEntries[i].Status != "" {
			m.userEntries[i].Progress = progress
			m.userEntries[i].Status = status
		}
	}
	if e := m.selectedEntry; e != nil && m.selectedAnime != nil && m.selectedAnime.ID == mediaID {
		e.Progress = progress
		e.Status = status
	}
}
