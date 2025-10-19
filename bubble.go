package main

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	torrents []Torrent
	selected map[int]struct{}
	cursor   int
	ready    bool
	viewport viewport.Model
	page     int
	perPage  int
}

func initialModel() model {
	return model{
		torrents: getTorrentList(),
		selected: make(map[int]struct{}),
		perPage:  20,
	}
}
func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
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

func (m model) View() string {
	return m.renderView()
}
