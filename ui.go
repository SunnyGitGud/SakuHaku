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
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return tea.Quit
	case "n":
		if m.page < m.totalPages()-1 {
			m.page++
			m.cursor = 0
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
		}
	case "p":
		if m.page > 0 {
			m.page--
			m.cursor = 0
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.viewport.SetContent(m.renderContent())
			m.ensureCursorVisible()
		}
	case "down", "j":
		maxCursor := len(m.visibleTorrents()) - 1
		if m.cursor < maxCursor {
			m.cursor++
			m.viewport.SetContent(m.renderContent())
			m.ensureCursorVisible()
		}
	case "enter":
		actualIndex := m.startIndex() + m.cursor
		if _, ok := m.selected[actualIndex]; ok {
			delete(m.selected, actualIndex)
		} else {
			m.selected[actualIndex] = struct{}{}
		}
		m.viewport.SetContent(m.renderContent())
	}
	return nil
}

func (m *model) ensureCursorVisible() {
	lineHeight := 3
	cursorY := m.cursor * lineHeight

	// Scroll up if the cursor is above the viewport
	if cursorY < m.viewport.YOffset {
		m.viewport.YOffset = cursorY
	}

	// Scroll down if cursor is below the visible area
	if cursorY > m.viewport.YOffset+m.viewport.Height-lineHeight {
		m.viewport.YOffset = cursorY - m.viewport.Height + lineHeight
	}

	// Clamp to bounds
	if m.viewport.YOffset < 0 {
		m.viewport.YOffset = 0
	}
	if m.viewport.YOffset > m.viewport.TotalLineCount()-m.viewport.Height {
		m.viewport.YOffset = max(0, m.viewport.TotalLineCount()-m.viewport.Height)
	}
}

func (m model) renderView() string {
	if !m.ready {
		return "\n  Loading..."
	}
	return fmt.Sprintf("%s\n%s\n%s",
		m.headerView(),
		m.viewport.View(),
		m.footerView())
}

// ----- View Components -----

func (m model) renderContent() string {
	visible := m.visibleTorrents()
	if len(visible) == 0 {
		return "No torrents found."
	}

	var sb strings.Builder
	for i, t := range visible {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}

		actualIndex := m.startIndex() + i
		checked := " "
		if _, ok := m.selected[actualIndex]; ok {
			checked = "x"
		}

		line := fmt.Sprintf("%s [%s] %s\n   💾 %d bytes | 🌱 %s | 🧲 %s\n\n",
			cursor, checked, t.Title, t.TotalSize, toString(t.Seeders), hyperlink("magnet", t.MagnetURI))
		sb.WriteString(line)
	}
	return sb.String()
}

func (m model) headerView() string {
	title := titleStyle.Render("Torrent Browser")
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(title)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, line)
}

func (m model) footerView() string {
	pageInfo := fmt.Sprintf("Page %d/%d | %d-%d of %d",
		m.page+1,
		m.totalPages(),
		m.startIndex()+1,
		m.endIndex(),
		len(m.torrents))

	info := infoStyle.Render(fmt.Sprintf("%s | %3.f%%", pageInfo, m.viewport.ScrollPercent()*100))
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)))
	return lipgloss.JoinHorizontal(lipgloss.Center, line, info)
}

func (m model) totalPages() int {
	if len(m.torrents) == 0 {
		return 1
	}
	return (len(m.torrents) + m.perPage - 1) / m.perPage
}

func (m model) startIndex() int {
	return m.page * m.perPage
}

func (m model) endIndex() int {
	end := (m.page + 1) * m.perPage
	if end > len(m.torrents) {
		return len(m.torrents)
	}
	return end
}

func (m model) visibleTorrents() []Torrent {
	return m.torrents[m.startIndex():m.endIndex()]
}
