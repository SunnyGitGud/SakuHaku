package main

import (
	"fmt"
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

func initialModel() *model {
	return &model{
		mode:             ModeAnime,
		anime:            getInitialAnime(),
		selectedTorrents: make(map[int]struct{}),
		animeTotalPages:  1,
	}
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
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

// ----- UI Handlers -----
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
			m.mode = ModeAnime
			m.selectedAnime = nil
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
			return nil
		}
		return tea.Quit
	case "s":
		if m.mode == ModeAnime {
			m.searchMode = true
			m.searchInput = ""
		}
		return nil
	}

	// Mode-specific keys
	if m.mode == ModeAnime {
		return m.handleAnimeKeys(msg)
	} else {
		return m.handleTorrentKeys(msg)
	}
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
	if m.mode == ModeAnime {
		cursorY = m.animeCursor * lineHeight
	} else {
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
	if !m.ready {
		return "\n  Loading..."
	}
	return fmt.Sprintf("%s\n%s\n%s",
		m.headerView(),
		m.viewport.View(),
		m.footerView())
}

// ----- View Components -----
func (m *model) renderContent() string {
	if m.mode == ModeAnime {
		return m.renderAnimeContent()
	}
	return m.renderTorrentContent()
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

		line := fmt.Sprintf("%s %s\n   📺 %s | 🎬 %s eps | ⭐ %s | 📅 %s %s | %s\n\n",
			cursor, title, a.Format, episodes, score, a.Season, year,
			hyperlink("AniList", a.SiteURL))
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

	// Show selected anime info
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
	} else if m.mode == ModeAnime {
		title = titleStyle.Render("AniList Browser")
	} else {
		title = titleStyle.Render("Torrent Results")
	}
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(title)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, line)
}

func (m *model) footerView() string {
	var pageInfo string
	if m.searchMode {
		pageInfo = "Enter to search | Esc to cancel"
	} else if m.mode == ModeAnime {
		pageInfo = fmt.Sprintf("Page %d/%d | %d results | s: search | n/p: page | Enter: torrents | q: quit",
			m.animePage+1,
			m.animeTotalPages,
			len(m.anime))
	} else {
		perPage := 20
		startIdx := m.torrentPage*perPage + 1
		endIdx := min(startIdx+len(m.visibleTorrents(perPage))-1, len(m.torrents))
		pageInfo = fmt.Sprintf("Page %d/%d | %d-%d of %d | Space/Enter: select | Esc: back | q: quit",
			m.torrentPage+1,
			m.totalTorrentPages(perPage),
			startIdx,
			endIdx,
			len(m.torrents))
	}

	info := infoStyle.Render(pageInfo)
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)))
	return lipgloss.JoinHorizontal(lipgloss.Center, line, info)
}

// ----- Torrent Pagination Helpers -----
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

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
