package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
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

// --------------------- MODEL ---------------------

type model struct {
	torrents []Torrent
	selected map[int]struct{}
	cursor   int

	ready    bool
	viewport viewport.Model
}

func initialModel() model {
	torrents := getTorrentList()
	return model{
		torrents: torrents,
		selected: make(map[int]struct{}),
	}
}

// --------------------- TEA CORE ---------------------

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
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

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.viewport.SetContent(m.renderContent())
			}

		case "down", "j":
			if m.cursor < len(m.torrents)-1 {
				m.cursor++
				m.viewport.SetContent(m.renderContent())
			}

		case "enter", " ":
			if _, ok := m.selected[m.cursor]; ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}
			m.viewport.SetContent(m.renderContent())
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if !m.ready {
		return "\n  Loading..."
	}
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

// --------------------- RENDERING ---------------------

func (m model) renderContent() string {
	if len(m.torrents) == 0 {
		return "No torrents found."
	}

	var sb strings.Builder
	for i, t := range m.torrents {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}

		checked := " "
		if _, ok := m.selected[i]; ok {
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
	info := infoStyle.Render(fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100))
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)))
	return lipgloss.JoinHorizontal(lipgloss.Center, line, info)
}
